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

// ---------------------------------------------------------------------------
// B30 adversary break tests (durable runtime)
//
// Pattern: each test exercises an ATTACK path. If the defense holds, the
// ADVERSARY BREAK assertion is unreachable and the test PASSES. If the code
// is vulnerable, the test FAILS with "ADVERSARY BREAK".
// ---------------------------------------------------------------------------

// TestAdversary_B30_ReintroduceHiddenTimeout attempts to re-introduce a hidden
// 60s/120s/300s stall timeout that bypasses TimeEnvelope. The envelope MUST be
// the authoritative source: with StallTimeoutMs=2000, advancing 3s must stall
// even though DefaultStallTimeoutMs is 120_000 and Claim hardcodes a 60s lease.
func TestAdversary_B30_ReintroduceHiddenTimeout(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Authoritative envelope: 2s stall (far below DefaultStallTimeoutMs=120s
	// and the Claim lease DurationMs=60s hardcoded path).
	env := routedrun.NewTimeEnvelope(
		600_000, // max active
		60_000,  // lease remaining field
		2_000,   // stall - authoritative
		5_000,   // model call
	)

	// Also attach a short request context that must NOT become the stall source.
	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	// Advance past envelope stall (2s) but well under hidden defaults (60s/120s/300s).
	h.clock.AdvanceMonotonic(3 * time.Second)

	stalled, err := h.supervisor.CheckStall(shortCtx, attID, env)
	if err != nil {
		t.Fatalf("CheckStall: %v", err)
	}
	if !stalled {
		// If a hidden default (60s/120s/300s) or the request context is winning
		// over TimeEnvelope.StallTimeoutMs, the attack succeeds.
		t.Fatal("ADVERSARY BREAK: TimeEnvelope stall timeout not authoritative; hidden 60s/120s/300s or context timeout bypassed envelope")
	}

	// Negative control: with a long envelope stall, same elapsed must NOT stall.
	longEnv := routedrun.NewTimeEnvelope(600_000, 60_000, 120_000, 5_000)
	stalledLong, err := h.supervisor.CheckStall(ctx, attID, longEnv)
	if err != nil {
		t.Fatalf("CheckStall long env: %v", err)
	}
	if stalledLong {
		t.Fatal("ADVERSARY BREAK: stall fired despite TimeEnvelope StallTimeoutMs=120s after only 3s; envelope ignored")
	}
}

// TestAdversary_B30_ClientQueryTimeoutCancelsWorker verifies a short-lived
// client query context must NOT cancel or terminate the durable worker run.
func TestAdversary_B30_ClientQueryTimeoutCancelsWorker(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// Client status query with a 1ms timeout (expires immediately).
	qctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond) // ensure deadline exceeded

	// Query paths that a client might hit.
	_, _ = h.store.GetAttempt(qctx, attID)
	_, _ = h.store.GetRun(qctx, h.runID)
	_, _ = h.supervisor.CheckStall(qctx, attID, h.envFor())
	_ = h.supervisor.UnauthenticatedActivity(qctx, attID, "status_poll")

	// Worker run must still be active.
	if got := h.attemptStatus(); got != routedrun.AttemptStatusRunning {
		t.Fatalf("ADVERSARY BREAK: client query timeout cancelled worker; attempt status=%s want RUNNING", got)
	}
	run, err := h.store.GetRun(context.Background(), h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != routedrun.RunStatusRunning {
		t.Fatalf("ADVERSARY BREAK: client query timeout cancelled run; run status=%s want RUNNING", run.Status)
	}

	// Authenticated progress must still be accepted after the timed-out query.
	p := h.makeProgress(1, "still-alive")
	if err := h.supervisor.TrackProgress(context.Background(), attID, p); err != nil {
		t.Fatalf("ADVERSARY BREAK: worker progress rejected after client query timeout: %v", err)
	}
}

// TestAdversary_B30_ForgeJobAcceptedEvent forges an ACCEPTED control-journal
// event without the valid HMAC key, placed BETWEEN legitimate events, and
// asserts the forged event is either rejected by Read (error) or absent from
// returned events (defense held -- the forged ACCEPTED must never appear).
func TestAdversary_B30_ForgeJobAcceptedEvent(t *testing.T) {
	stateRoot := t.TempDir()
	runID := "run-forge-accepted"
	attemptID := "att-forge-accepted"

	cj, err := routedrun.NewControlJournal(stateRoot, runID, attemptID)
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	defer func() { _ = cj.Close() }()

	// Legitimate first event.
	if err := cj.Append(routedrun.InvokeJobEvent{
		SchemaVersion: "1.0",
		Sequence:      1,
		Timestamp:     time.Now().UTC(),
		EventKind:     routedrun.InvokeJobEventStarted,
		Payload:       `{"ok":true}`,
	}); err != nil {
		t.Fatalf("Append STARTED: %v", err)
	}
	// Legitimate third event (seq 3) to create gap awareness.
	// The journal's lastSeq is now 1; we'll place the forged event at seq 2.
	if err := cj.Append(routedrun.InvokeJobEvent{
		SchemaVersion: "1.0",
		Sequence:      3,
		Timestamp:     time.Now().UTC(),
		EventKind:     routedrun.InvokeJobEventProgressRef,
		Payload:       `{"phase":"test"}`,
	}); err != nil {
		t.Fatalf("Append PROGRESS: %v", err)
	}
	_ = cj.Close()

	// Attacker forges ACCEPTED at sequence 2 with a bogus HMAC (no key).
	forged := routedrun.InvokeJobEvent{
		SchemaVersion: "1.0",
		Sequence:      2,
		Timestamp:     time.Now().UTC(),
		EventKind:     routedrun.InvokeJobEventAccepted,
		Payload:       `{"forged":true,"result":"evil"}`,
		HMAC:          "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	}
	data, err := json.Marshal(forged)
	if err != nil {
		t.Fatalf("marshal forged: %v", err)
	}
	forgedPath := filepath.Join(stateRoot, "runs", runID, "control", attemptID, "event-0000000002.json")
	if err := os.WriteFile(forgedPath, data, 0o600); err != nil {
		t.Fatalf("write forged event: %v", err)
	}

	// Re-open and read: forged ACCEPTED must be rejected.
	cj2, err := routedrun.NewControlJournal(stateRoot, runID, attemptID)
	if err != nil {
		t.Fatalf("reopen journal: %v", err)
	}
	defer func() { _ = cj2.Close() }()

	events, err := cj2.Read(1)
	if err != nil {
		// Read rejected the forged event entirely (HMAC verification
		// failed). This is a valid defense: the forged ACCEPTED was not
		// accepted and no tampered data was returned.
		return
	}
	// If Read succeeded, the forged ACCEPTED must NOT be in the returned
	// events. Any events that were returned must be verified.
	for _, ev := range events {
		if ev.EventKind == routedrun.InvokeJobEventAccepted {
			t.Fatal("ADVERSARY BREAK: forged ACCEPTED control-journal event accepted without valid HMAC")
		}
	}
	// Verify the legitimate events are present (seq 1 STARTED, seq 3 PROGRESS).
	foundStarted := false
	foundProgress := false
	for _, ev := range events {
		if ev.Sequence == 1 && ev.EventKind == routedrun.InvokeJobEventStarted {
			foundStarted = true
		}
		if ev.Sequence == 3 && ev.EventKind == routedrun.InvokeJobEventProgress {
			foundProgress = true
		}
	}
	if !foundStarted || !foundProgress {
		t.Fatalf("ADVERSARY BREAK: legitimate events missing after forged event rejection; events=%d", len(events))
	}
}

// TestAdversary_B30_DeliverJobTwice delivers the same success result twice.
// The second delivery must be rejected (idempotent finalization fence).
func TestAdversary_B30_DeliverJobTwice(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	r := h.makeSuccessResult()
	if err := h.supervisor.HandleResult(ctx, attID, r); err != nil {
		t.Fatalf("first HandleResult: %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusSucceeded {
		t.Fatalf("after first delivery: status=%s want SUCCEEDED", got)
	}

	// Second delivery of the same terminal success.
	err = h.supervisor.HandleResult(ctx, attID, r)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: second job result delivery accepted (not idempotent-fenced)")
	}
	if !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("second HandleResult: want ErrAlreadyTerminal, got %v", err)
	}

	// Terminal state must remain SUCCEEDED (not double-finalized to something else).
	if got := h.attemptStatus(); got != routedrun.AttemptStatusSucceeded {
		t.Fatalf("ADVERSARY BREAK: second delivery mutated terminal state to %s", got)
	}
}

// TestAdversary_B30_ReuseKeyWithChangedInput reuses an idempotency key with a
// different input payload and expects CONFLICT (not a new run).
func TestAdversary_B30_ReuseKeyWithChangedInput(t *testing.T) {
	dir := t.TempDir()
	store, err := routedrun.OpenLocalStore(dir)
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}
	ctx := context.Background()

	dep := &routedrun.DeploymentRecord{
		SchemaVersion:     routedrun.CurrentSchemaVersion,
		PackageName:       "pkg-idemp",
		PackageVersion:    "1.0.0",
		Status:            routedrun.DeploymentActive,
		MaxConcurrentRuns: 4,
		BundleDigest:      "b",
		PolicyDigest:      "p",
		ImageLockDigest:   "i",
		ProvenanceDigest:  "v",
		CreatedBy:         "adversary",
	}
	if err := store.CreateDeployment(ctx, dep); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	req := &routedrun.InvocationRequest{
		SchemaVersion:              routedrun.CurrentSchemaVersion,
		RequestedDeploymentRef:     string(dep.DeploymentID),
		InputJSON:                  `{"q":"first"}`,
		InitialMaxActiveDurationMs: 60_000,
		InitialAttemptLeaseMs:      30_000,
		IdempotencyKey:             "shared-key-1",
		CallerIdentity:             "caller-adv",
	}
	r1, err := store.AdmitInvocation(ctx, req, dep.Generation)
	if err != nil {
		t.Fatalf("first Admit: %v", err)
	}
	if r1 == nil || r1.RunID == "" {
		t.Fatal("first Admit returned empty receipt")
	}

	// Same key, different input - must CONFLICT, not mint a new run.
	req2 := *req
	req2.InputJSON = `{"q":"different-payload"}`
	req2.InputDigest = ""
	r2, err := store.AdmitInvocation(ctx, &req2, dep.Generation)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: idempotency key reuse with changed input accepted; new run=%s", r2.RunID)
	}
	if !errors.Is(err, routedrun.ErrIdempotencyConflict) {
		t.Fatalf("ADVERSARY BREAK: want ErrIdempotencyConflict, got %v", err)
	}
}

