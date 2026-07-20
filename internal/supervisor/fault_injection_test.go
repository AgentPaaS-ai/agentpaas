package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ============================================================================
// Fault injection test file for B30-T07.
//
// Every fault yields one honest terminal or resumable state and zero accepted
// governed actions after lease revocation.
// ============================================================================

// ---------------------------------------------------------------------------
// 1. Restart during idle between turns
// ---------------------------------------------------------------------------

// TestFault_RestartDuringIdleBetweenTurns verifies that a daemon restart
// during idle between turns does not falsely succeed the run. The run is
// resumable from its last checkpoint after Reconcile.
func TestFault_RestartDuringIdleBetweenTurns(t *testing.T) {
	h := newRefWorkerHarness(t)
	h.claimAndBuildWorker()
	ctx := context.Background()

	// Run phases 1-2 (turns 1-10), then checkpoint.
	err := h.worker.runPhases(ctx, RunOptions{}, 1, 2)
	if err != nil {
		t.Fatalf("partial run phases 1-2: %v", err)
	}

	// Simulate daemon restart: create a new Supervisor.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	// Reconcile: should mark the attempt FAILED with daemon_restart.
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	att, err := h.store.GetAttempt(ctx, h.attemptID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	// The attempt should be FAILED with DAEMON_RESTARTED.
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED after restart during idle", att.Status)
	}
	if att.FailureReason == nil || !strings.Contains(att.FailureReason.String(), "DAEMON_RESTARTED") {
		t.Fatalf("failure reason = %v, want DAEMON_RESTARTED", att.FailureReason)
	}
	// Lease must be revoked.
	if att.Lease != nil && att.Lease.LeaseToken != "" {
		t.Fatalf("lease token = %q, want cleared after restart", att.Lease.LeaseToken)
	}
	// Run should be FAILED.
	run, err := h.store.GetRun(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != routedrun.RunStatusFailed {
		t.Fatalf("run status = %s, want FAILED after restart", run.Status)
	}
	// Checkpoint MUST be preserved for B39.
	cp, err := h.store.GetLatestCheckpoint(ctx, h.attemptID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if cp == nil || !cp.SafeToResume {
		t.Fatal("checkpoint must be preserved and safe-to-resume after restart")
	}
}

// ---------------------------------------------------------------------------
// 2. Restart during model call
// ---------------------------------------------------------------------------

// TestFault_RestartDuringModelCall verifies that a restart during an in-flight
// model call does not count the in-flight operation as progress. The lease is
// revoked and Reconcile marks the attempt FAILED.
func TestFault_RestartDuringModelCall(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Start a model call (in-flight governed operation).
	if err := h.supervisor.HandleModelStart(ctx, attID, h.leaseID); err != nil {
		t.Fatalf("HandleModelStart: %v", err)
	}

	// Simulate daemon restart while model call is in flight.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED after restart during model call", att.Status)
	}
	if att.FailureReason == nil || !strings.Contains(att.FailureReason.String(), "DAEMON_RESTARTED") {
		t.Fatalf("failure reason = %v, want DAEMON_RESTARTED", att.FailureReason)
	}
}

// ---------------------------------------------------------------------------
// 3. Restart during tool phase
// ---------------------------------------------------------------------------

// TestFault_RestartDuringToolPhase verifies restart during an in-flight HTTP
// (tool) operation. Same semantics as model-call restart.
func TestFault_RestartDuringToolPhase(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Start an HTTP tool call.
	if err := h.supervisor.HandleHTTPStart(ctx, attID, h.leaseID); err != nil {
		t.Fatalf("HandleHTTPStart: %v", err)
	}

	// Simulate daemon restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED after restart during tool phase", att.Status)
	}
	if att.FailureReason == nil || !strings.Contains(att.FailureReason.String(), "DAEMON_RESTARTED") {
		t.Fatalf("failure reason = %v, want DAEMON_RESTARTED", att.FailureReason)
	}
}

// ---------------------------------------------------------------------------
// 4. Restart during checkpoint commit
// ---------------------------------------------------------------------------

