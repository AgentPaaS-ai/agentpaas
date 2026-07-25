package pipeline

import (
	"context"
	"runtime"
	"sort"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestReferenceProof_ThreeStageLinear documents the Hermes-absent library proof
// path: seed → claim+ack+commit for each stage → workflow SUCCEEDED →
// BuildPipelineInspect shows 3 nodes. No Docker required.
//
// This test asserts no duplicate launch keys are issued across stages,
// proving the idempotency-key generation is stable and stage-scoped.
//
// Hermes-absent library proof; Docker e2e residual T09.
func TestReferenceProof_ThreeStageLinear(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	// Seed: 3-stage workflow. Stage 0 READY, Stage 1-2 PENDING.
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}
	if len(nodeIDs) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodeIDs))
	}

	t.Logf("reference proof: workflow=%s nodes=%v", wfID, nodeIDs)

	// Track all launch keys to verify no duplicates.
	launchKeys := make(map[string]int)

	// Record snapshot/workflow IDs for documentation.
	snapshots := []string{string(wfID)}

	// Stage loop: claim → ack → success → next (or workflow done).
	for stage := 0; stage < 3; stage++ {
		// Claim the next READY node.
		claim, err := ctrl.ClaimNextReady(ctx, wfID)
		if err != nil {
			t.Fatalf("stage %d ClaimNextReady: %v", stage, err)
		}
		if claim == nil {
			t.Fatalf("stage %d ClaimNextReady: got nil claim", stage)
		}

		// Verify claim has expected node.
		if claim.NodeID != nodeIDs[stage] {
			t.Errorf("stage %d: claim.NodeID=%s, want %s", stage, claim.NodeID, nodeIDs[stage])
		}

		// Track launch key for duplicate detection.
		if prevStage, exists := launchKeys[claim.LaunchKey]; exists {
			t.Errorf("stage %d: duplicate launch key %q (also used at stage %d)",
				stage, claim.LaunchKey, prevStage)
		}
		launchKeys[claim.LaunchKey] = stage

		t.Logf("  stage %d: node=%s run=%s attempt=%s launchKey=%s",
			stage, claim.NodeID, claim.RunID, claim.Attempt.AttemptID, claim.LaunchKey)

		// Verify node is LAUNCHING.
		node, err := ctrl.Store.GetNode(ctx, claim.NodeID)
		if err != nil {
			t.Fatalf("stage %d GetNode post-claim: %v", stage, err)
		}
		if node.Status != routedrun.NodeStatusLaunching {
			t.Errorf("stage %d post-claim status: want LAUNCHING, got %s", stage, node.Status)
		}

		// Acknowledge running.
		if err := ctrl.AcknowledgeRunning(ctx, claim); err != nil {
			t.Fatalf("stage %d AcknowledgeRunning: %v", stage, err)
		}

		// Reconcile: verify node is RUNNING.
		node, err = ctrl.Store.GetNode(ctx, claim.NodeID)
		if err != nil {
			t.Fatalf("stage %d GetNode post-ack: %v", stage, err)
		}
		if node.Status != routedrun.NodeStatusRunning {
			t.Errorf("stage %d post-ack status: want RUNNING, got %s", stage, node.Status)
		}

		// Build success request.
		success := StageSuccess{
			WorkflowID: wfID,
			NodeID:     claim.NodeID,
			RunID:      claim.RunID,
			AttemptID:  claim.Attempt.AttemptID,
		}

		// Non-final stages require a handoff.
		if stage < 2 {
			nextNodeID := nodeIDs[stage+1]
			success.Handoff = &routedrun.HandoffEnvelope{
				WorkflowID:   wfID,
				SourceNodeID: claim.NodeID,
				TargetNodeID: nextNodeID,
				ContextJSON:  `{"stage":` + mustIntToStr(stage) + `,"status":"success"}`,
			}
		}

		// Commit stage success.
		if err := ctrl.CommitStageSuccess(ctx, success); err != nil {
			t.Fatalf("stage %d CommitStageSuccess: %v", stage, err)
		}

		snapshots = append(snapshots, "node:"+string(claim.NodeID)+":SUCCEEDED")
	}

	// Verify workflow is SUCCEEDED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusSucceeded {
		t.Errorf("workflow status: want SUCCEEDED, got %s", wf.Status)
	}

	// Inspect: verify 3 nodes, all SUCCEEDED.
	summary, err := BuildPipelineInspect(ctx, ctrl.Store, wfID)
	if err != nil {
		t.Fatalf("BuildPipelineInspect: %v", err)
	}
	if len(summary.Nodes) != 3 {
		t.Fatalf("inspect: expected 3 nodes, got %d", len(summary.Nodes))
	}

	wantStatus := routedrun.NodeStatusSucceeded.String()
	for i, n := range summary.Nodes {
		if n.StageOrder != i {
			t.Errorf("Node[%d].StageOrder: want %d, got %d", i, i, n.StageOrder)
		}
		if n.Status != wantStatus {
			t.Errorf("Node[%d].Status: want %s, got %s", i, wantStatus, n.Status)
		}
		t.Logf("  inspect node[%d]: id=%s stage=%d status=%s outcome=%s",
			i, n.NodeID, n.StageOrder, n.Status, n.Outcome)
	}

	// Verify handoff IDs collected.
	handoffs, err := ctrl.Store.ListHandoffs(ctx, wfID)
	if err != nil {
		t.Fatalf("ListHandoffs: %v", err)
	}
	if len(handoffs) != 2 {
		t.Errorf("expected 2 handoffs, got %d", len(handoffs))
	}

	// Verify nodes are ordered by stage in the store.
	nodes, err := ctrl.Store.ListNodes(ctx, wfID)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].StageOrder < nodes[j].StageOrder
	})
	for i, n := range nodes {
		if n.StageOrder != i {
			t.Errorf("node stage order: expected %d, got %d for NodeID=%s", i, n.StageOrder, n.NodeID)
		}
	}

	// Document the proof.
	t.Logf("=== B34 Reference Proof Complete ===")
	t.Logf("  Workflow ID: %s", wfID)
	t.Logf("  Nodes: %d (all SUCCEEDED)", len(nodes))
	t.Logf("  Handoffs committed: %d", len(handoffs))
	t.Logf("  Launch keys: %d (no duplicates)", len(launchKeys))
	t.Logf("  Snapshot IDs: %v", snapshots)
	t.Logf("  Go version: %s", runtime.Version())
	t.Logf("  Hermes-absent library proof: PASS")
}

// mustIntToStr converts an int to a string for JSON embedding.
// In production, use strconv.Itoa; this small helper avoids import churn.
func mustIntToStr(v int) string {
	// Simple implementation for small integers used in test fixtures.
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