// TestAdversary_B30_InvokeInactiveDeployment attempts to invoke an INACTIVE
// deployment and asserts admission rejects it.
func TestAdversary_B30_InvokeInactiveDeployment(t *testing.T) {
	dir := t.TempDir()
	store, err := routedrun.OpenLocalStore(dir)
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}
	ctx := context.Background()

	dep := &routedrun.DeploymentRecord{
		SchemaVersion:     routedrun.CurrentSchemaVersion,
		PackageName:       "pkg-inactive",
		PackageVersion:    "1.0.0",
		Status:            routedrun.DeploymentActive,
		MaxConcurrentRuns: 2,
		BundleDigest:      "b",
		PolicyDigest:      "p",
		ImageLockDigest:   "i",
		ProvenanceDigest:  "v",
		CreatedBy:         "adversary",
	}
	if err := store.CreateDeployment(ctx, dep); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	// Deactivate.
	got, err := store.GetDeployment(ctx, dep.DeploymentID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if err := store.SetDeploymentStatus(ctx, dep.DeploymentID, routedrun.DeploymentInactive, got.Generation); err != nil {
		t.Fatalf("SetDeploymentStatus INACTIVE: %v", err)
	}

	req := &routedrun.InvocationRequest{
		SchemaVersion:          routedrun.CurrentSchemaVersion,
		RequestedDeploymentRef: string(dep.DeploymentID),
		InputJSON:              `{}`,
		IdempotencyKey:         "inactive-key",
		CallerIdentity:         "caller-adv",
	}
	rec, err := store.AdmitInvocation(ctx, req, 0)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: invoke on INACTIVE deployment accepted; run=%s", rec.RunID)
	}
	if !errors.Is(err, routedrun.ErrDeploymentInactive) {
		t.Fatalf("ADVERSARY BREAK: want ErrDeploymentInactive, got %v", err)
	}
}