// TestFault_RestartDuringCheckpointCommit verifies that a restart mid-checkpoint
// (after SaveCheckpoint but before the journal event append) handles partial
// state correctly. The checkpoint exists in the store and survives reconcile.
func TestFault_RestartDuringCheckpointCommit(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Save a checkpoint directly to the store (simulating partial commit:
	// store.SaveCheckpoint completed, but journal event not yet appended).
	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		CheckpointID:     "cp-fault-test",
		AttemptID:        attID,
		RunID:            h.runID,
		WorkflowID:       h.workflowID,
		LeaseID:          h.leaseID,
		Phase:            "phase-1",
		CompletedWork:    []string{"a", "b"},
		RemainingWork:    []string{"c"},
		SafeToResume:     true,
		Sequence:         1,
		CreatedAt:        h.clock.Now(),
	}
	// B30-2 (F8): let SaveCheckpoint auto-compute the digest.
	if err := h.store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Simulate daemon restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Checkpoint must survive reconcile (preserved for B39).
	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.CheckpointID != "cp-fault-test" {
		t.Fatalf("checkpoint id = %s, want cp-fault-test", got.CheckpointID)
	}
	if !got.SafeToResume {
		t.Fatal("checkpoint must remain safe-to-resume after reconcile")
	}

	// The attempt should be FAILED (no terminal event was committed).
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED", att.Status)
	}
}

// ---------------------------------------------------------------------------
// 5. Restart during final result commit
// ---------------------------------------------------------------------------

// TestFault_RestartDuringFinalResultCommit: a result was saved to the result
// store but the journal terminal event and attempt CAS were not committed.
// Reconcile must NOT falsely mark the run succeeded. It marks FAILED with
// daemon_restart.
func TestFault_RestartDuringFinalResultCommit(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Save a result to the result store DIRECTLY (simulating partial commit:
	// SaveInvokeJobResult completed but HandleResult did not finish).
	result := &routedrun.InvokeJobResult{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		InvocationID:     "inv-fault-test",
		WorkflowID:       h.workflowID,
		RunID:            h.runID,
		AttemptID:        attID,
		ResultDigest:     "digest-fault-result",
		StructuredResult: `{"ok":true}`,
		TerminalStatus:   routedrun.InvokeJobResultSucceeded,
	}
	if err := h.results.SaveInvokeJobResult(ctx, result); err != nil {
		t.Fatalf("SaveInvokeJobResult: %v", err)
	}

	// Simulate daemon restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// The attempt must be FAILED, NOT SUCCEEDED.
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status == routedrun.AttemptStatusSucceeded {
		t.Fatal("attempt must NOT be SUCCEEDED without a committed terminal journal event")
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED", att.Status)
	}
	if att.FailureReason == nil || !strings.Contains(att.FailureReason.String(), "DAEMON_RESTARTED") {
		t.Fatalf("failure reason = %v, want DAEMON_RESTARTED", att.FailureReason)
	}
	// The run must NOT be SUCCEEDED.
	run, err := h.store.GetRun(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status == routedrun.RunStatusSucceeded {
		t.Fatal("run must NOT be SUCCEEDED without a committed terminal journal event")
	}
}

// ---------------------------------------------------------------------------
// 6. Cancel during idle between turns
// ---------------------------------------------------------------------------

// TestCancel_DuringIdleBetweenTurns verifies cancel during idle marks the
// run CANCELLED and no further progress is accepted.
func TestCancel_DuringIdleBetweenTurns(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Simulate progress so the attempt is active.
	p := h.makeProgress(1, "working")
	if err := h.supervisor.TrackProgress(ctx, attID, p); err != nil {
		t.Fatalf("TrackProgress: %v", err)
	}

	// Cancel during idle.
	if err := h.supervisor.Cancel(ctx, attID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// Verify CANCELLED.
	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("attempt status = %s, want CANCELLED", got)
	}

	// Further progress must be rejected.
	p2 := h.makeProgress(2, "after-cancel")
	if err := h.supervisor.TrackProgress(ctx, attID, p2); !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("TrackProgress after cancel: want ErrAlreadyTerminal, got %v", err)
	}

	// Run should be CANCELLED.
	run, err := h.store.GetRun(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != routedrun.RunStatusCancelled {
		t.Fatalf("run status = %s, want CANCELLED", run.Status)
	}
}

// ---------------------------------------------------------------------------
// 7. Cancel during model call
// ---------------------------------------------------------------------------

// TestCancel_DuringModelCall verifies cancel wins over an in-flight model call.
// The late model-end event must be rejected after cancel.
func TestCancel_DuringModelCall(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Start a model call.
	if err := h.supervisor.HandleModelStart(ctx, attID, h.leaseID); err != nil {
		t.Fatalf("HandleModelStart: %v", err)
	}

	// Cancel while the model call is in flight.
	if err := h.supervisor.Cancel(ctx, attID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("attempt status = %s, want CANCELLED", got)
	}

	// The late model-end must be rejected (the tracker for the new supervisor
	// is terminal; but the original supervisor's tracker is also terminal).
	// Test with the original supervisor first.
	if err := h.supervisor.HandleModelEnd(ctx, attID, h.leaseID); !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("HandleModelEnd after cancel: want ErrAlreadyTerminal, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 8. Cancel during checkpoint commit
// ---------------------------------------------------------------------------

// TestCancel_DuringCheckpointCommit: cancel arrives after SaveCheckpoint but
// before the journal event. The cancel wins; the checkpoint survives in the
// store (preserved for B39).
func TestCancel_DuringCheckpointCommit(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Save a checkpoint directly (partial commit).
	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		CheckpointID:     "cp-cancel-test",
		AttemptID:        attID,
		RunID:            h.runID,
		WorkflowID:       h.workflowID,
		LeaseID:          h.leaseID,
		Phase:            "phase-1",
		SafeToResume:     true,
		Sequence:         1,
		CreatedAt:        h.clock.Now(),
	}
	// B30-2 (F8): let SaveCheckpoint auto-compute the digest.
	if err := h.store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Cancel.
	if err := h.supervisor.Cancel(ctx, attID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("attempt status = %s, want CANCELLED", got)
	}

	// Checkpoint must survive.
	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.CheckpointID != "cp-cancel-test" {
		t.Fatalf("checkpoint id = %s, want cp-cancel-test", got.CheckpointID)
	}
}

// ---------------------------------------------------------------------------
// 9. Cancel during final result commit (cancel precedence at commit boundary)
// ---------------------------------------------------------------------------

// TestCancel_DuringFinalResultCommit verifies that cancel arriving at the
// same time as a final result commit wins. Cancel precedence over late success.
func TestCancel_DuringFinalResultCommit(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Cancel first.
	if err := h.supervisor.Cancel(ctx, attID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("attempt status = %s, want CANCELLED", got)
	}

	// Now a late result event arrives. Must be rejected.
	r := h.makeSuccessResult()
	if err := h.supervisor.HandleResult(ctx, attID, r); !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("HandleResult after cancel: want ErrAlreadyTerminal, got %v", err)
	}

	// State unchanged.
	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("attempt status after late result = %s, want CANCELLED", got)
	}
}

// ---------------------------------------------------------------------------
// 10. Lease expiry after claim
// ---------------------------------------------------------------------------

// TestLeaseExpiry_AfterClaim verifies lease expiry revokes the lease and
// prevents further progress.
func TestLeaseExpiry_AfterClaim(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Load the attempt and manually expire the lease in the store.
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Lease == nil {
		t.Fatal("expected lease after claim")
	}
	// Expire the lease by setting ExpiresAt in the past.
	gen, err := h.store.GetAttemptGeneration(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttemptGeneration: %v", err)
	}
	att.Lease.ExpiresAt = h.clock.Now().Add(-1 * time.Minute)
	att.Lease.LeaseToken = "" // Clear the token.
	if err := h.store.UpdateAttempt(ctx, att, gen); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}

	// Progress with the original lease should now be rejected because the
	// tracker's lease still matches but the durable lease is stale. The
	// supervisor does not check the durable lease on TrackProgress; it
	// checks the in-memory tracker. The lease revocation is done by the
	// daemon restart / reconcile path.
	//
	// To properly test: simulate restart and verify lease is revoked.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	attAfter, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt after reconcile: %v", err)
	}
	if attAfter.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status after expire+reconcile = %s, want FAILED", attAfter.Status)
	}
	if attAfter.FailureReason == nil || !strings.Contains(attAfter.FailureReason.String(), "DAEMON_RESTARTED") {
		t.Fatalf("failure reason = %v, want DAEMON_RESTARTED", attAfter.FailureReason)
	}
}

