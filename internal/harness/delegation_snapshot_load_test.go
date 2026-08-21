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
		"snapshot":             snap,
		"binding_capabilities": map[string]string{"report.verify": "cap-token"},
		"network_alias":        "net-alias-test",
		"workflow_id":          snap.WorkflowID,
		"workflow_kind":        "pipeline",
		"live_call_forbidden":  true,
		"callee_ingress_allow": makeDelegationIngress(),
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

func marshalDelegationSidecar(t *testing.T, snap delegation.CommunicationSnapshot, alias string) []byte {
	t.Helper()
	sidecar := map[string]any{
		"snapshot":             snap,
		"binding_capabilities": map[string]string{"report.verify": "cap-token"},
		"network_alias":        alias,
		"workflow_id":          snap.WorkflowID,
		"workflow_kind":        "fanout",
		"callee_ingress_allow": makeDelegationIngress(),
	}
	data, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	return data
}

func TestLoadDelegationSnapshot_FilePathSetsTrustState(t *testing.T) {
	s := &harnessRPCServer{done: make(chan struct{})}
	snap := makeDelegationSnapshot()
	dir := t.TempDir()
	path := filepath.Join(dir, "delegation-snapshot.json")
	if err := os.WriteFile(path, marshalDelegationSidecar(t, snap, "net-alias-file"), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	if err := s.LoadDelegationSnapshot(path); err != nil {
		t.Fatalf("LoadDelegationSnapshot: %v", err)
	}
	dts := s.getDelegationTrustState()
	if dts == nil {
		t.Fatal("file path load must set trust state")
	}
	if dts.NetworkAlias != "net-alias-file" {
		t.Fatalf("NetworkAlias = %q, want net-alias-file", dts.NetworkAlias)
	}

	resp := s.handleRequest(rpcRequest{
		ID:     "req-file-delegate",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-file-path",
		},
	})
	if !resp.OK {
		t.Fatalf("delegate after file load failed: %s (code=%s)", resp.Error, resp.Code)
	}
}

func TestLoadDelegationSnapshotJSON_SetsTrustState(t *testing.T) {
	s := &harnessRPCServer{done: make(chan struct{})}
	snap := makeDelegationSnapshot()
	data := marshalDelegationSidecar(t, snap, "net-alias-json")

	if err := s.LoadDelegationSnapshotJSON(string(data)); err != nil {
		t.Fatalf("LoadDelegationSnapshotJSON: %v", err)
	}
	dts := s.getDelegationTrustState()
	if dts == nil {
		t.Fatal("JSON-only load must set trust state")
	}
	if dts.NetworkAlias != "net-alias-json" {
		t.Fatalf("NetworkAlias = %q, want net-alias-json", dts.NetworkAlias)
	}
	if len(dts.Snapshot.Bindings) == 0 {
		t.Fatal("expected bindings from JSON snapshot")
	}

	resp := s.handleRequest(rpcRequest{
		ID:     "req-json-delegate",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-json-only",
		},
	})
	if !resp.OK {
		t.Fatalf("delegate after JSON load failed: %s (code=%s)", resp.Error, resp.Code)
	}
}

func TestLoadDelegationSnapshot_EmptyBoth_NoTrustState(t *testing.T) {
	s := &harnessRPCServer{done: make(chan struct{})}
	if err := s.LoadDelegationSnapshot(""); err != nil {
		t.Fatalf("empty path: %v", err)
	}
	if err := s.LoadDelegationSnapshotJSON(""); err != nil {
		t.Fatalf("empty JSON: %v", err)
	}
	if s.getDelegationTrustState() != nil {
		t.Fatal("empty path and JSON must leave trust state unset")
	}

	resp := s.handleRequest(rpcRequest{
		ID:     "req-empty-both",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-empty-both",
		},
	})
	if resp.OK {
		t.Fatal("expected OK=false when both path and JSON are empty")
	}
	if resp.Code != "no_trust_state" {
		t.Errorf("expected code no_trust_state, got %q", resp.Code)
	}
}