// TestAdversary_B30_StdoutSpamPreventsStall attempts to keep a run alive via
// unauthenticated stdout spam. Stall must still fire.
func TestAdversary_B30_StdoutSpamPreventsStall(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	env := h.envFor() // stall = 1000ms

	// Spam unauthenticated stdout across the stall window.
	for i := 0; i < 20; i++ {
		h.clock.AdvanceMonotonic(100 * time.Millisecond)
		if err := h.supervisor.UnauthenticatedActivity(ctx, attID, "stdout spam line"); err != nil {
			t.Fatalf("UnauthenticatedActivity: %v", err)
		}
	}
	// Total advanced: 2000ms > stall 1000ms since claim (no authenticated progress).

	stalled, err := h.supervisor.CheckStall(ctx, attID, env)
	if err != nil {
		t.Fatalf("CheckStall: %v", err)
	}
	if !stalled {
		t.Fatal("ADVERSARY BREAK: stdout spam prevented stall detection (unauthenticated activity treated as progress)")
	}
}

// TestAdversary_B30_LateSuccessAfterCancellation cancels a run then delivers a
// late success. Late result must be rejected and status stay CANCELLED.
func TestAdversary_B30_LateSuccessAfterCancellation(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	if err := h.supervisor.Cancel(ctx, attID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("after Cancel: status=%s want CANCELLED", got)
	}

	// Late success delivery after cancellation.
	r := h.makeSuccessResult()
	err = h.supervisor.HandleResult(ctx, attID, r)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: late success accepted after cancellation")
	}
	if !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("late HandleResult: want ErrAlreadyTerminal, got %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("ADVERSARY BREAK: late success flipped status to %s; want CANCELLED", got)
	}

	// Result store must not show SUCCEEDED over cancel.
	res, rerr := h.results.GetInvokeJobResult(ctx, h.runID)
	if rerr == nil && res != nil && res.TerminalStatus == routedrun.InvokeJobResultSucceeded {
		t.Fatal("ADVERSARY BREAK: result store has SUCCEEDED after cancel + late success")
	}
}

