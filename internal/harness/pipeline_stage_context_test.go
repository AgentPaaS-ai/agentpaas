package harness

import (
	"encoding/json"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline"
)

// TestPipelineStageContextFromParams verifies that PipelineStageContextFromParams
// correctly converts stage0 context (available=false for workflow_input)
// and mid-stage context with handoff JSON (available=true).
func TestPipelineStageContextFromParams(t *testing.T) {
	// Stage0: empty handoff, not final.
	params0 := pipeline.StageContextParams{
		WorkflowKind:        "pipeline",
		NodeID:              "node-0",
		StageOrder:          0,
		IsFinalStage:        false,
		IncomingHandoffJSON: nil,
		Classification:      "internal",
		ToNodeID:            "node-1",
	}
	ctx0 := PipelineStageContextFromParams(params0)
	if ctx0.WorkflowKind != "pipeline" {
		t.Fatalf("stage0: want WorkflowKind=pipeline, got %s", ctx0.WorkflowKind)
	}
	if ctx0.StageOrder != 0 {
		t.Fatalf("stage0: want StageOrder=0, got %d", ctx0.StageOrder)
	}
	if ctx0.IsFinalStage {
		t.Fatal("stage0: expected IsFinalStage=false")
	}
	if ctx0.IncomingHandoffJSON != nil {
		t.Fatalf("stage0: expected nil IncomingHandoffJSON, got %s", string(ctx0.IncomingHandoffJSON))
	}

	// Verify that stage0 context would make workflow_input return available=false.
	// (Logic is in handleWorkflowInput: StageOrder==0 || IncomingHandoffJSON==nil → available=false)
	available := ctx0.StageOrder != 0 && ctx0.IncomingHandoffJSON != nil
	if available {
		t.Fatal("stage0: expected available=false")
	}

	// Mid-stage: with handoff JSON.
	handoffData := json.RawMessage(`{"result":"ok"}`)
	params1 := pipeline.StageContextParams{
		WorkflowKind:        "pipeline",
		NodeID:              "node-1",
		StageOrder:          1,
		IsFinalStage:        true,
		IncomingHandoffJSON: handoffData,
		Classification:      "internal",
		ToNodeID:            "",
	}
	ctx1 := PipelineStageContextFromParams(params1)
	if ctx1.StageOrder != 1 {
		t.Fatalf("mid-stage: want StageOrder=1, got %d", ctx1.StageOrder)
	}
	if !ctx1.IsFinalStage {
		t.Fatal("mid-stage: expected IsFinalStage=true")
	}
	if string(ctx1.IncomingHandoffJSON) != `{"result":"ok"}` {
		t.Fatalf("mid-stage: want handoff '{\"result\":\"ok\"}', got %s", string(ctx1.IncomingHandoffJSON))
	}

	// Verify that mid-stage context would make workflow_input return available=true.
	available = ctx1.StageOrder != 0 && ctx1.IncomingHandoffJSON != nil
	if !available {
		t.Fatal("mid-stage: expected available=true")
	}
}
