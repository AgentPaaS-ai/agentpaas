package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

// writeDeployedPolicy writes a policy.yaml to the deployed agent directory.
func writeDeployedPolicy(t *testing.T, hp *home.HomePaths, agentName, policyYAML string) {
	t.Helper()
	deployedDir := pack.DeployedAgentPath(hp.Home, agentName)
	if err := os.MkdirAll(deployedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll deployed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deployedDir, "policy.yaml"), []byte(policyYAML), 0o600); err != nil {
		t.Fatalf("WriteFile policy.yaml: %v", err)
	}
}

// TestBuildInvokePayload_PolicyCredential resolves a credential declared in
// policy.yaml (not the LLM credential) and verifies it appears in the payload.
func TestBuildInvokePayload_PolicyCredential(t *testing.T) {
	fakeStore := secrets.NewFakeKeyStore()
	if err := fakeStore.Set(context.Background(), "test-api-key", []byte("«redacted:test-key»")); err != nil {
		t.Fatalf("FakeKeyStore.Set: %v", err)
	}

	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "cred-agent", &pack.AgentYAML{
		Name:    "cred-agent",
		Version: "1.0.0",
		// No LLM config — credential comes from policy.yaml only
	})
	writeDeployedPolicy(t, server.homePaths, "cred-agent", `version: "1.0"
agent:
  name: cred-agent
  description: "Test credential from policy"
egress:
  - domain: httpbin.org
    ports: [443]
credentials:
  - id: test-api-key
    type: header
    header: X-API-Key
`)

	payload, err := server.buildInvokePayload(context.Background(), "cred-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v, want nil", err)
	}

	// Should have credentials section (no LLM since no llm config).
	creds, ok := payload["credentials"].([]map[string]any)
	if !ok || len(creds) != 1 {
		t.Fatalf("payload[\"credentials\"] missing or wrong: %v", payload)
	}
	if creds[0]["id"] != "test-api-key" {
		t.Errorf("credentials[0].id = %q, want test-api-key", creds[0]["id"])
	}
	if creds[0]["header"] != "X-API-Key" {
		t.Errorf("credentials[0].header = %q, want X-API-Key", creds[0]["header"])
	}
	// Credential values are NOT in the payload — they come from a sidecar file.
	if _, hasValue := creds[0]["value"]; hasValue {
		t.Errorf("credentials[0].value should not be present in payload (values come from sidecar)")
	}

	// Should NOT have LLM section
	if _, hasLLM := payload["llm"]; hasLLM {
		t.Errorf("payload should not have llm section (no LLM config)")
	}
}

// TestBuildInvokePayload_PolicyCredentialDefaultHeader verifies that a
// credential with no header field defaults to "Authorization".
func TestBuildInvokePayload_PolicyCredentialDefaultHeader(t *testing.T) {
	fakeStore := secrets.NewFakeKeyStore()
	if err := fakeStore.Set(context.Background(), "my-cred", []byte("secret-value")); err != nil {
		t.Fatalf("FakeKeyStore.Set: %v", err)
	}

	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "def-header-agent", &pack.AgentYAML{
		Name:    "def-header-agent",
		Version: "1.0.0",
	})
	writeDeployedPolicy(t, server.homePaths, "def-header-agent", `version: "1.0"
agent:
  name: def-header-agent
egress:
  - domain: api.example.com
    ports: [443]
credentials:
  - id: my-cred
    type: header
`)

	payload, err := server.buildInvokePayload(context.Background(), "def-header-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v", err)
	}
	creds, ok := payload["credentials"].([]map[string]any)
	if !ok || len(creds) != 1 {
		t.Fatalf("credentials missing: %v", payload)
	}
	if creds[0]["header"] != "Authorization" {
		t.Errorf("header = %q, want Authorization (default)", creds[0]["header"])
	}
}

