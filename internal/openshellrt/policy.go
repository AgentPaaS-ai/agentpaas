package openshellrt

import (
	"crypto/sha256"
	"encoding/hex"
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
	// AllowWildcard is the policy allow_wildcard gate. Nil and false both
	// block a wildcard domain, matching the policy compiler. True is not
	// enough: the domain is still omitted unless the OpenShell glob means
	// the same match.
	AllowWildcard *bool
}

// SandboxPolicy builds an OpenShell sandbox policy from the agent allowlist.
// The broker host, when non-empty, is bound to providerName so the OpenShell
// provider injects the credential. The denied host is simply absent.
func SandboxPolicy(rules []EgressRule, providerName, brokerHost string) *osv1.SandboxPolicy {
	endpoints := make([]osv1.PolicyNetworkEndpoint, 0, len(rules))
	for _, rule := range rules {
		host := strings.TrimSpace(strings.ToLower(rule.Domain))
		if host == "" || EgressWildcardBlocked(host, rule.AllowWildcard) {
			continue
		}
		ports := declaredPorts(rule.Ports)
		// A declared list that is entirely outside 1-65535 must not become an
		// endpoint. preferredPort would otherwise substitute 443, and a uint16
		// truncation of 65536 is port 0, which OpenShell treats as unrestricted.
		if len(rule.Ports) > 0 && len(ports) == 0 {
			continue
		}
		ep := osv1.PolicyNetworkEndpoint{
			Host:        host,
			Port:        preferredPort(ports),
			Ports:       ports,
			Protocol:    "rest",
			Enforcement: osv1.NetworkEnforcementModeEnforce,
		}
		// Access and Rules are mutually exclusive. ReadWrite with an empty
		// rule list is not a method allow. Unrestricted hosts keep the preset.
		if allows := methodAllowRules(rule.Methods); len(allows) > 0 {
			ep.Rules = allows
		} else {
			ep.Access = osv1.NetworkAccessPresetReadWrite
		}
		if brokerHost != "" && host == strings.ToLower(brokerHost) && providerName != "" {
			ep.CredentialBinding = &ostypes.NetworkCredentialBinding{Provider: providerName}
		}
		endpoints = append(endpoints, ep)
	}
	return &osv1.SandboxPolicy{
		Version: 1,
		// An empty filesystem policy is replaced by the v0.1.1 proxy
		// baseline, which does not include /agentpaas. Landlock then
		// denies exec of /agentpaas/harness (os error 13) even though
		// the file is mode 0555. A set policy replaces that baseline,
		// so the entrypoint and the baseline paths must both be listed.
		Filesystem: &osv1.FilesystemPolicy{
			IncludeWorkdir: true,
			ReadOnly: []string{
				"/agentpaas",
				"/usr",
				"/lib",
				"/etc",
				"/app",
				"/var/log",
				"/proc",
				"/dev/urandom",
			},
		},
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
	// Hash the original id so cred-a and cred_a do not collapse to one name.
	sum := sha256.Sum256([]byte(credID))
	hash := hex.EncodeToString(sum[:8])
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
		name = "ap-broker"
	}
	suffix := "-" + hash
	const maxProviderName = 63
	if len(name)+len(suffix) > maxProviderName {
		name = "ap"
	}
	return name + suffix
}

// EgressWildcardBlocked reports whether domain must not be copied into an
// OpenShell host. The policy compiler skips a domain that contains '*' unless
// allow_wildcard is explicitly true. OpenShell '*.example.com' is a one-label
// DNS glob, so evil.example.com matches it. Policy '*.example.com' is an
// any-depth suffix and not the apex. Those matches differ, so a wildcard
// domain is omitted even when the gate is true.
func EgressWildcardBlocked(domain string, allowWildcard *bool) bool {
	host := strings.TrimSpace(strings.ToLower(domain))
	if host == "" || !strings.Contains(host, "*") {
		return false
	}
	if allowWildcard == nil || !*allowWildcard {
		return true
	}
	return !openShellGlobMatchesPolicyWildcard(host)
}

