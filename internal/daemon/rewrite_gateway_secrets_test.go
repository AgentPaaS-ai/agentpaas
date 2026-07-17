package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

func TestRewriteGatewayConfigSecrets_SubstitutesAPIKey(t *testing.T) {
	fake := secrets.NewFakeKeyStore()
	if err := fake.Set(context.Background(), "trigger-api-key", []byte(`sk-test"quoted`)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	server := testControlServerForPayload(t, fake)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	placeholder := policy.SecretPlaceholder("trigger-api-key")
	raw := "policies:\n  apiKey:\n    keys:\n      - key: " + placeholder + "\n"
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &policy.Policy{
		IngressAuth: &policy.IngressAuth{
			Type: "api_key",
			APIKey: &policy.APIKeyAuth{
				Header:     "X-API-Key",
				Credential: "trigger-api-key",
			},
		},
	}
	if err := server.rewriteGatewayConfigSecrets(cfgPath, p); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if strings.Contains(out, placeholder) {
		t.Fatalf("placeholder remained:\n%s", out)
	}
	if !strings.Contains(out, "sk-test") {
		t.Fatalf("secret missing:\n%s", out)
	}
	if !strings.Contains(out, `\"`) && !strings.Contains(out, "sk-test") {
		t.Fatalf("expected YAML-escaped quote form, got:\n%s", out)
	}
}

func TestRewriteGatewayConfigSecrets_FailClosedMissingCred(t *testing.T) {
	fake := secrets.NewFakeKeyStore()
	server := testControlServerForPayload(t, fake)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	placeholder := policy.SecretPlaceholder("missing-key")
	if err := os.WriteFile(cfgPath, []byte("key: "+placeholder+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &policy.Policy{
		IngressAuth: &policy.IngressAuth{
			Type:   "api_key",
			APIKey: &policy.APIKeyAuth{Credential: "missing-key"},
		},
	}
	err := server.rewriteGatewayConfigSecrets(cfgPath, p)
	if err == nil {
		t.Fatal("expected error for missing credential")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildInvokePayload_GuardrailsAndSystemPrompt(t *testing.T) {
	server := testControlServerForPayload(t, nil)
	writeDeployedLock(t, server.homePaths, "policy-agent", &pack.AgentYAML{
		Name:    "policy-agent",
		Version: "1.0.0",
	})

	deployedDir := pack.DeployedAgentPath(server.homePaths.Home, "policy-agent")
	policyYAML := `version: "1.0"
agent:
  name: policy-agent
egress:
  - domain: openrouter.ai
    ports: [443]
guardrails:
  - type: regex
    pattern: "(?i)password"
    action: block
transformations:
  request:
    inject_system_prompt: "Be concise."
`
	if err := os.WriteFile(filepath.Join(deployedDir, "policy.yaml"), []byte(policyYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := server.buildInvokePayload(context.Background(), "policy-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload: %v", err)
	}
	gsAny, ok := payload["guardrails"]
	if !ok {
		t.Fatalf("guardrails missing from payload: %#v", payload)
	}
	switch gs := gsAny.(type) {
	case []map[string]any:
		if len(gs) != 1 {
			t.Fatalf("guardrails len=%d", len(gs))
		}
	case []any:
		if len(gs) != 1 {
			t.Fatalf("guardrails len=%d", len(gs))
		}
	default:
		t.Fatalf("unexpected guardrails type %T", gsAny)
	}
	if payload["inject_system_prompt"] != "Be concise." {
		t.Fatalf("inject_system_prompt = %#v", payload["inject_system_prompt"])
	}
}
