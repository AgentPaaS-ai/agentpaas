package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// T07 Chunk 1 — Failure + Cancel terminal mapping tests
// ---------------------------------------------------------------------------

// TestFailStage0_NoNextReady: failure at stage0 marks workflow FAILED,
// stage1 stays PENDING, no next READY.
func TestFailStage0_NoNextReady(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Stage0 is READY, claim and ack.
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

	// Fail stage0.
	reason := routedrun.FailureAgentException
	if err := ctrl.CommitStageFailure(ctx, StageFailure{
		WorkflowID:    wfID,
		NodeID:        nodeIDs[0],
		RunID:         claim0.RunID,
		AttemptID:     claim0.Attempt.AttemptID,
		FailureReason: &reason,
	}); err != nil {
		t.Fatalf("CommitStageFailure stage0: %v", err)
	}

	// Workflow FAILED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusFailed {
		t.Fatalf("workflow: want FAILED, got %s", wf.Status)
	}
	if wf.TerminalReason == nil || *wf.TerminalReason != routedrun.FailureAgentException {
		t.Fatalf("workflow TerminalReason: want AGENT_EXCEPTION, got %v", wf.TerminalReason)
	}

	// Stage1 stays PENDING.
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("node1: want PENDING, got %s", n1.Status)
	}

	// Stage2 stays PENDING.
	n2, err := ctrl.Store.GetNode(ctx, nodeIDs[2])
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if n2.Status != routedrun.NodeStatusPending {
		t.Fatalf("node2: want PENDING, got %s", n2.Status)
	}

	// ClaimNextReady returns nil (workflow terminal).
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady after fail: %v", err)
	}
	if claim != nil {
		t.Fatal("ClaimNextReady after fail: expected nil")
	}
}

// TestFailStage1_MidPipeline_NoStage2: failure at stage1 (mid pipeline) marks
// workflow FAILED, stage2 stays PENDING.
func TestFailStage1_MidPipeline_NoStage2(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Complete stage0 successfully.
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

	// Stage1 should be READY.
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusReady {
		t.Fatalf("node1 post stage0: want READY, got %s", n1.Status)
	}

	// Claim and ack stage1.
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage1: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ClaimNextReady stage1: nil")
	}
	if claim1.NodeID != nodeIDs[1] {
		t.Fatalf("claim1: want node1, got %s", claim1.NodeID)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim1); err != nil {
		t.Fatalf("AcknowledgeRunning stage1: %v", err)
	}

	// Fail stage1.
	reason := routedrun.FailurePolicyDenied
	if err := ctrl.CommitStageFailure(ctx, StageFailure{
		WorkflowID:    wfID,
		NodeID:        nodeIDs[1],
		RunID:         claim1.RunID,
		AttemptID:     claim1.Attempt.AttemptID,
		FailureReason: &reason,
	}); err != nil {
		t.Fatalf("CommitStageFailure stage1: %v", err)
	}

	// Workflow FAILED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusFailed {
		t.Fatalf("workflow: want FAILED, got %s", wf.Status)
	}

	// Stage2 stays PENDING.
	n2, err := ctrl.Store.GetNode(ctx, nodeIDs[2])
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if n2.Status != routedrun.NodeStatusPending {
		t.Fatalf("node2: want PENDING, got %s", n2.Status)
	}

	// No further claim possible.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady after fail: %v", err)
	}
	if claim != nil {
		t.Fatal("ClaimNextReady after fail: expected nil")
	}
}

// TestFailFinalStage_WorkflowFailed: failure at final stage marks workflow FAILED
// with TerminalReason.
func TestFailFinalStage_WorkflowFailed(t *testing.T) {
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

	// Claim and ack final stage.
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

	// Fail final stage.
	reason := routedrun.FailureExternalDependencyFailed
	if err := ctrl.CommitStageFailure(ctx, StageFailure{
		WorkflowID:    wfID,
		NodeID:        nodeIDs[1],
		RunID:         claim1.RunID,
		AttemptID:     claim1.Attempt.AttemptID,
		FailureReason: &reason,
	}); err != nil {
		t.Fatalf("CommitStageFailure final: %v", err)
	}

	// Workflow FAILED with correct reason.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusFailed {
		t.Fatalf("workflow: want FAILED, got %s", wf.Status)
	}
	if wf.TerminalReason == nil || *wf.TerminalReason != routedrun.FailureExternalDependencyFailed {
		t.Fatalf("workflow TerminalReason: want EXTERNAL_DEPENDENCY_FAILED, got %v", wf.TerminalReason)
	}

	// No claim possible.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady after fail: %v", err)
	}
	if claim != nil {
		t.Fatal("ClaimNextReady after fail: expected nil")
	}
}

