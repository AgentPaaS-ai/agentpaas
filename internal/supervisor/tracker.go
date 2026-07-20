package supervisor

import (
	"context"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// attemptTracker is the in-memory liveness state for one active attempt. It
// is protected by the Supervisor.mu mutex (the supervisor never hands out a
// tracker pointer to callers). The durable truth lives in the store; the
// tracker is a cache of the liveness signals the supervisor has accepted.
type attemptTracker struct {
	attemptID  routedrun.AttemptID
	runID      routedrun.RunID
	workflowID routedrun.WorkflowID
	leaseID    routedrun.LeaseID

	// controlKey is the per-attempt HMAC key, loaded from the control journal.
	// The supervisor verifies progress/result/checkpoint HMACs against it.
	controlKey []byte

	// Stall / operation ceilings (from ClaimOptions, defaulted).
	stallTimeoutMs     int64
	modelCallTimeoutMs int64
	attemptLeaseMs     int64

	// lastActivityMonotonicMs is the monotonic-ms timestamp of the last
	// ACCEPTED activity (authenticated progress, governed-op start/end,
	// checkpoint commit). Unauthenticated activity does NOT update this.
	lastActivityMonotonicMs int64

	// lastProgressSequence is the highest accepted progress sequence. A
	// replayed or out-of-order progress event is rejected.
	lastProgressSequence int64

	// inFlight is the set of currently in-flight governed operations. While
	// non-empty, the stall timer is bounded by the operation deadline, not
	// the raw stall timeout.
	inFlight map[GovernedOperationKind]int

	// inFlightStartedMonotonicMs is the monotonic-ms timestamp the oldest
	// in-flight governed operation started. Used to compute the effective
	// operation deadline for the stall exemption.
	inFlightStartedMonotonicMs int64

	// terminal is true once the attempt has reached a terminal state via a
	// committed CAS transition. After this, further events are rejected or
	// treated as idempotent no-ops.
	terminal bool
	// terminalStatus records which terminal state was committed (for
	// idempotent finalization: a second Finalize matching the existing state
	// is a no-op).
	terminalStatus routedrun.InvokeJobResultStatus

	// verifiedResult is the verified InvokeJobResult event committed for the
	// active lease. Finalize success requires this to be set.
	verifiedResult *ResultEvent

	mu sync.Mutex
}

// newAttemptTracker constructs a tracker with the given lease and ceilings.
func newAttemptTracker(
	attemptID routedrun.AttemptID,
	runID routedrun.RunID,
	workflowID routedrun.WorkflowID,
	leaseID routedrun.LeaseID,
	controlKey []byte,
	opts ClaimOptions,
	nowMs int64,
) *attemptTracker {
	stall := opts.StallTimeoutMs
	if stall <= 0 {
		stall = routedrun.DefaultStallTimeoutMs
	}
	model := opts.ModelCallTimeoutMs
	if model <= 0 {
		model = routedrun.DefaultModelCallTimeoutMs
	}
	return &attemptTracker{
		attemptID:               attemptID,
		runID:                   runID,
		workflowID:              workflowID,
		leaseID:                 leaseID,
		controlKey:              controlKey,
		stallTimeoutMs:          stall,
		modelCallTimeoutMs:      model,
		attemptLeaseMs:          opts.AttemptLeaseMs,
		lastActivityMonotonicMs: nowMs,
		inFlight:                make(map[GovernedOperationKind]int),
	}
}

// hasInFlight returns true if any governed operation is currently in flight.
func (t *attemptTracker) hasInFlight() bool {
	for _, n := range t.inFlight {
		if n > 0 {
			return true
		}
	}
	return false
}

// stallDeadlineMonotonicMs returns the monotonic-ms deadline at which the
// attempt is considered stalled, given the current monotonic time. When a
// governed operation is in flight, the deadline is bounded by the operation
// deadline (min of operation timeout, lease remaining, active time remaining).
// Otherwise it is lastActivity + env.StallTimeoutMs.
//
// The TimeEnvelope is the authoritative source for the stall timeout and the
// operation-deadline ceilings. The tracker's own copies are fallbacks used only
// when the envelope carries a zero stall timeout.
func (t *attemptTracker) stallDeadlineMonotonicMs(env routedrun.TimeEnvelope, nowMs int64) int64 {
	stallTimeout := env.StallTimeoutMs
	if stallTimeout <= 0 {
		stallTimeout = t.stallTimeoutMs
	}
	if t.hasInFlight() {
		// Operation deadline: min(op timeout, lease remaining, active time
		// remaining). The operation timeout for the exemption is the model-
		// call timeout (the longest governed-operation ceiling); HTTP/MCP
		// operations are bounded by the same per-attempt ceiling.
		opTimeout := env.ModelCallTimeoutMs
		if opTimeout <= 0 {
			opTimeout = t.modelCallTimeoutMs
		}
		deadline := env.EffectiveOperationDeadlineMs(nowMs, opTimeout)
		if deadline <= 0 {
			return nowMs
		}
		start := t.inFlightStartedMonotonicMs
		return start + deadline
	}
	return t.lastActivityMonotonicMs + stallTimeout
}

// isStalled returns true if the attempt is stalled at nowMs: the stall
// deadline has passed and no in-flight governed operation exempts it (the
// exemption is itself bounded by the operation deadline, so an operation that
// exceeds its deadline DOES stall).
func (t *attemptTracker) isStalled(env routedrun.TimeEnvelope, nowMs int64) bool {
	return nowMs >= t.stallDeadlineMonotonicMs(env, nowMs)
}

// acceptActivity resets the stall timer to nowMs. Called only for ACCEPTED
// activity (authenticated progress, governed-op start/end, checkpoint commit).
func (t *attemptTracker) acceptActivity(nowMs int64) {
	if nowMs > t.lastActivityMonotonicMs {
		t.lastActivityMonotonicMs = nowMs
	}
}

// nowMonotonicMs returns the current monotonic-ms timestamp from the clock.
func (s *Supervisor) nowMonotonicMs() int64 {
	return s.clock.NowMonotonic().UnixMilli()
}

// nowWall returns the current wall-clock time (UTC) from the clock.
func (s *Supervisor) nowWall() time.Time {
	return s.clock.Now()
}

// handleGovernedStart records the start of an in-flight governed operation.
// It accepts the event as liveness activity (resets the stall timer) and
// enables the operation-deadline exemption.
func (s *Supervisor) handleGovernedStart(attemptID routedrun.AttemptID, leaseID routedrun.LeaseID, kind GovernedOperationKind) error {
	s.mu.Lock()
	t, ok := s.trackers[attemptID]
	s.mu.Unlock()
	if !ok {
		return ErrAttemptNotFound
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.terminal {
		return ErrAlreadyTerminal
	}
	if t.leaseID != "" && leaseID != "" && t.leaseID != leaseID {
		return ErrLeaseMismatch
	}
	wasEmpty := !t.hasInFlight()
	t.inFlight[kind]++
	nowMs := s.nowMonotonicMs()
	if wasEmpty {
		t.inFlightStartedMonotonicMs = nowMs
	}
	t.acceptActivity(nowMs)
	return nil
}

// handleGovernedEnd records the end of an in-flight governed operation. It
// accepts the event as liveness activity. When the last in-flight operation
// ends, the stall timer reverts to the raw stall timeout.
func (s *Supervisor) handleGovernedEnd(attemptID routedrun.AttemptID, leaseID routedrun.LeaseID, kind GovernedOperationKind) error {
	s.mu.Lock()
	t, ok := s.trackers[attemptID]
	s.mu.Unlock()
	if !ok {
		return ErrAttemptNotFound
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.terminal {
		return ErrAlreadyTerminal
	}
	if t.leaseID != "" && leaseID != "" && t.leaseID != leaseID {
		return ErrLeaseMismatch
	}
	if t.inFlight[kind] <= 0 {
		// Idempotent: an end without a matching start is a no-op (do not
		// error; the worker may have crashed mid-start).
		return nil
	}
	t.inFlight[kind]--
	if !t.hasInFlight() {
		t.inFlightStartedMonotonicMs = 0
	}
	t.acceptActivity(s.nowMonotonicMs())
	return nil
}

// CheckStall reports whether the attempt is stalled at the current monotonic
// time. It is the test/inpection seam for the stall timer: the daemon polls
// it (or a timer goroutine fires it) to drive stall-driven finalization.
//
// CheckStall is safe for concurrent use and does not mutate durable state.
func (s *Supervisor) CheckStall(ctx context.Context, attemptID routedrun.AttemptID, env routedrun.TimeEnvelope) (bool, error) {
	s.mu.Lock()
	t, ok := s.trackers[attemptID]
	s.mu.Unlock()
	if !ok {
		return false, ErrAttemptNotFound
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.terminal {
		return false, nil
	}
	nowMs := s.nowMonotonicMs()
	return t.isStalled(env, nowMs), nil
}
