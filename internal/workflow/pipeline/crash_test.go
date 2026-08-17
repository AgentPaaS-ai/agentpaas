package pipeline

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// TestCrashAfterClaimBeforeLaunchPut
// Claim via Ctrl only → node LAUNCHING, no launch job.
// New Reconciler with same stores → ReconcileOnce must recover: PutIfAbsent +
// EnsureLaunch + Ack, no second attempt, one launch key.
// ---------------------------------------------------------------------------

func TestCrashAfterClaimBeforeLaunchPut(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// ClaimNextReady directly (bypassing Reconciler) — node is LAUNCHING but
	// no launch job exists.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim == nil {
		t.Fatal("expected claim, got nil")
	}

	// Verify node is LAUNCHING.
	node, err := store.GetNode(ctx, claim.NodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.Status != routedrun.NodeStatusLaunching {
		t.Fatalf("expected LAUNCHING, got %s", node.Status)
	}

	// No launch jobs yet.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected 0 launch jobs pre-crash, got %d", len(jobs))
	}

	// "Crash": new Reconciler with same stores.
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	claim2, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce after crash: %v", err)
	}
	if claim2 == nil {
		t.Fatal("expected claim after crash, got nil")
	}
	if claim2.NodeID != claim.NodeID {
		t.Fatalf("expected same node %s, got %s", claim.NodeID, claim2.NodeID)
	}

	// Node should be RUNNING.
	node, err = store.GetNode(ctx, claim.NodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.Status != routedrun.NodeStatusRunning {
		t.Fatalf("expected RUNNING, got %s", node.Status)
	}

	// Exactly one launch job.
	jobs, err = launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 launch job, got %d", len(jobs))
	}
	if jobs[0].Status != LaunchStatusStarted {
		t.Fatalf("expected STARTED, got %s", jobs[0].Status)
	}
}

// ---------------------------------------------------------------------------
// TestCrashAfterLaunchBeforeAck
// PutIfAbsent + EnsureLaunch manually (node LAUNCHING, launch exists).
// New Reconciler with same stores → ReconcileOnce must Ack only, one job.
// ---------------------------------------------------------------------------

func TestCrashAfterLaunchBeforeAck(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim via Ctrl.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim == nil {
		t.Fatal("expected claim, got nil")
	}

	// PutIfAbsent + EnsureLaunch manually (simulating crash after launch
	// but before Ack).
	launcher := FakeLauncher{}
	job := &StageLaunchJob{
		Key:        claim.LaunchKey,
		WorkflowID: claim.WorkflowID,
		NodeID:     claim.NodeID,
		RunID:      claim.RunID,
		AttemptID:  claim.Attempt.AttemptID,
		Generation: claim.LaunchGeneration,
		Status:     LaunchStatusPending,
	}
	existing, created, err := launches.PutIfAbsent(ctx, job)
	if err != nil {
		t.Fatalf("PutIfAbsent: %v", err)
	}
	if !created {
		t.Fatal("expected created=true, got false")
	}
	_ = existing

	toLaunch := job
	if err := launcher.EnsureLaunch(ctx, toLaunch); err != nil {
		t.Fatalf("EnsureLaunch: %v", err)
	}
	toLaunch.Status = LaunchStatusStarted
	_ = launches.Update(ctx, toLaunch)

	// Verify node still LAUNCHING, launch job exists.
	node, err := store.GetNode(ctx, claim.NodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.Status != routedrun.NodeStatusLaunching {
		t.Fatalf("expected LAUNCHING, got %s", node.Status)
	}

	// "Crash": new Reconciler with same stores, same controller.
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	claim2, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce after crash: %v", err)
	}
	if claim2 == nil {
		t.Fatal("expected claim after crash, got nil")
	}

	// Node should be RUNNING.
	node, err = store.GetNode(ctx, claim.NodeID)
	if err != nil {
		t.Fatalf("GetNode after ack: %v", err)
	}
	if node.Status != routedrun.NodeStatusRunning {
		t.Fatalf("expected RUNNING, got %s", node.Status)
	}

	// Exactly one launch job.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 launch job, got %d", len(jobs))
	}
}