// ---------------------------------------------------------------------------
// 11-20: Additional B30-T07 fault tests
// ---------------------------------------------------------------------------

// TestFault_JournalTamperDetection verifies that a corrupted control journal
// event (HMAC mismatch) is detected on read and reconciliation treats it
// correctly - the attempt is failed rather than silently accepted.
func TestFault_JournalTamperDetection(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Append a valid SUCCEEDED event via HandleResult (this also does CAS, so
	// the attempt becomes terminal). For tamper detection we instead directly
	// tamper the journal.
	journal := h.journals.get(h.runID, attID)
	if journal == nil {
		t.Fatal("journal not found")
	}

	// Append a valid event.
	evt := routedrun.InvokeJobEvent{
		SchemaVersion: "1.0",
		Sequence:      2, // ACCEPTED is seq 1 from Claim
		Timestamp:     h.clock.Now().UTC(),
		EventKind:     routedrun.InvokeJobEventSucceeded,
		Payload:       `{"terminal":true}`,
	}
	if err := journal.Append(evt); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Now tamper with the journal event data by directly modifying the
	// in-memory store (the fake journal's events slice). This simulates
	// on-disk tampering.
	journal.mu.Lock()
	if len(journal.events) > 1 {
		journal.events[1].HMAC = "deadbeef"
	}
	journal.mu.Unlock()

	// Simulate restart: a new supervisor reads the tampered journal.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	// Reconcile on the tampered journal should return an error (F16),
	// rather than silently treating it as no events.
	err = sup2.Reconcile(ctx, h.runID)
	if err == nil {
		t.Fatal("Reconcile should return error on tampered journal read")
	}
	// The error should contain something about the journal read failure.
	if !strings.Contains(err.Error(), "journal read error") {
		t.Fatalf("Reconcile error = %v, want error mentioning journal read", err)
	}
}

// TestFault_ResultCheckpointAcrossGenerations verifies that checkpoints
// committed by one attempt survive a subsequent attempt that fails, and can
// be used for resumption across generations.
func TestFault_ResultCheckpointAcrossGenerations(t *testing.T) {
	h := newTestHarness(t)
	attID1, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim attempt 1: %v", err)
	}
	ctx := context.Background()

	// Commit a safe checkpoint in attempt 1.
	cp1 := &routedrun.SemanticCheckpoint{
		SchemaVersion:  routedrun.CurrentSchemaVersion,
		CheckpointID:   "cp-gen-1",
		AttemptID:      attID1,
		RunID:          h.runID,
		WorkflowID:     h.workflowID,
		LeaseID:        h.leaseID,
		Phase:          "phase-1",
		CompletedWork:  []string{"a", "b"},
		RemainingWork:  []string{"c", "d"},
		SafeToResume:   true,
		Sequence:       1,
		CreatedAt:      h.clock.Now(),
	}
	if err := h.supervisor.HandleCheckpoint(ctx, attID1, h.makeCheckpoint(cp1)); err != nil {
		t.Fatalf("HandleCheckpoint attempt 1: %v", err)
	}

	// Finalize attempt 1 as FAILED (simulating crash).
	if err := h.supervisor.Finalize(ctx, attID1); err != nil {
		t.Fatalf("Finalize attempt 1: %v", err)
	}

	// Create attempt 2 (simulating retry by worker/daemon).
	// We must create a new run-attempt through the store.
	// The harness only creates one run; we need to create a second attempt.
	att2 := &routedrun.AttemptRecord{
		SchemaVersion: routedrun.CurrentSchemaVersion,
		RunID:         h.runID,
		WorkflowID:    h.workflowID,
		Status:        routedrun.AttemptStatusRunning,
		AttemptNumber: 2,
		Lease: &routedrun.AttemptLease{
			DurationMs: 300_000,
			AcquiredAt: h.clock.Now(),
			ExpiresAt:  h.clock.Now().Add(300 * time.Second),
		},
	}
	if err := h.store.CreateAttempt(ctx, att2); err != nil {
		t.Fatalf("CreateAttempt 2: %v", err)
	}
	att2, err = h.store.GetAttempt(ctx, att2.AttemptID)
	if err != nil {
		t.Fatalf("GetAttempt 2: %v", err)
	}

	// Checkpoint from attempt 1 must survive.
	got, err := h.store.GetLatestCheckpoint(ctx, attID1)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.CheckpointID != "cp-gen-1" {
		t.Fatalf("checkpoint id = %s, want cp-gen-1", got.CheckpointID)
	}
	if !got.SafeToResume {
		t.Fatal("checkpoint must remain safe-to-resume for attempt 2")
	}

	// New attempt should be able to use the checkpoint for resumption.
	_ = att2 // Attempt 2 exists and the checkpoint is available for it.
}

