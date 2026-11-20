package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline"
)

// TestPipelineOperatorSeam_ThreeStageNoHermes proves the Hermes-absent
// pipeline operator seam: seed a 3-stage pipeline, drive claim+ack+success
// with the controller, and assert the inspect summary shows 3 SUCCEEDED nodes.
//
// This is the Hermes-absent control-plane proof. Live stage containers are
// covered by the Docker e2e workstream.
//
// Also exercises the daemon-level reconcile loop by registering the pipeline
// workflow, starting the runtime, and letting it advance nodes via the
// reconciler with FakeLauncher.
func TestPipelineOperatorSeam_ThreeStageNoHermes(t *testing.T) {
	ctx := context.Background()

	// ── Part A: Library-level proof (controller + seed) ──
	t.Run("controller_seed_inspect", func(t *testing.T) {
		store := routedrun.NewMemoryStore()
		ctrl := pipeline.NewController(store)

		wfID, nodeIDs, err := pipeline.SeedPipelineWorkflow(ctx, ctrl, 3)
		if err != nil {
			t.Fatalf("SeedPipelineWorkflow: %v", err)
		}
		if len(nodeIDs) != 3 {
			t.Fatalf("expected 3 nodes, got %d", len(nodeIDs))
		}

		// Advance all 3 stages: claim → ack → success.
		for stage := 0; stage < 3; stage++ {
			claim, err := ctrl.ClaimNextReady(ctx, wfID)
			if err != nil {
				t.Fatalf("stage %d ClaimNextReady: %v", stage, err)
			}
			if claim == nil {
				t.Fatalf("stage %d ClaimNextReady: nil claim", stage)
			}
			if claim.NodeID != nodeIDs[stage] {
				t.Errorf("stage %d: claim.NodeID=%s want %s", stage, claim.NodeID, nodeIDs[stage])
			}

			if err := ctrl.AcknowledgeRunning(ctx, claim); err != nil {
				t.Fatalf("stage %d AcknowledgeRunning: %v", stage, err)
			}

			success := pipeline.StageSuccess{
				WorkflowID: wfID,
				NodeID:     claim.NodeID,
				RunID:      claim.RunID,
				AttemptID:  claim.Attempt.AttemptID,
			}
			if stage < 2 {
				success.Handoff = &routedrun.HandoffEnvelope{
					WorkflowID:   wfID,
					SourceNodeID: claim.NodeID,
					TargetNodeID: nodeIDs[stage+1],
					ContextJSON:  `{"stage":` + itoa(stage) + `,"status":"success"}`,
				}
			}

			if err := ctrl.CommitStageSuccess(ctx, success); err != nil {
				t.Fatalf("stage %d CommitStageSuccess: %v", stage, err)
			}
		}

		// Verify workflow SUCCEEDED.
		wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
		if err != nil {
			t.Fatalf("GetWorkflow: %v", err)
		}
		if wf.Status != routedrun.WorkflowStatusSucceeded {
			t.Errorf("workflow status: want SUCCEEDED, got %s", wf.Status)
		}

		// Inspect: 3 nodes, all SUCCEEDED.
		summary, err := pipeline.BuildPipelineInspect(ctx, ctrl.Store, wfID)
		if err != nil {
			t.Fatalf("BuildPipelineInspect: %v", err)
		}
		if len(summary.Nodes) != 3 {
			t.Fatalf("inspect: expected 3 nodes, got %d", len(summary.Nodes))
		}
		wantStatus := routedrun.NodeStatusSucceeded.String()
		for i, n := range summary.Nodes {
			if n.StageOrder != i {
				t.Errorf("Node[%d].StageOrder: want %d got %d", i, i, n.StageOrder)
			}
			if n.Status != wantStatus {
				t.Errorf("Node[%d].Status: want %s got %s", i, wantStatus, n.Status)
			}
		}

		t.Logf("=== Hermes-absent control-plane proof ===\n"+
			"  Workflow ID: %s\n"+
			"  Nodes: %d (all SUCCEEDED)\n"+
			"  Status: %s",
			wfID, len(summary.Nodes), wf.Status)
	})

	// ── Part B: Daemon-level reconcile loop with FakeLauncher ──
	t.Run("reconcile_loop_advances_launching_to_running", func(t *testing.T) {
		store := routedrun.NewMemoryStore()
		ctrl := pipeline.NewController(store)
		launches := pipeline.NewMemoryLaunchStore()

		reconciler := &pipeline.Reconciler{
			Ctrl:     ctrl,
			Launches: launches,
			Launcher: pipeline.FakeLauncher{},
		}

		wfID, nodeIDs, err := pipeline.SeedPipelineWorkflow(ctx, ctrl, 3)
		if err != nil {
			t.Fatalf("SeedPipelineWorkflow: %v", err)
		}

		var reconcileCalls int32
		rt := newPipelineRuntime(store, func(ctx context.Context, workflowID routedrun.WorkflowID) error {
			atomic.AddInt32(&reconcileCalls, 1)
			_, err := reconciler.ReconcileOnce(ctx, workflowID)
			return err
		}, 10*time.Millisecond)
		rt.RegisterPipelineWorkflowForReconcile(wfID)

		loopCtx, loopCancel := context.WithCancel(ctx)
		defer loopCancel()
		rt.Start(loopCtx)

		// Wait for the first node to advance to RUNNING.
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
				for i, nid := range nodeIDs {
					n, _ := ctrl.Store.GetNode(ctx, nid)
					s := "<err>"
					if n != nil {
						s = n.Status.String()
					}
					t.Logf("  node[%d]=%s", i, s)
				}
				t.Fatalf("timed out waiting for node 0 RUNNING: calls=%d", atomic.LoadInt32(&reconcileCalls))
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}

		rt.Stop()

		node0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
		if err != nil {
			t.Fatalf("GetNode node0: %v", err)
		}
		if node0.Status != routedrun.NodeStatusRunning {
			t.Errorf("node 0: want RUNNING, got %s", node0.Status)
		}

		t.Logf("reconcile loop: node0=%s calls=%d", node0.Status, atomic.LoadInt32(&reconcileCalls))
	})
}

// itoa is a minimal int-to-string helper for small test integers.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	n := v
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
