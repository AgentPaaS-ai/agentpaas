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

	domain := llm.ProviderDomain(agentConfig.LLM.Provider)
	if domain == "" {
		// Unknown/non-standard provider — skip validation
		return nil
	}

	if policyFile == nil {
		return fmt.Errorf("policy is required when LLM provider is configured")
	}

	for _, rule := range policyFile.Egress {
		if strings.EqualFold(rule.Domain, domain) {
			return nil
		}
		// Also check wildcard domains
		if rule.AllowWildcard != nil && *rule.AllowWildcard {
			// Check if the domain matches a wildcard pattern
			// e.g., *.openai.com matches api.openai.com
			if strings.HasPrefix(rule.Domain, "*.") {
				suffix := rule.Domain[1:] // ".openai.com"
				if strings.HasSuffix(domain, suffix) {
					return nil
				}
			}
		}
	}

	return fmt.Errorf(
		"LLM provider %q requires egress to %q:443 but it is not in the egress policy. "+
			"Add it to policy.yaml or run: agentpaas policy init --template allow-llm --provider %s",
		agentConfig.LLM.Provider, domain, agentConfig.LLM.Provider,
	)
}

// ensureLLMProviderEgress does not stamp the LLM provider hostname onto
// AgentYAML.Egress. Auto-declaring a host omitted from policy.yaml punches
// M16 SC3 default-deny. Policy-allowed hosts are copied at lock time by
// mergePolicyEgressIntoAgentYAML.
func ensureLLMProviderEgress(_ *AgentYAML) {
	// Default-deny: do not append the provider host.
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

func ValidateLLMCredentialBinding(agentConfig *AgentYAML, policyFile *policy.Policy) error {
	if agentConfig == nil {
		return nil
	}
	cred := strings.TrimSpace(agentConfig.LLM.Credential)
	if cred == "" {
		return nil
	}
	if policyFile == nil || len(policyFile.Credentials) == 0 {
		return fmt.Errorf("llm.credential %q declared but policy.yaml credentials is empty (brokered secret not policy-bound)", cred)
	}
	for _, c := range policyFile.Credentials {
		if strings.TrimSpace(c.ID) == cred {
			return nil
		}
	}
	return fmt.Errorf("llm.credential %q is not a declared policy credential id", cred)
}