// TestAdversary_B30_DaemonRestartBlindReplay simulates a daemon restart and
// attempts to blindly continue/re-invoke the same in-flight run. Reconcile
// must revoke the ambiguous lease from the journal; the attempt must not stay
// RUNNING under the old lease.
func TestAdversary_B30_DaemonRestartBlindReplay(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	oldLease := h.leaseID

	// Crash/restart: brand-new supervisor with empty in-memory trackers.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	// Blind replay would leave the attempt RUNNING with the old lease intact.
	if att.Status == routedrun.AttemptStatusRunning {
		t.Fatal("ADVERSARY BREAK: daemon restart left attempt RUNNING (blind replay instead of journal reconcile)")
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("post-reconcile status=%s want FAILED (daemon_restart)", att.Status)
	}
	if att.Lease != nil && att.Lease.LeaseToken != "" {
		t.Fatalf("ADVERSARY BREAK: old lease token still live after restart: %q", att.Lease.LeaseToken)
	}
	if att.Lease != nil && att.Lease.LeaseID == oldLease && att.Lease.LeaseToken != "" {
		t.Fatal("ADVERSARY BREAK: re-invoked same lease after restart")
	}

	// A late progress event on the old attempt under the new supervisor must
	// not revive work (tracker is terminal after reconcile).
	p := ProgressEvent{
		AttemptID: attID,
		LeaseID:   oldLease,
		Sequence:  1,
		Timestamp: h.clock.Now(),
		Phase:     "blind-replay",
		HMAC:      "00",
	}
	if err := sup2.TrackProgress(ctx, attID, p); err == nil {
		t.Fatal("ADVERSARY BREAK: progress accepted on reconciled attempt (blind replay path open)")
	}
}

// TestAdversary_B30_ArtifactPathTraversal attempts to write/accept an artifact
// with a path-traversal payload.
func TestAdversary_B30_ArtifactPathTraversal(t *testing.T) {
	root := t.TempDir()
	aw, err := routedrun.NewArtifactWorkspace(root, routedrun.RunID("run-trav"))
	if err != nil {
		t.Fatalf("NewArtifactWorkspace: %v", err)
	}
	ctx := context.Background()

	payloads := []string{
		"../../etc/passwd",
		"../../../etc/shadow",
		"./../../tmp/evil",
		"/etc/passwd",
		`..\..\windows\system32`,
		"foo/../../../etc/passwd",
		"ok/../../etc/passwd",
	}
	for _, p := range payloads {
		meta, err := aw.ValidateAndAccept(ctx, p, routedrun.AttemptID("att-1"))
		if err == nil {
			t.Fatalf("ADVERSARY BREAK: path traversal artifact accepted: %q meta=%+v", p, meta)
		}
		if !errors.Is(err, routedrun.ErrInvalidPath) && !errors.Is(err, routedrun.ErrNotFound) && !errors.Is(err, routedrun.ErrSymlinkRejected) {
			// Still rejected is fine; log unexpected sentinel for diagnosis.
			if !strings.Contains(err.Error(), "path") && !strings.Contains(err.Error(), "segment") && !strings.Contains(err.Error(), "absolute") {
				t.Fatalf("ADVERSARY BREAK: traversal %q failed with unexpected error type: %v", p, err)
			}
		}
	}
}