// TestCancelWhileRunning_NoNext: cancel while stage is RUNNING marks node
// CANCELLED, workflow CANCELLED, no next READY.
func TestCancelWhileRunning_NoNext(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0 (now RUNNING).
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

	// Cancel workflow while stage0 is RUNNING.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
		Reason:     "USER_CANCELLED",
	}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// Node0 is CANCELLED.
	n0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0: %v", err)
	}
	if n0.Status != routedrun.NodeStatusCancelled {
		t.Fatalf("node0: want CANCELLED, got %s", n0.Status)
	}

	// Run is CANCELLED.
	run0, err := ctrl.Store.GetRun(ctx, claim0.RunID)
	if err != nil {
		t.Fatalf("get run0: %v", err)
	}
	if run0.Status != routedrun.RunStatusCancelled {
		t.Fatalf("run0: want CANCELLED, got %s", run0.Status)
	}

	// Attempt is CANCELLED.
	att0, err := ctrl.Store.GetAttempt(ctx, claim0.Attempt.AttemptID)
	if err != nil {
		t.Fatalf("get attempt0: %v", err)
	}
	if att0.Status != routedrun.AttemptStatusCancelled {
		t.Fatalf("attempt0: want CANCELLED, got %s", att0.Status)
	}

	// Workflow CANCELLED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusCancelled {
		t.Fatalf("workflow: want CANCELLED, got %s", wf.Status)
	}

	// Stage1 stays PENDING, not READY.
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("node1: want PENDING, got %s", n1.Status)
	}

	// Stage2 stays PENDING.
	n2, err := ctrl.Store.GetNode(ctx, nodeIDs[2])
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if n2.Status != routedrun.NodeStatusPending {
		t.Fatalf("node2: want PENDING, got %s", n2.Status)
	}

	// No claim possible.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady after cancel: %v", err)
	}
	if claim != nil {
		t.Fatal("ClaimNextReady after cancel: expected nil")
	}
}

// TestCancelWhileLaunching: cancel while node is LAUNCHING (claimed but not acked).
func TestCancelWhileLaunching(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim stage0 (now LAUNCHING).
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage0: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady stage0: nil")
	}

	// Verify LAUNCHING.
	n0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0: %v", err)
	}
	if n0.Status != routedrun.NodeStatusLaunching {
		t.Fatalf("node0: want LAUNCHING, got %s", n0.Status)
	}

	// Cancel while LAUNCHING.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// Node0 is CANCELLED.
	n0, err = ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0 post-cancel: %v", err)
	}
	if n0.Status != routedrun.NodeStatusCancelled {
		t.Fatalf("node0: want CANCELLED, got %s", n0.Status)
	}

	// Run is CANCELLED.
	run0, err := ctrl.Store.GetRun(ctx, claim0.RunID)
	if err != nil {
		t.Fatalf("get run0: %v", err)
	}
	if run0.Status != routedrun.RunStatusCancelled {
		t.Fatalf("run0: want CANCELLED, got %s", run0.Status)
	}

	// Attempt is CANCELLED.
	att0, err := ctrl.Store.GetAttempt(ctx, claim0.Attempt.AttemptID)
	if err != nil {
		t.Fatalf("get attempt0: %v", err)
	}
	if att0.Status != routedrun.AttemptStatusCancelled {
		t.Fatalf("attempt0: want CANCELLED, got %s", att0.Status)
	}

	// Workflow CANCELLED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusCancelled {
		t.Fatalf("workflow: want CANCELLED, got %s", wf.Status)
	}
}

// TestCancelIdempotent: repeated CancelWorkflow is idempotent.
func TestCancelIdempotent(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	// First cancel.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
		Reason:     "USER_CANCELLED",
	}); err != nil {
		t.Fatalf("CancelWorkflow #1: %v", err)
	}

	// Second cancel: idempotent.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
		Reason:     "USER_CANCELLED",
	}); err != nil {
		t.Fatalf("CancelWorkflow #2: %v", err)
	}

	// Workflow still CANCELLED, node still CANCELLED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusCancelled {
		t.Fatalf("workflow: want CANCELLED, got %s", wf.Status)
	}

	n0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0: %v", err)
	}
	if n0.Status != routedrun.NodeStatusCancelled {
		t.Fatalf("node0: want CANCELLED, got %s", n0.Status)
	}

	// Stage1 still PENDING.
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("node1: want PENDING, got %s", n1.Status)
	}
}

