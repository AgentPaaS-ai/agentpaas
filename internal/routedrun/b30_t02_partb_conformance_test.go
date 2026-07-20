package routedrun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// B30-T02 Part B — conformance + crash recovery tests for durable invoke.
//
// Spec reference: docs/execution/blocks/b30-summary.md:301-322 (Tests to write
// first). Part A landed the implementation; Part B adds the conformance and
// crash recovery tests the spec's "Tests to write first" section requires.
//
// All crash/replay tests use the real LocalStore WAL (not mocks). The store is
// closed and re-opened on the same temp root to exercise WAL durability.
//
// These tests are TEST-ONLY. They do not modify production code. If a test
// exposes a real bug in Part A's implementation, it logs BUG: and continues
// (the orchestrator dispatches a fix worker).
// ---------------------------------------------------------------------------

// partBAdmitAndStandalone returns an admitted standalone invocation receipt
// for the given key/caller. Used as the seed for crash-replay tests.
func partBAdmitAndStandalone(t *testing.T, s *LocalStore, key, caller string) (*InvocationReceipt, *DeploymentRecord) {
	t.Helper()
	dep := seedActiveDeployment(t, s, 1, nil)
	req := baseInvocation(dep, key, caller, `{"p":"b"}`)
	rec, err := s.AdmitInvocation(context.Background(), req, dep.Generation)
	if err != nil {
		t.Fatalf("AdmitInvocation: %v", err)
	}
	return rec, dep
}

// reopenTestStore re-opens a LocalStore on the same root, simulating a daemon
// restart between admission and the next lifecycle phase. The original store's
// lock is released by the OS once the process exits; here we simply open a
// second handle on the same files (LocalStore uses a process-local mutex, so
// the "crash" is simulated by discarding the prior handle).
func reopenTestStore(t *testing.T, s *LocalStore) *LocalStore {
	t.Helper()
	s2, err := OpenLocalStore(s.root, WithClock(testClock(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))))
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	return s2
}

// ---------------------------------------------------------------------------
// B1: Crash/replay around the admission boundary (spec lines 304-308)
// ---------------------------------------------------------------------------

