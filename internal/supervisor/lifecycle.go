package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// Claim
// ---------------------------------------------------------------------------

// Claim acquires the active lease for an invocation's run and starts tracking
// the attempt. It is the post-admission entry point.
//
// Claim is a convenience wrapper; the daemon wiring (T07) passes the already-
// resolved runID to ClaimForRun. Claim itself requires a run-resolution path
// that lives in the DeploymentStore, so it returns an error directing callers
// to ClaimForRun.
func (s *Supervisor) Claim(ctx context.Context, invocationID routedrun.InvocationID) (routedrun.AttemptID, error) {
	return "", fmt.Errorf("%w: Claim requires a resolved run; use ClaimForRun", ErrInvalidArgument)
}

// ClaimForRun acquires the active lease for an already-resolved run. This is
// the lower-level entry point the daemon calls after AdmitInvocation has
// committed the durable READY launch-intent and resolved the runID.
//
// ClaimForRun is idempotent on the run: if a non-terminal attempt with an
// active lease already exists, it re-establishes in-memory tracking for that
// attempt and returns its ID without creating a new attempt or a new lease.
func (s *Supervisor) ClaimForRun(ctx context.Context, runID routedrun.RunID, _ routedrun.InvocationID) (routedrun.AttemptID, error) {
	if runID == "" {
		return "", fmt.Errorf("%w: empty run id", ErrInvalidArgument)
	}
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return "", err
	}
	atts, err := s.store.ListAttempts(ctx, runID)
	if err != nil {
		return "", err
	}
	var existing *routedrun.AttemptRecord
	for _, a := range atts {
		if a.Status == routedrun.AttemptStatusRunning {
			existing = a
			break
		}
	}
	if existing != nil {
		if err := s.establishTracker(ctx, existing, run); err != nil {
			return "", err
		}
		return existing.AttemptID, nil
	}
	// Create a new attempt.
	att := &routedrun.AttemptRecord{
		SchemaVersion: routedrun.CurrentSchemaVersion,
		RunID:          runID,
		WorkflowID:     run.WorkflowID,
		Status:         routedrun.AttemptStatusRunning,
		AttemptNumber:  len(atts) + 1,
		Lease: &routedrun.AttemptLease{
			DurationMs: 60_000,
			AcquiredAt:  s.nowWall(),
			ExpiresAt:   s.nowWall().Add(60 * time.Second),
		},
	}
	if err := s.store.CreateAttempt(ctx, att); err != nil {
		return "", err
	}
	// Reload to get the store-issued lease ID/token.
	att, err = s.store.GetAttempt(ctx, att.AttemptID)
	if err != nil {
		return "", err
	}
	if err := s.establishTracker(ctx, att, run); err != nil {
		return "", err
	}
	// Append the ACCEPTED control-journal event (durable admission committed).
	s.mu.Lock()
	trk := s.trackers[att.AttemptID]
	s.mu.Unlock()
	if trk == nil {
		return "", ErrAttemptNotFound
	}
	if err := s.appendJournalEvent(trk, routedrun.InvokeJobEventAccepted, "{}"); err != nil {
		return "", fmt.Errorf("append accepted event: %w", err)
	}
	// Audit AFTER durable commit (the attempt record + journal event).
	s.audit.Append(audit.AuditRecord{
		Timestamp:      s.nowWall().Format(time.RFC3339Nano),
		EventType:      "supervisor_attempt_claimed",
		DeploymentMode: "local",
		Actor:          "supervisor",
		Payload: map[string]interface{}{
			"run_id":     string(runID),
			"attempt_id": string(att.AttemptID),
		},
	})
	return att.AttemptID, nil
}

func (s *Supervisor) establishTracker(_ context.Context, att *routedrun.AttemptRecord, _ *routedrun.RunRecord) error {
	key, err := s.loadOrCreateControlKey(att.RunID, att.AttemptID)
	if err != nil {
		return err
	}
	nowMs := s.nowMonotonicMs()
	trk := newAttemptTracker(
		att.AttemptID,
		att.RunID,
		att.WorkflowID,
		leaseIDOf(att),
		key,
		ClaimOptions{},
		nowMs,
	)
	s.mu.Lock()
	s.trackers[att.AttemptID] = trk
	s.mu.Unlock()
	return nil
}

