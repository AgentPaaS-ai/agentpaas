package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// Reconcile is the daemon-restart reconciliation entry point. It:
//   - Reads the control journal for each attempt of the run and replays events
//     to reconstruct state.
//   - If an active (non-terminal) attempt has NO committed terminal event in
//     its journal: revoke the lease and mark FAILED with reason
//     "daemon_restart" (ambiguous active lease). Never blindly re-invoke work.
//   - If a terminal event WAS committed before crash: accept it; do not replay
//     work.
//   - Preserve safe checkpoint/artifact state for B39 continuation (no action
//     needed - checkpoints are durable and never mutated).
//   - Reconcile active-time segment state: if the workflow is fully PAUSED or
//     NEEDS_REPLAN, conservatively close any open active segment WITHOUT
//     charging wall time (frozen states do not accrue). If PAUSE_REQUESTED,
//     accrue the elapsed wall time since the open segment start (it is an
//     accruing state). If RUNNING, accrue the elapsed wall time since the open
//     segment start and close the segment conservatively (exactly once).
func (s *Supervisor) Reconcile(ctx context.Context, runID routedrun.RunID) error {
	if runID == "" {
		return fmt.Errorf("%w: empty run id", ErrInvalidArgument)
	}
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	atts, err := s.store.ListAttempts(ctx, runID)
	if err != nil {
		return err
	}
	for _, att := range atts {
		if err := s.reconcileAttempt(ctx, att); err != nil {
			return err
		}
	}
	// If the run is not terminal and no attempt succeeded, fail the run.
	if !run.Status.IsTerminal() {
		// Check if any attempt succeeded; if so, the run should already be
		// SUCCEEDED via the terminal CAS. If not, fail it as daemon_restart.
		anySucceeded := false
		for _, att := range atts {
			if att.Status == routedrun.AttemptStatusSucceeded {
				anySucceeded = true
				break
			}
		}
		if !anySucceeded {
			_ = s.casRunTo(ctx, runID, routedrun.RunStatusFailed)
		}
	}
	// Reconcile active-time segment state from the workflow.
	if err := s.reconcileActiveTime(ctx, run.WorkflowID); err != nil {
		return err
	}
	return nil
}

// reconcileAttempt replays the control journal for an attempt. If the journal
// contains a terminal event (SUCCEEDED/FAILED/CANCELLED), the attempt is
// already finalized durably - accept it. If the journal contains NO terminal
// event and the attempt is non-terminal, revoke the lease and mark FAILED
// with reason "daemon_restart" (ambiguous active lease).
func (s *Supervisor) reconcileAttempt(ctx context.Context, att *routedrun.AttemptRecord) error {
	if att.Status.IsTerminal() {
		// Already terminal in the durable store. The journal should contain a
		// matching terminal event; we do NOT replay work. Establish in-memory
		// tracking as terminal (so a late event is rejected).
		s.markTerminalForRestart(att)
		return nil
	}
	// Read the journal to check for a committed terminal event.
	journal, err := s.journals.OpenControlJournal(att.RunID, att.AttemptID)
	if err != nil {
		return fmt.Errorf("reconcile: open journal: %w", err)
	}
	events, err := journal.Read(1)
	_ = journal.Close()
	if err != nil {
		// F16: distinguish absence from corruption/IO errors.
		// os.IsNotExist/fs.ErrNotExist-style errors mean no journal events
		// exist yet, which is ambiguous (fail the attempt). Real errors
		// (corruption, IO failure, HMAC tamper) must be audited and
		// returned for retry/escalation.
		if isNotFoundish(err) {
			// No journal directory or no events: ambiguous active lease.
			events = nil
		} else {
			_ = s.audit.Append(audit.AuditRecord{
				Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
				EventType:      "supervisor_journal_read_error",
				DeploymentMode: "local",
				Actor:          "supervisor",
				Payload: map[string]interface{}{
					"run_id":     string(att.RunID),
					"attempt_id": string(att.AttemptID),
					"error":      err.Error(),
				},
			})
			return fmt.Errorf("reconcile: journal read error for attempt %s: %w", att.AttemptID, err)
		}
	}
	hasTerminal := false
	var terminalKind routedrun.InvokeJobEventKind
	for _, ev := range events {
		switch ev.EventKind {
		case routedrun.InvokeJobEventSucceeded, routedrun.InvokeJobEventFailed, routedrun.InvokeJobEventCancelled:
			hasTerminal = true
			terminalKind = ev.EventKind
		}
	}
	if hasTerminal {
		if terminalKind == routedrun.InvokeJobEventSucceeded {
			// F13: a SUCCEEDED journal event was committed but the attempt
			// CAS did not complete (crash between journal append and CAS).
			// Check the result store: if a verified SUCCEEDED result
			// exists, complete the success CAS instead of failing.
			return s.reconcileSuccess(ctx, att)
		}
		// A FAILED or CANCELLED terminal event was committed but the
		// attempt is non-terminal in the store. Conservatively mark FAILED.
		return s.revokeLeaseAndFail(ctx, att, "daemon_restart")
	}
	// No terminal event: ambiguous active lease. Revoke and fail.
	return s.revokeLeaseAndFail(ctx, att, "daemon_restart")
}

