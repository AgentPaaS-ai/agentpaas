package cloudclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudClient_CreateWorkflow_PostsNameAndEnvelope(t *testing.T) {
	envelope := json.RawMessage(`{"stages":[{"id":"s1"}]}`)
	expected := WorkflowRecord{
		ID:        "wf_abc",
		TenantID:  "ten_1",
		Name:      "demo",
		Version:   1,
		Status:    "ready",
		CreatedAt: "2026-04-01T00:00:00Z",
		Envelope:  envelope,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workflows" {
			t.Errorf("expected /v1/workflows, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		var req CreateWorkflowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if req.Name != "demo" {
			t.Errorf("Name = %q, want demo", req.Name)
		}
		var env map[string]any
		if err := json.Unmarshal(req.Envelope, &env); err != nil {
			t.Errorf("decode envelope: %v", err)
		}
		if _, ok := env["stages"]; !ok {
			t.Errorf("envelope missing stages, got %s", string(req.Envelope))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	result, err := client.CreateWorkflow(context.Background(), "apc_test_token", CreateWorkflowRequest{
		Name:     "demo",
		Envelope: envelope,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if result.ID != "wf_abc" {
		t.Errorf("ID = %q, want wf_abc", result.ID)
	}
	if result.Name != "demo" {
		t.Errorf("Name = %q, want demo", result.Name)
	}
	if result.Status != "ready" {
		t.Errorf("Status = %q, want ready", result.Status)
	}
}

func TestCloudClient_ListWorkflows_DecodesWrappedList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workflows" {
			t.Errorf("expected /v1/workflows, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkflowListResponse{
			Workflows: []WorkflowRecord{
				{ID: "wf_1", Name: "one", Status: "ready"},
				{ID: "wf_2", Name: "two", Status: "draft"},
			},
		})
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	result, err := client.ListWorkflows(context.Background(), "apc_test_token")
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(result.Workflows) != 2 {
		t.Fatalf("len(Workflows) = %d, want 2", len(result.Workflows))
	}
	if result.Workflows[0].ID != "wf_1" || result.Workflows[1].Name != "two" {
		t.Errorf("unexpected list: %+v", result.Workflows)
	}
}

func TestCloudClient_StartWorkflowInstance_PostsToInstances(t *testing.T) {
	handoff := json.RawMessage(`{"goal":"run"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workflows/wf_abc/instances" {
			t.Errorf("expected /v1/workflows/wf_abc/instances, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		var req StartWorkflowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		var handoffObj map[string]any
		if err := json.Unmarshal(req.InitialHandoff, &handoffObj); err != nil {
			t.Errorf("decode initial_handoff: %v", err)
		}
		if handoffObj["goal"] != "run" {
			t.Errorf("initial_handoff.goal = %v, want run", handoffObj["goal"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(WorkflowInstanceRecord{
			ID:                "wfi_1",
			TenantID:          "ten_1",
			WorkflowID:        "wf_abc",
			Status:            "running",
			CurrentStageIndex: 0,
			CreatedAt:         "2026-04-01T00:00:00Z",
			UpdatedAt:         "2026-04-01T00:00:00Z",
		})
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	result, err := client.StartWorkflowInstance(context.Background(), "apc_test_token", "wf_abc", StartWorkflowRequest{
		InitialHandoff: handoff,
	})
	if err != nil {
		t.Fatalf("StartWorkflowInstance: %v", err)
	}
	if result.ID != "wfi_1" {
		t.Errorf("ID = %q, want wfi_1", result.ID)
	}
	if result.Status != "running" {
		t.Errorf("Status = %q, want running", result.Status)
	}
}

func TestCloudClient_GetWorkflowInstance_HitsWorkflowInstances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workflow-instances/wfi_1" {
			t.Errorf("expected /v1/workflow-instances/wfi_1, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkflowInstanceRecord{
			ID:                "wfi_1",
			WorkflowID:        "wf_abc",
			Status:            "running",
			CurrentStageIndex: 2,
		})
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	result, err := client.GetWorkflowInstance(context.Background(), "apc_test_token", "wfi_1")
	if err != nil {
		t.Fatalf("GetWorkflowInstance: %v", err)
	}
	if result.ID != "wfi_1" {
		t.Errorf("ID = %q, want wfi_1", result.ID)
	}
	if result.CurrentStageIndex != 2 {
		t.Errorf("CurrentStageIndex = %d, want 2", result.CurrentStageIndex)
	}
}

func TestCloudClient_GetWorkflowInstance_UnmarshalsStageCommits(t *testing.T) {
	const marker = "handoff-marker-stage-commits"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/workflow-instances/wfi_1" {
			t.Errorf("expected /v1/workflow-instances/wfi_1, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  "wfi_1",
			"workflow_id":         "wf_abc",
			"status":              "running",
			"current_stage_index": 1,
			"stage_commits": []map[string]any{
				{
					"stage_index":     0,
					"terminal_status": "succeeded",
					"handoff":         map[string]any{"summary": marker},
				},
			},
		})
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	result, err := client.GetWorkflowInstance(context.Background(), "apc_test_token", "wfi_1")
	if err != nil {
		t.Fatalf("GetWorkflowInstance: %v", err)
	}
	if !strings.Contains(string(result.StageCommits), marker) {
		t.Errorf("StageCommits = %s, want to contain %q", result.StageCommits, marker)
	}
}

func TestCloudClient_Workflow_PathTraversalIDRejected(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		t.Errorf("HTTP must not be sent for path-traversal id, got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func() error
	}{
		{"get slash", func() error {
			_, err := client.GetWorkflow(ctx, "token", "bad/id")
			return err
		}},
		{"get backslash", func() error {
			_, err := client.GetWorkflow(ctx, "token", `bad\id`)
			return err
		}},
		{"get newline", func() error {
			_, err := client.GetWorkflow(ctx, "token", "bad\nid")
			return err
		}},
		{"start slash", func() error {
			_, err := client.StartWorkflowInstance(ctx, "token", "../wf", StartWorkflowRequest{})
			return err
		}},
		{"instance slash", func() error {
			_, err := client.GetWorkflowInstance(ctx, "token", "../../secret")
			return err
		}},
		{"live-call slash", func() error {
			_, err := client.LiveCall(ctx, "token", "../../secret", LiveCallRequest{
				NamedCallee:    "dep_1",
				WorkOrder:      json.RawMessage(`{}`),
				IdempotencyKey: "k1",
			})
			return err
		}},
		{"hangup slash", func() error {
			_, err := client.Hangup(ctx, "token", "bad/id")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatal("expected error for invalid id")
			}
			if !strings.Contains(err.Error(), "invalid id") {
				t.Errorf("error should mention invalid id, got: %v", err)
			}
		})
	}
	if hits != 0 {
		t.Errorf("HTTP hits = %d, want 0", hits)
	}
}

func TestCloudClient_LiveCallAndHangup_HTTPMock(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   any
		call       func(t *testing.T, client *CloudClient)
		checkReq   func(t *testing.T, r *http.Request)
	}{
		{
			name:       "live-call posts named_callee work_order idempotency_key",
			method:     http.MethodPost,
			path:       "/v1/workflow-instances/wfi_parent/live-calls",
			wantStatus: http.StatusCreated,
			wantBody: LiveCallResponse{
				ChildID:          "run_child",
				RunID:            "run_child",
				ParentInstanceID: "wfi_parent",
				Reused:           false,
			},
			checkReq: func(t *testing.T, r *http.Request) {
				t.Helper()
				if auth := r.Header.Get("Authorization"); auth != "Bearer apc_test_token" {
					t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
				}
				if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
				var req LiveCallRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if req.NamedCallee != "dep_callee" {
					t.Errorf("named_callee = %q, want dep_callee", req.NamedCallee)
				}
				if req.IdempotencyKey != "idem-1" {
					t.Errorf("idempotency_key = %q, want idem-1", req.IdempotencyKey)
				}
				var wo map[string]any
				if err := json.Unmarshal(req.WorkOrder, &wo); err != nil {
					t.Fatalf("decode work_order: %v", err)
				}
				if wo["task"] != "summarize" {
					t.Errorf("work_order.task = %v, want summarize", wo["task"])
				}
			},
			call: func(t *testing.T, client *CloudClient) {
				t.Helper()
				got, err := client.LiveCall(context.Background(), "apc_test_token", "wfi_parent", LiveCallRequest{
					NamedCallee:    "dep_callee",
					WorkOrder:      json.RawMessage(`{"task":"summarize"}`),
					IdempotencyKey: "idem-1",
				})
				if err != nil {
					t.Fatalf("LiveCall: %v", err)
				}
				if got.ChildID != "run_child" {
					t.Errorf("ChildID = %q, want run_child", got.ChildID)
				}
				if got.RunID != "run_child" {
					t.Errorf("RunID = %q, want run_child", got.RunID)
				}
				if got.ParentInstanceID != "wfi_parent" {
					t.Errorf("ParentInstanceID = %q, want wfi_parent", got.ParentInstanceID)
				}
				if got.Reused {
					t.Error("Reused = true, want false")
				}
			},
		},
		{
			name:       "hangup posts empty path and decodes cancelled",
			method:     http.MethodPost,
			path:       "/v1/workflow-instances/wfi_parent/hangup",
			wantStatus: http.StatusOK,
			wantBody:   HangupResponse{Cancelled: 2},
			checkReq: func(t *testing.T, r *http.Request) {
				t.Helper()
				if auth := r.Header.Get("Authorization"); auth != "Bearer apc_test_token" {
					t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
				}
			},
			call: func(t *testing.T, client *CloudClient) {
				t.Helper()
				got, err := client.Hangup(context.Background(), "apc_test_token", "wfi_parent")
				if err != nil {
					t.Fatalf("Hangup: %v", err)
				}
				if got.Cancelled != 2 {
					t.Errorf("Cancelled = %d, want 2", got.Cancelled)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if r.Method != tc.method {
					t.Errorf("method = %s, want %s", r.Method, tc.method)
				}
				if r.URL.Path != tc.path {
					t.Errorf("path = %s, want %s", r.URL.Path, tc.path)
				}
				if tc.checkReq != nil {
					tc.checkReq(t, r)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.wantStatus)
				_ = json.NewEncoder(w).Encode(tc.wantBody)
			}))
			defer func() { server.Close() }()

			client := NewCloudClient(server.URL)
			tc.call(t, client)
			if hits != 1 {
				t.Errorf("HTTP hits = %d, want 1", hits)
			}
		})
	}
}
