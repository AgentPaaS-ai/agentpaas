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
	// agent with provider xai, policy has only openrouter.ai → error
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
	if err == nil {
		t.Fatal("ValidateLLMEgress(xai, policy without api.x.ai) = nil, want error")
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
	// agent with provider but nil policy → error
	agent := &AgentYAML{
		LLM: LLMConfig{
			Provider: "openai",
		},
	}
	err := ValidateLLMEgress(agent, nil)
	if err == nil {
		t.Fatal("ValidateLLMEgress(openai, nil) = nil, want error")
	}
}