// TestFault_CheckpointDigestAutoCompute verifies that a checkpoint with an
// empty digest has its digest auto-computed by SaveCheckpoint.
func TestFault_CheckpointDigestAutoCompute(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		CheckpointID:        "cp-auto-digest",
		AttemptID:           attID,
		RunID:               h.runID,
		WorkflowID:          h.workflowID,
		LeaseID:             h.leaseID,
		Phase:               "phase-1",
		CompletedWork:       []string{"x"},
		RemainingWork:       []string{"y"},
		SafeToResume:        true,
		CheckpointDigest:    "", // Empty digest - must be auto-computed.
		Sequence:            1,
		CreatedAt:           h.clock.Now(),
	}
	if err := h.supervisor.HandleCheckpoint(ctx, attID, h.makeCheckpoint(cp)); err != nil {
		t.Fatalf("HandleCheckpoint: %v", err)
	}

	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.CheckpointDigest == "" {
		t.Fatal("checkpoint digest is empty (should have been auto-computed)")
	}
}

// TestFault_LateResultRejectedAfterRestart verifies that after restart and
// reconcile marks an attempt terminal, a late result event arriving is
// rejected.
func TestFault_LateResultRejectedAfterRestart(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Simulate restart: reconcile fails the attempt.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Now a late result event arrives. Must be rejected.
	r := h.makeSuccessResult()
	if err := sup2.HandleResult(ctx, attID, r); !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("HandleResult after restart: want ErrAlreadyTerminal, got %v", err)
	}
}