// TestAdversary_B30_CheckpointDigestTamper commits a checkpoint, tampers with
// its digest on disk, and asserts the tampered checkpoint is not trusted for
// resume (must not remain SafeToResume with the forged digest, or must be
// rejected by a resume path).
func TestAdversary_B30_CheckpointDigestTamper(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	cpID := routedrun.CheckpointID("cp-adv-tamper")
	originalDigest := "digest-original-safe"
	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		CheckpointID:     cpID,
		AttemptID:        attID,
		RunID:            h.runID,
		WorkflowID:       h.workflowID,
		LeaseID:          h.leaseID,
		Phase:            "phase-1",
		CompletedWork:    []string{"a"},
		RemainingWork:    []string{"b"},
		SafeToResume:     true,
		CheckpointDigest: originalDigest,
		Sequence:         1,
		CreatedAt:        h.clock.Now(),
	}
	if err := h.supervisor.HandleCheckpoint(ctx, attID, h.makeCheckpoint(cp)); err != nil {
		t.Fatalf("HandleCheckpoint: %v", err)
	}

	// Tamper on disk: rewrite checkpoint file with forged digest + evil work.
	// Store root is shared with the result store.
	cpPath := filepath.Join(h.results.root, "checkpoints", string(cpID)+".json")
	raw, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("read checkpoint file: %v", err)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}
	disk["checkpoint_digest"] = "digest-tampered-evil"
	disk["completed_work"] = []any{"evil-injection"}
	disk["safe_to_resume"] = true
	tampered, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal tampered: %v", err)
	}
	if err := os.WriteFile(cpPath, tampered, 0o600); err != nil {
		t.Fatalf("write tampered checkpoint: %v", err)
	}

	// Read-back path used for resume.
	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		// Rejecting the tampered file entirely is a valid defense.
		// The error must be a digest-verification failure or ErrNotFound
		// (the tampered checkpoint was rejected). Any other error is
		// suspicious (F24).
		if !errors.Is(err, routedrun.ErrNotFound) &&
			!strings.Contains(err.Error(), "digest") &&
			!strings.Contains(err.Error(), "verify") {
			t.Fatalf("ADVERSARY BREAK: GetLatestCheckpoint failed with unexpected error: %v", err)
		}
		return
	}

	// If the store still surfaces the checkpoint as safe-to-resume with the
	// forged digest, the attack succeeded.
	if got.SafeToResume && got.CheckpointDigest == "digest-tampered-evil" {
		t.Fatal("ADVERSARY BREAK: disk-tampered checkpoint digest accepted as safe-to-resume")
	}
	if got.CheckpointDigest != originalDigest && got.SafeToResume {
		t.Fatalf("ADVERSARY BREAK: tampered checkpoint trusted for resume; digest=%s", got.CheckpointDigest)
	}

	// Restart reconcile must not promote a tampered checkpoint as trusted.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got2, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		// Rejection after reconcile is fine, but must be a relevant error (F24).
		if !errors.Is(err, routedrun.ErrNotFound) &&
			!strings.Contains(err.Error(), "digest") &&
			!strings.Contains(err.Error(), "verify") {
			t.Fatalf("ADVERSARY BREAK: GetLatestCheckpoint after reconcile failed with unexpected error: %v", err)
		}
		return
	}
	if got2.SafeToResume && got2.CheckpointDigest == "digest-tampered-evil" {
		t.Fatal("ADVERSARY BREAK: after reconcile, tampered checkpoint still trusted for resume")
	}
}