func leaseIDOf(att *routedrun.AttemptRecord) routedrun.LeaseID {
	if att.Lease == nil {
		return ""
	}
	return att.Lease.LeaseID
}

// ---------------------------------------------------------------------------
// TrackProgress
// ---------------------------------------------------------------------------

// TrackProgress accepts an authenticated progress/heartbeat event. It counts
// as liveness evidence ONLY when the HMAC verifies against the attempt's
// control key and the lease matches the active lease.
func (s *Supervisor) TrackProgress(_ context.Context, attemptID routedrun.AttemptID, p ProgressEvent) error {
	if attemptID == "" {
		return fmt.Errorf("%w: empty attempt id", ErrInvalidArgument)
	}
	s.mu.Lock()
	trk := s.trackers[attemptID]
	s.mu.Unlock()
	if trk == nil {
		return ErrAttemptNotFound
	}
	trk.mu.Lock()
	defer trk.mu.Unlock()
	if trk.terminal {
		return ErrAlreadyTerminal
	}
	if trk.leaseID != "" && p.LeaseID != "" && trk.leaseID != p.LeaseID {
		return ErrLeaseMismatch
	}
	if !verifyProgressHMAC(p, trk.controlKey) {
		return ErrInvalidHMAC
	}
	if p.Sequence <= trk.lastProgressSequence {
		return fmt.Errorf("%w: progress sequence %d <= last %d", ErrInvalidArgument, p.Sequence, trk.lastProgressSequence)
	}
	trk.lastProgressSequence = p.Sequence
	trk.acceptActivity(s.nowMonotonicMs())
	return nil
}

// ---------------------------------------------------------------------------
// HandleCheckpoint
// ---------------------------------------------------------------------------

// HandleCheckpoint accepts an authenticated checkpoint/artifact commit. A
// checkpoint commit counts as accepted activity (durable forward progress).
func (s *Supervisor) HandleCheckpoint(ctx context.Context, attemptID routedrun.AttemptID, event CheckpointEvent) error {
	if attemptID == "" || event.Checkpoint == nil {
		return fmt.Errorf("%w: empty attempt or checkpoint", ErrInvalidArgument)
	}
	s.mu.Lock()
	trk := s.trackers[attemptID]
	s.mu.Unlock()
	if trk == nil {
		return ErrAttemptNotFound
	}
	trk.mu.Lock()
	if trk.terminal {
		trk.mu.Unlock()
		return ErrAlreadyTerminal
	}
	if trk.leaseID != "" && event.LeaseID != "" && trk.leaseID != event.LeaseID {
		trk.mu.Unlock()
		return ErrLeaseMismatch
	}
	// T05 Part A: the supervisor itself commits checkpoints (authenticated path).
	// HMAC seam exists for worker-committed checkpoints (T07 wires it).
	cp := event.Checkpoint
	if cp.SchemaVersion == "" {
		cp.SchemaVersion = routedrun.CurrentSchemaVersion
	}
	if err := s.store.SaveCheckpoint(ctx, cp); err != nil {
		trk.mu.Unlock()
		if !errors.Is(err, routedrun.ErrAlreadyExists) {
			return err
		}
	}
	ref := fmt.Sprintf(`{"checkpoint_id":"%s","sequence":%d}`, string(cp.CheckpointID), cp.Sequence)
	if err := s.appendJournalEventLocked(trk, routedrun.InvokeJobEventProgressRef, ref); err != nil {
		trk.mu.Unlock()
		return err
	}
	trk.acceptActivity(s.nowMonotonicMs())
	trk.mu.Unlock()
	s.audit.Append(audit.AuditRecord{
		Timestamp:      s.nowWall().Format(time.RFC3339Nano),
		EventType:      "supervisor_checkpoint_committed",
		DeploymentMode: "local",
		Actor:          "supervisor",
		Payload: map[string]interface{}{
			"run_id":        string(trk.runID),
			"attempt_id":    string(trk.attemptID),
			"checkpoint_id": string(cp.CheckpointID),
		},
	})
	return nil
}

