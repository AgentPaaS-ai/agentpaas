package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
)

func TestLoadDelegationSnapshot_LiveCallForbiddenDeniesWithBindings(t *testing.T) {
	s := setupDelegationServer(t)
	snap := makeDelegationSnapshot()
	if len(snap.Bindings) == 0 {
		t.Fatal("test snapshot must include bindings")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "delegation-snapshot.json")
	sidecar := map[string]any{
		"snapshot":              snap,
		"binding_capabilities":  map[string]string{"report.verify": "cap-token"},
		"network_alias":         "net-alias-test",
		"workflow_id":           snap.WorkflowID,
		"workflow_kind":         "pipeline",
		"live_call_forbidden":   true,
		"callee_ingress_allow":  makeDelegationIngress(),
	}
	data, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	if err := s.LoadDelegationSnapshot(path); err != nil {
		t.Fatalf("LoadDelegationSnapshot: %v", err)
	}
	dts := s.getDelegationTrustState()
	if dts == nil {
		t.Fatal("expected trust state after load")
	}
	if !dts.LiveCallForbidden {
		t.Fatal("snapshot live_call_forbidden must set LiveCallForbidden")
	}
	if len(dts.Snapshot.Bindings) == 0 {
		t.Fatal("bindings must still be present; the bit is what denies")
	}

	resp := s.handleRequest(rpcRequest{
		ID:     "req-snap-forbidden",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-snap-forbidden",
		},
	})
	if !resp.OK {
		t.Fatalf("LiveCallForbidden must return a DENIED task, not an RPC error: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	if status, _ := result["status"].(string); status != delegation.TaskStatusDenied.String() {
		t.Fatalf("expected status DENIED, got %q", status)
	}
	taskID, _ := result["task_id"].(string)
	task, err := dts.Store.GetTask(context.Background(), delegation.TaskID(taskID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.DenialReason != "pipeline_or_child" {
		t.Errorf("DenialReason = %q, want pipeline_or_child", task.DenialReason)
	}
}