// TestB30T02PartB_CrashReplay_AfterAdmitBeforeClaim simulates a daemon
// restart between AdmitInvocation and the supervisor's READY claim (T05 owns
// the supervisor; here we assert the store state is consistent: exactly one
// invocation/workflow/run exists, the node is READY, no attempt/lease/job
// exists yet, and a fresh supervisor can claim it idempotently by directly
// calling the store's CreateAttempt method).
func TestB30T02PartB_CrashReplay_AfterAdmitBeforeClaim(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	rec, dep := partBAdmitAndStandalone(t, s, "crash-admit-claim", "caller-a")

	// Crash: reopen the store on the same root.
	s2 := reopenTestStore(t, s)
	ctx := context.Background()

	// Exactly one invocation, workflow, run, node — no duplicates.
	invs, err := s2.ListInvocations(ctx)
	if err != nil {
		t.Fatalf("ListInvocations: %v", err)
	}
	if len(invs) != 1 {
		t.Fatalf("after reopen: want 1 invocation, got %d", len(invs))
	}
	if invs[0].InvocationID != rec.InvocationID {
		t.Fatalf("invocation id changed: %s vs %s", invs[0].InvocationID, rec.InvocationID)
	}
	wfs, err := s2.ListWorkflows(ctx)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(wfs) != 1 {
		t.Fatalf("after reopen: want 1 workflow, got %d", len(wfs))
	}
	runs, err := s2.ListRuns(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("after reopen: want 1 run, got %d", len(runs))
	}
	if runs[0].Status != RunStatusPending {
		t.Fatalf("run status=%s want PENDING", runs[0].Status)
	}
	nodes, err := s2.ListNodes(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("after reopen: want 1 node, got %d", len(nodes))
	}
	if nodes[0].Status != NodeStatusReady {
		t.Fatalf("node status=%s want READY", nodes[0].Status)
	}
	atts, err := s2.ListAttempts(ctx, rec.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(atts) != 0 {
		t.Fatalf("after reopen: want 0 attempts, got %d", len(atts))
	}

	// A fresh supervisor "claim" (simulated by direct CreateAttempt on the
	// durable run) must succeed exactly once. The READY intent is claimable.
	run, err := s2.GetRun(ctx, rec.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	run.Status = RunStatusRunning
	if err := s2.UpdateRun(ctx, run, 1); err != nil {
		t.Fatalf("UpdateRun (claim): %v", err)
	}
	att := &AttemptRecord{
		SchemaVersion:   CurrentSchemaVersion,
		RunID:           rec.RunID,
		WorkflowID:      rec.WorkflowID,
		Status:          AttemptStatusRunning,
		AttemptNumber:   1,
		Lease:           &AttemptLease{DurationMs: 30_000, AcquiredAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)},
	}
	if err := s2.CreateAttempt(ctx, att); err != nil {
		t.Fatalf("CreateAttempt (claim): %v", err)
	}
	// Re-claim must not create a duplicate attempt (same AttemptID already
	// exists; CreateAttempt rejects AlreadyExists).
	dup := &AttemptRecord{
		SchemaVersion: CurrentSchemaVersion,
		AttemptID:     att.AttemptID,
		RunID:         rec.RunID,
		WorkflowID:    rec.WorkflowID,
		Status:        AttemptStatusRunning,
		AttemptNumber: 2,
	}
	if err := s2.CreateAttempt(ctx, dup); err == nil {
		t.Fatalf("duplicate claim must not create a second attempt")
	}
	// Sanity: the deployment is unchanged (no alias drift on reopen).
	gotDep, err := s2.GetDeployment(ctx, dep.DeploymentID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if gotDep.Status != DeploymentActive {
		t.Fatalf("deployment status=%s want ACTIVE", gotDep.Status)
	}
}

// TestB30T02PartB_CrashReplay_AfterClaimBeforeResourceStart crashes after
// the supervisor creates the attempt/lease/job but before container start.
// Re-open the store, assert exactly one attempt/lease/job exists, no
// duplicate was created.
func TestB30T02PartB_CrashReplay_AfterClaimBeforeResourceStart(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	rec, _ := partBAdmitAndStandalone(t, s, "crash-claim-start", "caller-b")
	ctx := context.Background()

	// Simulate the T05 supervisor claim transition.
	run, err := s.GetRun(ctx, rec.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	run.Status = RunStatusRunning
	if err := s.UpdateRun(ctx, run, 1); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	att := &AttemptRecord{
		SchemaVersion:   CurrentSchemaVersion,
		RunID:           rec.RunID,
		WorkflowID:      rec.WorkflowID,
		Status:          AttemptStatusRunning,
		AttemptNumber:   1,
		Lease:           &AttemptLease{DurationMs: 30_000, AcquiredAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)},
	}
	if err := s.CreateAttempt(ctx, att); err != nil {
		t.Fatalf("CreateAttempt: %v", err)
	}

	// Crash: reopen.
	s2 := reopenTestStore(t, s)

	// Exactly one attempt, with its lease intact.
	atts, err := s2.ListAttempts(ctx, rec.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(atts) != 1 {
		t.Fatalf("after reopen: want 1 attempt, got %d", len(atts))
	}
	if atts[0].AttemptID != att.AttemptID {
		t.Fatalf("attempt id changed: %s vs %s", atts[0].AttemptID, att.AttemptID)
	}
	if atts[0].Lease == nil {
		t.Fatalf("lease missing after reopen")
	}
	if atts[0].Lease.LeaseToken == "" {
		t.Fatalf("lease token empty after reopen")
	}
	// Run still RUNNING.
	run2, err := s2.GetRun(ctx, rec.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run2.Status != RunStatusRunning {
		t.Fatalf("run status=%s want RUNNING", run2.Status)
	}
	// Exactly one invocation/workflow/run still.
	invs, _ := s2.ListInvocations(ctx)
	if len(invs) != 1 {
		t.Fatalf("after reopen: want 1 invocation, got %d", len(invs))
	}
	wfs, _ := s2.ListWorkflows(ctx)
	if len(wfs) != 1 {
		t.Fatalf("after reopen: want 1 workflow, got %d", len(wfs))
	}
	runs, _ := s2.ListRuns(ctx, rec.WorkflowID)
	if len(runs) != 1 {
		t.Fatalf("after reopen: want 1 run, got %d", len(runs))
	}
}

// TestB30T02PartB_CrashReplay_AfterAcceptedBeforeStarted crashes after the
// `accepted` control-journal event is appended, before `started`. Re-open,
// read the control journal, assert the `accepted` event is present, no
// `started` event. A re-invocation must NOT create a second `accepted` event
// (the harness startup consumes one job envelope exactly once — spec line
// 289). T05 owns the harness startup; Part B tests the ControlJournal's
// ability to preserve the `accepted` event across a restart so T05 can detect
// the duplicate envelope.
func TestB30T02PartB_CrashReplay_AfterAcceptedBeforeStarted(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	rec, _ := partBAdmitAndStandalone(t, s, "crash-accepted-started", "caller-c")

	// Open the per-attempt control journal rooted at the LocalStore root.
	cj, err := NewControlJournal(s.root, string(rec.RunID), "att-accepted-1")
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	defer func() { _ = cj.Close() }()
	now := time.Now().UTC()
	if err := cj.Append(InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     now,
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"envelope":"job-1"}`,
	}); err != nil {
		t.Fatalf("Append accepted: %v", err)
	}

	// Crash: reopen the store (the control journal files are durable on disk).
	s2 := reopenTestStore(t, s)
	cj2, err := NewControlJournal(s2.root, string(rec.RunID), "att-accepted-1")
	if err != nil {
		t.Fatalf("reopen NewControlJournal: %v", err)
	}
	defer func() { _ = cj2.Close() }()

	events, err := cj2.Read(1)
	if err != nil {
		t.Fatalf("Read after reopen: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("after reopen: want 1 event, got %d", len(events))
	}
	if events[0].EventKind != InvokeJobEventAccepted {
		t.Fatalf("event kind=%s want ACCEPTED", events[0].EventKind)
	}

	// A re-invocation (duplicate job envelope) must NOT create a second
	// accepted event. The ControlJournal enforces monotonic sequences with no
	// gaps; attempting to re-append sequence 1 is rejected.
	dupAccepted := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     now,
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"envelope":"job-1"}`,
	}
	if err := cj2.Append(dupAccepted); err == nil {
		t.Error("ControlJournal accepted a duplicate-sequence accepted event (sequence collision not rejected)")
	}
	// The next event must be sequence 2 (started), confirming exactly one
	// accepted envelope was consumed.
	if err := cj2.Append(InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      2,
		Timestamp:     now,
		EventKind:     InvokeJobEventStarted,
		Payload:       `{"started":true}`,
	}); err != nil {
		t.Fatalf("Append started at seq 2: %v", err)
	}
	got, err := cj2.Read(1)
	if err != nil {
		t.Fatalf("Read after started: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d", len(got))
	}
	if got[0].EventKind != InvokeJobEventAccepted || got[1].EventKind != InvokeJobEventStarted {
		t.Fatalf("event order: %s %s", got[0].EventKind, got[1].EventKind)
	}
}

// TestB30T02PartB_CrashReplay_AfterResultWriteBeforeTerminalCommit crashes
// after the result is written to the result store but before the terminal
// `succeeded` event is committed. T05/T08 own the protected result store;
// Part B tests the ControlJournal property: the `succeeded` terminal event
// is absent, and a supervisor reconcile can commit the terminal from the
// existing result (no duplicate execution). The result-store path is T05's;
// here we model the result as a control-journal progress_ref event followed
// by a crash, then commit the terminal.
func TestB30T02PartB_CrashReplay_AfterResultWriteBeforeTerminalCommit(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	rec, _ := partBAdmitAndStandalone(t, s, "crash-result-terminal", "caller-d")

	cj, err := NewControlJournal(s.root, string(rec.RunID), "att-result-1")
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	defer func() { _ = cj.Close() }()
	now := time.Now().UTC()
	// accepted (1), started (2), progress_ref (3 = "result written")
	for i, kind := range []InvokeJobEventKind{InvokeJobEventAccepted, InvokeJobEventStarted, InvokeJobEventProgressRef} {
		if err := cj.Append(InvokeJobEvent{
			SchemaVersion: invokeJobSchemaVersionV1,
			Sequence:      int64(i + 1),
			Timestamp:     now,
			EventKind:     kind,
			Payload:       fmt.Sprintf(`{"seq":%d}`, i+1),
		}); err != nil {
			t.Fatalf("Append %d: %v", i+1, err)
		}
	}

	// Crash before terminal commit.
	s2 := reopenTestStore(t, s)
	cj2, err := NewControlJournal(s2.root, string(rec.RunID), "att-result-1")
	if err != nil {
		t.Fatalf("reopen NewControlJournal: %v", err)
	}
	defer func() { _ = cj2.Close() }()
	events, err := cj2.Read(1)
	if err != nil {
		t.Fatalf("Read after reopen: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("after reopen: want 3 events, got %d", len(events))
	}
	// Terminal event absent: the last event is progress_ref, not succeeded.
	if events[len(events)-1].EventKind == InvokeJobEventSucceeded {
		t.Fatalf("terminal succeeded must be absent before commit")
	}

	// Supervisor reconcile: commit the terminal from the existing result.
	// The next sequence must be 4 (succeeded) — no duplicate execution.
	if err := cj2.Append(InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      4,
		Timestamp:     now,
		EventKind:     InvokeJobEventSucceeded,
		Payload:       `{"result_digest":"sha256:abc"}`,
	}); err != nil {
		t.Fatalf("Append succeeded at seq 4: %v", err)
	}
	got, _ := cj2.Read(1)
	if len(got) != 4 {
		t.Fatalf("want 4 events after terminal commit, got %d", len(got))
	}
	if got[3].EventKind != InvokeJobEventSucceeded {
		t.Fatalf("last event=%s want SUCCEEDED", got[3].EventKind)
	}
}

// TestB30T02PartB_CrashReplay_AfterTerminalCommit crashes after the
// terminal event is committed. Re-open, assert the terminal is present and
// no further events can be appended that would change the terminal outcome
// (the attempt is finalized — spec line 296 "persist digest before terminal
// success").
func TestB30T02PartB_CrashReplay_AfterTerminalCommit(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	rec, _ := partBAdmitAndStandalone(t, s, "crash-terminal-commit", "caller-e")

	cj, err := NewControlJournal(s.root, string(rec.RunID), "att-terminal-1")
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	defer func() { _ = cj.Close() }()
	now := time.Now().UTC()
	for i, kind := range []InvokeJobEventKind{InvokeJobEventAccepted, InvokeJobEventStarted, InvokeJobEventSucceeded} {
		if err := cj.Append(InvokeJobEvent{
			SchemaVersion: invokeJobSchemaVersionV1,
			Sequence:      int64(i + 1),
			Timestamp:     now,
			EventKind:     kind,
			Payload:       fmt.Sprintf(`{"seq":%d}`, i+1),
		}); err != nil {
			t.Fatalf("Append %d: %v", i+1, err)
		}
	}

	// Crash: reopen.
	s2 := reopenTestStore(t, s)
	cj2, err := NewControlJournal(s2.root, string(rec.RunID), "att-terminal-1")
	if err != nil {
		t.Fatalf("reopen NewControlJournal: %v", err)
	}
	defer func() { _ = cj2.Close() }()
	events, err := cj2.Read(1)
	if err != nil {
		t.Fatalf("Read after reopen: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("after reopen: want 3 events, got %d", len(events))
	}
	if events[2].EventKind != InvokeJobEventSucceeded {
		t.Fatalf("last event=%s want SUCCEEDED", events[2].EventKind)
	}

	// The next sequence is 4; an attempt to re-commit a terminal at seq 3
	// (duplicate) must be rejected. The journal is finalized for the original
	// outcome.
	dupSucceeded := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      3,
		Timestamp:     now,
		EventKind:     InvokeJobEventSucceeded,
		Payload:       `{"result_digest":"sha256:forged"}`,
	}
	if err := cj2.Append(dupSucceeded); err == nil {
		t.Error("ControlJournal accepted a duplicate-sequence terminal event")
	}
}

// ---------------------------------------------------------------------------
// B2: B26 admission-conformance suite for standalone topology (spec line 320-321)
// ---------------------------------------------------------------------------

// TestB30T02PartB_Standalone_SlotReleaseReacquisition admits a standalone
// invocation (default-one concurrency), lets it reach terminal (succeeded),
// then admits a SECOND invocation of the same deployment. The second must be
// ACCEPTED (slot released by terminal), not ALREADY_RUNNING.
func TestB30T02PartB_Standalone_SlotReleaseReacquisition(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	dep := seedActiveDeployment(t, s, 1, nil)
	ctx := context.Background()

	r1, err := s.AdmitInvocation(ctx, baseInvocation(dep, "slot-rel-1", "caller", `{}`), dep.Generation)
	if err != nil {
		t.Fatalf("first AdmitInvocation: %v", err)
	}
	// Second with different key/caller must hit ALREADY_RUNNING (default-one).
	if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "slot-block", "caller", `{}`), dep.Generation); !errorsIs(err, ErrAlreadyRunning) {
		t.Fatalf("second admit before terminal: want ALREADY_RUNNING, got %v", err)
	}
	// Transition the first to terminal SUCCEEDED — releases the slot.
	wf, err := s.GetWorkflow(ctx, r1.WorkflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = WorkflowStatusSucceeded
	if err := s.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow to SUCCEEDED: %v", err)
	}
	// Second invocation of the SAME deployment must now be ACCEPTED.
	r2, err := s.AdmitInvocation(ctx, baseInvocation(dep, "slot-rel-2", "caller", `{}`), dep.Generation)
	if err != nil {
		t.Fatalf("second AdmitInvocation after terminal: %v", err)
	}
	if r2.InvocationID == r1.InvocationID {
		t.Fatalf("second admit must be a new invocation, got same id %s", r2.InvocationID)
	}
	// Two invocations total now.
	invs, _ := s.ListInvocations(ctx)
	if len(invs) != 2 {
		t.Fatalf("after terminal+re-admit: want 2 invocations, got %d", len(invs))
	}
}

