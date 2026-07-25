package pipeline

import (
	"context"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestControllerTwoStageHappyPath exercises a two-stage pipeline end-to-end.
func TestControllerTwoStageHappyPath(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}
	if len(nodeIDs) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodeIDs))
	}

	// Verify stage0 is READY, stage1 is PENDING.
	n0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0: %v", err)
	}
	if n0.Status != routedrun.NodeStatusReady {
		t.Fatalf("node0 status: want READY, got %s", n0.Status)
	}
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("node1 status: want PENDING, got %s", n1.Status)
	}

	// Claim stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage0: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady stage0: got nil claim")
	}
	if claim0.NodeID != nodeIDs[0] {
		t.Fatalf("claim0 node: want %s, got %s", nodeIDs[0], claim0.NodeID)
	}
	if claim0.Attempt.Lease == nil {
		t.Fatal("claim0: lease is nil")
	}

	// Verify node0 is LAUNCHING.
	n0, err = ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0 post-claim: %v", err)
	}
	if n0.Status != routedrun.NodeStatusLaunching {
		t.Fatalf("node0 post-claim: want LAUNCHING, got %s", n0.Status)
	}

	// Acknowledge stage0 running.
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning stage0: %v", err)
	}

	// Verify node0 is RUNNING.
	n0, err = ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0 post-ack: %v", err)
	}
	if n0.Status != routedrun.NodeStatusRunning {
		t.Fatalf("node0 post-ack: want RUNNING, got %s", n0.Status)
	}

	// Verify run is RUNNING.
	run0, err := ctrl.Store.GetRun(ctx, claim0.RunID)
	if err != nil {
		t.Fatalf("get run0 post-ack: %v", err)
	}
	if run0.Status != routedrun.RunStatusRunning {
		t.Fatalf("run0 post-ack: want RUNNING, got %s", run0.Status)
	}

	// Commit stage0 success with handoff.
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

	// Verify stage0 SUCCEEDED, stage1 READY.
	n0, err = ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0 post-success: %v", err)
	}
	if n0.Status != routedrun.NodeStatusSucceeded {
		t.Fatalf("node0 post-success: want SUCCEEDED, got %s", n0.Status)
	}
	n1, err = ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1 post-success: %v", err)
	}
	if n1.Status != routedrun.NodeStatusReady {
		t.Fatalf("node1 post-success: want READY, got %s", n1.Status)
	}

	// Verify handoff exists.
	handoffs, err := ctrl.Store.ListHandoffs(ctx, wfID)
	if err != nil {
		t.Fatalf("ListHandoffs: %v", err)
	}
	if len(handoffs) != 1 {
		t.Fatalf("expected 1 handoff, got %d", len(handoffs))
	}

	// Claim stage1.
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage1: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ClaimNextReady stage1: got nil claim")
	}
	if claim1.NodeID != nodeIDs[1] {
		t.Fatalf("claim1 node: want %s, got %s", nodeIDs[1], claim1.NodeID)
	}

	// Acknowledge stage1 running.
	if err := ctrl.AcknowledgeRunning(ctx, claim1); err != nil {
		t.Fatalf("AcknowledgeRunning stage1: %v", err)
	}

	// Commit stage1 success (final stage, no handoff).
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
		t.Fatalf("workflow status: want SUCCEEDED, got %s", wf.Status)
	}

	// Verify stage1 SUCCEEDED.
	n1, err = ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1 post-final: %v", err)
	}
	if n1.Status != routedrun.NodeStatusSucceeded {
		t.Fatalf("node1 post-final: want SUCCEEDED, got %s", n1.Status)
	}
}

// TestControllerThreeStageHappyPath exercises a three-stage pipeline.
func TestControllerThreeStageHappyPath(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}
	if len(nodeIDs) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodeIDs))
	}

	// Stage 0: claim → ack → success with handoff.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage0: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady stage0: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning stage0: %v", err)
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
			ContextJSON:  `{"step":1}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage0: %v", err)
	}

	// Stage 1: claim → ack → success with handoff.
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage1: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ClaimNextReady stage1: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim1); err != nil {
		t.Fatalf("AcknowledgeRunning stage1: %v", err)
	}
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[1],
		RunID:      claim1.RunID,
		AttemptID:  claim1.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[1],
			TargetNodeID: nodeIDs[2],
			ContextJSON:  `{"step":2}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage1: %v", err)
	}

	// Stage 2: claim → ack → final success.
	claim2, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage2: %v", err)
	}
	if claim2 == nil {
		t.Fatal("ClaimNextReady stage2: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim2); err != nil {
		t.Fatalf("AcknowledgeRunning stage2: %v", err)
	}
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[2],
		RunID:      claim2.RunID,
		AttemptID:  claim2.Attempt.AttemptID,
		Handoff:    nil,
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage2: %v", err)
	}

	// Verify workflow SUCCEEDED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusSucceeded {
		t.Fatalf("workflow status: want SUCCEEDED, got %s", wf.Status)
	}
}