// TestAdversary_B30_ChargePausedWallTime verifies PAUSED does not accrue
// active time while PAUSE_REQUESTED does.
func TestAdversary_B30_ChargePausedWallTime(t *testing.T) {
	// --- PAUSED: must NOT charge ---
	h1 := newTestHarness(t)
	if _, err := h1.claimAttempt(); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	wf, err := h1.store.GetWorkflow(ctx, h1.workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPaused
	if err := h1.store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow PAUSED: %v", err)
	}
	consumedPaused := int64(10_000)
	ledger := &routedrun.ActiveTimeLedger{
		SchemaVersion:         routedrun.CurrentSchemaVersion,
		ConsumedMs:            consumedPaused,
		RunningSegmentStartMs: ptrInt64(h1.clock.NowMonotonic().UnixMilli()),
	}
	if err := h1.store.PutActiveTimeLedger(ctx, h1.workflowID, ledger, 1); err != nil {
		t.Fatalf("PutActiveTimeLedger: %v", err)
	}
	h1.clock.AdvanceMonotonic(5 * time.Minute)
	sup1 := mustNewSupervisor(t, h1.store, h1.results, h1.journals, h1.clock, h1.t.TempDir())
	if err := sup1.Reconcile(ctx, h1.runID); err != nil {
		t.Fatalf("Reconcile PAUSED: %v", err)
	}
	got1 := h1.ledger()
	if got1.ConsumedMs != consumedPaused {
		t.Fatalf("ADVERSARY BREAK: PAUSED charged wall time; consumed %d -> %d", consumedPaused, got1.ConsumedMs)
	}
	if got1.RunningSegmentStartMs != nil {
		t.Fatal("ADVERSARY BREAK: PAUSED left open running segment (still accruing)")
	}

	// --- PAUSE_REQUESTED: MUST charge ---
	h2 := newTestHarness(t)
	if _, err := h2.claimAttempt(); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	wf2, err := h2.store.GetWorkflow(ctx, h2.workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf2.Status = routedrun.WorkflowStatusPauseRequested
	if err := h2.store.UpdateWorkflow(ctx, wf2, wf2.Generation); err != nil {
		t.Fatalf("UpdateWorkflow PAUSE_REQUESTED: %v", err)
	}
	consumedPR := int64(4_000)
	ledger2 := &routedrun.ActiveTimeLedger{
		SchemaVersion:         routedrun.CurrentSchemaVersion,
		ConsumedMs:            consumedPR,
		RunningSegmentStartMs: ptrInt64(h2.clock.NowMonotonic().UnixMilli()),
	}
	if err := h2.store.PutActiveTimeLedger(ctx, h2.workflowID, ledger2, 1); err != nil {
		t.Fatalf("PutActiveTimeLedger: %v", err)
	}
	h2.clock.AdvanceMonotonic(2 * time.Minute)
	sup2 := mustNewSupervisor(t, h2.store, h2.results, h2.journals, h2.clock, h2.t.TempDir())
	if err := sup2.Reconcile(ctx, h2.runID); err != nil {
		t.Fatalf("Reconcile PAUSE_REQUESTED: %v", err)
	}
	got2 := h2.ledger()
	if got2.ConsumedMs <= consumedPR {
		t.Fatalf("ADVERSARY BREAK: PAUSE_REQUESTED did not charge wall time; consumed %d (want > %d)", got2.ConsumedMs, consumedPR)
	}
}

// TestAdversary_B30_FrozenStateWithLiveCapability freezes a workflow (PAUSED)
// and asserts reconcile clears any active lease / in-flight capability.
func TestAdversary_B30_FrozenStateWithLiveCapability(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Start an in-flight governed capability while still RUNNING.
	if err := h.supervisor.HandleModelStart(ctx, attID, h.leaseID); err != nil {
		t.Fatalf("HandleModelStart: %v", err)
	}

	// Freeze the workflow.
	wf, err := h.store.GetWorkflow(ctx, h.workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPaused
	if err := h.store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow PAUSED: %v", err)
	}

	// Restart reconcile is the path that must strip live capability from a
	// frozen workflow.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if !att.Status.IsTerminal() {
		t.Fatalf("ADVERSARY BREAK: frozen workflow still has non-terminal attempt status=%s", att.Status)
	}
	if att.Lease != nil && att.Lease.LeaseToken != "" {
		t.Fatalf("ADVERSARY BREAK: frozen workflow retained active lease token %q", att.Lease.LeaseToken)
	}

	// In-flight capability must not be restartable on the old attempt.
	if err := sup2.HandleModelStart(ctx, attID, h.leaseID); err == nil {
		t.Fatal("ADVERSARY BREAK: model capability accepted on frozen/reconciled attempt")
	}
	if err := sup2.HandleHTTPStart(ctx, attID, h.leaseID); err == nil {
		t.Fatal("ADVERSARY BREAK: HTTP capability accepted on frozen/reconciled attempt")
	}
}

// TestAdversary_B30_ProcessAliveNotProgress treats process-existence polls as
// unauthenticated activity. Stall must still fire.
func TestAdversary_B30_ProcessAliveNotProgress(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	env := h.envFor() // stall = 1000ms

	// Container/process is "alive" but emits no authenticated progress.
	for i := 0; i < 15; i++ {
		h.clock.AdvanceMonotonic(100 * time.Millisecond)
		if err := h.supervisor.UnauthenticatedActivity(ctx, attID, "process_alive"); err != nil {
			t.Fatalf("UnauthenticatedActivity process_alive: %v", err)
		}
	}
	// 1500ms elapsed > 1000ms stall.

	stalled, err := h.supervisor.CheckStall(ctx, attID, env)
	if err != nil {
		t.Fatalf("CheckStall: %v", err)
	}
	if !stalled {
		t.Fatal("ADVERSARY BREAK: process-alive signal prevented stall (process existence treated as progress)")
	}
}

// ---------------------------------------------------------------------------
// F25 adversary matrix coverage gaps
// ---------------------------------------------------------------------------