// ---------------------------------------------------------------------------
// HandleResult
// ---------------------------------------------------------------------------

// HandleResult accepts the terminal job-result event. It finalizes the
// attempt per the result's terminal status, ONLY from a verified result event
// for the active lease.
func (s *Supervisor) HandleResult(ctx context.Context, attemptID routedrun.AttemptID, event ResultEvent) error {
	if attemptID == "" {
		return fmt.Errorf("%w: empty attempt id", ErrInvalidArgument)
	}
	s.mu.Lock()
	trk := s.trackers[attemptID]
	s.mu.Unlock()
	if trk == nil {
		return ErrAttemptNotFound
	}
	trk.mu.Lock()
	if trk.terminal {
		trk.mu.Unlock()
		return ErrAlreadyTerminal
	}
	if trk.leaseID != "" && event.LeaseID != "" && trk.leaseID != event.LeaseID {
		trk.mu.Unlock()
		return ErrLeaseMismatch
	}
	if !verifyResultHMAC(event, trk.controlKey) {
		trk.mu.Unlock()
		return ErrInvalidHMAC
	}
	trk.verifiedResult = &event
	trk.mu.Unlock()

	switch event.TerminalStatus {
	case routedrun.InvokeJobResultSucceeded:
		return s.finalizeSuccess(ctx, trk, event)
	case routedrun.InvokeJobResultFailed:
		return s.finalizeFailed(ctx, trk, "result_event_failed")
	case routedrun.InvokeJobResultCancelled:
		return s.cancelAttempt(ctx, trk)
	default:
		return fmt.Errorf("%w: unknown terminal status %d", ErrInvalidArgument, event.TerminalStatus)
	}
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

// Cancel finalizes the attempt as CANCELLED. Idempotent: a second call on an
// already-CANCELLED attempt returns nil; on a different terminal state it
// returns ErrAlreadyTerminal.
func (s *Supervisor) Cancel(ctx context.Context, attemptID routedrun.AttemptID) error {
	if attemptID == "" {
		return fmt.Errorf("%w: empty attempt id", ErrInvalidArgument)
	}
	s.mu.Lock()
	trk := s.trackers[attemptID]
	s.mu.Unlock()
	if trk == nil {
		return ErrAttemptNotFound
	}
	trk.mu.Lock()
	if trk.terminal {
		already := trk.terminalStatus == routedrun.InvokeJobResultCancelled
		trk.mu.Unlock()
		if already {
			return nil
		}
		return ErrAlreadyTerminal
	}
	trk.mu.Unlock()
	return s.cancelAttempt(ctx, trk)
}

// ---------------------------------------------------------------------------
// Finalize
// ---------------------------------------------------------------------------

// Finalize is the idempotent terminal transition called when the container
// exits or the lease expires. It does NOT mark success unless a verified
// result event has been committed for the active lease.
func (s *Supervisor) Finalize(ctx context.Context, attemptID routedrun.AttemptID) error {
	if attemptID == "" {
		return fmt.Errorf("%w: empty attempt id", ErrInvalidArgument)
	}
	s.mu.Lock()
	trk := s.trackers[attemptID]
	s.mu.Unlock()
	if trk == nil {
		return ErrAttemptNotFound
	}
	trk.mu.Lock()
	if trk.terminal {
		trk.mu.Unlock()
		return nil
	}
	verified := trk.verifiedResult
	trk.mu.Unlock()
	if verified != nil && verified.TerminalStatus == routedrun.InvokeJobResultSucceeded {
		return s.finalizeSuccess(ctx, trk, *verified)
	}
	return s.finalizeFailed(ctx, trk, "no_verified_result")
}

// ---------------------------------------------------------------------------
// terminal transition implementations (CAS-driven, idempotent)
// ---------------------------------------------------------------------------

// finalizeSuccess commits the SUCCEEDED terminal state via CAS on the attempt
// and run, persists the InvokeJobResult, and appends the SUCCEEDED control-
// journal event. Idempotent on the attempt.
func (s *Supervisor) finalizeSuccess(ctx context.Context, trk *attemptTracker, event ResultEvent) error {
	if err := s.casAttemptTo(ctx, trk, routedrun.AttemptStatusSucceeded, "verified_result"); err != nil {
		return err
	}
	result := &routedrun.InvokeJobResult{
		SchemaVersion:      routedrun.CurrentSchemaVersion,
		InvocationID:       event.InvocationID,
		WorkflowID:         event.WorkflowID,
		RunID:              event.RunID,
		AttemptID:          event.AttemptID,
		ResultDigest:       event.ResultDigest,
		ArtifactReferences: event.ArtifactReferences,
		StructuredResult:   event.StructuredResult,
		TerminalStatus:     routedrun.InvokeJobResultSucceeded,
		StartedAt:          s.nowWall(),
		FinishedAt:         s.nowWall(),
	}
	_ = s.results.SaveInvokeJobResult(ctx, result)
	_ = s.appendJournalEvent(trk, routedrun.InvokeJobEventSucceeded, "{}")
	_ = s.casRunTo(ctx, trk.runID, routedrun.RunStatusSucceeded)
	s.markTerminal(trk, routedrun.InvokeJobResultSucceeded)
	s.audit.Append(audit.AuditRecord{
		Timestamp:      s.nowWall().Format(time.RFC3339Nano),
		EventType:      "supervisor_attempt_succeeded",
		DeploymentMode: "local",
		Actor:          "supervisor",
		Payload: map[string]interface{}{
			"run_id":     string(trk.runID),
			"attempt_id": string(trk.attemptID),
		},
	})
	return nil
}

func (s *Supervisor) finalizeFailed(ctx context.Context, trk *attemptTracker, reason string) error {
	if err := s.casAttemptTo(ctx, trk, routedrun.AttemptStatusFailed, reason); err != nil {
		return err
	}
	if trk.verifiedResult != nil {
		result := &routedrun.InvokeJobResult{
			SchemaVersion:      routedrun.CurrentSchemaVersion,
			InvocationID:       trk.verifiedResult.InvocationID,
			WorkflowID:         trk.verifiedResult.WorkflowID,
			RunID:              trk.verifiedResult.RunID,
			AttemptID:          trk.verifiedResult.AttemptID,
			ResultDigest:       trk.verifiedResult.ResultDigest,
			ArtifactReferences: trk.verifiedResult.ArtifactReferences,
			StructuredResult:   trk.verifiedResult.StructuredResult,
			TerminalStatus:     routedrun.InvokeJobResultFailed,
			FinishedAt:         s.nowWall(),
		}
		_ = s.results.SaveInvokeJobResult(ctx, result)
	}
	_ = s.appendJournalEvent(trk, routedrun.InvokeJobEventFailed, "{}")
	_ = s.casRunTo(ctx, trk.runID, routedrun.RunStatusFailed)
	s.markTerminal(trk, routedrun.InvokeJobResultFailed)
	s.audit.Append(audit.AuditRecord{
		Timestamp:      s.nowWall().Format(time.RFC3339Nano),
		EventType:      "supervisor_attempt_failed",
		DeploymentMode: "local",
		Actor:          "supervisor",
		Payload: map[string]interface{}{
			"run_id":     string(trk.runID),
			"attempt_id": string(trk.attemptID),
			"reason":      reason,
		},
	})
	return nil
}

func (s *Supervisor) cancelAttempt(ctx context.Context, trk *attemptTracker) error {
	if err := s.casAttemptTo(ctx, trk, routedrun.AttemptStatusCancelled, "user_cancelled"); err != nil {
		return err
	}
	_ = s.appendJournalEvent(trk, routedrun.InvokeJobEventCancelled, "{}")
	_ = s.casRunTo(ctx, trk.runID, routedrun.RunStatusCancelled)
	s.markTerminal(trk, routedrun.InvokeJobResultCancelled)
	s.audit.Append(audit.AuditRecord{
		Timestamp:      s.nowWall().Format(time.RFC3339Nano),
		EventType:      "supervisor_attempt_cancelled",
		DeploymentMode: "local",
		Actor:          "supervisor",
		Payload: map[string]interface{}{
			"run_id":     string(trk.runID),
			"attempt_id": string(trk.attemptID),
		},
	})
	return nil
}

// casAttemptTo transitions the attempt to the target status via CAS, retrying
// on generation conflict. Idempotent: if the attempt is already in the target
// terminal state, it is a no-op. If it is in a DIFFERENT terminal state, it
// returns ErrAlreadyTerminal (cancel precedence).
func (s *Supervisor) casAttemptTo(ctx context.Context, trk *attemptTracker, target routedrun.AttemptStatus, reason string) error {
	for i := 0; i < 8; i++ {
		att, err := s.store.GetAttempt(ctx, trk.attemptID)
		if err != nil {
			return err
		}
		if att.Status.IsTerminal() {
			if att.Status == target {
				return nil
			}
			return ErrAlreadyTerminal
		}
		gen, err := s.store.GetAttemptGeneration(ctx, trk.attemptID)
		if err != nil {
			return err
		}
		now := s.nowWall()
		att.Status = target
		att.FailureReason = failureReasonFor(reason)
		att.UpdatedAt = now
		att.TerminatedAt = &now
		if att.Lease != nil {
			att.Lease.ExpiresAt = now
			att.Lease.LeaseToken = ""
		}
		if err := s.store.UpdateAttempt(ctx, att, gen); err != nil {
			if errors.Is(err, routedrun.ErrCASConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return routedrun.ErrCASConflict
}

// casRunTo transitions the run to the target status via CAS, retrying on
// generation conflict. It is a no-op if the run is already terminal.
func (s *Supervisor) casRunTo(ctx context.Context, runID routedrun.RunID, target routedrun.RunStatus) error {
	for i := 0; i < 8; i++ {
		run, err := s.store.GetRun(ctx, runID)
		if err != nil {
			return err
		}
		if run.Status.IsTerminal() {
			return nil
		}
		gen, err := s.store.GetRunGeneration(ctx, runID)
		if err != nil {
			return err
		}
		run.Status = target
		now := s.nowWall()
		run.UpdatedAt = now
		run.TerminatedAt = &now
		if err := s.store.UpdateRun(ctx, run, gen); err != nil {
			if errors.Is(err, routedrun.ErrCASConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return routedrun.ErrCASConflict
}

func failureReasonFor(reason string) *routedrun.FailureReason {
	var r routedrun.FailureReason
	switch reason {
	case "user_cancelled":
		r = routedrun.FailureUserCancelled
	case "daemon_restart":
		r = routedrun.FailureDaemonRestarted
	case "stall":
		r = routedrun.FailureStallTimeout
	case "result_event_failed":
		r = routedrun.FailureAgentException
	default:
		r = routedrun.FailureAgentException
	}
	return &r
}

func (s *Supervisor) markTerminal(trk *attemptTracker, status routedrun.InvokeJobResultStatus) {
	trk.mu.Lock()
	trk.terminal = true
	trk.terminalStatus = status
	trk.mu.Unlock()
}

// resultStatusFor maps an AttemptStatus to the corresponding InvokeJobResult
// terminal status.
func resultStatusFor(attStatus routedrun.AttemptStatus) routedrun.InvokeJobResultStatus {
	switch attStatus {
	case routedrun.AttemptStatusSucceeded:
		return routedrun.InvokeJobResultSucceeded
	case routedrun.AttemptStatusCancelled:
		return routedrun.InvokeJobResultCancelled
	default:
		return routedrun.InvokeJobResultFailed
	}
}
