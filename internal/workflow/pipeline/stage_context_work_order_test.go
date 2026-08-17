package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

func TestCollectStageContextParams_WorkOrderOnly(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	rec := &Reconciler{
		Ctrl:     ctrl,
		Launches: NewMemoryLaunchStore(),
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

	// Prior stage carries a work order plus notes, smash, and conversation.
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim0.RunID,
		AttemptID:  claim0.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON: `{
				"work_order": {"task": "summarize", "doc_id": "d1"},
				"notes": "private A notes",
				"query": "smash-query",
				"conversation": [{"role":"user","text":"parent chat"}]
			}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess: %v", err)
	}

	claim1, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce stage1: %v", err)
	}
	if claim1 == nil {
		t.Fatal("ReconcileOnce stage1: nil claim")
	}

	params, err := CollectStageContextParams(ctx, store, claim1)
	if err != nil {
		t.Fatalf("CollectStageContextParams: %v", err)
	}
	raw := string(params.IncomingHandoffJSON)
	if raw == "" {
		t.Fatal("expected work-order IncomingHandoffJSON")
	}

	var got map[string]any
	if err := json.Unmarshal(params.IncomingHandoffJSON, &got); err != nil {
		t.Fatalf("unmarshal IncomingHandoffJSON: %v (%s)", err, raw)
	}
	if got["task"] != "summarize" || got["doc_id"] != "d1" {
		t.Fatalf("work order fields missing: %s", raw)
	}

	leaked := []string{
		"notes", "query", "conversation", "handoff_id", "workflow_id",
		"context_json", "source_node_id", "target_node_id", "work_order",
	}
	for _, key := range leaked {
		if _, ok := got[key]; ok {
			t.Errorf("IncomingHandoffJSON leaked %q: %s", key, raw)
		}
		if strings.Contains(raw, `"notes"`) && key == "notes" {
			t.Errorf("IncomingHandoffJSON still contains notes: %s", raw)
		}
	}
	if strings.Contains(raw, "private A notes") {
		t.Errorf("IncomingHandoffJSON leaked parent notes: %s", raw)
	}
	if strings.Contains(raw, "parent chat") {
		t.Errorf("IncomingHandoffJSON leaked parent conversation: %s", raw)
	}
	if strings.Contains(raw, "smash-query") {
		t.Errorf("IncomingHandoffJSON leaked smash field: %s", raw)
	}
}