// TestDoubleCommitStageFailureIdempotent: repeated CommitStageFailure is idempotent.
func TestDoubleCommitStageFailureIdempotent(t *testing.T) {
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
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	reason := routedrun.FailureAgentException
	req := StageFailure{
		WorkflowID:    wfID,
		NodeID:        nodeIDs[0],
		RunID:         claim0.RunID,
		AttemptID:     claim0.Attempt.AttemptID,
		FailureReason: &reason,
	}

	// First failure.
	if err := ctrl.CommitStageFailure(ctx, req); err != nil {
		t.Fatalf("CommitStageFailure #1: %v", err)
	}

	// Second failure: idempotent.
	if err := ctrl.CommitStageFailure(ctx, req); err != nil {
		t.Fatalf("CommitStageFailure #2: %v", err)
	}

	// Node still FAILED.
	n0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0: %v", err)
	}
	if n0.Status != routedrun.NodeStatusFailed {
		t.Fatalf("node0: want FAILED, got %s", n0.Status)
	}

	// Workflow still FAILED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusFailed {
		t.Fatalf("workflow: want FAILED, got %s", wf.Status)
	}
}

// TestLateSuccessAfterCancelRejected: CommitStageSuccess after cancel is rejected.
func TestLateSuccessAfterCancelRejected(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	// Cancel.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// Try to commit stage success after cancel: should be rejected.
	err = ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{"late":true}`,
		},
	})
	if err == nil {
		t.Fatal("CommitStageSuccess after cancel: expected error, got nil")
	}

	// Workflow still CANCELLED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusCancelled {
		t.Fatalf("workflow: want CANCELLED, got %s", wf.Status)
	}

	// Node still CANCELLED.
	n0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0: %v", err)
	}
	if n0.Status != routedrun.NodeStatusCancelled {
		t.Fatalf("node0: want CANCELLED, got %s", n0.Status)
	}
}

// TestLateSuccessAfterFailRejected: CommitStageSuccess after failure is rejected.
func TestLateSuccessAfterFailRejected(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	// Fail stage0.
	if err := ctrl.CommitStageFailure(ctx, StageFailure{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
	}); err != nil {
		t.Fatalf("CommitStageFailure: %v", err)
	}

	// Try to commit stage success after failure: should be rejected.
	err = ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{"late":true}`,
		},
	})
	if err == nil {
		t.Fatal("CommitStageSuccess after fail: expected error, got nil")
	}

	// Workflow still FAILED.
	wf, err := ctrl.Store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusFailed {
		t.Fatalf("workflow: want FAILED, got %s", wf.Status)
	}
}

// TestReconcileOnceAfterCancel_Nothing: reconcile after cancel returns nil claim.
func TestReconcileOnceAfterCancel_Nothing(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	// Cancel.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// Simulate daemon restart reconcile: ClaimNextReady should return nil.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady after cancel: %v", err)
	}
	if claim != nil {
		t.Fatal("ClaimNextReady after cancel: expected nil")
	}
}

// TestCancelThenPauseDesiredState_StillCancelled: cancel with pause requested
// should stay CANCELLED.
func TestCancelThenPauseDesiredState_StillCancelled(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	// Request pause.
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlPause,
		IdempotencyKey: "pause-before-cancel",
	}); err != nil {
		t.Fatalf("RequestControl: %v", err)
	}

	// Set workflow to PAUSE_REQUESTED.
	wf, err := store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPauseRequested
	wf.UpdatedAt = time.Now().UTC()
	if err := store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}

	// Cancel the workflow.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// Workflow should be CANCELLED, not PAUSED.
	wf, err = store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusCancelled {
		t.Fatalf("workflow: want CANCELLED, got %s", wf.Status)
	}

	// Node is CANCELLED.
	n0, err := store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("get node0: %v", err)
	}
	if n0.Status != routedrun.NodeStatusCancelled {
		t.Fatalf("node0: want CANCELLED, got %s", n0.Status)
	}
}

// ---------------------------------------------------------------------------
// T07 Chunk 2 — Resume from PAUSED + race tests
// ---------------------------------------------------------------------------

