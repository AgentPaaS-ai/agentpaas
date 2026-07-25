package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// Test 1: LaunchIdempotencyKey format
// ---------------------------------------------------------------------------

func TestLaunchIdempotencyKeyFormat(t *testing.T) {
	key := LaunchIdempotencyKey("wf-abc", "node-xyz", 1)
	expected := "wf-abc|node-xyz|1"
	if key != expected {
		t.Fatalf("LaunchIdempotencyKey: want %q, got %q", expected, key)
	}

	key2 := LaunchIdempotencyKey("wf-123", "node-456", 42)
	expected2 := "wf-123|node-456|42"
	if key2 != expected2 {
		t.Fatalf("LaunchIdempotencyKey: want %q, got %q", expected2, key2)
	}
}

// ---------------------------------------------------------------------------
// Test 2: MemoryLaunchStore PutIfAbsent
// ---------------------------------------------------------------------------

func TestMemoryLaunchStorePutIfAbsent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryLaunchStore()

	job := &StageLaunchJob{
		Key:        "wf|node|1",
		WorkflowID: "wf-1",
		NodeID:     "node-1",
		Status:     LaunchStatusPending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	// First put: should create.
	existing, created, err := store.PutIfAbsent(ctx, job)
	if err != nil {
		t.Fatalf("PutIfAbsent #1: %v", err)
	}
	if !created {
		t.Fatal("PutIfAbsent #1: expected created=true, got false")
	}
	if existing != nil {
		t.Fatal("PutIfAbsent #1: expected nil existing, got non-nil")
	}

	// Second put with same key: should return existing.
	job2 := &StageLaunchJob{
		Key:        "wf|node|1",
		WorkflowID: "wf-1",
		NodeID:     "node-1",
		Status:     LaunchStatusPending,
	}
	existing, created, err = store.PutIfAbsent(ctx, job2)
	if err != nil {
		t.Fatalf("PutIfAbsent #2: %v", err)
	}
	if created {
		t.Fatal("PutIfAbsent #2: expected created=false, got true")
	}
	if existing == nil {
		t.Fatal("PutIfAbsent #2: expected existing job returned, got nil")
	}
	if existing.Key != "wf|node|1" {
		t.Fatalf("PutIfAbsent #2: key mismatch: %s", existing.Key)
	}
}

// ---------------------------------------------------------------------------
// Test 3: ReconcileOnce two-stage fake pipeline
// ---------------------------------------------------------------------------

func TestReconcileOnceTwoStageFake(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// ── Tick 1: claim stage0, launch, ack ──
	claim0, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce #1: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ReconcileOnce #1: expected claim, got nil")
	}
	if claim0.NodeID != nodeIDs[0] {
		t.Fatalf("ReconcileOnce #1: expected node0, got %s", claim0.NodeID)
	}

	// Verify node0 is RUNNING.
	n0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0: %v", err)
	}
	if n0.Status != routedrun.NodeStatusRunning {
		t.Fatalf("node0: want RUNNING, got %s", n0.Status)
	}

	// Verify launch job exists and is STARTED.
	job0, err := launches.Get(ctx, claim0.LaunchKey)
	if err != nil {
		t.Fatalf("get launch job stage0: %v", err)
	}
	if job0.Status != LaunchStatusStarted {
		t.Fatalf("launch job stage0: want STARTED, got %s", job0.Status)
	}

	// ── Commit stage0 success + handoff ──
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{"result":"ok"}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage0: %v", err)
	}

	// ── Tick 2: claim stage1, launch, ack ──
	claim1, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce #2: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ReconcileOnce #2: expected claim, got nil")
	}
	if claim1.NodeID != nodeIDs[1] {
		t.Fatalf("ReconcileOnce #2: expected node1, got %s", claim1.NodeID)
	}

	// Verify node1 is RUNNING.
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusRunning {
		t.Fatalf("node1: want RUNNING, got %s", n1.Status)
	}

	// ── Commit stage1 success (final) ──
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[1],
		RunID:      claim1.RunID,
		AttemptID:  claim1.Attempt.AttemptID,
		Handoff:    nil,
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage1: %v", err)
	}

	// Verify workflow SUCCEEDED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusSucceeded {
		t.Fatalf("workflow: want SUCCEEDED, got %s", wf.Status)
	}

	// Exactly 2 launch keys ever created.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 launch jobs, got %d", len(jobs))
	}
}

// ---------------------------------------------------------------------------
// Test 4: idempotent while LAUNCHING
// ---------------------------------------------------------------------------

func TestReconcileOnceIdempotentWhileLaunching(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// First reconcile: claim + launch + ack.
	claim0, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce #1: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ReconcileOnce #1: expected claim, got nil")
	}

	// Second reconcile: stage0 is RUNNING, nothing READY → nil.
	claim1, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce #2: %v", err)
	}
	if claim1 != nil {
		t.Fatal("ReconcileOnce #2: expected nil (nothing ready), got claim")
	}

	// Launch count unchanged (still 1).
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 launch job, got %d", len(jobs))
	}
}

