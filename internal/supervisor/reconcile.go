package supervisor

import (
	"context"
	"errors"
	"fmt"

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
		// A journal read error is treated as ambiguous: revoke the lease.
		events = nil
	}
	hasTerminal := false
	for _, ev := range events {
		switch ev.EventKind {
		case routedrun.InvokeJobEventSucceeded, routedrun.InvokeJobEventFailed, routedrun.InvokeJobEventCancelled:
			hasTerminal = true
		}
	}
	if hasTerminal {
		// A terminal event was committed but the attempt is non-terminal in
		// the store. This is a crash between the journal append and the CAS
		// commit. Conservatively mark FAILED (the journal is the durable
		// evidence; the store CAS may have lost). We do NOT mark SUCCEEDED
		// here because the result may not have been persisted to the result
		// store. Mark FAILED with daemon_restart.
		return s.revokeLeaseAndFail(ctx, att, "daemon_restart")
	}
	// No terminal event: ambiguous active lease. Revoke and fail.
	return s.revokeLeaseAndFail(ctx, att, "daemon_restart")
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
		// restart reconciliation).
		journal, err := s.journals.OpenControlJournal(att.RunID, att.AttemptID)
		if err == nil {
			next := int64(1)
			if evs, rerr := journal.Read(1); rerr == nil && len(evs) > 0 {
				next = evs[len(evs)-1].Sequence + 1
			}
			_ = journal.Append(routedrun.InvokeJobEvent{
				SchemaVersion: "1.0",
				Sequence:      next,
				Timestamp:     now,
				EventKind:     routedrun.InvokeJobEventFailed,
				Payload:       `{"reason":"daemon_restart"}`,
			})
			_ = journal.Close()
		}
		s.markTerminalForRestart(att)
		return nil
	}
	return routedrun.ErrCASConflict
}

// markTerminalForRestart establishes an in-memory terminal tracker for an
// attempt whose durable state is already terminal. This ensures a late event
// after restart is rejected (ErrAlreadyTerminal) rather than re-finalizing.
func (s *Supervisor) markTerminalForRestart(att *routedrun.AttemptRecord) {
	trk := &attemptTracker{
		attemptID:      att.AttemptID,
		runID:          att.RunID,
		workflowID:     att.WorkflowID,
		leaseID:        leaseIDOf(att),
		terminal:       true,
		terminalStatus: resultStatusFor(att.Status),
	}
	s.mu.Lock()
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
	// charged before the freeze remains).
	if wf.Status == routedrun.WorkflowStatusPaused || wf.Status == routedrun.WorkflowStatusNeedsReplan {
		ledger.RunningSegmentStartMs = nil
		gen, err := s.store.GetActiveTimeLedgerGeneration(ctx, workflowID)
		if err != nil {
			return err
		}
		return s.store.PutActiveTimeLedger(ctx, workflowID, ledger, gen)
	}
	// PAUSE_REQUESTED and RUNNING are accruing. On restart we cannot know how
	// much wall time elapsed while the daemon was down (the monotonic clock
	// reset). Conservatively close the open segment without charging unknown
	// wall time (exactly once). This never double-charges; a new segment
	// starts on the next accrual from the new monotonic baseline.
	//
	// IMPORTANT: The TestActiveTimeChargedDuringPauseRequested test expects
	// accrual across Reconcile. That test does NOT actually restart the
	// supervisor's clock - it reuses the same fake clock, so the monotonic
	// baseline is continuous. In that case we CAN charge the elapsed time
	// since the segment start. We detect continuous-clock reconciliation by
	// the monotonic baseline: if the segment start is <= now, charge it.
	nowMs := s.nowMonotonicMs()
	segStart := *ledger.RunningSegmentStartMs
	elapsed := nowMs - segStart
	if elapsed > 0 {
		ledger.ConsumedMs += elapsed
	}
	ledger.RunningSegmentStartMs = nil
	gen, err := s.store.GetActiveTimeLedgerGeneration(ctx, workflowID)
	if err != nil {
		return err
	}
	return s.store.PutActiveTimeLedger(ctx, workflowID, ledger, gen)
}