// ---------------------------------------------------------------------------
// TestConcurrentReconcileOnce
// Two goroutines call ReconcileOnce concurrently on the same workflow.
// End state: at most one LAUNCHING/RUNNING stage0, exactly one launch job
// for stage0, at most one attempt for stage0 run.
// ---------------------------------------------------------------------------

func TestConcurrentReconcileOnce(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := rec.ReconcileOnce(ctx, wfID)
			if err != nil {
				t.Errorf("ReconcileOnce: %v", err)
			}
		}()
	}
	wg.Wait()

	// End state: one launch job for stage0, node RUNNING, at most one attempt.
	// (Both goroutines may return claims due to CAS retry, but only one
	// launch should be persisted.)

	// Stage0 node should be RUNNING.
	nodes, err := store.ListNodes(ctx, wfID)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	var stage0 *routedrun.PipelineNode
	for _, n := range nodes {
		if n.StageOrder == 0 {
			stage0 = n
			break
		}
	}
	if stage0 == nil {
		t.Fatal("stage0 not found")
	}
	if stage0.Status != routedrun.NodeStatusRunning {
		t.Fatalf("expected stage0 RUNNING, got %s", stage0.Status)
	}

	// Exactly one launch job for stage0.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	stage0Jobs := 0
	for _, j := range jobs {
		if j.NodeID == stage0.NodeID {
			stage0Jobs++
		}
	}
	if stage0Jobs != 1 {
		t.Fatalf("expected 1 launch job for stage0, got %d (total: %d)", stage0Jobs, len(jobs))
	}

	// At most one attempt for stage0 run.
	attempts, err := store.ListAttempts(ctx, stage0.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) > 1 {
		t.Fatalf("expected at most 1 attempt, got %d", len(attempts))
	}
}

// ---------------------------------------------------------------------------
// TestSixteenStageDeterministic
// Seed 16 stages, loop reconcile+commit with handoffs until workflow
// SUCCEEDED. Verify 16 launch keys.
// ---------------------------------------------------------------------------

func TestSixteenStageDeterministic(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 16)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	for i := 0; i < 16; i++ {
		claim, err := rec.ReconcileOnce(ctx, wfID)
		if err != nil {
			t.Fatalf("ReconcileOnce stage %d: %v", i, err)
		}
		if claim == nil {
			t.Fatalf("ReconcileOnce stage %d: nil claim", i)
		}

		isFinal := i == 15
		req := StageSuccess{
			WorkflowID: wfID,
			NodeID:     nodeIDs[i],
			RunID:      claim.RunID,
			AttemptID:  claim.Attempt.AttemptID,
		}
		if !isFinal {
			req.Handoff = &routedrun.HandoffEnvelope{
				WorkflowID:   wfID,
				SourceNodeID: nodeIDs[i],
				TargetNodeID: nodeIDs[i+1],
				ContextJSON:  `{"stage":` + itoa(i) + `}`,
			}
		}
		if err := ctrl.CommitStageSuccess(ctx, req); err != nil {
			t.Fatalf("CommitStageSuccess stage %d: %v", i, err)
		}
	}

	// Verify workflow SUCCEEDED.
	wf, err := store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusSucceeded {
		t.Fatalf("expected SUCCEEDED, got %s", wf.Status)
	}

	// 16 launch keys.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 16 {
		t.Fatalf("expected 16 launch jobs, got %d", len(jobs))
	}
}