// ---------------------------------------------------------------------------
// Test 5: restart no duplicate launch
// ---------------------------------------------------------------------------

func TestReconcileRestartNoDuplicateLaunch(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()

	// Complete stage0.
	ctrl1 := NewController(store)
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl1, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	launches := NewMemoryLaunchStore()
	rec1 := &Reconciler{
		Ctrl:     ctrl1,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	claim0, err := rec1.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce #1: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ReconcileOnce #1: nil claim")
	}

	if err := ctrl1.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage0: %v", err)
	}

	// Simulate restart: new Controller + Reconciler, same MemoryStore + MemoryLaunchStore.
	ctrl2 := NewController(store)
	rec2 := &Reconciler{
		Ctrl:     ctrl2,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	// ReconcileOnce must claim stage1 only, not re-launch stage0.
	claim1, err := rec2.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce #2: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ReconcileOnce #2: expected claim for stage1, got nil")
	}
	if claim1.NodeID != nodeIDs[1] {
		t.Fatalf("ReconcileOnce #2: expected node1 (%s), got %s", nodeIDs[1], claim1.NodeID)
	}

	// Launch store still has stage0 key exactly once.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}

	// Count jobs for stage0.
	stage0Count := 0
	for _, j := range jobs {
		if j.NodeID == nodeIDs[0] {
			stage0Count++
		}
	}
	if stage0Count != 1 {
		t.Fatalf("stage0 launch count: want 1, got %d (total jobs: %d)", stage0Count, len(jobs))
	}

	// Should have 2 launch jobs total (stage0 + stage1).
	if len(jobs) != 2 {
		t.Fatalf("expected 2 total launch jobs, got %d", len(jobs))
	}
}

// ---------------------------------------------------------------------------
// Test 6: Reconcile respects pause
// ---------------------------------------------------------------------------

func TestReconcileRespectsPause(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Set pause desired state before claim.
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:       wfID,
		Command:          routedrun.ControlPause,
		IdempotencyKey:   "pause-1",
	}); err != nil {
		t.Fatalf("RequestControl pause: %v", err)
	}
	wf, err := store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPauseRequested
	wf.UpdatedAt = time.Now().UTC()
	if err := store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}

	// ReconcileOnce returns nil (nothing to launch).
	claim, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce under pause: %v", err)
	}
	if claim != nil {
		t.Fatal("ReconcileOnce under pause: expected nil, got claim")
	}

	// No launch jobs created.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected 0 launch jobs under pause, got %d", len(jobs))
	}
}

// ---------------------------------------------------------------------------
// Test 7: CollectStageContextParams stage0 and mid
// ---------------------------------------------------------------------------

func TestCollectStageContextParamsStage0AndMid(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	claim0, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce stage0: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ReconcileOnce stage0: nil claim")
	}

	// Stage0 params: empty handoff.
	params0, err := CollectStageContextParams(ctx, store, claim0)
	if err != nil {
		t.Fatalf("CollectStageContextParams stage0: %v", err)
	}
	if params0.WorkflowKind != "pipeline" {
		t.Fatalf("stage0: want WorkflowKind=pipeline, got %s", params0.WorkflowKind)
	}
	if params0.StageOrder != 0 {
		t.Fatalf("stage0: want StageOrder=0, got %d", params0.StageOrder)
	}
	if params0.IsFinalStage {
		t.Fatal("stage0: expected IsFinalStage=false (2-stage pipeline)")
	}
	if params0.IncomingHandoffJSON != nil {
		t.Fatalf("stage0: expected nil IncomingHandoffJSON, got %s", string(params0.IncomingHandoffJSON))
	}

	// Commit stage0 success + handoff.
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{"result":"ok"}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage0: %v", err)
	}

	claim1, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce stage1: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ReconcileOnce stage1: nil claim")
	}

	// Stage1 params: handoff JSON present, StageOrder=1.
	params1, err := CollectStageContextParams(ctx, store, claim1)
	if err != nil {
		t.Fatalf("CollectStageContextParams stage1: %v", err)
	}
	if params1.StageOrder != 1 {
		t.Fatalf("stage1: want StageOrder=1, got %d", params1.StageOrder)
	}
	if !params1.IsFinalStage {
		t.Fatal("stage1: expected IsFinalStage=true (2-stage pipeline)")
	}
	if string(params1.IncomingHandoffJSON) != `{"result":"ok"}` {
		t.Fatalf("stage1: want handoff context '{\"result\":\"ok\"}', got %s", string(params1.IncomingHandoffJSON))
	}
}