// TestPauseThenResume_NextStageReadyOnce: pause after stage0 success,
// then resume marks exactly one next stage READY.
func TestPauseThenResume_NextStageReadyOnce(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0.
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

	// Request pause.
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlPause,
		IdempotencyKey: "pause-resume-test",
	}); err != nil {
		t.Fatalf("RequestControl: %v", err)
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

	// Commit stage0 success → workflow PAUSED, stage1 stays PENDING.
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

	// Verify PAUSED.
	wf, err = store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusPaused {
		t.Fatalf("workflow: want PAUSED, got %s", wf.Status)
	}

	// Verify stage1 PENDING, stage2 PENDING.
	n1, err := store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("node1: want PENDING, got %s", n1.Status)
	}
	n2, err := store.GetNode(ctx, nodeIDs[2])
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if n2.Status != routedrun.NodeStatusPending {
		t.Fatalf("node2: want PENDING, got %s", n2.Status)
	}

	// Clear desired state (simulate control-plane processing resume command).
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlResume,
		IdempotencyKey: "resume-ctrl",
	}); err != nil {
		t.Fatalf("RequestControl resume: %v", err)
	}

	// Resume workflow.
	if err := ctrl.ResumeWorkflow(ctx, ResumeRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("ResumeWorkflow: %v", err)
	}

	// Workflow RUNNING.
	wf, err = store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow post-resume: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusRunning {
		t.Fatalf("workflow post-resume: want RUNNING, got %s", wf.Status)
	}

	// Stage1 is READY (exactly one).
	n1, err = store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1 post-resume: %v", err)
	}
	if n1.Status != routedrun.NodeStatusReady {
		t.Fatalf("node1 post-resume: want READY, got %s", n1.Status)
	}

	// Stage2 still PENDING.
	n2, err = store.GetNode(ctx, nodeIDs[2])
	if err != nil {
		t.Fatalf("get node2 post-resume: %v", err)
	}
	if n2.Status != routedrun.NodeStatusPending {
		t.Fatalf("node2 post-resume: want PENDING, got %s", n2.Status)
	}

	// ClaimNextReady should return stage1.
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady post-resume: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ClaimNextReady post-resume: expected claim for stage1")
	}
	if claim1.NodeID != nodeIDs[1] {
		t.Fatalf("claim1: want node1, got %s", claim1.NodeID)
	}
}

// TestResumeIdempotent: calling ResumeWorkflow twice on a PAUSED workflow
// only marks the next stage READY once.
func TestResumeIdempotent(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Complete stage0 under pause → PAUSED.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlPause,
		IdempotencyKey: "pause-idem",
	}); err != nil {
		t.Fatalf("RequestControl: %v", err)
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

	// Clear desired state.
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlResume,
		IdempotencyKey: "resume-idem",
	}); err != nil {
		t.Fatalf("RequestControl resume: %v", err)
	}

	// First resume.
	if err := ctrl.ResumeWorkflow(ctx, ResumeRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("ResumeWorkflow #1: %v", err)
	}

	// Verify stage1 READY.
	n1, err := store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusReady {
		t.Fatalf("node1: want READY, got %s", n1.Status)
	}

	// Second resume: idempotent, no error.
	if err := ctrl.ResumeWorkflow(ctx, ResumeRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("ResumeWorkflow #2: %v", err)
	}

	// Workflow still RUNNING.
	wf, err = store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusRunning {
		t.Fatalf("workflow: want RUNNING, got %s", wf.Status)
	}

	// Stage1 still READY, not double-advanced.
	n1, err = store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusReady {
		t.Fatalf("node1: want READY, got %s", n1.Status)
	}

	// Stage2 still PENDING.
	n2, err := store.GetNode(ctx, nodeIDs[2])
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if n2.Status != routedrun.NodeStatusPending {
		t.Fatalf("node2: want PENDING, got %s", n2.Status)
	}
}

// TestResumeRejectedWhenCancelled: ResumeWorkflow on a CANCELLED workflow
// returns an error.
func TestResumeRejectedWhenCancelled(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Cancel the workflow.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// Resume should be rejected.
	err = ctrl.ResumeWorkflow(ctx, ResumeRequest{
		WorkflowID: wfID,
	})
	if err == nil {
		t.Fatal("ResumeWorkflow on cancelled: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ResumeWorkflow on cancelled: want ErrInvalidTransition, got %v", err)
	}
}

// TestResumeRejectedWhenFailed: ResumeWorkflow on a FAILED workflow
// returns an error.
func TestResumeRejectedWhenFailed(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0, then fail.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
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

	// Resume should be rejected.
	err = ctrl.ResumeWorkflow(ctx, ResumeRequest{
		WorkflowID: wfID,
	})
	if err == nil {
		t.Fatal("ResumeWorkflow on failed: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ResumeWorkflow on failed: want ErrInvalidTransition, got %v", err)
	}
}

