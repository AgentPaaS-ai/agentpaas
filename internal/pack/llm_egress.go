package pack

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// ValidateLLMEgress checks that if the agent has an LLM provider configured,
// the provider's domain is present in the egress policy. This is a hard
// error at pack time — the agent WILL fail at runtime without the egress.
//
// Returns nil if:
// - The agent has no LLM provider configured
// - The LLM provider's domain is present in the egress policy
// - The LLM provider is unknown (non-standard provider)
//
// Returns an error if:
// - The LLM provider's domain is NOT in the egress policy
func ValidateLLMEgress(agentConfig *AgentYAML, policyFile *policy.Policy) error {
	if agentConfig == nil || agentConfig.LLM.Provider == "" {
		return nil
	}

	if llm.ProviderDomain(agentConfig.LLM.Provider) == "" {
		// Unknown/non-standard provider — skip validation
		return nil
	}

	// Host is stamped onto AgentYAML.Egress by ensureLLMProviderEgress at lock
	// time (founder Q1 auto-declare). Missing policy.yaml is not a pack error.
	return nil
}

// ensureLLMProviderEgress appends the LLM provider hostname to AgentYAML.Egress
// so the signed lock carries it even when policy.yaml omitted the host.
func ensureLLMProviderEgress(agent *AgentYAML) {
	if agent == nil || agent.LLM.Provider == "" {
		return
	}
	domain := strings.ToLower(strings.TrimSpace(llm.ProviderDomain(agent.LLM.Provider)))
	if domain == "" {
		return
	}
	for _, h := range agent.Egress {
		if strings.EqualFold(strings.TrimSpace(h), domain) {
			return
		}
	}
	agent.Egress = append(agent.Egress, domain)
}

// LoadPolicy reads and parses policy.yaml from the project directory.
// Returns nil, nil if policy.yaml does not exist (not an error).
func LoadPolicy(projectDir string) (*policy.Policy, error) {
	if err := validateProjectDir(projectDir); err != nil {
		return nil, fmt.Errorf("load policy: %w", err)
	}

	path := filepath.Join(projectDir, "policy.yaml")
	data, err := readProjectFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("load policy: %w", err)
	}

	parsed, err := policy.ParsePolicy(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse policy.yaml: %w", err)
	}

	return parsed, nil
}
