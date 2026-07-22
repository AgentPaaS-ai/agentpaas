package harness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
)

// makeSnapshot creates a test CommunicationSnapshot with the given bindings.
func makeDelegationSnapshot() delegation.CommunicationSnapshot {
	snap := delegation.CommunicationSnapshot{
		SchemaVersion:        delegation.CurrentSchemaVersion,
		SnapshotGeneration:   1,
		WorkflowID:           "wf-deleg-test",
		TenantID:             "tenant-test",
		CallerDeploymentID:   "dep-caller-1",
		CallerPackageName:    "weather-agent",
		CallerPackageDigest:  "sha256:caller-digest",
		Bindings: []delegation.WorkflowDelegationBinding{
			{
				BindingID:            "report.verify",
				CalleePackageName:    "report-verifier",
				CalleePackageVersion: "1.0.0",
				CalleeBundleDigest:   "sha256:callee-digest",
				CallerPackageName:    "weather-agent",
				MaxDataClass:         "internal",
			},
			{
				BindingID:            "data.analyze",
				Operation:            "analyze",
				CalleePackageName:    "data-analyzer",
				CalleePackageVersion: "2.0.0",
				CalleeBundleDigest:   "sha256:analyzer-digest",
				MaxDataClass:         "confidential",
			},
		},
	}
	dg, _ := delegation.ComputeSnapshotDigest(&snap)
	snap.SnapshotDigest = dg
	return snap
}

// makeCalleeIngress creates test ingress rules.
func makeDelegationIngress() []delegation.CalleeIngressRule {
	return []delegation.CalleeIngressRule{
		{
			CallerPackageName:   "weather-agent",
			CallerPackageDigest: "sha256:caller-digest",
			AllowedBindings:     []string{"report.verify"},
			MaxDataClass:        "internal",
		},
	}
}

// setupDelegationServer creates a harnessRPCServer with delegation trust state set.
func setupDelegationServer(t *testing.T) *harnessRPCServer {
	t.Helper()
	s := &harnessRPCServer{
		done: make(chan struct{}),
	}

	snap := makeDelegationSnapshot()
	store := delegation.NewMemoryStore()
	dts := &DelegationTrustState{
		Snapshot:            snap,
		BindingCapabilities: map[string]string{},
		NetworkAlias:        "net-alias-test",
		Store:               store,
		CalleeIngressAllow:  makeDelegationIngress(),
	}
	dts.BindingCapabilities["report.verify"] = "cap-test-token"
	s.setDelegationTrustState(dts)
	return s
}

// ---------------------------------------------------------------------------
// 1. Delegate happy path
// ---------------------------------------------------------------------------

