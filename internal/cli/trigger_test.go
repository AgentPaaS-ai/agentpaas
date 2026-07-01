package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestTriggerInvokeCmd_RequiresArg(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetArgs([]string{"trigger", "invoke"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when running trigger invoke without arg, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Errorf("expected 'accepts 1 arg' error, got: %v", err)
	}
}

func TestTriggerInvokeCmd_NoServer(t *testing.T) {
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", "127.0.0.1:19999")

	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetArgs([]string{"trigger", "invoke", "test-agent"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no trigger server is running, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") &&
		!strings.Contains(err.Error(), "trigger invoke failed") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

func TestTriggerInvokeCmd_Success(t *testing.T) {
	mockResp := `{"run":{"runId":"run-123","agentName":"test-agent","status":"RUN_STATUS_RUNNING"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/trigger/invoke" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockResp))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", addr)

	stdout := captureStdout(t, func() {
		resetAgentCmd()
		cmd := freshCmd()
		cmd.SetArgs([]string{"trigger", "invoke", "test-agent"})
		_ = cmd.Execute()
	})

	if !strings.Contains(stdout, "run_id=run-123") {
		t.Errorf("expected run_id=run-123 in output, got: %s", stdout)
	}
}

func TestTriggerInvokeCmd_SuccessJSON(t *testing.T) {
	mockResp := `{"run":{"runId":"run-456","agentName":"json-agent","status":"RUN_STATUS_RUNNING"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/trigger/invoke" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockResp))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", addr)

	stdout := captureStdout(t, func() {
		resetAgentCmd()
		cmd := freshCmd()
		cmd.SetArgs([]string{"--json", "trigger", "invoke", "json-agent"})
		_ = cmd.Execute()
	})

	if !strings.Contains(stdout, `"run_id":`) {
		t.Errorf("expected JSON output with run_id field, got: %s", stdout)
	}
	if !strings.Contains(stdout, "run-456") {
		t.Errorf("expected run-456 in JSON output, got: %s", stdout)
	}
}

func TestTriggerInvokeCmd_WithAuthKey(t *testing.T) {
	authReceived := false
	mockResp := `{"run":{"runId":"run-auth","agentName":"auth-agent","status":"RUN_STATUS_RUNNING"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/trigger/invoke" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") == "Bearer test-api-key" {
			authReceived = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockResp))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", addr)
	t.Setenv("AGENTPAAS_TRIGGER_API_KEY", "test-api-key")

	stdout := captureStdout(t, func() {
		resetAgentCmd()
		cmd := freshCmd()
		cmd.SetArgs([]string{"trigger", "invoke", "auth-agent"})
		_ = cmd.Execute()
	})

	if !strings.Contains(stdout, "run_id=run-auth") {
		t.Errorf("expected run_id=run-auth in output, got: %s", stdout)
	}
	if !authReceived {
		t.Error("expected Authorization header to be sent with API key")
	}
}

func TestTriggerInvokeCmd_WithPayloadFile(t *testing.T) {
	mockResp := `{"run":{"runId":"run-payload","agentName":"payload-agent","status":"RUN_STATUS_RUNNING"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/trigger/invoke" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockResp))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", addr)

	// Create a temporary payload file.
	payloadFile, err := os.CreateTemp("", "trigger-payload-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() { _ = os.Remove(payloadFile.Name()) }()
	if _, err := payloadFile.Write([]byte(`{"key":"value"}`)); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	_ = payloadFile.Close()

	stdout := captureStdout(t, func() {
		resetAgentCmd()
		cmd := freshCmd()
		cmd.SetArgs([]string{"trigger", "invoke", "payload-agent", "--payload", payloadFile.Name()})
		_ = cmd.Execute()
	})

	if !strings.Contains(stdout, "run_id=run-payload") {
		t.Errorf("expected run_id=run-payload in output, got: %s", stdout)
	}
}