// openShellGlobMatchesPolicyWildcard reports whether an OpenShell host glob
// allows exactly the hosts the policy wildcard allows. It does not: policy
// '*.<base>' matches nested labels, and OpenShell '*' matches one label.
func openShellGlobMatchesPolicyWildcard(domain string) bool {
	if !strings.HasPrefix(domain, "*.") || strings.Count(domain, "*") != 1 {
		return false
	}
	base := domain[2:]
	if base == "" || strings.Contains(base, "*") {
		return false
	}
	nested := "a.b." + base
	policyMatchesNested := strings.HasSuffix(nested, "."+base)
	openShellStarMatchesNested := strings.Count(nested, ".") == strings.Count(base, ".")+1
	return policyMatchesNested && openShellStarMatchesNested
}

func declaredPorts(ports []int) []uint32 {
	if len(ports) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(ports))
	seen := make(map[int]struct{}, len(ports))
	for _, p := range ports {
		if p < 1 || p > 65535 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, uint32(p))
	}
	return out
}

// preferredPort matches the policy compiler: 443 when it is listed, otherwise
// the first declared port, otherwise 443. The Ports slice still carries every
// declared port. OpenShell uses that slice when it is set.
func preferredPort(ports []uint32) uint32 {
	if len(ports) == 0 {
		return 443
	}
	for _, p := range ports {
		if p == 443 {
			return 443
		}
	}
	return ports[0]
}

func methodAllowRules(methods []string) []osv1.L7Rule {
	if len(methods) == 0 {
		return nil
	}
	out := make([]osv1.L7Rule, 0, len(methods))
	for _, method := range methods {
		method = strings.TrimSpace(method)
		if method == "" {
			continue
		}
		out = append(out, osv1.L7Rule{
			Allow: &osv1.L7Allow{
				Method: method,
				// "**" is the documented any-path glob. An empty path is not.
				Path: "**",
			},
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

// brokerProvider is the provider Ensure sends. ProfileWorkspace must be the
// workspace the profile was imported into. An empty value is the global scope,
// and Providers().Ensure then reports the workspace profile as missing even
// when Profiles().Get succeeded. The credential key is the profile env var:
// that is the key the gateway accepts and the placeholder it injects. The
// secret value stays in this spec, not in the agent environment.
func brokerProvider(name, secret string) *osv1.Provider {
	key := placeholderEnv
	if profile := brokerProfile(); len(profile.Credentials) == 1 && len(profile.Credentials[0].EnvVars) == 1 && profile.Credentials[0].EnvVars[0] != "" {
		key = profile.Credentials[0].EnvVars[0]
	}
	return &osv1.Provider{
		Name: name,
		Type: profileID,
		Spec: osv1.ProviderSpec{
			Credentials:      map[string]string{key: secret},
			ProfileWorkspace: workspaceDefault,
		},
	}
}

// profileVisibleToEnsure reports whether a profile returned by
// Profiles().Get is in the scope Providers().Ensure uses. Get returns the
// workspace catalog's effective profile. Ensure looks up provider.profile_workspace.
// Empty is the global scope and does not see a workspace-scoped import.
func profileVisibleToEnsure(p *osv1.ProviderProfile, profileWorkspace string) bool {
	if p == nil || p.ID != profileID {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(p.Scope)) {
	case "workspace":
		return profileWorkspace == workspaceDefault
	case "platform", "":
		return profileWorkspace == "" || profileWorkspace == workspaceDefault
	default:
		return false
	}
}

func importAlreadyPresent(diags []osv1.ProfileDiagnostic) bool {
	if len(diags) == 0 {
		return false
	}
	present := false
	for _, d := range diags {
		msg := strings.ToLower(d.Message)
		if strings.Contains(msg, "already exists") {
			present = true
			continue
		}
		if d.Severity == "" || strings.EqualFold(d.Severity, "error") {
			return false
		}
	}
	return present
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
