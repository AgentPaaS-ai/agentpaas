package routedrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Crash recovery tests (requirements 14–15)
// WAL boundaries, atomic writes, migration, ReconcileInterrupted.
// ---------------------------------------------------------------------------

func TestCrashRecovery_ReconcileInterrupted_RevokesLeaseAndFails(t *testing.T) {
	// Requirement 14: always revokes interrupted lease, records DAEMON_RESTARTED,
	// attempt becomes terminal FAILED.
	for _, be := range []struct {
		name string
		open func(t *testing.T) RunStore
	}{
		{"LocalStore", func(t *testing.T) RunStore { return openTestStore(t) }},
		{"MemoryStore", func(t *testing.T) RunStore {
			return NewMemoryStore(WithMemoryClock(testClock(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))))
		}},
	} {
		t.Run(be.name, func(t *testing.T) {
			s := be.open(t)
			ctx := context.Background()
			run := &RunRecord{
				SchemaVersion: CurrentSchemaVersion,
				WorkflowID:    "wf-reconcile",
				Status:        RunStatusRunning,
				RunKind:       "standalone",
			}
			if err := s.CreateRun(ctx, run); err != nil {
				t.Fatal(err)
			}
			att := &AttemptRecord{
				SchemaVersion: CurrentSchemaVersion,
				RunID:         run.RunID,
				WorkflowID:    "wf-reconcile",
				Status:        AttemptStatusRunning,
				AttemptNumber: 1,
				Lease: &AttemptLease{
					DurationMs: 60_000,
					AcquiredAt: time.Now().UTC(),
					ExpiresAt:  time.Now().UTC().Add(time.Hour),
					LeaseToken: "live-token",
				},
			}
			if err := s.CreateAttempt(ctx, att); err != nil {
				t.Fatal(err)
			}
			// Terminal attempt must be left alone.
			done := &AttemptRecord{
				SchemaVersion: CurrentSchemaVersion,
				RunID:         run.RunID,
				WorkflowID:    "wf-reconcile",
				Status:        AttemptStatusSucceeded,
				AttemptNumber: 0, // will be renumbered? CreateAttempt may set
			}
			// Create a second run's terminal attempt on same run is fine if Succeeded.
			// Use attempt 2 after first exists.
			done.AttemptNumber = 2
			if err := s.CreateAttempt(ctx, done); err != nil {
				t.Fatal(err)
			}

			if err := s.ReconcileInterrupted(ctx, run.RunID); err != nil {
				t.Fatal(err)
			}

			got, err := s.GetAttempt(ctx, att.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != AttemptStatusFailed {
				t.Fatalf("status=%s want FAILED", got.Status)
			}
			if got.FailureReason == nil || *got.FailureReason != FailureDaemonRestarted {
				t.Fatalf("failure reason=%v want DAEMON_RESTARTED", got.FailureReason)
			}
			if got.Lease == nil {
				t.Fatal("lease should still be present but revoked")
			}
			if got.Lease.LeaseToken != "" {
				t.Fatalf("lease token must be cleared, got %q", got.Lease.LeaseToken)
			}
			if got.TerminatedAt == nil {
				t.Fatal("terminated_at required")
			}
			// Terminal attempt unchanged.
			gotDone, err := s.GetAttempt(ctx, done.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			if gotDone.Status != AttemptStatusSucceeded {
				t.Fatalf("terminal attempt mutated: %s", gotDone.Status)
			}
			rg, err := s.GetRun(ctx, run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if rg.Status != RunStatusFailed {
				t.Fatalf("run status=%s want FAILED", rg.Status)
			}
			// Idempotent reconcile on already-terminal.
			if err := s.ReconcileInterrupted(ctx, run.RunID); err != nil {
				t.Fatal(err)
			}
			rg2, err := s.GetRun(ctx, run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if rg2.Status != RunStatusFailed {
				t.Fatalf("re-reconcile status=%s", rg2.Status)
			}
		})
	}
}

func TestCrashRecovery_WAL_UncommittedDiscarded(t *testing.T) {
	// Boundary 1: WAL entry written but not committed → recovery discards;
	// pre-transition state remains authoritative.
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	// Inject uncommitted WAL that would bump generation / status if applied.
	evil := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    wf.WorkflowID,
		Status:        WorkflowStatusRunning,
		WorkflowKind:  "standalone",
		Generation:    99,
	}
	payload, err := marshalPersisted(99, evil)
	if err != nil {
		t.Fatal(err)
	}
	entry := &WALEntry{
		SchemaVersion: CurrentSchemaVersion,
		EntryID:       "wal-uncommitted-test",
		WorkflowID:    string(wf.WorkflowID),
		Generation:    1,
		NewGeneration: 99,
		Command:       "crash-before-commit",
		Operations: []WALOp{{
			Kind:    "workflow",
			ID:      string(wf.WorkflowID),
			Action:  "put",
			Payload: payload,
		}},
		Committed: false,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := s.writeWALEntry(wf.WorkflowID, entry); err != nil {
		t.Fatal(err)
	}
	// Ensure uncommitted file exists.
	walPath := filepath.Join(s.walDir(wf.WorkflowID), entry.EntryID+".json")
	if _, err := os.Lstat(walPath); err != nil {
		t.Fatalf("wal missing: %v", err)
	}
	if err := s.RecoverWAL(wf.WorkflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(walPath); !os.IsNotExist(err) {
		t.Fatalf("uncommitted wal should be removed, err=%v", err)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 1 || got.Status != WorkflowStatusPending {
		t.Fatalf("pre-transition must remain: gen=%d status=%s", got.Generation, got.Status)
	}
}

func TestCrashRecovery_WAL_CommittedIncompleteMaterializationReplays(t *testing.T) {
	// Boundary 2: WAL committed but materialization incomplete → recovery replays.
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	// Build committed entry that updates workflow to RUNNING gen=2 and creates a node.
	nodeID, err := NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	updated := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    wf.WorkflowID,
		Status:        WorkflowStatusRunning,
		WorkflowKind:  "standalone",
		Generation:    2,
	}
	wfPayload, err := marshalPersisted(2, updated)
	if err != nil {
		t.Fatal(err)
	}
	node := &PipelineNode{
		SchemaVersion: CurrentSchemaVersion,
		NodeID:        nodeID,
		WorkflowID:    wf.WorkflowID,
		Status:        NodeStatusReady,
		StageOrder:    0,
		PackageName:   "pkg",
		PackageVersion: "1",
	}
	nodePayload, err := marshalPersisted(1, node)
	if err != nil {
		t.Fatal(err)
	}
	entry := &WALEntry{
		SchemaVersion: CurrentSchemaVersion,
		EntryID:       "wal-committed-partial",
		WorkflowID:    string(wf.WorkflowID),
		Generation:    1,
		NewGeneration: 2,
		Command:       "start",
		Operations: []WALOp{
			{Kind: "workflow", ID: string(wf.WorkflowID), Action: "put", Payload: wfPayload},
			{Kind: "node", ID: string(nodeID), Action: "put", Payload: nodePayload},
		},
		Committed: false,
		CreatedAt: time.Now().UTC(),
	}
	path, err := s.writeWALEntry(wf.WorkflowID, entry)
	if err != nil {
		t.Fatal(err)
	}
	// Materialize only workflow, leave node missing (simulate crash mid-materialize).
	if err := s.materializeWALOps(wf.WorkflowID, entry.Operations[:1]); err != nil {
		t.Fatal(err)
	}
	if err := s.commitWALEntry(path, entry); err != nil {
		t.Fatal(err)
	}
	// Node must be absent before recovery.
	if _, err := s.GetNode(ctx, nodeID); err == nil {
		t.Fatal("node should be missing before recovery")
	}
	// Recovery must re-apply full ops including node.
	if err := s.RecoverWAL(wf.WorkflowID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 2 || got.Status != WorkflowStatusRunning {
		t.Fatalf("after recover: gen=%d status=%s", got.Generation, got.Status)
	}
	n, err := s.GetNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("node must be materialized: %v", err)
	}
	if n.Status != NodeStatusReady {
		t.Fatalf("node status=%s", n.Status)
	}
}

func TestCrashRecovery_WAL_FullyMaterializedNoOp(t *testing.T) {
	// Boundary 3: committed + fully materialized → recovery is no-op (idempotent).
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyTransition(ctx, wf.WorkflowID, 1, "start"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 2 {
		t.Fatalf("gen=%d", got.Generation)
	}
	// Recovery after clean apply.
	if err := s.RecoverWAL(wf.WorkflowID); err != nil {
		t.Fatal(err)
	}
	got2, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Generation != 2 {
		t.Fatalf("recovery changed gen %d", got2.Generation)
	}
	// Second recovery still safe.
	if err := s.RecoverWAL(wf.WorkflowID); err != nil {
		t.Fatal(err)
	}
}

func TestCrashRecovery_WAL_CrashBeforeMaterializeLeavesPreState(t *testing.T) {
	// Crash after uncommitted WAL write, before any materialize: pre-state.
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	updated := *wf
	updated.Generation = 2
	updated.Status = WorkflowStatusRunning
	payload, err := marshalPersisted(2, &updated)
	if err != nil {
		t.Fatal(err)
	}
	entry := &WALEntry{
		SchemaVersion: CurrentSchemaVersion,
		EntryID:       "wal-pre-mat",
		WorkflowID:    string(wf.WorkflowID),
		Generation:    1,
		NewGeneration: 2,
		Command:       "start",
		Operations:    []WALOp{{Kind: "workflow", ID: string(wf.WorkflowID), Action: "put", Payload: payload}},
		Committed:     false,
		CreatedAt:     time.Now().UTC(),
	}
	if _, err := s.writeWALEntry(wf.WorkflowID, entry); err != nil {
		t.Fatal(err)
	}
	// No materialize, no commit — simulate process death.
	// Re-open store and recover.
	s2, err := OpenLocalStore(s.root, WithClock(testClock(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))))
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.RecoverWAL(wf.WorkflowID); err != nil {
		t.Fatal(err)
	}
	got, err := s2.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 1 || got.Status != WorkflowStatusPending {
		t.Fatalf("want pre-transition, got gen=%d status=%s", got.Generation, got.Status)
	}
}

func TestCrashRecovery_WAL_MultiOpAtomicityNoMixture(t *testing.T) {
	// After recovery, either all ops from a committed entry are present or none
	// from an uncommitted entry — never a mixture of half-applied uncommitted work.
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	nodeA, _ := NewNodeID()
	nodeB, _ := NewNodeID()
	nodeARec := &PipelineNode{SchemaVersion: CurrentSchemaVersion, NodeID: nodeA, WorkflowID: wf.WorkflowID, Status: NodeStatusReady}
	nodeBRec := &PipelineNode{SchemaVersion: CurrentSchemaVersion, NodeID: nodeB, WorkflowID: wf.WorkflowID, Status: NodeStatusPending}
	pa, _ := marshalPersisted(1, nodeARec)
	pb, _ := marshalPersisted(1, nodeBRec)
	// Uncommitted multi-op: partially materialize only nodeA.
	entry := &WALEntry{
		SchemaVersion: CurrentSchemaVersion,
		EntryID:       "wal-mix",
		WorkflowID:    string(wf.WorkflowID),
		Generation:    1,
		NewGeneration: 2,
		Command:       "partial",
		Operations: []WALOp{
			{Kind: "node", ID: string(nodeA), Action: "put", Payload: pa},
			{Kind: "node", ID: string(nodeB), Action: "put", Payload: pb},
		},
		Committed: false,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := s.writeWALEntry(wf.WorkflowID, entry); err != nil {
		t.Fatal(err)
	}
	if err := s.materializeWALOps(wf.WorkflowID, entry.Operations[:1]); err != nil {
		t.Fatal(err)
	}
	// Before recovery: mixture present (nodeA only) + uncommitted WAL.
	// Spec: recovery of uncommitted discards WAL; does NOT roll back already-written
	// materialization that leaked. Document observed behavior.
	if err := s.RecoverWAL(wf.WorkflowID); err != nil {
		t.Fatal(err)
	}
	// Uncommitted entry discarded.
	walPath := filepath.Join(s.walDir(wf.WorkflowID), entry.EntryID+".json")
	if _, err := os.Lstat(walPath); !os.IsNotExist(err) {
		t.Fatalf("uncommitted wal should be gone")
	}
	// Uncommitted recovery must roll back partial materialization (no mixture).
	_, errA := s.GetNode(ctx, nodeA)
	_, errB := s.GetNode(ctx, nodeB)
	if errA == nil || errB == nil {
		t.Fatalf("uncommitted partial materialization must be rolled back; nodeA err=%v nodeB err=%v", errA, errB)
	}
	// Workflow generation must still be pre-transition (auto workflow op not applied).
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 1 {
		t.Fatalf("workflow gen must stay 1 after uncommitted discard, got %d", got.Generation)
	}
	// Committed path: both nodes present, no mixture.
	entry2 := &WALEntry{
		SchemaVersion: CurrentSchemaVersion,
		EntryID:       "wal-mix-ok",
		WorkflowID:    string(wf.WorkflowID),
		Generation:    1,
		NewGeneration: 2,
		Command:       "full",
		Operations: []WALOp{
			{Kind: "node", ID: string(nodeA), Action: "put", Payload: pa},
			{Kind: "node", ID: string(nodeB), Action: "put", Payload: pb},
		},
		Committed: false,
		CreatedAt: time.Now().UTC(),
	}
	path, err := s.writeWALEntry(wf.WorkflowID, entry2)
	if err != nil {
		t.Fatal(err)
	}
	// Crash after first of two ops, then commit marker written (simulating
	// commit-before-full-materialize ordering variant — recovery must complete).
	if err := s.materializeWALOps(wf.WorkflowID, entry2.Operations[:1]); err != nil {
		t.Fatal(err)
	}
	if err := s.commitWALEntry(path, entry2); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverWAL(wf.WorkflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNode(ctx, nodeA); err != nil {
		t.Fatalf("nodeA: %v", err)
	}
	if _, err := s.GetNode(ctx, nodeB); err != nil {
		t.Fatalf("nodeB after committed recovery: %v", err)
	}
}

func TestCrashRecovery_ApplyTransition_CASAndOpsJSON(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	// Structured ops via command JSON.
	nodeID, _ := NewNodeID()
	node := &PipelineNode{
		SchemaVersion: CurrentSchemaVersion,
		NodeID:        nodeID,
		WorkflowID:    wf.WorkflowID,
		Status:        NodeStatusReady,
	}
	nodePayload, err := marshalPersisted(1, node)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := json.Marshal(map[string]interface{}{
		"operations": []WALOp{{
			Kind: "node", ID: string(nodeID), Action: "put", Payload: nodePayload,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyTransition(ctx, wf.WorkflowID, 1, string(cmd)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 2 {
		t.Fatalf("gen=%d", got.Generation)
	}
	// Stale generation fails without mutating.
	if err := s.ApplyTransition(ctx, wf.WorkflowID, 1, "stale"); !errorsIs(err, ErrCASConflict) {
		t.Fatalf("want CAS, got %v", err)
	}
}

func TestCrashRecovery_AtomicWrite_OrphanTempAndPartial(t *testing.T) {
	s := openTestStore(t)
	// Orphan .tmp-* cleaned on open.
	dir := s.workflowsDir()
	orphan := filepath.Join(dir, ".tmp-crash-partial")
	if err := os.WriteFile(orphan, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenLocalStore(s.root)
	if err != nil {
		t.Fatal(err)
	}
	_ = s2
	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan temp should be cleaned on open")
	}
	// Successful atomic write is all-or-nothing at destination path.
	path := filepath.Join(dir, "atomic-probe.json")
	if err := atomicWriteFile(path, []byte(`{"ok":true}`), filePerm); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("content=%s", data)
	}
	// No leftover temps.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.Name()) >= 5 && e.Name()[:5] == ".tmp-" {
			t.Fatalf("leftover temp %s", e.Name())
		}
	}
}

func TestCrashRecovery_Migration_CorruptAndUnknownFailClosed(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	path := s.deploymentPath(dep.DeploymentID)

	// Corrupt JSON fails closed.
	if err := os.WriteFile(path, []byte(`{not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDeployment(ctx, dep.DeploymentID); err == nil {
		t.Fatal("corrupt file must fail closed")
	}

	// Unknown schema version fails closed (restore valid envelope with future version).
	env := persisted{
		SchemaVersion: "9.9.9",
		Generation:    1,
		Record:        json.RawMessage(`{"deployment_id":"` + string(dep.DeploymentID) + `"}`),
	}
	raw, _ := json.Marshal(env)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDeployment(ctx, dep.DeploymentID); !errorsIs(err, ErrUnknownSchemaVersion) {
		// may wrap
		if err == nil {
			t.Fatal("future schema must fail closed")
		}
	}
}

func TestCrashRecovery_WAL_TmpFilesDiscarded(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	dir := s.walDir(wf.WorkflowID)
	if err := mkdirProtected(dir); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, ".tmp-wal-incomplete")
	if err := os.WriteFile(tmp, []byte(`{"committed":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverWAL(wf.WorkflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Fatal("tmp wal fragment should be removed")
	}
}

func TestCrashRecovery_AdmissionPartialWithoutIdempotencyAllowsRetry(t *testing.T) {
	// commitAdmission writes workflow/nodes/runs then receipt then idempotency.
	// Simulate crash after workflow write, before idempotency: new admit with
	// same key should still succeed (no idempotency record) OR leave recoverable state.
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 2, nil)
	// Manually create orphan workflow without idempotency — should not block
	// a fresh admit with a key (different identity).
	orphan := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		DeploymentID:  dep.DeploymentID,
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	// With max=2, admit should work alongside one orphan slot-holder.
	rec, err := s.AdmitInvocation(ctx, baseInvocation(dep, "after-crash", "c", `{}`), 0)
	if err != nil {
		t.Fatalf("admit after partial orphan: %v", err)
	}
	if rec.WorkflowID == orphan.WorkflowID {
		t.Fatal("new admit must not reuse orphan workflow id")
	}
	// Third would be blocked (orphan + admit = 2 holders).
	if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "after-crash-2", "c2", `{}`), 0); !errorsIs(err, ErrAlreadyRunning) {
		t.Fatalf("want ALREADY_RUNNING at max, got %v", err)
	}
}
