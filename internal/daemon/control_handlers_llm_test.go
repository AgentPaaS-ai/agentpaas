package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

// testControlServerForPayload creates a controlServer with a temp home dir
// and an optional FakeKeyStore for credential resolution.
func testControlServerForPayload(t *testing.T, store secrets.SecretStore) *controlServer {
	t.Helper()
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	return &controlServer{
		homePaths:          hp,
		secretStoreForTest: store,
	}
}

// writeDeployedLock writes an agent.lock file to the deployed agent directory.
func writeDeployedLock(t *testing.T, hp *home.HomePaths, agentName string, agentYAML *pack.AgentYAML) {
	t.Helper()
	deployedDir := pack.DeployedAgentPath(hp.Home, agentName)
	if err := os.MkdirAll(deployedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll deployed dir: %v", err)
	}

	lock := &pack.AgentLock{
		SchemaVersion: pack.LockSchemaVersion,
		AgentName:     agentName,
		AgentVersion:  "1.0.0",
		AgentYAML:     agentYAML,
	}
	if err := pack.WriteAgentLock(lock, filepath.Join(deployedDir, "agent.lock")); err != nil {
		t.Fatalf("WriteAgentLock: %v", err)
	}
}

func TestBuildInvokePayload_NoDeployment(t *testing.T) {
	server := testControlServerForPayload(t, nil)
	payload, err := server.buildInvokePayload(context.Background(), "nonexistent-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v, want nil", err)
	}
	if len(payload) != 0 {
		t.Fatalf("buildInvokePayload() = %v, want empty map", payload)
	}
}

func TestBuildInvokePayload_NoLLMConfig(t *testing.T) {
	server := testControlServerForPayload(t, nil)
	writeDeployedLock(t, server.homePaths, "no-llm-agent", &pack.AgentYAML{
		Name:    "no-llm-agent",
		Version: "1.0.0",
		// No LLM field
	})

	payload, err := server.buildInvokePayload(context.Background(), "no-llm-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v, want nil", err)
	}
	if len(payload) != 0 {
		t.Fatalf("buildInvokePayload() = %v, want empty map (no LLM config)", payload)
	}
}

func TestBuildInvokePayload_WithLLMConfig(t *testing.T) {
	fakeStore := secrets.NewFakeKeyStore()
	if err := fakeStore.Set(context.Background(), "openai-key", []byte("sk-test-api-key-12345")); err != nil {
		t.Fatalf("FakeKeyStore.Set: %v", err)
	}

	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "llm-agent", &pack.AgentYAML{
		Name:    "llm-agent",
		Version: "1.0.0",
		LLM: pack.LLMConfig{
			Provider:   "openai",
			Model:      "gpt-4o",
			Credential: "openai-key",
		},
	})

	payload, err := server.buildInvokePayload(context.Background(), "llm-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v, want nil", err)
	}

	// Check LLM section.
	llm, ok := payload["llm"].(map[string]any)
	if !ok {
		t.Fatalf("payload[\"llm\"] missing or wrong type: %v", payload)
	}
	if llm["provider"] != "openai" {
		t.Errorf("llm.provider = %q, want openai", llm["provider"])
	}
	if llm["model"] != "gpt-4o" {
		t.Errorf("llm.model = %q, want gpt-4o", llm["model"])
	}
	if llm["credential"] != "openai-key" {
		t.Errorf("llm.credential = %q, want openai-key", llm["credential"])
	}

	// Check credentials section.
	creds, ok := payload["credentials"].([]map[string]any)
	if !ok || len(creds) != 1 {
		t.Fatalf("payload[\"credentials\"] missing or wrong type: %v", payload)
	}
	if creds[0]["id"] != "openai-key" {
		t.Errorf("credentials[0].id = %q, want openai-key", creds[0]["id"])
	}
	if creds[0]["header"] != "Authorization" {
		t.Errorf("credentials[0].header = %q, want Authorization", creds[0]["header"])
	}
	if creds[0]["value"] != "sk-test-api-key-12345" {
		t.Errorf("credentials[0].value = %q, want sk-test-api-key-12345", creds[0]["value"])
	}
}

func TestBuildInvokePayload_CredentialNotFound(t *testing.T) {
	// Use a FakeKeyStore that does NOT contain the credential.
	fakeStore := secrets.NewFakeKeyStore()
	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "missing-cred-agent", &pack.AgentYAML{
		Name:    "missing-cred-agent",
		Version: "1.0.0",
		LLM: pack.LLMConfig{
			Provider:   "openai",
			Model:      "gpt-4o",
			Credential: "nonexistent-key",
		},
	})

	payload, err := server.buildInvokePayload(context.Background(), "missing-cred-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v, want nil (graceful fallback)", err)
	}
	if len(payload) != 0 {
		t.Fatalf("buildInvokePayload() = %v, want empty map (credential not found)", payload)
	}
}

func TestBuildInvokePayload_AnthropicHeader(t *testing.T) {
	fakeStore := secrets.NewFakeKeyStore()
	if err := fakeStore.Set(context.Background(), "anthropic-key", []byte("sk-ant-api-key")); err != nil {
		t.Fatalf("FakeKeyStore.Set: %v", err)
	}

	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "anthropic-agent", &pack.AgentYAML{
		Name:    "anthropic-agent",
		Version: "1.0.0",
		LLM: pack.LLMConfig{
			Provider:   "anthropic",
			Model:      "claude-sonnet-4",
			Credential: "anthropic-key",
		},
	})

	payload, err := server.buildInvokePayload(context.Background(), "anthropic-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v, want nil", err)
	}
	creds, ok := payload["credentials"].([]map[string]any)
	if !ok || len(creds) != 1 {
		t.Fatalf("payload[\"credentials\"] missing: %v", payload)
	}
	if creds[0]["header"] != "x-api-key" {
		t.Errorf("credentials[0].header = %q, want x-api-key for anthropic", creds[0]["header"])
	}
}

func TestBuildInvokePayload_SecretNotInError(t *testing.T) {
	// Verify that when credential resolution fails, the error message
	// never contains the credential value. buildInvokePayload returns empty
	// payload gracefully — it does NOT return an error containing the secret.
	fakeStore := secrets.NewFakeKeyStore()
	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "secret-agent", &pack.AgentYAML{
		Name:    "secret-agent",
		Version: "1.0.0",
		LLM: pack.LLMConfig{
			Provider:   "openai",
			Model:      "gpt-4o",
			Credential: "my-secret",
		},
	})

	payload, err := server.buildInvokePayload(context.Background(), "secret-agent", nil)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "my-secret") {
			t.Errorf("error message contains credential name: %s", errStr)
		}
	}
	// The payload should be empty (graceful fallback).
	if len(payload) != 0 {
		t.Fatalf("buildInvokePayload() = %v, want empty map (graceful fallback)", payload)
	}
}