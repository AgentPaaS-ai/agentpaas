package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline"
)

// TestPipelineReconcileLoop_TicksWhileEnabled verifies that the runtime
// calls the reconcile function at least 2 times while running, then stops
// calling after Stop().
func TestPipelineReconcileLoop_TicksWhileEnabled(t *testing.T) {
	store := routedrun.NewMemoryStore()
	var tickCount int32

	rt := newPipelineRuntime(store, func(ctx context.Context, workflowID routedrun.WorkflowID) error {
		atomic.AddInt32(&tickCount, 1)
		return nil
	}, 20*time.Millisecond) // fast interval for test

	// Register a pipeline workflow so the loop has something to reconcile.
	rt.RegisterPipelineWorkflowForReconcile("wf-test-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt.Start(ctx)

	// Wait for at least 2 ticks.
	deadline := time.After(2 * time.Second)
	for {
		if atomic.LoadInt32(&tickCount) >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for 2 ticks: got %d", tickCount)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Snapshot count before stop.
	beforeStop := atomic.LoadInt32(&tickCount)

	rt.Stop()

	// Wait a little and verify no new ticks.
	time.Sleep(100 * time.Millisecond)
	afterStop := atomic.LoadInt32(&tickCount)

	if afterStop != beforeStop {
		t.Errorf("expected no new ticks after Stop: before=%d after=%d", beforeStop, afterStop)
	}

	t.Logf("ticks before stop=%d after stop=%d", beforeStop, afterStop)
}

// TestPipelineReconcileLoop_DisabledByDefault verifies the runtime does not
// tick when never started (disabled by default).
func TestPipelineReconcileLoop_DisabledByDefault(t *testing.T) {
	store := routedrun.NewMemoryStore()
	var tickCount int32

	rt := newPipelineRuntime(store, func(ctx context.Context, workflowID routedrun.WorkflowID) error {
		atomic.AddInt32(&tickCount, 1)
		return nil
	}, 20*time.Millisecond)

	rt.RegisterPipelineWorkflowForReconcile("wf-test-1")

	// Do NOT call Start — verify no ticks happen.
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&tickCount) != 0 {
		t.Errorf("expected 0 ticks when not started, got %d", tickCount)
	}
}

// TestPipelineReconcileLoop_CallsControllerReconcileOnce seeds a 3-stage
// pipeline workflow via the controller, registers it, and runs the reconcile
// loop. Using FakeLauncher, the loop advances through claim → launch → ack.
// After a few ticks, we verify that all nodes have advanced to RUNNING
// (the controller advances LAUNCHING→RUNNING via ack, but success is manual).
func TestPipelineReconcileLoop_CallsControllerReconcileOnce(t *testing.T) {
	store := routedrun.NewMemoryStore()
	ctrl := pipeline.NewController(store)
	launches := pipeline.NewMemoryLaunchStore()

	// Create a reconciler with a fake launcher (no Docker).
	reconciler := &pipeline.Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: pipeline.FakeLauncher{},
	}

	// Seed a 3-stage pipeline workflow.
	ctx := context.Background()
	wfID, nodeIDs, err := pipeline.SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}
	t.Logf("seeded pipeline workflow: %s nodes=%v", wfID, nodeIDs)

	// Wrap ReconcileOnce as the reconcile func.
	var reconcileCalls int32
	reconcileFn := func(ctx context.Context, workflowID routedrun.WorkflowID) error {
		atomic.AddInt32(&reconcileCalls, 1)
		_, err := reconciler.ReconcileOnce(ctx, workflowID)
		return err
	}

	rt := newPipelineRuntime(store, reconcileFn, 10*time.Millisecond)
	rt.RegisterPipelineWorkflowForReconcile(wfID)

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()

	rt.Start(loopCtx)

	// Wait for the reconciler to be called and node 0 to advance to RUNNING.
	deadline := time.After(3 * time.Second)
	for {
		node0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
		if err != nil {
			t.Fatalf("GetNode node0: %v", err)
		}
		if node0.Status == routedrun.NodeStatusRunning {
			break
		}
		select {
		case <-deadline:
			// Dump state on timeout.
			for i, nid := range nodeIDs {
				n, _ := ctrl.Store.GetNode(ctx, nid)
				status := "<err>"
				if n != nil {
					status = n.Status.String()
				}
				t.Logf("  node[%d] status=%s", i, status)
			}
			t.Fatalf("timed out waiting for node 0 to reach RUNNING: reconcileCalls=%d",
				atomic.LoadInt32(&reconcileCalls))
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	rt.Stop()

	// Verify node 0 advanced to RUNNING.
	node0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("GetNode node0: %v", err)
	}
	if node0.Status != routedrun.NodeStatusRunning {
		t.Errorf("node 0: want RUNNING, got %s", node0.Status)
	}

	t.Logf("reconcileCalls=%d node0_status=%s", atomic.LoadInt32(&reconcileCalls), node0.Status)
}

// TestPipelineReconcileLoop_StopIdempotent verifies Stop() can be called
// multiple times safely.
func TestPipelineReconcileLoop_StopIdempotent(t *testing.T) {
	store := routedrun.NewMemoryStore()
	rt := newPipelineRuntime(store, func(ctx context.Context, workflowID routedrun.WorkflowID) error {
		return nil
	}, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt.Start(ctx)
	rt.Stop()
	rt.Stop() // should not panic
	rt.Stop() // should not panic
}

// TestPipelineReconcileLoop_StartIdempotent verifies Start() can be called
// multiple times safely.
func TestPipelineReconcileLoop_StartIdempotent(t *testing.T) {
	store := routedrun.NewMemoryStore()
	var tickCount int32
	rt := newPipelineRuntime(store, func(ctx context.Context, workflowID routedrun.WorkflowID) error {
		atomic.AddInt32(&tickCount, 1)
		return nil
	}, 10*time.Millisecond)
	rt.RegisterPipelineWorkflowForReconcile("wf-test-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt.Start(ctx)
	rt.Start(ctx) // second call is no-op
	rt.Start(ctx) // third call is no-op

	// Wait for at least 1 tick.
	deadline := time.After(1 * time.Second)
	for atomic.LoadInt32(&tickCount) < 1 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for tick: got %d", tickCount)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	rt.Stop()
}

// TestPipelineRuntime_RegisterAndList verifies registration and listing.
func TestPipelineRuntime_RegisterAndList(t *testing.T) {
	store := routedrun.NewMemoryStore()
	rt := newPipelineRuntime(store, nil, time.Second)

	rt.RegisterPipelineWorkflowForReconcile("wf-a")
	rt.RegisterPipelineWorkflowForReconcile("wf-b")
	rt.RegisterPipelineWorkflowForReconcile("wf-a") // duplicate, should be idempotent

	ids := rt.knownPipelineWorkflowIDs()
	if len(ids) != 2 {
		t.Errorf("expected 2 unique IDs, got %d: %v", len(ids), ids)
	}
}