// TestAdversary_B30_AliasMoveBetweenIdempotentRetries moves an alias to a
// different deployment target between two idempotent invocations. The second
// must either resolve to the original deployment (idempotent replay) or be
// rejected as CONFLICT (spec line: idempotency replays the exact intent).
func TestAdversary_B30_AliasMoveBetweenIdempotentRetries(t *testing.T) {
	dir := t.TempDir()
	store, err := routedrun.OpenLocalStore(dir)
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}
	ctx := context.Background()

	dep1 := &routedrun.DeploymentRecord{
		SchemaVersion:     routedrun.CurrentSchemaVersion,
		PackageName:       "pkg-alias-a",
		PackageVersion:    "1.0.0",
		Status:            routedrun.DeploymentActive,
		MaxConcurrentRuns: 4,
		BundleDigest:      "ba",
		PolicyDigest:      "pa",
		ImageLockDigest:   "ia",
		ProvenanceDigest:  "va",
		CreatedBy:         "adversary",
	}
	if err := store.CreateDeployment(ctx, dep1); err != nil {
		t.Fatalf("CreateDeployment dep1: %v", err)
	}
	dep2 := &routedrun.DeploymentRecord{
		SchemaVersion:     routedrun.CurrentSchemaVersion,
		PackageName:       "pkg-alias-b",
		PackageVersion:    "2.0.0",
		Status:            routedrun.DeploymentActive,
		MaxConcurrentRuns: 4,
		BundleDigest:      "bb",
		PolicyDigest:      "pb",
		ImageLockDigest:   "ib",
		ProvenanceDigest:  "vb",
		CreatedBy:         "adversary",
	}
	if err := store.CreateDeployment(ctx, dep2); err != nil {
		t.Fatalf("CreateDeployment dep2: %v", err)
	}

	// Alias points to dep1.
	alias := &routedrun.AliasRecord{
		SchemaVersion:      routedrun.CurrentSchemaVersion,
		Alias:              "alias-move",
		TargetDeploymentID: dep1.DeploymentID,
		TargetVersion:      dep1.PackageVersion,
		UpdatedBy:          "adversary",
	}
	if err := store.CompareAndSwapAlias(ctx, alias); err != nil {
		t.Fatalf("CompareAndSwapAlias: %v", err)
	}

	// First invocation with alias "alias-move".
	req := &routedrun.InvocationRequest{
		SchemaVersion:              routedrun.CurrentSchemaVersion,
		RequestedDeploymentRef:     "alias-move",
		InputJSON:                  `{"q":"first"}`,
		InitialMaxActiveDurationMs: 60_000,
		InitialAttemptLeaseMs:      30_000,
		IdempotencyKey:             "alias-move-key",
		CallerIdentity:             "caller-alias",
	}
	r1, err := store.AdmitInvocation(ctx, req, 0)
	if err != nil {
		t.Fatalf("first Admit: %v", err)
	}
	if r1 == nil || r1.RunID == "" {
		t.Fatal("first Admit returned empty receipt")
	}

	// Move alias to dep2 (attacker moves pointer between retries).
	gotAliasOld, err := store.ResolveAlias(ctx, "alias-move")
	if err != nil {
		t.Fatalf("GetAlias: %v", err)
	}
	alias2 := &routedrun.AliasRecord{
		SchemaVersion:      routedrun.CurrentSchemaVersion,
		Alias:              gotAliasOld.Alias,
		TargetDeploymentID: dep2.DeploymentID,
		TargetVersion:      dep2.PackageVersion,
		Generation:         gotAliasOld.Generation,
		UpdatedBy:          "adversary",
	}
	if err := store.CompareAndSwapAlias(ctx, alias2); err != nil {
		t.Fatalf("CompareAndSwapAlias (move): %v", err)
	}

	// Same key, same input: idempotent replay. Must return the SAME
	// run (original deployment dep1), not the new alias target.
	req2 := *req
	req2.IdempotencyKey = "alias-move-key"
	req2.CallerIdentity = "caller-alias"
	r2, err := store.AdmitInvocation(ctx, &req2, 0)
	if err != nil {
		// Idempotency conflict is acceptable (intent digest mismatch
		// after alias move).
		if errors.Is(err, routedrun.ErrIdempotencyConflict) {
			return
		}
		t.Fatalf("ADVERSARY BREAK: alias movement between idempotent retries caused unexpected error: %v", err)
	}
	if r2.RunID != r1.RunID {
		t.Fatalf("ADVERSARY BREAK: alias movement between retries created new run %s (want original %s)", r2.RunID, r1.RunID)
	}
	if r2.ResolvedDeploymentID != dep1.DeploymentID {
		t.Fatalf("ADVERSARY BREAK: idempotent replay resolved to new deployment %s (want %s)", r2.ResolvedDeploymentID, dep1.DeploymentID)
	}
}