// TestBuildInvokePayload_BothLLMAndPolicyCreds verifies that both the LLM
// credential and a policy-declared credential are resolved and injected,
// without duplicates if they share the same ID.
func TestBuildInvokePayload_BothLLMAndPolicyCreds(t *testing.T) {
	fakeStore := secrets.NewFakeKeyStore()
	if err := fakeStore.Set(context.Background(), "openrouter-key", []byte("«redacted:or-key»")); err != nil {
		t.Fatalf("FakeKeyStore.Set: %v", err)
	}
	if err := fakeStore.Set(context.Background(), "api-key-2", []byte("«redacted:api2»")); err != nil {
		t.Fatalf("FakeKeyStore.Set: %v", err)
	}

	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "both-agent", &pack.AgentYAML{
		Name:    "both-agent",
		Version: "1.0.0",
		LLM: pack.LLMConfig{
			Provider:   "openrouter",
			Model:      "deepseek/deepseek-v4-flash",
			Credential: "openrouter-key",
		},
	})
	writeDeployedPolicy(t, server.homePaths, "both-agent", `version: "1.0"
agent:
  name: both-agent
egress:
  - domain: openrouter.ai
    ports: [443]
  - domain: api.example.com
    ports: [443]
credentials:
  - id: openrouter-key
    type: header
    header: Authorization
  - id: api-key-2
    type: header
    header: X-Custom-Auth
`)

	payload, err := server.buildInvokePayload(context.Background(), "both-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v", err)
	}

	// Should have LLM section
	llm, ok := payload["llm"].(map[string]any)
	if !ok {
		t.Fatalf("payload[\"llm\"] missing: %v", payload)
	}
	if llm["credential"] != "openrouter-key" {
		t.Errorf("llm.credential = %q, want openrouter-key", llm["credential"])
	}

	// Should have 2 credentials (openrouter-key + api-key-2, no duplicate)
	creds, ok := payload["credentials"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[\"credentials\"] missing: %v", payload)
	}
	if len(creds) != 2 {
		t.Fatalf("expected 2 credentials, got %d: %v", len(creds), creds)
	}

	// Verify both credential IDs are present
	credIDs := make(map[string]bool)
	for _, c := range creds {
		credIDs[c["id"].(string)] = true
	}
	if !credIDs["openrouter-key"] {
		t.Errorf("openrouter-key not in credentials")
	}
	if !credIDs["api-key-2"] {
		t.Errorf("api-key-2 not in credentials")
	}
}

// TestBuildInvokePayload_PolicyCredNotFound verifies that a policy-declared
// credential that doesn't exist in Keychain is silently skipped (graceful).
func TestBuildInvokePayload_PolicyCredNotFound(t *testing.T) {
	fakeStore := secrets.NewFakeKeyStore()
	// No credentials stored

	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "missing-cred-agent", &pack.AgentYAML{
		Name:    "missing-cred-agent",
		Version: "1.0.0",
	})
	writeDeployedPolicy(t, server.homePaths, "missing-cred-agent", `version: "1.0"
agent:
  name: missing-cred-agent
egress:
  - domain: api.example.com
    ports: [443]
credentials:
  - id: nonexistent-key
    type: header
    header: Authorization
`)

	payload, err := server.buildInvokePayload(context.Background(), "missing-cred-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v, want nil", err)
	}
	// Credential metadata (id+header) is always included even when Keychain
	// resolution fails. Values come from the sidecar file, not the payload.
	creds, ok := payload["credentials"].([]map[string]any)
	if !ok || len(creds) != 1 {
		t.Fatalf("payload[%q] missing or wrong count: %v", "credentials", payload)
	}
	if creds[0]["id"] != "nonexistent-key" {
		t.Errorf("credentials[0].id = %q, want nonexistent-key", creds[0]["id"])
	}
	if _, hasValue := creds[0]["value"]; hasValue {
		t.Errorf("credentials[0].value should not be present (values come from sidecar)")
	}
}

// TestBuildInvokePayload_PolicyCredWithLLMNoDup verifies that when the LLM
// credential and a policy credential share the same ID, only one entry exists.
func TestBuildInvokePayload_PolicyCredWithLLMNoDup(t *testing.T) {
	fakeStore := secrets.NewFakeKeyStore()
	if err := fakeStore.Set(context.Background(), "shared-key", []byte("shared-value")); err != nil {
		t.Fatalf("FakeKeyStore.Set: %v", err)
	}

	server := testControlServerForPayload(t, fakeStore)
	writeDeployedLock(t, server.homePaths, "shared-agent", &pack.AgentYAML{
		Name:    "shared-agent",
		Version: "1.0.0",
		LLM: pack.LLMConfig{
			Provider:   "openai",
			Model:      "gpt-4o",
			Credential: "shared-key",
		},
	})
	writeDeployedPolicy(t, server.homePaths, "shared-agent", `version: "1.0"
agent:
  name: shared-agent
egress:
  - domain: api.openai.com
    ports: [443]
credentials:
  - id: shared-key
    type: header
    header: Authorization
`)

	payload, err := server.buildInvokePayload(context.Background(), "shared-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload() error = %v", err)
	}
	creds, ok := payload["credentials"].([]map[string]any)
	if !ok {
		t.Fatalf("credentials missing: %v", payload)
	}
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential (dedup), got %d: %v", len(creds), creds)
	}
	if creds[0]["id"] != "shared-key" {
		t.Errorf("id = %q, want shared-key", creds[0]["id"])
	}
}
