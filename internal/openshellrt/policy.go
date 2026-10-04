package openshellrt

import (
	"fmt"
	"strings"

	osv1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"
	ostypes "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1/types"
)

// EgressRule is the AgentPaaS allowlist entry translated into an OpenShell
// network policy. Hosts not listed are denied by OpenShell default-deny.
type EgressRule struct {
	Domain  string
	Ports   []int
	Methods []string
}

// SandboxPolicy builds an OpenShell sandbox policy from the agent allowlist.
// The broker host, when non-empty, is bound to providerName so the OpenShell
// provider injects the credential. The denied host is simply absent.
func SandboxPolicy(rules []EgressRule, providerName, brokerHost string) *osv1.SandboxPolicy {
	endpoints := make([]osv1.PolicyNetworkEndpoint, 0, len(rules))
	for _, rule := range rules {
		host := strings.TrimSpace(strings.ToLower(rule.Domain))
		if host == "" {
			continue
		}
		port := uint32(443)
		if len(rule.Ports) > 0 && rule.Ports[0] > 0 {
			port = uint32(rule.Ports[0])
		}
		ep := osv1.PolicyNetworkEndpoint{
			Host:        host,
			Port:        port,
			Protocol:    "rest",
			Access:      osv1.NetworkAccessPresetReadWrite,
			Enforcement: osv1.NetworkEnforcementModeEnforce,
		}
		if brokerHost != "" && host == strings.ToLower(brokerHost) && providerName != "" {
			ep.CredentialBinding = &ostypes.NetworkCredentialBinding{Provider: providerName}
		}
		endpoints = append(endpoints, ep)
	}
	return &osv1.SandboxPolicy{
		Version: 1,
		NetworkPolicies: map[string]osv1.NetworkPolicyRule{
			"agentpaas-egress": {
				Name:      "agentpaas-egress",
				Endpoints: endpoints,
				Binaries: []osv1.PolicyNetworkBinary{
					{Path: "/agentpaas/harness"},
					{Path: "/usr/bin/python3"},
					{Path: "/usr/local/bin/python3"},
				},
			},
		},
	}
}

func providerName(credID string) string {
	var b strings.Builder
	b.WriteString("ap-")
	for _, r := range strings.ToLower(credID) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "ap" || name == "" {
		return "ap-broker"
	}
	return name
}

func brokerProfile() osv1.ProviderProfile {
	return osv1.ProviderProfile{
		ID:               profileID,
		DisplayName:      "AgentPaaS broker",
		Description:      "Brokered credential injected by the OpenShell provider, not the agent environment",
		Category:         osv1.ProfileCategoryInference,
		InferenceCapable: true,
		Credentials: []osv1.ProfileCredential{
			{
				Name:       "api_key",
				EnvVars:    []string{placeholderEnv},
				Required:   true,
				Secret:     true,
				AuthStyle:  "bearer",
				HeaderName: "authorization",
			},
		},
		// No endpoints. The pinned SDK NetworkEndpoint has only Host, Port,
		// and Protocol, and NetworkEndpointToProto copies only those three
		// fields. Protocol "rest" would be imported without access or rules
		// and the gateway rejects it. Any endpoint would also become the
		// credential boundary and block SandboxPolicy credential_binding.
		// Injection stays on SandboxPolicy, which sets Access and the binding.
		// The secret stays in the OpenShell provider, not the agent env.
		Binaries: []osv1.NetworkBinary{
			{Path: "/agentpaas/harness"},
			{Path: "/usr/bin/python3"},
			{Path: "/usr/local/bin/python3"},
		},
	}
}

func formatDiag(prefix string, diags []osv1.ProfileDiagnostic) string {
	if len(diags) == 0 {
		return prefix
	}
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		parts = append(parts, fmt.Sprintf("%s: %s", d.Field, d.Message))
	}
	return prefix + ": " + strings.Join(parts, "; ")
}