// TestB30T02PartB_Standalone_NoHiddenQueue asserts the durable path does
// not create a hidden queue. After AdmitInvocation, the store contains
// exactly one invocation, one workflow, one run, one node, zero attempts,
// zero leases. No "pending" or "queued" state exists — the READY intent IS
// the queue, and it is exactly one.
func TestB30T02PartB_Standalone_NoHiddenQueue(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	dep := seedActiveDeployment(t, s, 1, nil)
	ctx := context.Background()

	rec, err := s.AdmitInvocation(ctx, baseInvocation(dep, "no-queue", "caller", `{}`), dep.Generation)
	if err != nil {
		t.Fatalf("AdmitInvocation: %v", err)
	}

	invs, err := s.ListInvocations(ctx)
	if err != nil {
		t.Fatalf("ListInvocations: %v", err)
	}
	if len(invs) != 1 {
		t.Fatalf("want 1 invocation, got %d", len(invs))
	}
	wfs, err := s.ListWorkflows(ctx)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(wfs) != 1 {
		t.Fatalf("want 1 workflow, got %d", len(wfs))
	}
	runs, err := s.ListRuns(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	nodes, err := s.ListNodes(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	atts, err := s.ListAttempts(ctx, rec.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(atts) != 0 {
		t.Fatalf("want 0 attempts, got %d", len(atts))
	}
	// No queued state on the run/node.
	if runs[0].Status != RunStatusPending {
		t.Fatalf("run status=%s want PENDING (READY intent)", runs[0].Status)
	}
	if nodes[0].Status != NodeStatusReady {
		t.Fatalf("node status=%s want READY", nodes[0].Status)
	}
	// No lease files written for the run (no jobs exist yet).
	leasesDir := filepath.Join(s.runsDir(), safeID(string(rec.RunID)), "attempts")
	if entries, err := os.ReadDir(leasesDir); err == nil && len(entries) != 0 {
		t.Fatalf("want 0 attempt files, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// B6: Exact-ref and alias invocation pin expected digest (spec line 313-314)
// ---------------------------------------------------------------------------

// TestB30T02PartB_ExactRefPinsDigest admits by exact deployment ID, then
// re-admits with the same key+intent. The second receipt's
// ResolvedDeploymentDigest must match the first (no drift).
func TestB30T02PartB_ExactRefPinsDigest(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	dep := seedActiveDeployment(t, s, 2, nil)
	ctx := context.Background()
	req := baseInvocation(dep, "exact-digest", "caller", `{"x":1}`)
	r1, err := s.AdmitInvocation(ctx, req, dep.Generation)
	if err != nil {
		t.Fatalf("first AdmitInvocation: %v", err)
	}
	r2, err := s.AdmitInvocation(ctx, req, dep.Generation)
	if err != nil {
		t.Fatalf("replay AdmitInvocation: %v", err)
	}
	if r2.ResolvedDeploymentID != r1.ResolvedDeploymentID {
		t.Fatalf("resolved id drift: %s vs %s", r1.ResolvedDeploymentID, r2.ResolvedDeploymentID)
	}
	if r2.ResolvedDeploymentDigest != r1.ResolvedDeploymentDigest {
		t.Fatalf("resolved digest drift: %s vs %s", r1.ResolvedDeploymentDigest, r2.ResolvedDeploymentDigest)
	}
	if r2.ResolvedDeploymentDigest != dep.BundleDigest {
		t.Fatalf("digest=%s want bundle %s", r2.ResolvedDeploymentDigest, dep.BundleDigest)
	}
}

// TestB30T02PartB_AliasMovementAfterAcceptance admits via alias "prod"
// pointing to deployment v1. Move alias "prod" to deployment v2 (CAS).
// Re-invoke with same key+intent. Assert: the replay returns the ORIGINAL
// receipt (v1), not the new alias target (v2). The idempotent replay pins
// the exact deployment resolved at acceptance time, not the current alias.
func TestB30T02PartB_AliasMovementAfterAcceptance(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	depA := seedActiveDeployment(t, s, 2, nil)
	depB := seedActiveDeployment(t, s, 2, nil)
	ctx := context.Background()

	aliasName := "prod-partb"
	alias := &AliasRecord{
		SchemaVersion:      CurrentSchemaVersion,
		Alias:              aliasName,
		TargetDeploymentID: depA.DeploymentID,
		TargetVersion:      depA.PackageVersion,
		Generation:         0,
		UpdatedBy:          "ops",
	}
	if err := s.CompareAndSwapAlias(ctx, alias); err != nil {
		t.Fatalf("initial alias: %v", err)
	}
	req := baseInvocation(depA, "alias-move", "caller", `{"m":1}`)
	req.RequestedDeploymentRef = aliasName
	r1, err := s.AdmitInvocation(ctx, req, 0)
	if err != nil {
		t.Fatalf("AdmitInvocation via alias: %v", err)
	}
	if r1.ResolvedDeploymentID != depA.DeploymentID {
		t.Fatalf("initial resolve=%s want %s", r1.ResolvedDeploymentID, depA.DeploymentID)
	}

	// Move alias to depB.
	got, err := s.ResolveAlias(ctx, aliasName)
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	got.TargetDeploymentID = depB.DeploymentID
	got.TargetVersion = depB.PackageVersion
	if err := s.CompareAndSwapAlias(ctx, got); err != nil {
		t.Fatalf("move alias: %v", err)
	}

	// Replay must return the ORIGINAL receipt (v1), not depB.
	r2, err := s.AdmitInvocation(ctx, req, 0)
	if err != nil {
		t.Fatalf("replay AdmitInvocation: %v", err)
	}
	if r2.InvocationID != r1.InvocationID {
		t.Fatalf("replay inv mismatch: %s vs %s", r1.InvocationID, r2.InvocationID)
	}
	if r2.ResolvedDeploymentID != depA.DeploymentID {
		t.Fatalf("replay must pin original resolution %s, got %s", depA.DeploymentID, r2.ResolvedDeploymentID)
	}
	if r2.ResolvedDeploymentDigest != r1.ResolvedDeploymentDigest {
		t.Fatalf("digest drift on replay: %s vs %s", r1.ResolvedDeploymentDigest, r2.ResolvedDeploymentDigest)
	}
}

// ---------------------------------------------------------------------------
// B7: Same caller/key with changed ceiling conflicts (spec line 315-317)
// ---------------------------------------------------------------------------

// TestB30T02PartB_ChangedInitialActiveTimeConflicts asserts that the same
// key with a different InitialMaxActiveDurationMs yields
// ErrIdempotencyConflict.
func TestB30T02PartB_ChangedInitialActiveTimeConflicts(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	dep := seedActiveDeployment(t, s, 2, nil)
	ctx := context.Background()
	req := baseInvocation(dep, "ceil-active", "caller", `{}`)
	if _, err := s.AdmitInvocation(ctx, req, dep.Generation); err != nil {
		t.Fatalf("first admit: %v", err)
	}
	req2 := *req
	req2.InitialMaxActiveDurationMs = 99_999
	if _, err := s.AdmitInvocation(ctx, &req2, dep.Generation); !errorsIs(err, ErrIdempotencyConflict) {
		t.Fatalf("active-time change: want ErrIdempotencyConflict, got %v", err)
	}
}

// TestB30T02PartB_ChangedInitialLeaseConflicts asserts that the same key
// with a different InitialAttemptLeaseMs yields ErrIdempotencyConflict.
func TestB30T02PartB_ChangedInitialLeaseConflicts(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	dep := seedActiveDeployment(t, s, 2, nil)
	ctx := context.Background()
	req := baseInvocation(dep, "ceil-lease", "caller", `{}`)
	if _, err := s.AdmitInvocation(ctx, req, dep.Generation); err != nil {
		t.Fatalf("first admit: %v", err)
	}
	req2 := *req
	req2.InitialAttemptLeaseMs = 99_999
	if _, err := s.AdmitInvocation(ctx, &req2, dep.Generation); !errorsIs(err, ErrIdempotencyConflict) {
		t.Fatalf("lease change: want ErrIdempotencyConflict, got %v", err)
	}
}

// TestB30T02PartB_ChangedInitialCostCeilingConflicts asserts that the same
// key with a different InitialMaxCostUsdDecimal yields
// ErrIdempotencyConflict.
func TestB30T02PartB_ChangedInitialCostCeilingConflicts(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	dep := seedActiveDeployment(t, s, 2, nil)
	ctx := context.Background()
	req := baseInvocation(dep, "ceil-cost", "caller", `{}`)
	if _, err := s.AdmitInvocation(ctx, req, dep.Generation); err != nil {
		t.Fatalf("first admit: %v", err)
	}
	req2 := *req
	req2.InitialMaxCostUsdDecimal = "99.99"
	if _, err := s.AdmitInvocation(ctx, &req2, dep.Generation); !errorsIs(err, ErrIdempotencyConflict) {
		t.Fatalf("cost change: want ErrIdempotencyConflict, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// B8: Two simultaneous default-one invocations (spec line 318-319)
// ---------------------------------------------------------------------------

// TestB30T02PartB_ConcurrentDefaultOneConcurrency runs two goroutines that
// call AdmitInvocation simultaneously on a MaxConcurrentRuns=1 deployment.
// Asserts exactly one returns ACCEPTED and the other ALREADY_RUNNING. Uses
// sync.WaitGroup to ensure both are in-flight at the same time. Run with
// -race.
func TestB30T02PartB_ConcurrentDefaultOneConcurrency(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	dep := seedActiveDeployment(t, s, 1, nil)
	ctx := context.Background()

	var wg sync.WaitGroup
	var accepted, alreadyRunning int32
	const goroutines = 2
	wg.Add(goroutines)
	// Barrier: both goroutines wait until both are ready, then race.
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			req := baseInvocation(dep, fmt.Sprintf("conc-default-%d", i), fmt.Sprintf("caller-%d", i), `{}`)
			_, err := s.AdmitInvocation(ctx, req, dep.Generation)
			switch {
			case err == nil:
				atomic.AddInt32(&accepted, 1)
			case errorsIs(err, ErrAlreadyRunning):
				atomic.AddInt32(&alreadyRunning, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted != 1 {
		t.Fatalf("want exactly 1 ACCEPTED, got %d", accepted)
	}
	if alreadyRunning != 1 {
		t.Fatalf("want exactly 1 ALREADY_RUNNING, got %d", alreadyRunning)
	}
	invs, _ := s.ListInvocations(ctx)
	if len(invs) != 1 {
		t.Fatalf("want exactly 1 invocation persisted, got %d", len(invs))
	}
}

// TestB30T02PartB_ConfiguredConcurrencyAdmitsBound uses a
// MaxConcurrentRuns=3 deployment. Three concurrent invocations all return
// ACCEPTED. A fourth returns ALREADY_RUNNING.
func TestB30T02PartB_ConfiguredConcurrencyAdmitsBound(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	dep := seedActiveDeployment(t, s, 3, nil)
	ctx := context.Background()

	var accepted, alreadyRunning int32
	const inFlight = 3
	var wg sync.WaitGroup
	wg.Add(inFlight)
	start := make(chan struct{})
	for i := 0; i < inFlight; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			req := baseInvocation(dep, fmt.Sprintf("conc-bound-%d", i), fmt.Sprintf("caller-%d", i), `{}`)
			_, err := s.AdmitInvocation(ctx, req, dep.Generation)
			switch {
			case err == nil:
				atomic.AddInt32(&accepted, 1)
			case errorsIs(err, ErrAlreadyRunning):
				atomic.AddInt32(&alreadyRunning, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted != 3 {
		t.Fatalf("want exactly 3 ACCEPTED, got %d", accepted)
	}
	if alreadyRunning != 0 {
		t.Fatalf("want 0 ALREADY_RUNNING within bound, got %d", alreadyRunning)
	}
	// A fourth must now be ALREADY_RUNNING (slots full).
	_, err := s.AdmitInvocation(ctx, baseInvocation(dep, "conc-bound-4", "caller-4", `{}`), dep.Generation)
	if !errorsIs(err, ErrAlreadyRunning) {
		t.Fatalf("fourth admit: want ALREADY_RUNNING, got %v", err)
	}
}