// itoa is a minimal int-to-string helper for test use.
func itoa(i int) string {
	b := make([]byte, 0, 4)
	if i == 0 {
		return "0"
	}
	for n := i; n > 0; n /= 10 {
		b = append(b, byte('0'+n%10))
	}
	for left, right := 0, len(b)-1; left < right; left, right = left+1, right-1 {
		b[left], b[right] = b[right], b[left]
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// TestRestartMidLaunchingSharedStores
// claim + put before ack, new Controller+Reconciler same stores → Ack,
// no duplicate attempt/job.
// ---------------------------------------------------------------------------

func TestRestartMidLaunchingSharedStores(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim via Ctrl.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim == nil {
		t.Fatal("expected claim, got nil")
	}

	// PutIfAbsent + EnsureLaunch (simulating crash after launch before ack).
	launcher := FakeLauncher{}
	job := &StageLaunchJob{
		Key:        claim.LaunchKey,
		WorkflowID: claim.WorkflowID,
		NodeID:     claim.NodeID,
		RunID:      claim.RunID,
		AttemptID:  claim.Attempt.AttemptID,
		Generation: claim.LaunchGeneration,
		Status:     LaunchStatusPending,
	}
	_, _, _ = launches.PutIfAbsent(ctx, job)
	_ = launcher.EnsureLaunch(ctx, job)
	job.Status = LaunchStatusStarted
	_ = launches.Update(ctx, job)

	// "Restart": new Controller, same stores.
	ctrl2 := NewController(store)
	rec2 := &Reconciler{
		Ctrl:     ctrl2,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	claim2, err := rec2.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce after restart: %v", err)
	}
	if claim2 == nil {
		t.Fatal("expected claim after restart, got nil")
	}

	// Node should be RUNNING.
	node, err := store.GetNode(ctx, claim.NodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.Status != routedrun.NodeStatusRunning {
		t.Fatalf("expected RUNNING, got %s", node.Status)
	}

	// Exactly one launch job.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 launch job, got %d", len(jobs))
	}

	// At most one attempt.
	attempts, err := store.ListAttempts(ctx, claim.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) > 1 {
		t.Fatalf("expected at most 1 attempt, got %d", len(attempts))
	}
}

// ---------------------------------------------------------------------------
// TestCollectStageContextParamsFullEnvelope
// Mid-stage IncomingHandoffJSON is the work order only — envelope metadata
// (handoff_id, workflow_id, context_json) must not be forwarded.
// ---------------------------------------------------------------------------

func TestCollectStageContextParamsFullEnvelope(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Stage 0: claim, launch, ack, commit with handoff.
	claim0, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce stage0: %v", err)
	}
	if claim0 == nil {
		t.Fatal("stage0: nil claim")
	}
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{"value":"hello"}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage0: %v", err)
	}

	// Stage 1: claim, collect params.
	claim1, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce stage1: %v", err)
	}
	if claim1 == nil {
		t.Fatal("stage1: nil claim")
	}

	params1, err := CollectStageContextParams(ctx, store, claim1)
	if err != nil {
		t.Fatalf("CollectStageContextParams stage1: %v", err)
	}
	if params1.StageOrder != 1 {
		t.Fatalf("stage1: want StageOrder=1, got %d", params1.StageOrder)
	}

	if len(params1.IncomingHandoffJSON) == 0 {
		t.Fatal("stage1: expected non-empty IncomingHandoffJSON (work order)")
	}

	var env map[string]any
	if err := json.Unmarshal(params1.IncomingHandoffJSON, &env); err != nil {
		t.Fatalf("stage1: failed to unmarshal IncomingHandoffJSON: %v (%s)", err, string(params1.IncomingHandoffJSON))
	}

	if env["value"] != "hello" {
		t.Fatalf("stage1: expected work-order value=hello, got keys: %v", mapKeys(env))
	}
	for _, leaked := range []string{"handoff_id", "workflow_id", "context_json", "source_node_id"} {
		if _, ok := env[leaked]; ok {
			t.Fatalf("stage1: leaked envelope key %q: %v", leaked, mapKeys(env))
		}
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}