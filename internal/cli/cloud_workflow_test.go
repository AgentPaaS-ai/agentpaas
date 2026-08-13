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
}