// TestDoubleClaimNextReady ensures a second claim while a node is already
// LAUNCHING returns nil,nil (no second claim issued).
func TestDoubleClaimNextReady(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// First claim: succeeds.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady #1: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady #1: nil")
	}

	// Second claim: stage0 is now LAUNCHING, so nothing READY.
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady #2: %v", err)
	}
	if claim1 != nil {
		t.Fatal("ClaimNextReady #2: expected nil, got claim")
	}
}

// TestDoubleCommitStageSuccess ensures idempotent commit.
func TestDoubleCommitStageSuccess(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	req := StageSuccess{
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
	}

	// First commit: succeeds.
	if err := ctrl.CommitStageSuccess(ctx, req); err != nil {
		t.Fatalf("CommitStageSuccess #1: %v", err)
	}

	// Second commit: idempotent — node already SUCCEEDED.
	if err := ctrl.CommitStageSuccess(ctx, req); err != nil {
		t.Fatalf("CommitStageSuccess #2: %v", err)
	}

	// Verify only one handoff created.
	handoffs, err := ctrl.Store.ListHandoffs(ctx, wfID)
	if err != nil {
		t.Fatalf("ListHandoffs: %v", err)
	}
	if len(handoffs) != 1 {
		t.Fatalf("expected 1 handoff, got %d", len(handoffs))
	}
}

// TestCommitStageSuccessWithoutHandoffNonFinal ensures error on missing handoff
// for a non-final stage.
func TestCommitStageSuccessWithoutHandoffNonFinal(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	// Commit without handoff on non-final stage.
	err = ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
		Handoff:    nil,
	})
	if err != ErrHandoffMissing {
		t.Fatalf("expected ErrHandoffMissing, got %v", err)
	}

	// Verify stage1 is still PENDING, not READY.
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("node1 status: want PENDING, got %s", n1.Status)
	}
}

// TestCommitStageFailure makes sure failure marks workflow failed and next node
// stays PENDING.
func TestCommitStageFailureMarkWorkflowFailed(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	if err := ctrl.CommitStageFailure(ctx, StageFailure{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
	}); err != nil {
		t.Fatalf("CommitStageFailure: %v", err)
	}

	// Workflow FAILED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusFailed {
		t.Fatalf("workflow status: want FAILED, got %s", wf.Status)
	}

	// Stage1 still PENDING.
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("node1 status: want PENDING, got %s", n1.Status)
	}
}

// TestClaimAfterFinal verifies that claiming after all stages complete returns nil.
func TestClaimAfterFinal(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Complete stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage0: %v", err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning stage0: %v", err)
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
			ContextJSON:  `{}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage0: %v", err)
	}

	// Complete stage1 (final).
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage1: %v", err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim1); err != nil {
		t.Fatalf("AcknowledgeRunning stage1: %v", err)
	}
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[1],
		RunID:      claim1.RunID,
		AttemptID:  claim1.Attempt.AttemptID,
		Handoff:    nil,
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage1: %v", err)
	}

	// Try to claim again — workflow is terminal.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady after final: %v", err)
	}
	if claim != nil {
		t.Fatal("ClaimNextReady after final: expected nil, got claim")
	}
}

// TestCASConflictSimulation ensures stale generation update fails closed.
func TestCASConflictSimulation(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim stage0 normally.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}

	// Try to update node with a stale generation (0 instead of 2).
	node, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	node.Status = routedrun.NodeStatusRunning
	if err := ctrl.Store.UpdateNode(ctx, node, 0); err == nil {
		t.Fatal("expected CAS conflict with stale generation 0, got nil")
	}

	// The proper path via AcknowledgeRunning should still work.
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning after stale attempt: %v", err)
	}
}

// TestRestartSimulation ensures re-seeding with completed stages doesn't duplicate.
func TestRestartSimulation(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Complete stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
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
			ContextJSON:  `{}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess: %v", err)
	}

	// Simulate restart: ClaimNextReady should pick up stage1, not re-claim stage0.
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady post-restart: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ClaimNextReady post-restart: expected claim for stage1")
	}
	if claim1.NodeID != nodeIDs[1] {
		t.Fatalf("post-restart: expected stage1 (%s), got %s", nodeIDs[1], claim1.NodeID)
	}
}