// isNotFoundish returns true when err is an "not found" / "not exist" error
// that indicates absence of data (safe to treat as "no events" during
// reconcile). Real IO/corruption errors are NOT notFoundish.
func isNotFoundish(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, routedrun.ErrNotFound)
}

// reconcileSuccess completes the success CAS pipeline when a SUCCEEDED journal
// event exists but the attempt is non-terminal (F13). It first verifies that a
// committed result exists in the result store; if so, it completes the attempt
// CAS and run CAS to SUCCEEDED. If no result exists, it falls back to failing
// the attempt (the journal event alone is insufficient evidence).
func (s *Supervisor) reconcileSuccess(ctx context.Context, att *routedrun.AttemptRecord) error {
	result, err := s.results.GetInvokeJobResult(ctx, att.RunID)
	if err != nil || result == nil {
		// No committed result exists: the journal event alone is not
		// sufficient. Revoke the lease and fail.
		return s.revokeLeaseAndFail(ctx, att, "daemon_restart")
	}
	// Verify the result matches this attempt and is SUCCEEDED.
	if result.AttemptID != att.AttemptID || result.TerminalStatus != routedrun.InvokeJobResultSucceeded {
		return s.revokeLeaseAndFail(ctx, att, "daemon_restart")
	}
	// Complete the attempt CAS to SUCCEEDED.
	if err := s.casAttemptToByRecord(ctx, att, routedrun.AttemptStatusSucceeded, "verified_result"); err != nil {
		// If the attempt is already terminal (raced with another reconciler),
		// accept it. Otherwise return the error.
		if errors.Is(err, ErrAlreadyTerminal) {
			return nil
		}
		return err
	}
	// Complete the run CAS to SUCCEEDED.
	if err := s.casRunTo(ctx, att.RunID, routedrun.RunStatusSucceeded); err != nil {
		_ = s.audit.Append(audit.AuditRecord{
			Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
			EventType:      "supervisor_reconcile_run_cas_error",
			DeploymentMode: "local",
			Actor:          "supervisor",
			Payload: map[string]interface{}{
				"run_id":     string(att.RunID),
				"attempt_id": string(att.AttemptID),
				"error":      err.Error(),
			},
		})
		return fmt.Errorf("reconcile: cas run to succeeded: %w", err)
	}
	// Establish in-memory tracking as terminal.
	s.markTerminalForRestart(att)
	return nil
}

