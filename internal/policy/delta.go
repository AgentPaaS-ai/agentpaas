package policy

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PolicyDelta records additions and removals between parent and child canonical
// policies. Field names and JSON tags match pack.PolicyDelta for trivial conversion.
type PolicyDelta struct {
	EgressAdded        []string `json:"egress_added,omitempty"`
	EgressRemoved      []string `json:"egress_removed,omitempty"`
	CredentialsAdded   []string `json:"credentials_added,omitempty"`
	CredentialsRemoved []string `json:"credentials_removed,omitempty"`
	MCPToolsAdded      []string `json:"mcp_tools_added,omitempty"`
	MCPToolsRemoved    []string `json:"mcp_tools_removed,omitempty"`
	IngressAdded       []string `json:"ingress_added,omitempty"`
	IngressRemoved     []string `json:"ingress_removed,omitempty"`
	HooksAdded         []string `json:"hooks_added,omitempty"`
	HooksRemoved       []string `json:"hooks_removed,omitempty"`
	ModelRoutesAdded   []string `json:"model_routes_added,omitempty"`
	ModelRoutesRemoved []string `json:"model_routes_removed,omitempty"`
	RoutedRunChanged   bool     `json:"routed_run_changed,omitempty"`
}

// ComputeDelta compares canonical forms of parent and child policy YAML.
// It returns nil (JSON null) when there is no difference.
func ComputeDelta(parentYAML, childYAML []byte) (*PolicyDelta, error) {
	parent, err := ParsePolicy(bytes.NewReader(parentYAML))
	if err != nil {
		return nil, fmt.Errorf("compute delta: %w", err)
	}
	child, err := ParsePolicy(bytes.NewReader(childYAML))
	if err != nil {
		return nil, fmt.Errorf("compute delta: %w", err)
	}

	parentC, _ := Canonicalize(parent) // warnings discarded; digest/validation uses result
	childC, _ := Canonicalize(child)   // warnings discarded; digest/validation uses result

	delta := &PolicyDelta{}

	delta.EgressAdded, delta.EgressRemoved = diffEgress(parentC.Egress, childC.Egress)
	delta.CredentialsAdded, delta.CredentialsRemoved = diffStringSets(
		credentialLabels(parentC.Credentials),
		credentialLabels(childC.Credentials),
	)
	delta.MCPToolsAdded, delta.MCPToolsRemoved = diffStringSets(
		mcpLabels(parentC.MCPServers),
		mcpLabels(childC.MCPServers),
	)
	delta.HooksAdded, delta.HooksRemoved = diffStringSets(
		hookLabels(parentC.Hooks),
		hookLabels(childC.Hooks),
	)
	delta.IngressAdded, delta.IngressRemoved = diffStringSets(
		ingressLabels(parentC.Ingress),
		ingressLabels(childC.Ingress),
	)

	// --- ModelRoutes: compare by route key ---
	delta.ModelRoutesAdded, delta.ModelRoutesRemoved = diffStringSets(
		modelRouteKeys(parentC.ModelRoutes),
		modelRouteKeys(childC.ModelRoutes),
	)

	// --- RoutedRun: compare presence ---
	parentHasRoutedRun := parentC.RoutedRun != nil
	childHasRoutedRun := childC.RoutedRun != nil
	if parentHasRoutedRun != childHasRoutedRun {
		delta.RoutedRunChanged = true
	}

	if delta.isEmpty() {
		return nil, nil
	}
	return delta, nil
}

func (d *PolicyDelta) isEmpty() bool {
	if d == nil {
		return true
	}
	return len(d.EgressAdded) == 0 &&
		len(d.EgressRemoved) == 0 &&
		len(d.CredentialsAdded) == 0 &&
		len(d.CredentialsRemoved) == 0 &&
		len(d.MCPToolsAdded) == 0 &&
		len(d.MCPToolsRemoved) == 0 &&
		len(d.IngressAdded) == 0 &&
		len(d.IngressRemoved) == 0 &&
		len(d.HooksAdded) == 0 &&
		len(d.HooksRemoved) == 0 &&
		len(d.ModelRoutesAdded) == 0 &&
		len(d.ModelRoutesRemoved) == 0 &&
		!d.RoutedRunChanged
}

func credentialLabels(creds []CanonicalCredential) []string {
	out := make([]string, len(creds))
	for i, c := range creds {
		out[i] = c.ID
	}
	return out
}

func mcpLabels(servers []CanonicalMCPServer) []string {
	out := make([]string, len(servers))
	for i, s := range servers {
		out[i] = s.Name
	}
	return out
}

func hookLabels(hooks []CanonicalHook) []string {
	out := make([]string, len(hooks))
	for i, h := range hooks {
		out[i] = h.Name
	}
	return out
}

func ingressLabels(rules []CanonicalIngressRule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = fmt.Sprintf("%s:%d", r.Path, r.Port)
	}
	return out
}

func canonicalEgressDiffKey(r CanonicalEgressRule) string {
	ports := sortedInts(r.Ports)
	aw := formatBoolPtr(r.AllowWildcard)
	ap := formatBoolPtr(r.AllowPrivate)
	return fmt.Sprintf("%s|%s|%v|%s|%s", r.Domain, r.CIDR, ports, aw, ap)
}

func egressDeltaLabel(r CanonicalEgressRule) string {
	host := r.Domain
	if host == "" {
		host = r.CIDR
	}
	ports := sortedInts(r.Ports)
	if len(ports) == 0 {
		return host
	}
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = strconv.Itoa(p)
	}
	return host + ":" + strings.Join(parts, ",")
}

func egressLabelMaps(rules []CanonicalEgressRule) map[string]string {
	m := make(map[string]string, len(rules))
	for _, r := range rules {
		k := canonicalEgressDiffKey(r)
		m[k] = egressDeltaLabel(r)
	}
	return m
}

func diffEgress(parent, child []CanonicalEgressRule) (added, removed []string) {
	return diffLabelMaps(egressLabelMaps(parent), egressLabelMaps(child))
}

func diffLabelMaps(parentMap, childMap map[string]string) (added, removed []string) {
	var addKeys, remKeys []string
	for k := range childMap {
		if _, ok := parentMap[k]; !ok {
			addKeys = append(addKeys, k)
		}
	}
	for k := range parentMap {
		if _, ok := childMap[k]; !ok {
			remKeys = append(remKeys, k)
		}
	}
	sort.Strings(addKeys)
	sort.Strings(remKeys)
	for _, k := range addKeys {
		added = append(added, childMap[k])
	}
	for _, k := range remKeys {
		removed = append(removed, parentMap[k])
	}
	return added, removed
}

func diffStringSets(parent, child []string) (added, removed []string) {
	parentSet := stringSet(parent)
	childSet := stringSet(child)
	for k := range childSet {
		if !parentSet[k] {
			added = append(added, k)
		}
	}
	for k := range parentSet {
		if !childSet[k] {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func stringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func modelRouteKeys(routes map[string]CanonicalModelRoute) []string {
	if len(routes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
