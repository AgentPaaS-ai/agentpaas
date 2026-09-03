package pack

import (
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestValidateLLMEgress_NoLLMConfig(t *testing.T) {
	// nil agent config should return nil
	err := ValidateLLMEgress(nil, nil)
	if err != nil {
		t.Errorf("ValidateLLMEgress(nil, nil) = %v, want nil", err)
	}
}

func TestValidateLLMEgress_NoProvider(t *testing.T) {
	// agent with empty LLM.Provider should return nil
	agent := &AgentYAML{
		LLM: LLMConfig{
			Provider: "",
		},
	}
	err := ValidateLLMEgress(agent, nil)
	if err != nil {
		t.Errorf("ValidateLLMEgress(empty provider, nil) = %v, want nil", err)
	}
}

func TestValidateLLMEgress_DomainPresent(t *testing.T) {
	// agent with provider xai, policy has api.x.ai → no error
	agent := &AgentYAML{
		LLM: LLMConfig{
			Provider: "xai",
		},
	}
	pol := &policy.Policy{
		Egress: []policy.EgressRule{
			{Domain: "api.x.ai"},
		},
	}
	err := ValidateLLMEgress(agent, pol)
	if err != nil {
		t.Errorf("ValidateLLMEgress(xai, policy with api.x.ai) = %v, want nil", err)
	}
}

func TestValidateLLMEgress_DomainMissing(t *testing.T) {
	// auto-declare covers a missing provider host; pack must not fail
	agent := &AgentYAML{
		LLM: LLMConfig{
			Provider: "xai",
		},
	}
	pol := &policy.Policy{
		Egress: []policy.EgressRule{
			{Domain: "openrouter.ai"},
		},
	}
	err := ValidateLLMEgress(agent, pol)
	if err != nil {
		t.Errorf("ValidateLLMEgress(xai, policy without api.x.ai) = %v, want nil", err)
	}
}

func TestValidateLLMEgress_UnknownProvider(t *testing.T) {
	// agent with provider "custom" → skip validation, no error
	agent := &AgentYAML{
		LLM: LLMConfig{
			Provider: "custom",
		},
	}
	err := ValidateLLMEgress(agent, nil)
	if err != nil {
		t.Errorf("ValidateLLMEgress(custom, nil) = %v, want nil", err)
	}
}

func TestValidateLLMEgress_WildcardMatch(t *testing.T) {
	// agent with provider openai, policy has *.openai.com with allow_wildcard → no error
	agent := &AgentYAML{
		LLM: LLMConfig{
			Provider: "openai",
		},
	}
	pol := &policy.Policy{
		Egress: []policy.EgressRule{
			{
				Domain:        "*.openai.com",
				AllowWildcard: boolPtr(true),
			},
		},
	}
	err := ValidateLLMEgress(agent, pol)
	if err != nil {
		t.Errorf("ValidateLLMEgress(openai, policy with *.openai.com wildcard) = %v, want nil", err)
	}
}

func TestValidateLLMEgress_NilPolicyWithProvider(t *testing.T) {
	// auto-declare covers a nil policy; pack must not fail
	agent := &AgentYAML{
		LLM: LLMConfig{
			Provider: "openai",
		},
	}
	err := ValidateLLMEgress(agent, nil)
	if err != nil {
		t.Errorf("ValidateLLMEgress(openai, nil) = %v, want nil", err)
	}
}

func TestEnsureLLMProviderEgress_AppendsOnce(t *testing.T) {
	agent := &AgentYAML{
		LLM:    LLMConfig{Provider: "openrouter"},
		Egress: []string{"wttr.in"},
	}
	ensureLLMProviderEgress(agent)
	if len(agent.Egress) != 2 || agent.Egress[0] != "wttr.in" || agent.Egress[1] != "openrouter.ai" {
		t.Fatalf("egress = %v, want [wttr.in openrouter.ai]", agent.Egress)
	}
	ensureLLMProviderEgress(agent)
	if len(agent.Egress) != 2 {
		t.Fatalf("second call mutated egress: %v", agent.Egress)
	}
}