// revokeLeaseAndFail revokes the attempt's lease and marks it FAILED with the
// given reason via CAS. Idempotent: if the attempt is already terminal, no-op.
func (s *Supervisor) revokeLeaseAndFail(ctx context.Context, att *routedrun.AttemptRecord, reason string) error {
	for i := 0; i < 8; i++ {
		current, err := s.store.GetAttempt(ctx, att.AttemptID)
		if err != nil {
			return err
		}
		if current.Status.IsTerminal() {
			return nil
		}
		if err := routedrun.ValidateAttemptTransition(current.Status, routedrun.AttemptStatusFailed); err != nil {
			return err
		}
		gen, err := s.store.GetAttemptGeneration(ctx, att.AttemptID)
		if err != nil {
			return err
		}
		now := s.nowWall()
		current.Status = routedrun.AttemptStatusFailed
		current.FailureReason = failureReasonFor(reason)
		current.UpdatedAt = now
		current.TerminatedAt = &now
		if current.Lease != nil {
			current.Lease.ExpiresAt = now
			current.Lease.LeaseToken = ""
		}
		if err := s.store.UpdateAttempt(ctx, current, gen); err != nil {
			if errors.Is(err, routedrun.ErrCASConflict) {
				continue
			}
			return err
		}
		// Append the FAILED control-journal event (durable evidence of the
		// restart reconciliation). Retry on sequence conflicts (F17).
		journal, err := s.journals.OpenControlJournal(att.RunID, att.AttemptID)
		if err == nil {
			for attempt := 0; attempt < 3; attempt++ {
				next := int64(1)
				if evs, rerr := journal.Read(1); rerr == nil && len(evs) > 0 {
					next = evs[len(evs)-1].Sequence + 1
				}
				aerr := journal.Append(routedrun.InvokeJobEvent{
					SchemaVersion: "1.0",
					Sequence:      next,
					Timestamp:     now,
					EventKind:     routedrun.InvokeJobEventFailed,
					Payload:       `{"reason":"daemon_restart"}`,
				})
				if aerr == nil {
					break
				}
				if !errors.Is(aerr, routedrun.ErrJournalSequenceConflict) {
					_ = journal.Close()
					return fmt.Errorf("reconcile: append FAILED journal event: %w", aerr)
				}
			}
			_ = journal.Close()
		}
		s.markTerminalForRestart(att)
		return nil
	}
	return routedrun.ErrCASConflict
}

// markTerminalForRestart establishes an in-memory terminal tracker for an
// attempt whose durable state is already terminal. If a tracker already exists
// for the attempt (e.g. from an in-flight HandleResult), it marks THAT tracker
// terminal under its lock rather than replacing it (F18). This prevents a
// live tracker from being orphaned with a stale reference. Only creates a new
// tracker when none exists.
func (s *Supervisor) markTerminalForRestart(att *routedrun.AttemptRecord) {
	s.mu.Lock()
	existing, ok := s.trackers[att.AttemptID]
	if ok {
		s.mu.Unlock()
		// A live tracker already exists - mark it terminal under its lock.
		existing.mu.Lock()
		if !existing.terminal {
			existing.terminal = true
			existing.terminalStatus = resultStatusFor(att.Status)
		}
		existing.mu.Unlock()
		return
	}
	trk := &attemptTracker{
		attemptID:      att.AttemptID,
		runID:          att.RunID,
		workflowID:     att.WorkflowID,
		leaseID:        leaseIDOf(att),
		terminal:       true,
		terminalStatus: resultStatusFor(att.Status),
	}
	s.trackers[att.AttemptID] = trk
	s.mu.Unlock()
}

