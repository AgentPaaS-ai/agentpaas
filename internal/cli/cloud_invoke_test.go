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
		_, _ = w.Write([]byte(`{"id":"run-invoked-001","status":"queued"}`))
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
	if !strings.Contains(stdout, "Status: queued") {
		t.Errorf("expected status in output, got %q", stdout)
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
		_, _ = w.Write([]byte(`{"id":"run-json-001","status":"running","result":{"ok":true}}`))
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
	if parsed["id"] != "run-json-001" || parsed["status"] != "running" {
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
		_, _ = w.Write([]byte(`{"id":"run-file-001","status":"queued"}`))
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
		_, _ = w.Write([]byte(`{"id":"run-stdin-001","status":"queued"}`))
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
