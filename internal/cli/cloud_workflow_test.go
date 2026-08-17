package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudWorkflow_CommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	paths := [][]string{
		{"cloud", "workflow"},
		{"cloud", "workflow", "create"},
		{"cloud", "workflow", "list"},
		{"cloud", "workflow", "get"},
		{"cloud", "workflow", "start"},
		{"cloud", "workflow", "instance"},
		{"cloud", "workflow", "live-call"},
		{"cloud", "workflow", "hangup"},
	}
	for _, p := range paths {
		if _, _, err := cmd.Find(p); err != nil {
			t.Fatalf("Find %s: %v", strings.Join(p, " "), err)
		}
	}
}

func TestCloudHelp_HasWorkflow(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "--help")
	if err != nil {
		t.Fatalf("cloud --help: %v", err)
	}
	if !strings.Contains(stdout, "workflow") {
		t.Errorf("cloud --help should mention workflow, got: %s", stdout)
	}
}

func TestCloudWorkflow_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "workflow", "list")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "Not logged in") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected 'not logged in' message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudWorkflowCreate_PostsNameAndEnvelopeStages(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_wf_token")

	dir := t.TempDir()
	envPath := filepath.Join(dir, "envelope.json")
	if err := os.WriteFile(envPath, []byte(`{"stages":[{"id":"s1","agent":"demo"}]}`), 0o600); err != nil {
		t.Fatalf("write envelope: %v", err)
	}

	var gotName string
	var gotStages bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workflows" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_wf_token" {
			t.Errorf("Authorization = %q", auth)
		}
		var body struct {
			Name     string          `json:"name"`
			Envelope json.RawMessage `json:"envelope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		gotName = body.Name
		var env map[string]any
		if err := json.Unmarshal(body.Envelope, &env); err != nil {
			t.Errorf("decode envelope: %v", err)
		}
		if _, ok := env["stages"]; ok {
			gotStages = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "wf_created",
			"tenant_id":  "ten_1",
			"name":       body.Name,
			"version":    1,
			"status":     "ready",
			"created_at": "2026-04-01T00:00:00Z",
		})
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "workflow", "create",
		"--name", "pipeline", "--envelope", envPath)
	if err != nil {
		t.Fatalf("create: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if gotName != "pipeline" {
		t.Errorf("POST name = %q, want pipeline", gotName)
	}
	if !gotStages {
		t.Error("POST body envelope missing stages")
	}
	if !strings.Contains(stdout, "wf_created") {
		t.Errorf("stdout should print id, got: %q", stdout)
	}
	if !strings.Contains(stdout, "pipeline") {
		t.Errorf("stdout should print name, got: %q", stdout)
	}
	if !strings.Contains(stdout, "ready") {
		t.Errorf("stdout should print status, got: %q", stdout)
	}
}

func TestCloudWorkflowCreate_MissingEnvelope_NoHTTP(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_wf_token")

	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		t.Errorf("HTTP must not be sent when --envelope is missing, got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	_, stderr, err := executeCloudCmd(t, "", "cloud", "workflow", "create", "--name", "pipeline")
	if err == nil {
		t.Fatal("expected error for missing --envelope")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "envelope") {
		t.Errorf("error should mention envelope, got: %v", combined)
	}
	if hits != 0 {
		t.Errorf("HTTP hits = %d, want 0", hits)
	}
}

func TestCloudWorkflowStart_PrintsInstanceID(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_wf_token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workflows/wf_abc/instances" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  "wfi_started",
			"tenant_id":           "ten_1",
			"workflow_id":         "wf_abc",
			"status":              "running",
			"current_stage_index": 0,
			"created_at":          "2026-04-01T00:00:00Z",
			"updated_at":          "2026-04-01T00:00:00Z",
		})
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "workflow", "start", "wf_abc")
	if err != nil {
		t.Fatalf("start: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "wfi_started") {
		t.Errorf("stdout should print instance id, got: %q", stdout)
	}
	if !strings.Contains(stdout, "running") {
		t.Errorf("stdout should print status, got: %q", stdout)
	}
}

func TestCloudWorkflowInstance_PrintsStatus(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_wf_token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/workflow-instances/wfi_1" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                  "wfi_1",
			"tenant_id":           "ten_1",
			"workflow_id":         "wf_abc",
			"status":              "waiting",
			"current_stage_index": 3,
			"created_at":          "2026-04-01T00:00:00Z",
			"updated_at":          "2026-04-01T00:00:01Z",
			"stage_commits": []map[string]any{
				{
					"stage_index":     0,
					"terminal_status": "succeeded",
					"handoff":         map[string]any{"summary": "handoff-preview-ok"},
				},
			},
		})
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "workflow", "instance", "wfi_1")
	if err != nil {
		t.Fatalf("instance: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "wfi_1") {
		t.Errorf("stdout should print id, got: %q", stdout)
	}
	if !strings.Contains(stdout, "wf_abc") {
		t.Errorf("stdout should print workflow_id, got: %q", stdout)
	}
	if !strings.Contains(stdout, "waiting") {
		t.Errorf("stdout should print status, got: %q", stdout)
	}
	if !strings.Contains(stdout, "3") {
		t.Errorf("stdout should print current_stage_index, got: %q", stdout)
	}
	if !strings.Contains(stdout, "0") || !strings.Contains(stdout, "succeeded") || !strings.Contains(stdout, "handoff-preview-ok") {
		t.Errorf("stdout should print stage_commits preview, got: %q", stdout)
	}

	jsonOut, jsonErrOut, err := executeCloudCmd(t, "", "cloud", "workflow", "instance", "--json", "wfi_1")
	if err != nil {
		t.Fatalf("instance --json: err=%v stdout=%q stderr=%q", err, jsonOut, jsonErrOut)
	}
	if !strings.Contains(jsonOut, "stage_commits") || !strings.Contains(jsonOut, "handoff-preview-ok") {
		t.Errorf("JSON should include stage_commits, got: %q", jsonOut)
	}
}

func TestCloudWorkflowLiveCallAndHangup_HTTPMock(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		wantStatus int
		resp       any
		checkReq   func(t *testing.T, r *http.Request)
		wantOut    []string
		wantErr    string
		wantHits   int
	}{
		{
			name:       "live-call posts callee work_order and idempotency_key",
			args:       []string{"cloud", "workflow", "live-call", "wfi_parent", "--callee", "dep_callee", "--work-order-json", `{"task":"summarize"}`, "--idempotency-key", "idem-1"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/workflow-instances/wfi_parent/live-calls",
			wantStatus: http.StatusCreated,
			resp: map[string]any{
				"child_id":           "run_child",
				"run_id":             "run_child",
				"parent_instance_id": "wfi_parent",
				"reused":             false,
			},
			checkReq: func(t *testing.T, r *http.Request) {
				t.Helper()
				if auth := r.Header.Get("Authorization"); auth != "Bearer apc_wf_token" {
					t.Errorf("Authorization = %q", auth)
				}
				var body struct {
					NamedCallee    string          `json:"named_callee"`
					WorkOrder      json.RawMessage `json:"work_order"`
					IdempotencyKey string          `json:"idempotency_key"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body.NamedCallee != "dep_callee" {
					t.Errorf("named_callee = %q, want dep_callee", body.NamedCallee)
				}
				if body.IdempotencyKey != "idem-1" {
					t.Errorf("idempotency_key = %q, want idem-1", body.IdempotencyKey)
				}
				var wo map[string]any
				if err := json.Unmarshal(body.WorkOrder, &wo); err != nil {
					t.Fatalf("decode work_order: %v", err)
				}
				if wo["task"] != "summarize" {
					t.Errorf("work_order.task = %v, want summarize", wo["task"])
				}
			},
			wantOut:  []string{"run_child", "wfi_parent"},
			wantHits: 1,
		},
		{
			name:       "hangup posts instance hangup and prints cancelled",
			args:       []string{"cloud", "workflow", "hangup", "wfi_parent"},
			wantMethod: http.MethodPost,
			wantPath:   "/v1/workflow-instances/wfi_parent/hangup",
			wantStatus: http.StatusOK,
			resp:       map[string]any{"cancelled": 3},
			wantOut:    []string{"3"},
			wantHits:   1,
		},
		{
			name:     "live-call missing callee sends no HTTP",
			args:     []string{"cloud", "workflow", "live-call", "wfi_parent", "--work-order-json", `{}`, "--idempotency-key", "k"},
			wantErr:  "callee",
			wantHits: 0,
		},
		{
			name:     "live-call missing work-order-json sends no HTTP",
			args:     []string{"cloud", "workflow", "live-call", "wfi_parent", "--callee", "dep_callee", "--idempotency-key", "k"},
			wantErr:  "work-order-json",
			wantHits: 0,
		},
		{
			name:     "live-call missing idempotency-key sends no HTTP",
			args:     []string{"cloud", "workflow", "live-call", "wfi_parent", "--callee", "dep_callee", "--work-order-json", `{}`},
			wantErr:  "idempotency-key",
			wantHits: 0,
		},
		{
			name:     "live-call rejects non-object work-order-json",
			args:     []string{"cloud", "workflow", "live-call", "wfi_parent", "--callee", "dep_callee", "--work-order-json", `[]`, "--idempotency-key", "k"},
			wantErr:  "work-order-json",
			wantHits: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := setupFakeTokenStore(t)
			_ = store.Set(context.Background(), "apc_wf_token")

			hits := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if tc.wantHits == 0 {
					t.Errorf("HTTP must not be sent, got %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusOK)
					return
				}
				if r.Method != tc.wantMethod || r.URL.Path != tc.wantPath {
					t.Errorf("request = %s %s, want %s %s", r.Method, r.URL.Path, tc.wantMethod, tc.wantPath)
				}
				if tc.checkReq != nil {
					tc.checkReq(t, r)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.wantStatus)
				_ = json.NewEncoder(w).Encode(tc.resp)
			}))
			defer func() { server.Close() }()

			t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
			stdout, stderr, err := executeCloudCmd(t, "", tc.args...)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got stdout=%q", tc.wantErr, stdout)
				}
				combined := err.Error() + stderr
				if !strings.Contains(combined, tc.wantErr) {
					t.Errorf("error should mention %q, got: %v", tc.wantErr, combined)
				}
			} else if err != nil {
				t.Fatalf("cmd: err=%v stdout=%q stderr=%q", err, stdout, stderr)
			}
			for _, want := range tc.wantOut {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout should contain %q, got: %q", want, stdout)
				}
			}
			if hits != tc.wantHits {
				t.Errorf("HTTP hits = %d, want %d", hits, tc.wantHits)
			}
		})
	}
}