// reconcileActiveTime reconciles the workflow's active-time ledger after a
// daemon restart. The rules (b30-summary.md T05):
//   - If the workflow is fully PAUSED or NEEDS_REPLAN (frozen): close any open
//     segment WITHOUT charging wall time. Frozen states do not accrue.
//   - If the workflow is PAUSE_REQUESTED or RUNNING (accruing): close the open
//     segment and charge the elapsed wall time since the segment start
//     (conservatively, exactly once). The elapsed is computed from the
//     monotonic clock; on restart the monotonic clock resets, so we use the
//     ledger's recorded consumed and conservatively clear the open segment
//     without charging unknown wall time (we cannot know how long the daemon
//     was down). This never double-charges and never forgives already-charged
//     time.
//
// The conservative close (clear the open segment without charging) is the safe
// default: it never double-charges. For PAUSE_REQUESTED and RUNNING, the
// supervisor's post-restart accounting resumes from the new monotonic baseline,
// so no time is lost going forward (the segment that was open at crash is
// closed conservatively, and a new segment starts on the next accrual).
func (s *Supervisor) reconcileActiveTime(ctx context.Context, workflowID routedrun.WorkflowID) error {
	if workflowID == "" {
		return nil
	}
	wf, err := s.store.GetWorkflow(ctx, workflowID)
	if err != nil {
		if errors.Is(err, routedrun.ErrNotFound) {
			return nil
		}
		return err
	}
	ledger, err := s.store.GetActiveTimeLedger(ctx, workflowID)
	if err != nil {
		if errors.Is(err, routedrun.ErrNotFound) {
			return nil
		}
		return err
	}
	if ledger == nil {
		return nil
	}
	// If there is no open segment, nothing to reconcile.
	if ledger.RunningSegmentStartMs == nil {
		return nil
	}
	// PAUSED and NEEDS_REPLAN are frozen: close the open segment WITHOUT
	// charging wall time. The consumed total is preserved (the time already
	// charged before the freeze remains). Record FrozenConsumedMs for
	// consistency with the store's syncActiveTimeOnStatusChangeLocked (F21).
	if wf.Status == routedrun.WorkflowStatusPaused || wf.Status == routedrun.WorkflowStatusNeedsReplan {
		ledger.RunningSegmentStartMs = nil
		ledger.SegmentStartWallMs = nil
		ledger.FrozenConsumedMs = ledger.ConsumedMs
		gen, err := s.store.GetActiveTimeLedgerGeneration(ctx, workflowID)
		if err != nil {
			return err
		}
		return s.store.PutActiveTimeLedger(ctx, workflowID, ledger, gen)
	}
	// PAUSE_REQUESTED and RUNNING are accruing. Compute elapsed: on a real
	// restart the monotonic clock resets to 0, so nowMs < segStart indicates
	// a new process. In that case, use the wall-clock delta (F20). Otherwise
	// use the monotonic delta (normal case, including fake-clock tests).
	nowMs := s.nowMonotonicMs()
	segStart := *ledger.RunningSegmentStartMs
	var elapsed int64
	if nowMs < segStart && ledger.SegmentStartWallMs != nil {
		// Monotonic clock reset: use wall-clock delta, capped at the
		// remaining active-time envelope so we never overcharge.
		nowWallMs := s.nowWall().UnixMilli()
		wallElapsed := nowWallMs - *ledger.SegmentStartWallMs
		if wallElapsed < 0 {
			wallElapsed = 0
		}
		// Remaining envelope: max active minus already consumed.
		wf, wfErr := s.store.GetWorkflow(ctx, workflowID)
		remainingEnv := int64(24 * 60 * 60 * 1000) // default 24h
		if wfErr == nil && wf.MaxActiveDurationMs > 0 {
			remainingEnv = wf.MaxActiveDurationMs - ledger.ConsumedMs
			if remainingEnv < 0 {
				remainingEnv = 0
			}
		}
		if wallElapsed > remainingEnv {
			elapsed = remainingEnv
		} else {
			elapsed = wallElapsed
		}
	} else {
		elapsed = nowMs - segStart
	}
	if elapsed > 0 {
		ledger.ConsumedMs += elapsed
	}
	ledger.RunningSegmentStartMs = nil
	ledger.SegmentStartWallMs = nil
	gen, err := s.store.GetActiveTimeLedgerGeneration(ctx, workflowID)
	if err != nil {
		return err
	}
	return s.store.PutActiveTimeLedger(ctx, workflowID, ledger, gen)
}
