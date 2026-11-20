package harness

import (
	"os"
	"strings"
	"testing"
)

func TestLoadCredentialsFromJSON_LoadsAllEntries(t *testing.T) {
	jsonInput := `[
		{"id": "api-key-1", "header": "Authorization", "value": "sk-secret-1"},
		{"id": "api-key-2", "header": "X-API-Key", "value": "sk-secret-2"}
	]`

	srv := &harnessRPCServer{}
	if err := srv.LoadCredentialsFromJSON(jsonInput); err != nil {
		t.Fatalf("LoadCredentialsFromJSON failed: %v", err)
	}

	srv.mu.RLock()
	creds := srv.credentials
	srv.mu.RUnlock()

	if len(creds) != 2 {
		t.Fatalf("got %d credentials, want 2", len(creds))
	}
	if creds["api-key-1"].Header != "Authorization" || creds["api-key-1"].Value != "sk-secret-1" {
		t.Fatalf("api-key-1 = %+v, want header=Authorization value=sk-secret-1", creds["api-key-1"])
	}
	if creds["api-key-2"].Header != "X-API-Key" || creds["api-key-2"].Value != "sk-secret-2" {
		t.Fatalf("api-key-2 = %+v, want header=X-API-Key value=sk-secret-2", creds["api-key-2"])
	}
}

func TestLoadCredentialsFromJSON_EmptyInput(t *testing.T) {
	srv := &harnessRPCServer{}
	if err := srv.LoadCredentialsFromJSON(""); err != nil {
		t.Fatalf("LoadCredentialsFromJSON empty string failed: %v", err)
	}
}

func TestLoadCredentialsFromJSON_EmptyArray(t *testing.T) {
	srv := &harnessRPCServer{}
	if err := srv.LoadCredentialsFromJSON("[]"); err != nil {
		t.Fatalf("LoadCredentialsFromJSON empty array failed: %v", err)
	}
	srv.mu.RLock()
	creds := srv.credentials
	srv.mu.RUnlock()
	if len(creds) != 0 {
		t.Fatalf("got %d credentials, want 0 for empty array", len(creds))
	}
}

func TestLoadCredentialsFromJSON_SkipsEmptyID(t *testing.T) {
	jsonInput := `[
		{"id": "", "header": "X-Ignored", "value": "secret"},
		{"id": "valid", "header": "Authorization", "value": "sk-valid"}
	]`

	srv := &harnessRPCServer{}
	if err := srv.LoadCredentialsFromJSON(jsonInput); err != nil {
		t.Fatalf("LoadCredentialsFromJSON failed: %v", err)
	}

	srv.mu.RLock()
	creds := srv.credentials
	srv.mu.RUnlock()

	if len(creds) != 1 {
		t.Fatalf("got %d credentials, want 1 (empty ID skipped)", len(creds))
	}
	if creds["valid"].Value != "sk-valid" {
		t.Fatalf("valid credential not found or wrong value: %+v", creds["valid"])
	}
}

func TestLoadCredentialsFromJSON_NoValueInError(t *testing.T) {
	const secret = "SENTINEL_SECRET_NEVER_IN_ERROR"

	// Malformed JSON — the secret is inside a value field.
	// The error message must not contain the credential value.
	jsonInput := `[{"id": "k", "header": "Auth", "value": "` + secret + `"}`
	// Intentionally missing closing bracket to trigger parse error.

	srv := &harnessRPCServer{}
	err := srv.LoadCredentialsFromJSON(jsonInput)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	errStr := err.Error()
	if strings.Contains(errStr, secret) {
		t.Fatalf("error message leaked credential value: %s", errStr)
	}
}

