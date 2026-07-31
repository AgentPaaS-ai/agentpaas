package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
)

func TestCloudResultAndLogsCommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	for _, args := range [][]string{
		{"cloud", "result"},
		{"cloud", "logs"},
	} {
		if _, _, err := cmd.Find(args); err != nil {
			t.Fatalf("Find %v: %v", args, err)
		}
	}
}

func TestCloudResult_HumanOutputIncludesFailureDetailsAndArtifacts(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_result_cli")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/runs/run-failed/result" {
			t.Errorf("request = %s %s, want GET result", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_result_cli" {
			t.Errorf("Authorization = %q, want tenant token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"run_id":"run-failed",
			"status":"failed",
			"error":"container_runtime_unavailable",
			"finished_at":"2026-07-31T10:00:00Z",
			"final_output":{"answer":42},
			"artifacts":[
				{"name":"result.json","size_bytes":123,"url":"https://cloud.example/result-token","expires_in_sec":3600},
				{"name":"logs.txt","size_bytes":18,"url":"https://cloud.example/log-token","expires_in_sec":3600}
			]
		}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "result", "run-failed")
	if err != nil {
		t.Fatalf("cloud result: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	for _, want := range []string{
		"Run: run-failed",
		"Status: failed",
		"Error: container_runtime_unavailable",
		"Final output:",
		"answer",
		"Artifacts:",
		"result.json",
		"https://cloud.example/result-token",
		"logs.txt",
		"https://cloud.example/log-token",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output, got %q", want, stdout)
		}
	}
}

func TestCloudResult_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_result_json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-json","status":"succeeded","error":null,"final_output":"done","artifacts":[]}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, _, err := executeCloudCmd(t, "", "cloud", "result", "--json", "run-json")
	if err != nil {
		t.Fatalf("cloud result --json: %v", err)
	}
	var parsed cloudclient.RunResult
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal result JSON: %v\noutput: %s", err, stdout)
	}
	if parsed.RunID != "run-json" || parsed.Status != "succeeded" {
		t.Errorf("result = %+v, want run-json/succeeded", parsed)
	}
}

func TestCloudLogs_FetchesLogsArtifact(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_logs_cli")

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runs/run-logs/result":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"run_id":"run-logs","status":"failed","artifacts":[{"name":"logs.txt","size_bytes":20,"url":"` + server.URL + `/signed/logs","expires_in_sec":3600}]}`))
		case "/signed/logs":
			if auth := r.Header.Get("Authorization"); auth != "" {
				t.Errorf("signed artifact request leaked Authorization header %q", auth)
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("worker failed\nreason: timeout\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "logs", "run-logs")
	if err != nil {
		t.Fatalf("cloud logs: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "worker failed\nreason: timeout\n" {
		t.Errorf("logs output = %q, want raw logs", stdout)
	}
}

func TestCloudInvokeToken_StoresTokenForDeployment(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	invokeStore := cloudclient.NewFakeInvokeTokenStore()
	oldFactory := cloudInvokeTokenStoreFactory
	cloudInvokeTokenStoreFactory = func(_ string) (cloudclient.InvokeTokenStore, error) {
		return invokeStore, nil
	}
	t.Cleanup(func() { cloudInvokeTokenStoreFactory = oldFactory })

	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_tenant_for_invoke_token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"deployment_id":"dep-store","invoke_token":"inv_stored","invoke_token_prefix":"inv_stor","message":"one time"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, _, err := executeCloudCmd(t, "", "cloud", "invoke-token", "dep-store")
	if err != nil {
		t.Fatalf("cloud invoke-token: %v", err)
	}
	if !strings.Contains(stdout, "inv_stored") {
		t.Errorf("output = %q, want printed token", stdout)
	}
	got, err := invokeStore.Get(context.Background(), "dep-store")
	if err != nil {
		t.Fatalf("stored invoke token: %v", err)
	}
	if got != "inv_stored" {
		t.Errorf("stored token = %q, want inv_stored", got)
	}
}

func TestCloudInvoke_UsesStoredTokenWhenFlagAndEnvOmitted(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "")
	invokeStore := cloudclient.NewFakeInvokeTokenStore()
	if err := invokeStore.Set(context.Background(), "dep-stored", "inv_from_store"); err != nil {
		t.Fatalf("seed invoke token store: %v", err)
	}
	oldFactory := cloudInvokeTokenStoreFactory
	cloudInvokeTokenStoreFactory = func(_ string) (cloudclient.InvokeTokenStore, error) {
		return invokeStore, nil
	}
	t.Cleanup(func() { cloudInvokeTokenStoreFactory = oldFactory })
	_ = setupFakeTokenStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer inv_from_store" {
			t.Errorf("Authorization = %q, want stored invoke token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"run-from-store","status":"queued"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "invoke", "dep-stored")
	if err != nil {
		t.Fatalf("cloud invoke: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "run-from-store") {
		t.Errorf("output = %q, want run ID", stdout)
	}
}