// TestCancelBeatsPause: pause requested before any claim,
// then cancel arrives — cancel wins, workflow CANCELLED.
func TestCancelBeatsPause(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)

	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Request pause.
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlPause,
		IdempotencyKey: "pause-pre",
	}); err != nil {
		t.Fatalf("RequestControl: %v", err)
	}

	// Set workflow to PAUSE_REQUESTED.
	wf, err := store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPauseRequested
	wf.UpdatedAt = time.Now().UTC()
	if err := store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}

	// Cancel the workflow — no node claimed yet.
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{
		WorkflowID: wfID,
		Reason:     "USER_CANCELLED",
	}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// Workflow CANCELLED, not PAUSED.
	wf, err = store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusCancelled {
		t.Fatalf("workflow: want CANCELLED, got %s", wf.Status)
	}

	// Resume should be rejected.
	err = ctrl.ResumeWorkflow(ctx, ResumeRequest{
		WorkflowID: wfID,
	})
	if err == nil {
		t.Fatal("ResumeWorkflow after cancel: expected error, got nil")
	}
}

// TestResumeAfterControllerRestart_NoDoubleReady: a new controller with the
// same store resumes a paused workflow exactly once.
func TestResumeAfterControllerRestart_NoDoubleReady(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Complete stage0 under pause → PAUSED.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlPause,
		IdempotencyKey: "pause-restart",
	}); err != nil {
		t.Fatalf("RequestControl: %v", err)
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

	// Clear desired state.
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlResume,
		IdempotencyKey: "resume-restart",
	}); err != nil {
		t.Fatalf("RequestControl resume: %v", err)
	}

	// Simulate daemon restart: new controller, same store.
	ctrl2 := NewController(store)

	// Resume via new controller.
	if err := ctrl2.ResumeWorkflow(ctx, ResumeRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("ResumeWorkflow: %v", err)
	}

	// Workflow RUNNING.
	wf, err = store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusRunning {
		t.Fatalf("workflow: want RUNNING, got %s", wf.Status)
	}

	// Stage1 READY.
	n1, err := store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusReady {
		t.Fatalf("node1: want READY, got %s", n1.Status)
	}

	// Stage2 PENDING (not also READY).
	n2, err := store.GetNode(ctx, nodeIDs[2])
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if n2.Status != routedrun.NodeStatusPending {
		t.Fatalf("node2: want PENDING, got %s", n2.Status)
	}
}

// TestPauseDuringStage_ResumeLaunchesNext: pause during stage0,
// commit PAUSED, resume → next stage ready and claimable.
func TestPauseDuringStage_ResumeLaunchesNext(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Claim and ack stage0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady: nil")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	// Request pause during stage0.
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlPause,
		IdempotencyKey: "pause-during",
	}); err != nil {
		t.Fatalf("RequestControl: %v", err)
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

	// Stage0 succeeds → PAUSED, stage1 stays PENDING.
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
		t.Fatalf("CommitStageSuccess under pause: %v", err)
	}

	// Verify PAUSED + stage1 PENDING.
	wf, err = store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusPaused {
		t.Fatalf("workflow: want PAUSED, got %s", wf.Status)
	}
	n1, err := store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("node1: want PENDING, got %s", n1.Status)
	}

	// Clear desired state (simulate control plane processing resume).
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlResume,
		IdempotencyKey: "resume-during",
	}); err != nil {
		t.Fatalf("RequestControl resume: %v", err)
	}

	// Resume.
	if err := ctrl.ResumeWorkflow(ctx, ResumeRequest{
		WorkflowID: wfID,
	}); err != nil {
		t.Fatalf("ResumeWorkflow: %v", err)
	}

	// Workflow RUNNING.
	wf, err = store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow post-resume: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusRunning {
		t.Fatalf("workflow post-resume: want RUNNING, got %s", wf.Status)
	}

	// Stage1 is now READY.
	n1, err = store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("get node1 post-resume: %v", err)
	}
	if n1.Status != routedrun.NodeStatusReady {
		t.Fatalf("node1 post-resume: want READY, got %s", n1.Status)
	}

	// ClaimNextReady works.
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady post-resume: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ClaimNextReady post-resume: expected claim for stage1")
	}
	if claim1.NodeID != nodeIDs[1] {
		t.Fatalf("claim1: want node1, got %s", claim1.NodeID)
	}
}
