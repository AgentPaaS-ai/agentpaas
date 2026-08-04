package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudAuditCommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	for _, args := range [][]string{
		{"cloud", "events"},
		{"cloud", "audit"},
		{"cloud", "audit", "export"},
		{"cloud", "metrics"},
	} {
		if _, _, err := cmd.Find(args); err != nil {
			t.Fatalf("Find %v: %v", args, err)
		}
	}
}

func TestCloudEvents_HumanOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_events_cli"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/run-cli/events" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-cli","events":[{"timestamp":"2026-08-04T00:00:00Z","event_type":"run_started","actor":"user","payload":"{\"phase\":\"boot\"}"}]}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "events", "run-cli")
	if err != nil {
		t.Fatalf("cloud events: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	for _, want := range []string{"Run: run-cli", "Events (1):", "run_started", "user", `payload={"phase":"boot"}`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("events output %q does not contain %q", stdout, want)
		}
	}
}

func TestCloudAudit_JSONAndQueryFlags(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_audit_cli"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audit" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("since") != "2026-08-01" || query.Get("until") != "2026-08-04" || query.Get("limit") != "10" {
			t.Errorf("query = %v", query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[{"seq":9,"event_type":"run_finished"}],"next_cursor":"next"}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "audit", "--since", "2026-08-01", "--until", "2026-08-04", "--limit", "10", "--json")
	if err != nil {
		t.Fatalf("cloud audit --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("cloud audit --json stderr = %q", stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode audit JSON: %v; output=%q", err, stdout)
	}
	if got["next_cursor"] != "next" {
		t.Fatalf("audit JSON = %#v", got)
	}
}

func TestCloudAuditExport_JSON(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_export_cli"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/run-export/audit/export" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-export","format":"jsonl","records":[{"seq":1,"event_type":"run_started"}]}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "audit", "export", "run-export", "--json")
	if err != nil {
		t.Fatalf("cloud audit export --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("cloud audit export --json stderr = %q", stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode export JSON: %v; output=%q", err, stdout)
	}
	if got["run_id"] != "run-export" || got["format"] != "jsonl" {
		t.Fatalf("export JSON = %#v", got)
	}
}

func TestCloudMetrics_HumanAndJSON(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_metrics_cli"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runs_total":3,"runs_active":1,"events_total":12,"latency_ms_p95":42.5}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "metrics")
	if err != nil {
		t.Fatalf("cloud metrics: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	for _, want := range []string{"Runs total: 3", "Runs active: 1", "Events total: 12", "Latency p95: 42.5 ms"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("metrics output %q does not contain %q", stdout, want)
		}
	}

	stdout, stderr, err = executeCloudCmd(t, "", "cloud", "metrics", "--json")
	if err != nil {
		t.Fatalf("cloud metrics --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode metrics JSON: %v; output=%q", err, stdout)
	}
	if got["runs_total"] != float64(3) {
		t.Fatalf("metrics JSON = %#v", got)
	}
}

func TestCloudMetrics_JSONErrorUsesCloudWrapper(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_metrics_error"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota_exceeded","reason":"metrics_limit","message":"try later"}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "metrics", "--json")
	if err == nil {
		t.Fatal("expected cloud metrics error")
	}
	if stderr != "" {
		t.Fatalf("cloud metrics error stderr = %q", stderr)
	}
	var got CloudErrorJSON
	if decodeErr := json.Unmarshal([]byte(stdout), &got); decodeErr != nil {
		t.Fatalf("decode typed error: %v; output=%q", decodeErr, stdout)
	}
	if got.Error != "quota_exceeded" || got.Reason != "metrics_limit" {
		t.Fatalf("typed error = %+v", got)
	}
}
