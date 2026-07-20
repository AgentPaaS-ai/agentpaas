package runtime

// ActivationLifecycle tracks activation state transitions and enforces
// that they are legal under the declared ActivationPolicy (B29-T06).
//
// The lifecycle is:
//
//	cold → prepared → running → (warm: idle) → (on_demand: stopped) → cleaned
//
// On a running→idle transition under warm, the lifecycle strips all
// authority (lease, route capability, credentials) from the workload so
// that the warm sandbox holds nothing until re-admission. The stripped
// idle state is retrievable via StrippedIdle and must pass
// ZeroAuthorityInvariant.

import (
	"fmt"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// ActivationLifecycle records the most recent transition and the idle
// state produced when authority was stripped on entry to warm idle. It is
// safe for use by a single goroutine managing one workload; concurrent
// transitions to the same lifecycle are the caller's responsibility.
type ActivationLifecycle struct {
	current  WorkloadState
	stripped IdleState
}

// NewActivationLifecycle returns an ActivationLifecycle starting in the
// cold (empty) state.
func NewActivationLifecycle() *ActivationLifecycle {
	return &ActivationLifecycle{current: ""}
}

// Current returns the most recent state the lifecycle transitioned into.
func (l *ActivationLifecycle) Current() WorkloadState { return l.current }

// StrippedIdle returns the authority-stripped IdleState produced by the
// most recent running→idle transition under warm. It is the empty IdleState
// if no such transition has occurred.
func (l *ActivationLifecycle) StrippedIdle() IdleState { return l.stripped }

// Transition validates that the transition from current to target is legal
// under the declared policy and updates the lifecycle. The target IdleState
// carries the authority-bearing fields the runtime will use going forward;
// for a running→idle warm transition it is overwritten with an
// authority-stripped copy.
//
// Legal transitions (independent of mode):
//
//	""           → prepared     (initial prepare)
//	prepared     → running      (start)
//	prepared     → stopped      (abandon without running)
//	running      → stopped       (scale to zero — on_demand, or warm teardown)
//	running      → idle          (warm only — keep sandbox, strip authority)
//	idle         → running       (warm only — requires re-admission)
//	idle         → stopped       (warm idle timeout expired)
//	stopped      → cleaned       (terminal cleanup)
//
// Illegal transitions include:
//
//	running      → idle          under on_demand (no idle retention)
//	stopped      → running        (must re-prepare)
//	idle         → running        without re-admission (no credentials)
func (l *ActivationLifecycle) Transition(
	current, target WorkloadState,
	policy port.ActivationPolicy,
	targetState IdleState,
) error {
	if current != l.current && l.current != "" {
		// The caller passed a `current` that disagrees with the
		// lifecycle's recorded state. Treat as an illegal transition to
		// avoid silently accepting stale state.
		return fmt.Errorf(
			"%w: current=%q but lifecycle is in %q",
			ErrIllegalTransition, current, l.current,
		)
	}

	switch {
	case current == "" && target == StatePrepared:
		l.current = target
		return nil

	case current == StatePrepared && target == StateRunning:
		l.current = target
		return nil

	case current == StatePrepared && target == StateStopped:
		l.current = target
		return nil

	case current == StateRunning && target == StateStopped:
		// running→stopped is the on_demand scale-to-zero path and also
		// the warm teardown path. Always legal.
		l.current = target
		return nil

	case current == StateRunning && target == StateIdle:
		// running→idle is legal only under warm. on_demand has no idle
		// retention; resident is continuously metered and does not go
		// idle.
		if policy.Mode != port.ActivationWarm {
			return fmt.Errorf(
				"%w: running→idle requires warm mode, got %q",
				ErrIllegalTransition, policy.Mode,
			)
		}
		// Strip all authority on entry to idle: the warm sandbox keeps no
		// task lease, route capability, or applied credentials.
		// B29-7 TODO: Add egress fencing at sandbox level on warm->idle.
		// Currently this is zero-authority bookkeeping-only — the idle
		// sandbox retains network access. When the sandbox/egress gateway
		// interface supports per-sandbox egress controls, fence the idle
		// sandbox's network access alongside stripping authority.
		stripped := stripAuthority(targetState)
		if err := ZeroAuthorityInvariant(stripped); err != nil {
			// stripAuthority should make this impossible; surface it as
			// an authority leak if it ever happens.
			return err
		}
		l.stripped = stripped
		l.current = target
		return nil

	case current == StateIdle && target == StateRunning:
		// idle→running requires re-admission: a credential binding must
		// be present. This is the explicit re-admission check that
		// prevents a warm sandbox from running with retained authority.
		if len(targetState.CredentialBindings) == 0 {
			return fmt.Errorf(
				"%w: idle→running under %q requires a credential binding",
				ErrReadmissionRequired, policy.Mode,
			)
		}
		l.stripped = IdleState{}
		l.current = target
		return nil

	case current == StateIdle && target == StateStopped:
		// idle→stopped: warm idle timeout expired. Legal.
		l.stripped = IdleState{}
		l.current = target
		return nil

	case current == StateStopped && target == StateCleaned:
		l.current = target
		return nil

	default:
		return fmt.Errorf(
			"%w: %q→%q under %q",
			ErrIllegalTransition, current, target, policy.Mode,
		)
	}
}

// stripAuthority returns a copy of w with all authority-bearing fields
// cleared. The State and ActivationMode are preserved so the result can
// be inspected by ZeroAuthorityInvariant.
func stripAuthority(w IdleState) IdleState {
	return IdleState{
		State:              w.State,
		ActivationMode:     w.ActivationMode,
		LeaseID:            "",
		RouteCapability:    "",
		CredentialBindings: nil,
	}
}