func TestLoadCredentialsFromJSON_OverwritesPrevious(t *testing.T) {
	srv := &harnessRPCServer{}

	// First load
	if err := srv.LoadCredentialsFromJSON(`[{"id": "k", "header": "H1", "value": "v1"}]`); err != nil {
		t.Fatalf("first LoadCredentialsFromJSON failed: %v", err)
	}

	// Second load should overwrite, not merge
	if err := srv.LoadCredentialsFromJSON(`[{"id": "k2", "header": "H2", "value": "v2"}]`); err != nil {
		t.Fatalf("second LoadCredentialsFromJSON failed: %v", err)
	}

	srv.mu.RLock()
	creds := srv.credentials
	srv.mu.RUnlock()

	if len(creds) != 1 {
		t.Fatalf("got %d credentials after overwrite, want 1", len(creds))
	}
	if _, ok := creds["k"]; ok {
		t.Fatalf("credential 'k' from first load should have been overwritten")
	}
	if creds["k2"].Value != "v2" {
		t.Fatalf("credential 'k2' not found after overwrite: %+v", creds)
	}
}

func TestLoadCredentialsFromJSON_EnvVarEndToEnd(t *testing.T) {
	const secret = "SENTINEL_E2E_JSON_SECRET"

	jsonInput := `[{"id":"my-api-key","header":"Authorization","value":"` + secret + `"}]`
	t.Setenv("AGENTPAAS_CREDENTIALS_JSON", jsonInput)
	t.Setenv("AGENTPAAS_CREDENTIALS_PATH", "")

	cfg := Config{
		AgentPath: writeAgent(t, `from agentpaas_sdk import agent
@agent.on_invoke
def handle(payload):
    return {"done": True}
`),
		CredentialsJSON: jsonInput,
	}
	srv := NewServer(cfg)
	t.Cleanup(func() { _ = srv.Close() })

	// Trigger the startup path via /readyz, which calls Start()->runPythonWorker()
	// That calls LoadCredentials. Since CredentialsPath is empty but CredentialsJSON
	// is set, it should load from JSON.
	_ = invokeSDKAgent(t, srv, `{"question":"hello","credentials":[{"id":"my-api-key","header":"Authorization","value":"`+secret+`"}]}`)

	// Now check that the credential was loaded into the RPC server
	srv.worker.rpc.mu.RLock()
	creds := srv.worker.rpc.credentials
	srv.worker.rpc.mu.RUnlock()

	if creds["my-api-key"].Value != secret {
		t.Fatalf("end-to-end: credential not loaded from JSON env; credentials map: %+v", creds)
	}

	// Verify the credential value does not appear in any visible state
	// (not in the credentials map printed via %+v? Actually we already checked
	// it IS there — the point is it shouldn't leak to agent code.)
	// This is covered by existing credential_invisibility_test.go tests.
}

func TestPythonWorker_PrefersCredentialsPathOverJSON(t *testing.T) {
	credPath := writeCredentialsFile(t, `[{"id":"file-key","header":"FileAuth","value":"file-secret"}]`)

	jsonInput := `[{"id":"json-key","header":"JsonAuth","value":"json-secret"}]`
	t.Setenv("AGENTPAAS_CREDENTIALS_JSON", jsonInput)

	cfg := Config{
		AgentPath: writeAgent(t, `from agentpaas_sdk import agent
@agent.on_invoke
def handle(payload):
    return {"done": True}
`),
		CredentialsPath: credPath,
		CredentialsJSON: jsonInput,
	}
	srv := NewServer(cfg)
	t.Cleanup(func() { _ = srv.Close() })

	_ = invokeSDKAgent(t, srv, `{"question":"hello"}`)

	srv.worker.rpc.mu.RLock()
	creds := srv.worker.rpc.credentials
	srv.worker.rpc.mu.RUnlock()

	// Should have loaded from the file, not from JSON
	if creds["file-key"].Value != "file-secret" {
		t.Fatalf("expected file credential 'file-key', got: %+v", creds)
	}
	if _, ok := creds["json-key"]; ok {
		t.Fatalf("JSON credential 'json-key' should NOT be loaded when file path is set")
	}
}

func writeCredentialsFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "aws-credentials-*.json")
	if err != nil {
		t.Fatalf("create temp credentials file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatalf("write temp credentials file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp credentials file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}