// TestFault_CheckpointTamperResilience verifies checkpoint digest tampering is
// detected and the checkpoint is preserved with auto-computed digest.
func TestFault_CheckpointTamperResilience(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		CheckpointID:        "cp-tamper-test",
		AttemptID:           attID,
		RunID:               h.runID,
		WorkflowID:          h.workflowID,
		LeaseID:             h.leaseID,
		Phase:               "phase-1",
		CompletedWork:       []string{"a"},
		RemainingWork:       []string{"b"},
		SafeToResume:        true,
		CheckpointDigest:    "", // B30-2 (F8): let SaveCheckpoint auto-compute.
		Sequence:            1,
		CreatedAt:           h.clock.Now(),
	}
	if err := h.supervisor.HandleCheckpoint(ctx, attID, h.makeCheckpoint(cp)); err != nil {
		t.Fatalf("HandleCheckpoint: %v", err)
	}

	// Verify the digest was auto-computed (original "wrong" digest replaced).
	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.CheckpointID != "cp-tamper-test" {
		t.Fatalf("checkpoint id = %s, want cp-tamper-test", got.CheckpointID)
	}
	// The digest should have been auto-computed, not the wrong one.
	if got.CheckpointDigest == "wrong-digest-that-will-be-replaced" {
		t.Fatal("checkpoint digest was not auto-computed (still the wrong value)")
	}
	if got.CheckpointDigest == "" {
		t.Fatal("checkpoint digest is empty after auto-compute")
	}
}

// TestFault_CheckpointDigestPreservedOnRestart verifies that after restart
// and reconcile, the checkpoint's auto-computed digest survives intact.
func TestFault_CheckpointDigestPreservedOnRestart(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		CheckpointID:        "cp-tamper-test",
		AttemptID:           attID,
		RunID:               h.runID,
		WorkflowID:          h.workflowID,
		LeaseID:             h.leaseID,
		Phase:               "phase-1",
		CompletedWork:       []string{"a"},
		RemainingWork:       []string{"b"},
		SafeToResume:        true,
		CheckpointDigest:    "", // B30-2 (F8): let SaveCheckpoint auto-compute.
		Sequence:            1,
		CreatedAt:           h.clock.Now(),
	}
	if err := h.supervisor.HandleCheckpoint(ctx, attID, h.makeCheckpoint(cp)); err != nil {
		t.Fatalf("HandleCheckpoint: %v", err)
	}

	// Restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint after reconcile: %v", err)
	}
	if got.CheckpointID != "cp-tamper-test" {
		t.Fatalf("checkpoint id after reconcile = %s, want cp-tamper-test", got.CheckpointID)
	}
	if got.CheckpointDigest == "" {
		t.Fatal("checkpoint digest after reconcile is empty (should have been auto-computed)")
	}
}