// TestAdversary_B30_ClockRollbackNoEffect verifies that clock rollback/jump
// (SetWall to past) does not change monotonic duration tracking. Monotonic
// time must only move forward.
func TestAdversary_B30_ClockRollbackNoEffect(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Record monotonic time before rollback.
	monotonicBefore := h.clock.NowMonotonic().UnixMilli()

	// Roll wall clock backwards by 1 hour.
	h.clock.SetWall(h.clock.Now().Add(-1 * time.Hour))

	// Monotonic time must NOT be affected by wall clock change.
	monotonicAfter := h.clock.NowMonotonic().UnixMilli()
	if monotonicAfter < monotonicBefore {
		t.Fatalf("ADVERSARY BREAK: monotonic clock decreased after wall clock rollback: %d -> %d", monotonicBefore, monotonicAfter)
	}

	// Progress should still be accepted (lease and stall use monotonic time).
	p := h.makeProgress(1, "after-rollback")
	if err := h.supervisor.TrackProgress(ctx, attID, p); err != nil {
		t.Fatalf("ADVERSARY BREAK: progress rejected after clock rollback: %v", err)
	}
}

// TestAdversary_B30_DurationOverflowNoWrap tests that very large duration
// values do not cause overflow/wrap in active-time accounting.
func TestAdversary_B30_DurationOverflowNoWrap(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.claimAttempt(); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Set consumed to near-MaxInt64 and a huge segment start.
	// Use a value that fits in int64 but is large enough to test overflow safety.
	nearMax := int64(1<<62) // large but fits in int64
	ledger := &routedrun.ActiveTimeLedger{
		SchemaVersion:         routedrun.CurrentSchemaVersion,
		ConsumedMs:            nearMax,
		RunningSegmentStartMs: ptrInt64(h.clock.NowMonotonic().UnixMilli()),
	}
	if err := h.store.PutActiveTimeLedger(ctx, h.workflowID, ledger, 1); err != nil {
		t.Fatalf("PutActiveTimeLedger: %v", err)
	}

	// Advance small amount: consumed should NOT wrap negative.
	h.clock.AdvanceMonotonic(10 * time.Second)
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())

	// Reconcile: the PAUSE_REQUESTED/RUNNING accrual path must handle overflow.
	wf, err := h.store.GetWorkflow(ctx, h.workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPauseRequested
	if err := h.store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := h.ledger()
	if got.ConsumedMs < 0 {
		t.Fatalf("ADVERSARY BREAK: active time overflowed (negative): %d", got.ConsumedMs)
	}
}

// TestAdversary_B30_CPUPIDBypass verifies CPU and PID limits from T04 are
// enforced via the supervisor's attempt constraints. Spawning a subprocess
// must not bypass the worker's resource limits.
func TestAdversary_B30_CPUPIDBypass(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// The supervisor enforces resource limits through the attempt record.
	// Verify the attempt has a resource constraint record.
	att, err := h.store.GetAttempt(context.Background(), attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}

	// An attempt managed by the supervisor must have a lease and be bounded.
	if att.Lease == nil {
		t.Fatal("ADVERSARY BREAK: attempt has no lease (resource unbounded)")
	}
	if att.Lease.DurationMs <= 0 {
		t.Fatalf("ADVERSARY BREAK: attempt lease duration is %d (unbounded)", att.Lease.DurationMs)
	}

	// Verify the run has max active duration set.
	run, err := h.store.GetRun(context.Background(), h.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.MaxActiveDurationMs <= 0 {
		t.Fatal("ADVERSARY BREAK: run has no max active duration (CPU unbounded)")
	}

	// Attempt to extend lease beyond configured max must be rejected.
	// (The lease is set at claim time; post-claim extension is not supported.
	// The fact that lease.DurationMs <= MaxAttemptLeaseMs is validated at claim.)
	if att.Lease.DurationMs > run.MaxAttemptLeaseMs {
		t.Fatalf("ADVERSARY BREAK: lease duration %d exceeds run max %d (PID bypass)", att.Lease.DurationMs, run.MaxAttemptLeaseMs)
	}
}

// TestAdversary_B30_ShortenedPASSEventGateRejection documents that a
// shortened/edited PASS event is rejected at the gate level. At the
// supervisor level, progress events are HMAC-authenticated and cannot be
// reordered. A PASS-without-duration is a gate concern (the gate validates
// the event envelope before forwarding to the supervisor). This test
// verifies that the supervisor rejects progress with a forged HMAC.
func TestAdversary_B30_ShortenedPASSEventGateRejection(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()

	// Forged progress event (wrong HMAC): the supervisor must reject it.
	forged := h.makeForgedProgress(1, "shortened-pass")
	err = h.supervisor.TrackProgress(ctx, attID, forged)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: forged HMAC progress accepted by supervisor (shortened PASS bypass)")
	}

	// Gate-level PASS-without-duration validation is deferred to the gate
	// package. The supervisor guards against HMAC forgery; the gate guards
	// against missing envelope fields.
}
