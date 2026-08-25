package harness

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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

type liveCallRoundTrip func(*http.Request) (*http.Response, error)

func (f liveCallRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDelegateTask_AdmittedToolSurfacesChildOutput(t *testing.T) {
	t.Setenv("AGENTPAAS_AGENT_KIND", "tool")
	s := setupToolDelegationServer(t, phoneCallToolSnapshot(), false)
	s.liveCallHop = liveCallRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s", req.Method)
		}
		if req.URL.Host != "livecall.agentpaas.internal" {
			t.Fatalf("host = %q, want livecall.agentpaas.internal", req.URL.Host)
		}
		if req.URL.Path != "/delegate" {
			t.Fatalf("path = %q, want /delegate", req.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode hop body: %v", err)
		}
		if _, ok := body["parent_instance_id"]; ok {
			t.Fatal("harness must not send parent_instance_id")
		}
		if _, ok := body["deployment_id"]; ok {
			t.Fatal("harness must not send deployment_id")
		}
		if body["named_callee"] != "dep-agent-peer" {
			t.Fatalf("named_callee = %v", body["named_callee"])
		}
		if _, ok := body["work_order"]; !ok {
			t.Fatal("work_order missing")
		}
		if _, ok := body["idempotency_key"]; !ok {
			t.Fatal("idempotency_key missing")
		}
		if _, ok := body["parent_remaining_ms"]; !ok {
			t.Fatal("parent_remaining_ms missing")
		}
		// Real wire shape of child /invoke (handleRunInvoke returns the container body).
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"final_output":"agent A answer"}`)),
		}, nil
	})

	resp := s.handleRequest(rpcRequest{
		ID:     "req-tool-hop",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "dep-agent-peer",
			"idempotency_key": "idem-tool-hop",
			"message":         map[string]any{"task": "lookup"},
		},
	})
	if !resp.OK {
		t.Fatalf("admitted hop must succeed: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	if status, _ := result["status"].(string); status != delegation.TaskStatusSucceeded.String() {
		t.Fatalf("expected status SUCCEEDED after hop, got %q", status)
	}
	taskID, _ := result["task_id"].(string)

	got := s.handleRequest(rpcRequest{
		ID:     "req-tool-hop-get",
		Method: "get_task",
		Params: map[string]any{"task_id": taskID},
	})
	if !got.OK {
		t.Fatalf("get_task: %s", got.Error)
	}
	taskResult, ok := got.Result.(map[string]any)
	if !ok {
		t.Fatalf("get_task result is not a map: %T", got.Result)
	}
	if status, _ := taskResult["status"].(string); status != delegation.TaskStatusSucceeded.String() {
		t.Fatalf("get_task status = %q, want SUCCEEDED", status)
	}
	output, ok := taskResult["output"].(map[string]any)
	if !ok {
		t.Fatalf("get_task output missing or wrong type: %#v", taskResult["output"])
	}
	if output["final_output"] != "agent A answer" {
		t.Fatalf("output.final_output = %v", output["final_output"])
	}

	events := s.handleRequest(rpcRequest{
		ID:     "req-tool-hop-events",
		Method: "list_task_events",
		Params: map[string]any{"task_id": taskID},
	})
	if !events.OK {
		t.Fatalf("list_task_events: %s", events.Error)
	}
	evMap, _ := events.Result.(map[string]any)
	evList, _ := evMap["events"].([]map[string]any)
	if evList == nil {
		if raw, ok := evMap["events"].([]any); ok {
			found := false
			for _, item := range raw {
				m, _ := item.(map[string]any)
				if m["type"] == string(delegation.EventTaskSucceeded) {
					found = true
				}
			}
			if !found {
				t.Fatalf("events missing TASK_SUCCEEDED: %#v", raw)
			}
			return
		}
		t.Fatalf("events type %T", evMap["events"])
	}
}

func TestDelegateTask_HopTimeoutFailsTask(t *testing.T) {
	t.Setenv("AGENTPAAS_AGENT_KIND", "tool")
	s := setupToolDelegationServer(t, phoneCallToolSnapshot(), false)
	s.liveCallHop = liveCallRoundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 504,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"invoke_timeout"}`)),
		}, nil
	})

	resp := s.handleRequest(rpcRequest{
		ID:     "req-tool-timeout",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "dep-agent-peer",
			"idempotency_key": "idem-tool-timeout",
		},
	})
	if resp.OK {
		t.Fatalf("timeout hop must be an RPC error so the parent stage fails, got OK %+v", resp.Result)
	}
	if resp.Code != "invoke_timeout" {
		t.Fatalf("code = %q, want invoke_timeout", resp.Code)
	}
}

func TestDelegateTask_WaitingSeatThenSeatWaitTimeoutDoesNotSucceed(t *testing.T) {
	t.Setenv("AGENTPAAS_AGENT_KIND", "tool")
	s := setupToolDelegationServer(t, phoneCallToolSnapshot(), false)
	s.liveCallHopSleep = func(time.Duration) {}
	var hops int
	s.liveCallHop = liveCallRoundTrip(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode hop body: %v", err)
		}
		if body["named_callee"] != "dep-agent-peer" {
			t.Fatalf("named_callee = %v", body["named_callee"])
		}
		if body["idempotency_key"] != "idem-wait-then-timeout" {
			t.Fatalf("idempotency_key = %v", body["idempotency_key"])
		}
		if _, ok := body["work_order"]; !ok {
			t.Fatal("work_order missing")
		}
		hops++
		if hops == 1 {
			return &http.Response{
				StatusCode: 409,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":"waiting_seat","code":"waiting_seat","run_id":"run_wait"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: 504,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"seat_wait_timeout","code":"seat_wait_timeout","run_id":"run_wait"}`)),
		}, nil
	})

	resp := s.handleRequest(rpcRequest{
		ID:     "req-wait-then-timeout",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "dep-agent-peer",
			"idempotency_key": "idem-wait-then-timeout",
			"message":         map[string]any{"task": "lookup"},
		},
	})
	if hops != 2 {
		t.Fatalf("hops = %d, want 2 (retry after waiting_seat)", hops)
	}
	dts := s.getDelegationTrustState()
	task, err := dts.Store.GetTaskByIdempotencyKey(context.Background(), dts.Snapshot.CallerDeploymentID, "idem-wait-then-timeout")
	if err != nil || task == nil {
		t.Fatalf("GetTaskByIdempotencyKey: %v", err)
	}
	if task.Status == delegation.TaskStatusSucceeded {
		t.Fatal("409 waiting_seat then 504 seat_wait_timeout must not succeed the task")
	}
	if task.FailureReason != "seat_wait_timeout" {
		t.Fatalf("FailureReason = %q, want seat_wait_timeout", task.FailureReason)
	}
	if resp.OK {
		t.Fatalf("seat_wait_timeout must be an RPC error, got OK %+v", resp.Result)
	}
	if resp.Code != "seat_wait_timeout" {
		t.Fatalf("code = %q, want seat_wait_timeout", resp.Code)
	}
}