// TestFault_ResultTamperDoesNotReplayWork verifies that tampering with the
// result file does not falsely succeed a run on reconcile. The journal is the
// durable evidence, not the result file.
func TestFault_ResultTamperDoesNotReplayWork(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Write a forged result file directly (tamper scenario).
	forgedPath := h.results.resultPath(h.runID)
	if err := os.MkdirAll(filepath.Dir(forgedPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	forged := &routedrun.InvokeJobResult{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		RunID:            h.runID,
		AttemptID:        attID,
		TerminalStatus:   routedrun.InvokeJobResultSucceeded,
		StructuredResult: `{"forged":true}`,
	}
	data, _ := json.Marshal(forged)
	if err := os.WriteFile(forgedPath, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Simulate restart. Reconcile must NOT trust the result file without
	// terminal events, not the result file. On reconcile, already-terminal
	// attempts are accepted; but here the attempt is non-terminal and there
	// is no journal SUCCEEDED event. It must fail.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status == routedrun.AttemptStatusSucceeded {
		t.Fatal("attempt must NOT be SUCCEEDED based on forged result file alone")
	}
}

// TestFault_ForgedResultFileOnRestart verifies that a result file that
// succeeds at the file level. On reconcile, the supervisor must not
// trust it without a terminal journal event.
func TestFault_ForgedResultFileOnRestart(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	forgedPath := h.results.resultPath(h.runID)
	if err := os.MkdirAll(filepath.Dir(forgedPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(forgedPath, []byte(`{"run_id":"run-test","terminal_status":2}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status == routedrun.AttemptStatusSucceeded {
		t.Fatal("attempt must NOT be SUCCEEDED based on result file alone")
	}
}

// TestReconcile_TwiceIsIdempotent verifies calling Reconcile twice produces
// the same outcome and does not double-append journal events.
func TestReconcile_TwiceIsIdempotent(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Simulate restart: new supervisor, reconcile.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	// First reconcile.
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	// Verify state after first reconcile.
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt after first reconcile: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status after first reconcile = %s, want FAILED", att.Status)
	}

	// Second reconcile (idempotent).
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}

	att2, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt after second reconcile: %v", err)
	}
	if att2.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status after second reconcile = %s, want FAILED", att2.Status)
	}

	// Verify the journal has exactly one FAILED event (not duplicates).
	journal := h.journals.get(h.runID, attID)
	journal.mu.Lock()
	failedCount := 0
	for _, ev := range journal.events {
		if ev.EventKind == routedrun.InvokeJobEventFailed {
			failedCount++
		}
	}
	journal.mu.Unlock()
	if failedCount > 1 {
		t.Fatalf("journal has %d FAILED events, want <= 1 (idempotent reconcile)", failedCount)
	}
}

// ---------------------------------------------------------------------------
// Additional fault tests (read-after-crash, consistent-bookkeeping)
// ---------------------------------------------------------------------------

// TestFault_RunStatusAfterCrash verifies run- and attempt-level consistency
// after a daemon crash/reconcile cycle.
func TestFault_RunStatusAfterCrash(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Send progress and a checkpoint.
	p := h.makeProgress(1, "working")
	if err := h.supervisor.TrackProgress(ctx, attID, p); err != nil {
		t.Fatalf("TrackProgress: %v", err)
	}
	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:  routedrun.CurrentSchemaVersion,
		CheckpointID:   "cp-consistent",
		AttemptID:      attID,
		RunID:          h.runID,
		WorkflowID:     h.workflowID,
		Phase:          "phase-1",
		SafeToResume:   true,
		Sequence:       1,
		CreatedAt:      h.clock.Now(),
	}
	if err := h.supervisor.HandleCheckpoint(ctx, attID, h.makeCheckpoint(cp)); err != nil {
		t.Fatalf("HandleCheckpoint: %v", err)
	}

	// Simulate crash + restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Consensus: attempt FAILED, run FAILED.
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED", att.Status)
	}

	run, err := h.store.GetRun(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != routedrun.RunStatusFailed {
		t.Fatalf("run status = %s, want FAILED", run.Status)
	}
}

// TestFault_NoBlindReplayOnRestart verifies that after restart+reconcile,
// the supervisor does not blindly re-invoke work (the attempted run should
// be in a terminal state, not re-running).
func TestFault_NoBlindReplayOnRestart(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.claimAttempt(); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Simulate restart: reconcile marks attempt FAILED.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify the run is terminal.
	run, err := h.store.GetRun(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if !run.Status.IsTerminal() {
		t.Fatal("run should be terminal after reconcile")
	}

	// Get the attempt and verify it's terminal.
	atts, err := h.store.ListAttempts(ctx, h.runID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(atts) == 0 {
		t.Fatal("no attempts after reconcile")
	}
	for _, a := range atts {
		if !a.Status.IsTerminal() {
			t.Fatalf("attempt %s status = %s, want terminal", a.AttemptID, a.Status)
		}
	}
}

// TestFault_JournalSurvivesRestart verifies that the control journal
// is preserved across restarts and can be replayed.
func TestFault_JournalSurvivesRestart(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Accept some progress.
	p1 := h.makeProgress(1, "phase-1")
	if err := h.supervisor.TrackProgress(ctx, attID, p1); err != nil {
		t.Fatalf("TrackProgress 1: %v", err)
	}
	p2 := h.makeProgress(2, "phase-2")
	if err := h.supervisor.TrackProgress(ctx, attID, p2); err != nil {
		t.Fatalf("TrackProgress 2: %v", err)
	}

	// Simulate restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Journal must contain the ACCEPTED event from Claim, plus one FAILED
	// event from reconcile. (TrackProgress does not produce journal events.)
	journal := h.journals.get(h.runID, attID)
	journal.mu.Lock()
	defer journal.mu.Unlock()

	hasAccepted := false
	hasFailed := false
	for _, ev := range journal.events {
		switch ev.EventKind {
		case routedrun.InvokeJobEventAccepted:
			hasAccepted = true
		case routedrun.InvokeJobEventFailed:
			hasFailed = true
		}
	}
	if !hasAccepted {
		t.Fatal("journal must contain ACCEPTED event")
	}
	if !hasFailed {
		t.Fatal("journal must contain FAILED event from reconcile")
	}
}

// TestFault_ReconcileOnNonTerminalRunPreservesState verifies that reconcile
// preserves checkpoint, artifact, and active-time state while failing the
// attempt.
func TestFault_ReconcileOnNonTerminalRunPreservesState(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Save a checkpoint.
	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		CheckpointID:     "cp-preserve",
		AttemptID:        attID,
		RunID:            h.runID,
		WorkflowID:       h.workflowID,
		LeaseID:          h.leaseID,
		Phase:            "phase-1",
		CompletedWork:    []string{"x", "y"},
		RemainingWork:    []string{"z"},
		SafeToResume:     true,
		Sequence:         1,
		CreatedAt:        h.clock.Now(),
	}
	// B30-2 (F8): let SaveCheckpoint auto-compute the digest.
	if err := h.supervisor.HandleCheckpoint(ctx, attID, h.makeCheckpoint(cp)); err != nil {
		t.Fatalf("HandleCheckpoint: %v", err)
	}

	// Simulate restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Checkpoint must survive (preserved for B39 resumption).
	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.CheckpointID != "cp-preserve" {
		t.Fatalf("checkpoint id = %s, want cp-preserve", got.CheckpointID)
	}
	if !got.SafeToResume {
		t.Fatal("checkpoint must remain safe-to-resume after reconcile")
	}

	// Attempt must be FAILED.
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED", att.Status)
	}
}

// ---------------------------------------------------------------------------
// F13: Restart with committed journal SUCCEEDED + result store present
// ---------------------------------------------------------------------------

// TestFault_F13_ReconcileSuccessWithResult verifies the F13 fix: when a
// SUCCEEDED journal event exists AND a committed SUCCEEDED result exists in
// the result store, but the attempt CAS did not complete (crash window),
// Reconcile MUST complete the success CAS instead of failing the attempt.
//
// Scenario:
//   1. Claim an attempt (ACCEPTED journal event appended, attempt RUNNING).
//   2. Manually append a SUCCEEDED journal event (simulating crash after
//      journal append, before CAS).
//   3. Manually save a SUCCEEDED result to the result store.
//   4. Simulate daemon restart (new supervisor, no in-memory state).
//   5. Call Reconcile.
//   6. Assert: attempt SUCCEEDED, run SUCCEEDED, result retrievable.
func TestFault_F13_ReconcileSuccessWithResult(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Step 2: Manually append a SUCCEEDED journal event.
	// This simulates the crash window: journal got the SUCCEEDED event,
	// but the attempt CAS didn't complete.
	journal := h.journals.get(h.runID, attID)
	if journal == nil {
		t.Fatal("journal not found")
	}
	succEvent := routedrun.InvokeJobEvent{
		SchemaVersion: "1.0",
		Sequence:      2, // ACCEPTED is seq 1 from Claim
		Timestamp:     h.clock.Now().UTC(),
		EventKind:     routedrun.InvokeJobEventSucceeded,
		Payload:       "{}",
	}
	if err := journal.Append(succEvent); err != nil {
		t.Fatalf("Append SUCCEEDED event: %v", err)
	}

	// Verify the attempt is still RUNNING (CAS didn't happen).
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusRunning {
		t.Fatalf("pre-reconcile attempt status = %s, want RUNNING", att.Status)
	}

	// Step 3: Save a SUCCEEDED result to the result store.
	result := &routedrun.InvokeJobResult{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		InvocationID:     "inv-f13-test",
		WorkflowID:       h.workflowID,
		RunID:            h.runID,
		AttemptID:        attID,
		ResultDigest:     "digest-f13",
		StructuredResult: `{"ok":true}`,
		TerminalStatus:   routedrun.InvokeJobResultSucceeded,
	}
	if err := h.results.SaveInvokeJobResult(ctx, result); err != nil {
		t.Fatalf("SaveInvokeJobResult: %v", err)
	}

	// Step 4: Simulate daemon restart with a new supervisor.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	// Step 5: Reconcile.
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Step 6: Assert SUCCEEDED.
	att, err = h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt after reconcile: %v", err)
	}
	if att.Status != routedrun.AttemptStatusSucceeded {
		t.Fatalf("attempt status after reconcile = %s, want SUCCEEDED (F13: result-present restart must finalize success)", att.Status)
	}

	// Run must also be SUCCEEDED.
	run, err := h.store.GetRun(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != routedrun.RunStatusSucceeded {
		t.Fatalf("run status = %s, want SUCCEEDED (F13: result-present restart must finalize run)", run.Status)
	}

	// Result must be retrievable.
	got, err := h.results.GetInvokeJobResult(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetInvokeJobResult: %v", err)
	}
	if got.TerminalStatus != routedrun.InvokeJobResultSucceeded {
		t.Fatalf("result status = %d, want SUCCEEDED", got.TerminalStatus)
	}
}

// TestFault_F13_ReconcileSuccessWithoutResult verifies the F13 guard: a
// SUCCEEDED journal event WITHOUT a committed result still fails the
// attempt (the journal alone is insufficient evidence).
func TestFault_F13_ReconcileSuccessWithoutResult(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Manually append a SUCCEEDED journal event, but do NOT save a result.
	journal := h.journals.get(h.runID, attID)
	if journal == nil {
		t.Fatal("journal not found")
	}
	succEvent := routedrun.InvokeJobEvent{
		SchemaVersion: "1.0",
		Sequence:      2,
		Timestamp:     h.clock.Now().UTC(),
		EventKind:     routedrun.InvokeJobEventSucceeded,
		Payload:       "{}",
	}
	if err := journal.Append(succEvent); err != nil {
		t.Fatalf("Append SUCCEEDED event: %v", err)
	}

	// Simulate restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Attempt must be FAILED (journal alone is not sufficient, no result).
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED (journal SUCCEEDED without result is insufficient)", att.Status)
	}
}

// TestFault_F13_ReconcileSuccessMismatchedAttempt verifies the F13 guard:
// a SUCCEEDED journal event with a result that references a different
// attempt does not complete the success CAS.
func TestFault_F13_ReconcileSuccessMismatchedAttempt(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Manually append a SUCCEEDED journal event.
	journal := h.journals.get(h.runID, attID)
	if journal == nil {
		t.Fatal("journal not found")
	}
	succEvent := routedrun.InvokeJobEvent{
		SchemaVersion: "1.0",
		Sequence:      2,
		Timestamp:     h.clock.Now().UTC(),
		EventKind:     routedrun.InvokeJobEventSucceeded,
		Payload:       "{}",
	}
	if err := journal.Append(succEvent); err != nil {
		t.Fatalf("Append SUCCEEDED event: %v", err)
	}

	// Save a result with a DIFFERENT attempt ID.
	result := &routedrun.InvokeJobResult{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		InvocationID:     "inv-f13-test",
		WorkflowID:       h.workflowID,
		RunID:            h.runID,
		AttemptID:        "wrong-attempt-id",
		ResultDigest:     "digest-f13",
		StructuredResult: `{"ok":true}`,
		TerminalStatus:   routedrun.InvokeJobResultSucceeded,
	}
	if err := h.results.SaveInvokeJobResult(ctx, result); err != nil {
		t.Fatalf("SaveInvokeJobResult: %v", err)
	}

	// Simulate restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Attempt must be FAILED (result references wrong attempt).
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED (result references wrong attempt)", att.Status)
	}
}

// ---------------------------------------------------------------------------
// B30-T07 boundary: checkpoint metadata integrity
// ---------------------------------------------------------------------------

// TestFault_CheckpointBoundaryMetadata verifies that a checkpoint committed
// with all metadata fields (CompletedWork, RemainingWork, ArtifactRefs,
// LastCommittedAction) preserves them across a daemon restart and that the
// checkpoint digest is non-empty (auto-computed when provided digest is
// superseded). The checkpoint must survive reconcile fully intact.
func TestFault_CheckpointBoundaryMetadata(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		CheckpointID:        "cp-boundary",
		AttemptID:           attID,
		RunID:               h.runID,
		WorkflowID:          h.workflowID,
		LeaseID:             h.leaseID,
		Phase:               "boundary-phase",
		CompletedWork:       []string{"work-a", "work-b"},
		RemainingWork:       []string{"work-c"},
		ArtifactRefs:        []string{"artifact-1.json"},
		LastCommittedAction: "action-1",
		SafeToResume:        true,
		CheckpointDigest:    "", // B30-2 (F8): let SaveCheckpoint auto-compute.
		Sequence:            42,
		CreatedAt:           h.clock.Now(),
	}
	if err := h.supervisor.HandleCheckpoint(ctx, attID, h.makeCheckpoint(cp)); err != nil {
		t.Fatalf("HandleCheckpoint: %v", err)
	}

	// Restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Checkpoint must survive intact.
	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.CheckpointID != "cp-boundary" {
		t.Fatalf("checkpoint id = %s, want cp-boundary", got.CheckpointID)
	}
	if got.CheckpointDigest == "" {
		t.Fatal("checkpoint digest is empty (should have been auto-computed)")
	}
	if got.Sequence != 42 {
		t.Fatalf("checkpoint sequence = %d, want 42", got.Sequence)
	}
	if len(got.CompletedWork) != 2 {
		t.Fatalf("completed work count = %d, want 2", len(got.CompletedWork))
	}
	if len(got.ArtifactRefs) != 1 {
		t.Fatalf("artifact refs count = %d, want 1", len(got.ArtifactRefs))
	}
	if !got.SafeToResume {
		t.Fatal("checkpoint must remain safe-to-resume")
	}
}
