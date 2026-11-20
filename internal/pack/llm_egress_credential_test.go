package pack

import (
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func TestCompileLLMCredentialIntoPolicy_stampsBrokeredKeychain(t *testing.T) {
	agent := &AgentYAML{LLM: LLMConfig{Credential: "openrouter-key"}}
	pol := &policy.Policy{Version: "1.0", Credentials: nil}
	if !CompileLLMCredentialIntoPolicy(agent, pol) {
		t.Fatal("expected stamp")
	}
	if err := ValidateLLMCredentialBinding(agent, pol); err != nil {
		t.Fatalf("validate after compile: %v", err)
	}
	if len(pol.Credentials) != 1 || pol.Credentials[0].ID != "openrouter-key" {
		t.Fatalf("credentials = %+v", pol.Credentials)
	}
	if pol.Credentials[0].Type != "brokered" || pol.Credentials[0].Service != "keychain" {
		t.Fatalf("stamp shape %+v", pol.Credentials[0])
	}
}

func TestCompileLLMCredentialIntoPolicy_idempotent(t *testing.T) {
	agent := &AgentYAML{LLM: LLMConfig{Credential: "openrouter-key"}}
	pol := &policy.Policy{Credentials: []policy.Credential{{
		ID: "openrouter-key", Type: "brokered", Service: "keychain",
	}}}
	if CompileLLMCredentialIntoPolicy(agent, pol) {
		t.Fatal("should not restamp")
	}
}

func TestCompileLLMCredentialPolicyYAML_stampsLockBytes(t *testing.T) {
	agent := &AgentYAML{LLM: LLMConfig{Credential: "openrouter-key"}}
	src := []byte("version: \"1.0\"\nagent:\n  name: weather-agent\negress:\n  - domain: wttr.in\ncredentials: []\ningress: []\n")
	out, err := compileLLMCredentialPolicyYAML(agent, src)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("expected compiled bytes")
	}
	if !strings.Contains(string(out), "openrouter-key") {
		t.Fatalf("compiled policy missing credential:\n%s", out)
	}
}
