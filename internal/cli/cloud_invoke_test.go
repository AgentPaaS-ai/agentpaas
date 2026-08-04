package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
)

func TestCloudInvokeCommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	for _, args := range [][]string{
		{"cloud", "invoke-token"},
		{"cloud", "invoke"},
	} {
		if _, _, err := cmd.Find(args); err != nil {
			t.Fatalf("Find %v: %v", args, err)
		}
	}
}

func TestCloudInvokeToken_HumanOutput(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_tenant_token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/deployments/dep-abc/invoke-token" {
			t.Errorf("request = %s %s, want POST invoke-token", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_tenant_token" {
			t.Errorf("Authorization = %q, want tenant token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"deployment_id":"dep-abc","invoke_token":"inv_secret_token","invoke_token_prefix":"inv_secret","message":"Store this token securely."}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "invoke-token", "dep-abc")
	if err != nil {
		t.Fatalf("invoke-token: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if strings.Count(stdout, "inv_secret_token") != 1 {
		t.Errorf("invoke token should be printed once, got %q", stdout)
	}
	for _, want := range []string{
		"Invoke token: inv_secret_token",
		"Prefix: inv_secret",
		"Warning:",
		"shown once",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output, got %q", want, stdout)
		}
	}
	_ = stderr
}

func TestCloudInvokeToken_JSONOutput(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_json_tenant")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"deployment_id":"dep-json","invoke_token":"inv_json_token","invoke_token_prefix":"inv_json","message":"one time"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "invoke-token", "dep-json", "--json")
	if err != nil {
		t.Fatalf("invoke-token --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if parsed["invoke_token"] != "inv_json_token" {
		t.Errorf("invoke_token = %v, want inv_json_token", parsed["invoke_token"])
	}
	if parsed["message"] != "one time" {
		t.Errorf("message = %v, want one time", parsed["message"])
	}
	_ = stderr
}

func TestCloudInvokeToken_StoreRedactsTokenAndPersists(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	homeDir := t.TempDir()
	t.Setenv("AGENTPAAS_HOME", homeDir)
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_store_tenant"); err != nil {
		t.Fatalf("store tenant token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"deployment_id":"dep-store","invoke_token":"inv_store_secret","invoke_token_prefix":"inv_store","message":"one time"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "invoke-token", "dep-store", "--store", "--json")
	if err != nil {
		t.Fatalf("invoke-token --store: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("invoke-token --store stderr = %q", stderr)
	}
	if strings.Contains(stdout, "inv_store_secret") {
		t.Fatalf("stored invoke token leaked in output: %q", stdout)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode store output: %v; stdout=%q", err, stdout)
	}
	if result["invoke_token_prefix"] != "inv_store" {
		t.Fatalf("prefix = %#v, want inv_store", result["invoke_token_prefix"])
	}
	if _, ok := result["invoke_token"]; ok {
		t.Fatalf("stored output contains invoke_token field: %#v", result)
	}

	stored, err := cloudclient.NewFileInvokeTokenStore(filepath.Join(homeDir, cloudInvokeTokenStoreName))
	if err != nil {
		t.Fatalf("open invoke-token store: %v", err)
	}
	got, err := stored.Get(context.Background(), "dep-store")
	if err != nil {
		t.Fatalf("read stored invoke token: %v", err)
	}
	if got != "inv_store_secret" {
		t.Fatalf("stored token = %q, want inv_store_secret", got)
	}
}

func TestCloudInvoke_HumanOutput_UsesInvokeTokenAndBody(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "inv_env_token")
	_ = setupFakeTokenStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/deployments/dep-abc/invoke" {
			t.Errorf("request = %s %s, want POST invoke", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer inv_env_token" {
			t.Errorf("Authorization = %q, want invoke token", auth)
		}
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if string(requestBody) != `{"name":"Ada"}` {
			t.Errorf("request body = %q, want JSON body", requestBody)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"run_id":"run-invoked-001","status":"succeeded","error":null,"final_output":"hello Ada"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "invoke", "dep-abc", "--body", `{"name":"Ada"}`)
	if err != nil {
		t.Fatalf("invoke: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Run ID: run-invoked-001") {
		t.Errorf("expected run ID in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "Status: succeeded") {
		t.Errorf("expected status in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "Final output:\nhello Ada") {
		t.Errorf("expected final output in output, got %q", stdout)
	}
	_ = stderr
}

func TestCloudInvoke_JSONOutput_UsesFlagToken(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "inv_env_token")
	_ = setupFakeTokenStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer inv_flag_token" {
			t.Errorf("Authorization = %q, want flag token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"run_id":"run-json-001","status":"running"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "invoke", "--token", "inv_flag_token", "--json", "dep-json")
	if err != nil {
		t.Fatalf("invoke --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if parsed["run_id"] != "run-json-001" || parsed["status"] != "running" {
		t.Errorf("response = %#v, want run-json-001/running", parsed)
	}
	_ = stderr
}

func TestCloudInvoke_BodyFile(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "inv_file_token")
	_ = setupFakeTokenStore(t)

	bodyPath := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(bodyPath, []byte(`{"from":"file"}`), 0o600); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if string(requestBody) != `{"from":"file"}` {
			t.Errorf("request body = %q, want file body", requestBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-file-001","status":"queued"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "invoke", "dep-file", "--body-file", bodyPath)
	if err != nil {
		t.Fatalf("invoke --body-file: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "run-file-001") {
		t.Errorf("expected run ID in output, got %q", stdout)
	}
	_ = stderr
}

func TestCloudInvoke_BodyAndBodyFileAreExclusive(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "inv_token")
	_ = setupFakeTokenStore(t)

	bodyPath := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(bodyPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write body file: %v", err)
	}

	_, stderr, err := executeCloudCmd(t, "", "cloud", "invoke", "dep-abc", "--body", `{}`, "--body-file", bodyPath)
	if err == nil {
		t.Fatal("expected error when --body and --body-file are both set")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "mutually exclusive") {
		t.Errorf("error should mention mutually exclusive flags, got %q", combined)
	}
}

func TestCloudInvoke_BodyFileStdin(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "inv_stdin_token")
	_ = setupFakeTokenStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if string(requestBody) != `{"from":"stdin"}` {
			t.Errorf("request body = %q, want stdin body", requestBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-stdin-001","status":"queued"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, `{"from":"stdin"}`, "cloud", "invoke", "dep-stdin", "--body-file", "-")
	if err != nil {
		t.Fatalf("invoke --body-file -: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "run-stdin-001") {
		t.Errorf("expected run ID in output, got %q", stdout)
	}
	_ = stderr
}

func TestCloudInvoke_MissingToken(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "")
	oldFactory := cloudInvokeTokenStoreFactory
	cloudInvokeTokenStoreFactory = func(_ string) (cloudclient.InvokeTokenStore, error) {
		return cloudclient.NewFakeInvokeTokenStore(), nil
	}
	t.Cleanup(func() { cloudInvokeTokenStoreFactory = oldFactory })
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "invoke", "dep-abc")
	if err == nil {
		t.Fatal("expected error when invoke token is missing")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "invoke token") || !strings.Contains(combined, "AGENTPAAS_CLOUD_INVOKE_TOKEN") {
		t.Errorf("expected clear missing invoke token error, got %q", combined)
	}
}

func TestCloudInvoke_AlreadyRunning429(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "inv_busy_token")
	_ = setupFakeTokenStore(t)

	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{
			name:    "retry after supplied",
			body:    `{"error":"conflict","reason":"already_running","retry_after_sec":17}`,
			wantMsg: "agent already running; retry in 17s",
		},
		{
			name:    "retry after default",
			body:    `{"error":"already_running"}`,
			wantMsg: "agent already running; retry in 30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer func() { server.Close() }()

			t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
			_, stderr, err := executeCloudCmd(t, "", "cloud", "invoke", "dep-busy")
			if err == nil {
				t.Fatal("expected already-running error")
			}
			combined := err.Error() + stderr
			if !strings.Contains(combined, tt.wantMsg) {
				t.Errorf("error = %q, want %q", combined, tt.wantMsg)
			}
		})
	}
}

func TestCloudInvoke_WaitPollsAndPrintsFinalResult(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "inv_wait_token")
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_wait_tenant"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/deployments/dep-wait/invoke":
			if got := r.Header.Get("Authorization"); got != "Bearer inv_wait_token" {
				t.Errorf("invoke authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"run_id":"run-wait","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run-wait":
			statusCalls++
			status := "running"
			if statusCalls >= 2 {
				status = "completed"
			}
			_, _ = w.Write([]byte(`{"id":"run-wait","deployment_id":"dep-wait","status":"` + status + `"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run-wait/result":
			if got := r.Header.Get("Authorization"); got != "Bearer apc_wait_tenant" {
				t.Errorf("result authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"run_id":"run-wait","status":"completed","error":null,"finished_at":"2026-08-03T00:00:00Z","final_output":{"answer":"done"},"artifacts":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "invoke", "dep-wait", "--wait", "--wait-timeout", "2s", "--json")
	if err != nil {
		t.Fatalf("invoke --wait: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("invoke --wait stderr = %q", stderr)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode final result: %v; stdout=%q", err, stdout)
	}
	if result["run_id"] != "run-wait" || result["status"] != "completed" {
		t.Fatalf("final result = %#v, want run-wait/completed", result)
	}
	if _, ok := result["final_output"].(map[string]interface{}); !ok {
		t.Fatalf("final_output = %#v, want object", result["final_output"])
	}
	if statusCalls != 2 {
		t.Fatalf("status calls = %d, want 2", statusCalls)
	}
}
