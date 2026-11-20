package pipeline

import (
	"context"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestBuildPipelineInspect_Success exercises BuildPipelineInspect after a
// successful two-stage pipeline to verify summary shows correct outcomes,
// handoff IDs, and node ordering.
func TestBuildPipelineInspect_Success(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Run stage 0.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage0: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady stage0: got nil claim")
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
			ContextJSON:  `{"result":"ok"}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage0: %v", err)
	}

	// Run stage 1 (final).
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage1: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ClaimNextReady stage1: got nil claim")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim1); err != nil {
		t.Fatalf("AcknowledgeRunning stage1: %v", err)
	}
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[1],
		RunID:      claim1.RunID,
		AttemptID:  claim1.Attempt.AttemptID,
		// Final stage: no handoff required.
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage1: %v", err)
	}

	// Inspect.
	summary, err := BuildPipelineInspect(ctx, ctrl.Store, wfID)
	if err != nil {
		t.Fatalf("BuildPipelineInspect: %v", err)
	}

	// Verify workflow-level fields.
	if summary.WorkflowID != wfID {
		t.Errorf("WorkflowID: want %s, got %s", wfID, summary.WorkflowID)
	}
	if summary.WorkflowKind != "pipeline" {
		t.Errorf("WorkflowKind: want pipeline, got %s", summary.WorkflowKind)
	}
	wantWfStatus := routedrun.WorkflowStatusSucceeded.String()
	if summary.Status != wantWfStatus {
		t.Errorf("Status: want %s, got %s", wantWfStatus, summary.Status)
	}

	// Verify nodes.
	if len(summary.Nodes) != 2 {
		t.Fatalf("len(Nodes): want 2, got %d", len(summary.Nodes))
	}
	for i, n := range summary.Nodes {
		if n.StageOrder != i {
			t.Errorf("Node[%d].StageOrder: want %d, got %d", i, i, n.StageOrder)
		}
	}

	// Stage 0 should be SUCCEEDED.
	wantNodeStatus := routedrun.NodeStatusSucceeded.String()
	wantRunStatus := routedrun.RunStatusSucceeded.String()
	if summary.Nodes[0].Status != wantNodeStatus {
		t.Errorf("Node[0].Status: want %s, got %s", wantNodeStatus, summary.Nodes[0].Status)
	}
	if summary.Nodes[0].Outcome != wantRunStatus {
		t.Errorf("Node[0].Outcome: want %s, got %s", wantRunStatus, summary.Nodes[0].Outcome)
	}

	// Stage 1 should be SUCCEEDED.
	if summary.Nodes[1].Status != wantNodeStatus {
		t.Errorf("Node[1].Status: want %s, got %s", wantNodeStatus, summary.Nodes[1].Status)
	}
	if summary.Nodes[1].Outcome != wantRunStatus {
		t.Errorf("Node[1].Outcome: want %s, got %s", wantRunStatus, summary.Nodes[1].Outcome)
	}

	// At least one handoff should be present.
	if len(summary.HandoffIDs) == 0 {
		t.Error("HandoffIDs should not be empty after a successful pipeline with handoff")
	}

	// No active node after success.
	if summary.ActiveNodeID != "" {
		t.Errorf("ActiveNodeID should be empty, got %s", summary.ActiveNodeID)
	}
}

// TestBuildPipelineInspect_MidFailure exercises BuildPipelineInspect after
// a mid-pipeline failure to verify terminal reason and correct node outcomes.
func TestBuildPipelineInspect_MidFailure(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())

	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Run stage 0 successfully.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage0: %v", err)
	}
	if claim0 == nil {
		t.Fatal("ClaimNextReady stage0: got nil claim")
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
			ContextJSON:  `{"result":"ok"}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess stage0: %v", err)
	}

	// Run stage 1 - fail it.
	claim1, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady stage1: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ClaimNextReady stage1: got nil claim")
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim1); err != nil {
		t.Fatalf("AcknowledgeRunning stage1: %v", err)
	}
	reason := routedrun.FailureUserCancelled
	if err := ctrl.CommitStageFailure(ctx, StageFailure{
		WorkflowID:    wfID,
		NodeID:        nodeIDs[1],
		RunID:         claim1.RunID,
		AttemptID:     claim1.Attempt.AttemptID,
		FailureReason: &reason,
	}); err != nil {
		t.Fatalf("CommitStageFailure stage1: %v", err)
	}

	// Inspect.
	summary, err := BuildPipelineInspect(ctx, ctrl.Store, wfID)
	if err != nil {
		t.Fatalf("BuildPipelineInspect: %v", err)
	}

	// Workflow should be FAILED.
	if summary.Status != routedrun.WorkflowStatusFailed.String() {
		t.Errorf("Status: want %s, got %s", routedrun.WorkflowStatusFailed.String(), summary.Status)
	}

	// Terminal reason should be present.
	if summary.TerminalReason == "" {
		t.Error("TerminalReason should not be empty after failure")
	}

	// Stage 0 SUCCEEDED, Stage 1 FAILED, Stage 2 PENDING.
	if len(summary.Nodes) != 3 {
		t.Fatalf("len(Nodes): want 3, got %d", len(summary.Nodes))
	}
	if summary.Nodes[0].Status != routedrun.NodeStatusSucceeded.String() {
		t.Errorf("Node[0].Status: want %s, got %s", routedrun.NodeStatusSucceeded.String(), summary.Nodes[0].Status)
	}
	if summary.Nodes[1].Status != routedrun.NodeStatusFailed.String() {
		t.Errorf("Node[1].Status: want %s, got %s", routedrun.NodeStatusFailed.String(), summary.Nodes[1].Status)
	}
	if summary.Nodes[2].Status != routedrun.NodeStatusPending.String() {
		t.Errorf("Node[2].Status: want %s, got %s", routedrun.NodeStatusPending.String(), summary.Nodes[2].Status)
	}
}
