package harness

import (
	"context"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
)

func setupToolDelegationServer(t *testing.T, snap delegation.CommunicationSnapshot, liveCallForbidden bool) *harnessRPCServer {
	t.Helper()
	s := &harnessRPCServer{
		done: make(chan struct{}),
	}
	store := delegation.NewMemoryStore()
	ingress := []delegation.CalleeIngressRule{
		{
			CallerPackageName:   snap.CallerPackageName,
			CallerPackageDigest: snap.CallerPackageDigest,
			AllowedBindings:     bindingIDs(snap),
			MaxDataClass:        "internal",
		},
	}
	dts := &DelegationTrustState{
		Snapshot:            snap,
		BindingCapabilities: map[string]string{},
		NetworkAlias:        "net-alias-tool",
		Store:               store,
		CalleeIngressAllow:  ingress,
		LiveCallForbidden:   liveCallForbidden,
	}
	if len(snap.Bindings) > 0 {
		dts.BindingCapabilities[snap.Bindings[0].BindingID] = "cap-tool-token"
	}
	s.setDelegationTrustState(dts)
	return s
}

func phoneCallToolSnapshot() delegation.CommunicationSnapshot {
	snap := delegation.CommunicationSnapshot{
		SchemaVersion:       delegation.CurrentSchemaVersion,
		SnapshotGeneration:  1,
		WorkflowID:          "wf-phone-tool",
		TenantID:            "tenant-tool",
		CallerDeploymentID:  "dep-tool-caller",
		CallerPackageName:   "lookup-tool",
		CallerPackageDigest: "sha256:tool-caller",
		Bindings: []delegation.WorkflowDelegationBinding{
			{
				BindingID:            "dep-agent-peer",
				CalleePackageName:    "research-agent",
				CalleePackageVersion: "1.0.0",
				CalleeBundleDigest:   "sha256:agent-peer",
				CallerPackageName:    "lookup-tool",
				MaxDataClass:         "internal",
			},
		},
	}
	dg, _ := delegation.ComputeSnapshotDigest(&snap)
	snap.SnapshotDigest = dg
	return snap
}

func bindingIDs(snap delegation.CommunicationSnapshot) []string {
	ids := make([]string, 0, len(snap.Bindings))
	for _, b := range snap.Bindings {
		ids = append(ids, b.BindingID)
	}
	return ids
}

func TestDelegateTask_ToolPhoneCallSnapshotAdmits(t *testing.T) {
	t.Setenv("AGENTPAAS_AGENT_KIND", "tool")
	s := setupToolDelegationServer(t, phoneCallToolSnapshot(), false)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-tool-phone",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "dep-agent-peer",
			"idempotency_key": "idem-tool-phone",
		},
	})
	if !resp.OK {
		t.Fatalf("tool + phone-call snapshot must admit delegate_task: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	status, _ := result["status"].(string)
	if status != delegation.TaskStatusAdmitted.String() {
		t.Fatalf("expected status ADMITTED, got %q", status)
	}
	taskID, _ := result["task_id"].(string)
	dts := s.getDelegationTrustState()
	task, err := dts.Store.GetTask(context.Background(), delegation.TaskID(taskID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != delegation.TaskStatusAdmitted {
		t.Errorf("stored task status = %s, want ADMITTED", task.Status)
	}
}

func TestDelegateTask_StandaloneToolDenied(t *testing.T) {
	t.Setenv("AGENTPAAS_AGENT_KIND", "tool")
	snap := phoneCallToolSnapshot()
	snap.WorkflowID = ""
	s := setupToolDelegationServer(t, snap, true)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-tool-standalone",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "dep-agent-peer",
			"idempotency_key": "idem-tool-standalone",
		},
	})
	if !resp.OK {
		t.Fatalf("standalone tool must return a DENIED task, not an RPC error: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	status, _ := result["status"].(string)
	if status != delegation.TaskStatusDenied.String() {
		t.Fatalf("expected status DENIED, got %q", status)
	}
	taskID, _ := result["task_id"].(string)
	dts := s.getDelegationTrustState()
	task, err := dts.Store.GetTask(context.Background(), delegation.TaskID(taskID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.DenialReason != "standalone" {
		t.Errorf("DenialReason = %q, want standalone", task.DenialReason)
	}
}

func TestDelegateTask_ToolUndeclaredPeerDenied(t *testing.T) {
	t.Setenv("AGENTPAAS_AGENT_KIND", "tool")
	s := setupToolDelegationServer(t, phoneCallToolSnapshot(), false)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-tool-undeclared",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "dep-not-on-list",
			"idempotency_key": "idem-tool-undeclared",
		},
	})
	if !resp.OK {
		t.Fatalf("undeclared peer must return a DENIED task, not an RPC error: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	status, _ := result["status"].(string)
	if status != delegation.TaskStatusDenied.String() {
		t.Fatalf("expected status DENIED, got %q", status)
	}
	taskID, _ := result["task_id"].(string)
	dts := s.getDelegationTrustState()
	task, err := dts.Store.GetTask(context.Background(), delegation.TaskID(taskID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.DenialReason != "not_on_list" {
		t.Errorf("DenialReason = %q, want not_on_list", task.DenialReason)
	}
}

func TestDelegateTask_UnsetToolKindPhoneCallAdmits(t *testing.T) {
	s := setupToolDelegationServer(t, phoneCallToolSnapshot(), false)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-tool-unset",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "dep-agent-peer",
			"idempotency_key": "idem-tool-unset",
		},
	})
	if !resp.OK {
		t.Fatalf("unset tool pack + phone-call snapshot must admit: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	if status, _ := result["status"].(string); status != delegation.TaskStatusAdmitted.String() {
		t.Fatalf("expected status ADMITTED, got %q", status)
	}
}