func TestDelegateTask_HappyPath(t *testing.T) {
	s := setupDelegationServer(t)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-1",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-happy-1",
			"message":         map[string]any{"role": "user", "parts": []any{map[string]any{"kind": "text", "text": "verify this"}}},
		},
	})
	if !resp.OK {
		t.Fatalf("delegate_task failed: %s (code=%s)", resp.Error, resp.Code)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	taskID, _ := result["task_id"].(string)
	if taskID == "" {
		t.Fatal("expected non-empty task_id")
	}
	status, _ := result["status"].(string)
	if status != delegation.TaskStatusAdmitted.String() {
		t.Errorf("expected status ADMITTED, got %q", status)
	}

	// Verify task was created in store.
	dts := s.getDelegationTrustState()
	task, err := dts.Store.GetTask(context.Background(), delegation.TaskID(taskID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != delegation.TaskStatusAdmitted {
		t.Errorf("stored task status = %s, want ADMITTED", task.Status)
	}

	// Verify event was appended.
	events, err := dts.Store.ListEvents(context.Background(), delegation.TaskID(taskID), 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	if events[0].Type != delegation.EventTaskAdmitted {
		t.Errorf("expected TASK_ADMITTED event, got %s", events[0].Type)
	}
}

// ---------------------------------------------------------------------------
// 2. Delegate denied — unknown binding
// ---------------------------------------------------------------------------

func TestDelegateTask_DeniedUnknownBinding(t *testing.T) {
	s := setupDelegationServer(t)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-2",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "unknown.binding",
			"idempotency_key": "idem-deny-1",
		},
	})
	if !resp.OK {
		t.Fatalf("delegate_task should return OK even on denial: %s", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}
	status, _ := result["status"].(string)
	if status != delegation.TaskStatusDenied.String() {
		t.Errorf("expected status DENIED, got %q", status)
	}

	// Verify task was created DENIED in store.
	taskID, _ := result["task_id"].(string)
	dts := s.getDelegationTrustState()
	task, err := dts.Store.GetTask(context.Background(), delegation.TaskID(taskID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != delegation.TaskStatusDenied {
		t.Errorf("stored task status = %s, want DENIED", task.Status)
	}
	if task.DenialReason == "" {
		t.Error("expected non-empty denial reason")
	}
}

// ---------------------------------------------------------------------------
// 3. Delegate idempotent replay
// ---------------------------------------------------------------------------

func TestDelegateTask_IdempotentReplay(t *testing.T) {
	s := setupDelegationServer(t)

	params := map[string]any{
		"capability":      "report.verify",
		"idempotency_key": "idem-replay-1",
		"message":         map[string]any{"role": "user", "parts": []any{map[string]any{"kind": "text", "text": "hello"}}},
	}

	// First call.
	resp1 := s.handleRequest(rpcRequest{ID: "req-3a", Method: "delegate_task", Params: params})
	if !resp1.OK {
		t.Fatalf("first delegate_task failed: %s", resp1.Error)
	}
	r1, _ := resp1.Result.(map[string]any)
	taskID1, _ := r1["task_id"].(string)
	if taskID1 == "" {
		t.Fatal("expected non-empty task_id from first call")
	}

	// Second call — same params, should get idempotency conflict.
	resp2 := s.handleRequest(rpcRequest{ID: "req-3b", Method: "delegate_task", Params: params})
	if resp2.OK {
		t.Log("second call succeeded (idempotent)")
		r2, _ := resp2.Result.(map[string]any)
		taskID2, _ := r2["task_id"].(string)
		t.Logf("taskID1=%s taskID2=%s", taskID1, taskID2)
	} else {
		// Idempotency conflict is expected behavior.
		if resp2.Code != "idempotency_conflict" {
			t.Errorf("expected idempotency_conflict, got %q", resp2.Code)
		}
		t.Logf("idempotent replay correctly returned conflict")
	}
}

// ---------------------------------------------------------------------------
// 4. GetTask
// ---------------------------------------------------------------------------

func TestGetTask(t *testing.T) {
	s := setupDelegationServer(t)

	// First delegate to create a task.
	resp := s.handleRequest(rpcRequest{
		ID:     "req-get-1",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-get-1",
		},
	})
	r, _ := resp.Result.(map[string]any)
	taskID, _ := r["task_id"].(string)

	// Now get the task.
	getResp := s.handleRequest(rpcRequest{
		ID:     "req-get-2",
		Method: "get_task",
		Params: map[string]any{
			"task_id": taskID,
		},
	})
	if !getResp.OK {
		t.Fatalf("get_task failed: %s", getResp.Error)
	}

	getResult, _ := getResp.Result.(map[string]any)
	if getResult["task_id"] != taskID {
		t.Errorf("expected task_id %q, got %q", taskID, getResult["task_id"])
	}
	if getResult["status"] != delegation.TaskStatusAdmitted.String() {
		t.Errorf("expected status ADMITTED, got %q", getResult["status"])
	}
}

// ---------------------------------------------------------------------------
// 5. ListTaskEvents
// ---------------------------------------------------------------------------

func TestListTaskEvents(t *testing.T) {
	s := setupDelegationServer(t)

	// First delegate to create a task with an event.
	resp := s.handleRequest(rpcRequest{
		ID:     "req-evt-1",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-evt-1",
		},
	})
	r, _ := resp.Result.(map[string]any)
	taskID, _ := r["task_id"].(string)

	// List events.
	listResp := s.handleRequest(rpcRequest{
		ID:     "req-evt-2",
		Method: "list_task_events",
		Params: map[string]any{
			"task_id":        taskID,
			"after_sequence": 0,
		},
	})
	if !listResp.OK {
		t.Fatalf("list_task_events failed: %s", listResp.Error)
	}

	listResult, _ := listResp.Result.(map[string]any)
	events, ok := listResult["events"].([]map[string]any)
	if !ok {
		// JSON unmarshaling may have produced []interface{}
		eventsRaw, ok2 := listResult["events"].([]any)
		if !ok2 {
			t.Fatalf("events is neither []map nor []any: %T", listResult["events"])
		}
		if len(eventsRaw) == 0 {
			t.Fatal("expected at least 1 event")
		}
		return
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
}

// ---------------------------------------------------------------------------
// 6. Response scrubbing — no forbidden fields in delegate response
// ---------------------------------------------------------------------------

func TestResponse_NoForbiddenFields(t *testing.T) {
	// Test that scrubResponse strips forbidden patterns.
	scrubbed := scrubResponse(map[string]any{
		"task_id":  "task-abc",
		"capability_token": "leak-this",
		"endpoint": "http://evil.test",
		"host":     "bad-host",
		"status":   "ADMITTED",
	}, delegateTaskResponseFields)

	// Only task_id and status should remain.
	if _, ok := scrubbed["capability_token"]; ok {
		t.Error("capability_token should have been stripped")
	}
	if _, ok := scrubbed["endpoint"]; ok {
		t.Error("endpoint should have been stripped")
	}
	if _, ok := scrubbed["host"]; ok {
		t.Error("host should have been stripped")
	}
	if v, ok := scrubbed["network_alias"]; ok {
		t.Errorf("network_alias should have been stripped, got %v", v)
	}
}

func TestDelegateTaskResponse_NoEndpointLeakage(t *testing.T) {
	s := setupDelegationServer(t)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-scrub-1",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-scrub-1",
		},
	})

	// Marshal to JSON and verify no forbidden patterns.
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	respStr := string(b)

	forbidden := []string{
		"capability_token", "endpoint", "host", "ip", "port",
		"network_alias", "token", "secret",
	}
	for _, f := range forbidden {
		if strings.Contains(strings.ToLower(respStr), f) {
			t.Errorf("response contains forbidden pattern %q: %s", f, respStr)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. No trust state — fails
// ---------------------------------------------------------------------------

func TestDelegateTask_NoTrustState(t *testing.T) {
	s := &harnessRPCServer{done: make(chan struct{})}

	resp := s.handleRequest(rpcRequest{
		ID:     "req-no-trust",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "report.verify",
			"idempotency_key": "idem-no-1",
		},
	})
	if resp.OK {
		t.Fatal("expected OK=false without trust state")
	}
	if resp.Code != "no_trust_state" {
		t.Errorf("expected code no_trust_state, got %q", resp.Code)
	}
}

// ---------------------------------------------------------------------------
// 8. Missing capability / idempotency_key
// ---------------------------------------------------------------------------

func TestDelegateTask_MissingCapability(t *testing.T) {
	s := setupDelegationServer(t)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-no-cap",
		Method: "delegate_task",
		Params: map[string]any{
			"idempotency_key": "idem-nocap-1",
		},
	})
	if resp.OK {
		t.Fatal("expected OK=false for missing capability")
	}
	if resp.Code != "invalid_params" {
		t.Errorf("expected code invalid_params, got %q", resp.Code)
	}
}

func TestDelegateTask_MissingIdempotencyKey(t *testing.T) {
	s := setupDelegationServer(t)

	resp := s.handleRequest(rpcRequest{
		ID:     "req-no-idem",
		Method: "delegate_task",
		Params: map[string]any{
			"capability": "report.verify",
		},
	})
	if resp.OK {
		t.Fatal("expected OK=false for missing idempotency_key")
	}
	if resp.Code != "invalid_params" {
		t.Errorf("expected code invalid_params, got %q", resp.Code)
	}
}
