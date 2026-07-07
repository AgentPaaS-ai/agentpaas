package install

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// PolicyStructuralDiff is a locally computed diff between two policy YAML blobs.
type PolicyStructuralDiff struct {
	EgressAdded       []string
	EgressRemoved     []string
	CredentialsAdded  []string
	CredentialsRemoved []string
	MCPAdded          []string
	MCPRemoved        []string
	IngressAdded      []string
	IngressRemoved    []string
}

// ComputeStructuralPolicyDiff parses old and new policy bytes and diffs egress,
// credentials, MCP, and ingress sections (not signer-claimed deltas).
func ComputeStructuralPolicyDiff(oldYAML, newYAML []byte) (*PolicyStructuralDiff, error) {
	oldPol, err := policy.ParsePolicy(bytes.NewReader(oldYAML))
	if err != nil {
		return nil, fmt.Errorf("parse old policy: %w", err)
	}
	newPol, err := policy.ParsePolicy(bytes.NewReader(newYAML))
	if err != nil {
		return nil, fmt.Errorf("parse new policy: %w", err)
	}
	out := &PolicyStructuralDiff{}
	out.EgressAdded, out.EgressRemoved = diffStringSets(egressKeys(oldPol), egressKeys(newPol))
	out.CredentialsAdded, out.CredentialsRemoved = diffStringSets(credKeys(oldPol), credKeys(newPol))
	out.MCPAdded, out.MCPRemoved = diffStringSets(mcpKeys(oldPol), mcpKeys(newPol))
	out.IngressAdded, out.IngressRemoved = diffStringSets(ingressKeys(oldPol), ingressKeys(newPol))
	return out, nil
}

// FormatPolicyStructuralDiff renders human-readable diff lines for the consent card.
func FormatPolicyStructuralDiff(d *PolicyStructuralDiff) []string {
	if d == nil {
		return nil
	}
	var lines []string
	appendDiff := func(label string, added, removed []string) {
		for _, a := range added {
			lines = append(lines, fmt.Sprintf("+ %s: %s", label, a))
		}
		for _, r := range removed {
			lines = append(lines, fmt.Sprintf("- %s: %s", label, r))
		}
	}
	appendDiff("egress", d.EgressAdded, d.EgressRemoved)
	appendDiff("credential", d.CredentialsAdded, d.CredentialsRemoved)
	appendDiff("mcp", d.MCPAdded, d.MCPRemoved)
	appendDiff("ingress", d.IngressAdded, d.IngressRemoved)
	sort.Strings(lines)
	return lines
}

func diffStringSets(oldSet, newSet []string) (added, removed []string) {
	om := sliceToSet(oldSet)
	nm := sliceToSet(newSet)
	for k := range nm {
		if !om[k] {
			added = append(added, k)
		}
	}
	for k := range om {
		if !nm[k] {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func sliceToSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func egressKeys(pol *policy.Policy) []string {
	if pol == nil {
		return nil
	}
	var keys []string
	for _, e := range pol.Egress {
		if strings.TrimSpace(e.CIDR) != "" {
			keys = append(keys, "cidr:"+e.CIDR)
			continue
		}
		d := strings.TrimSpace(e.Domain)
		if d == "" {
			continue
		}
		ports := e.Ports
		if len(ports) == 0 {
			ports = []int{443}
		}
		var ps []string
		for _, p := range ports {
			ps = append(ps, fmt.Sprintf("%d", p))
		}
		keys = append(keys, fmt.Sprintf("domain:%s ports:%s", d, strings.Join(ps, ",")))
	}
	return keys
}

func credKeys(pol *policy.Policy) []string {
	if pol == nil {
		return nil
	}
	var keys []string
	for _, c := range pol.Credentials {
		keys = append(keys, fmt.Sprintf("id=%s type=%s", c.ID, c.Type))
	}
	return keys
}

func mcpKeys(pol *policy.Policy) []string {
	if pol == nil {
		return nil
	}
	var keys []string
	for _, m := range pol.MCPServers {
		keys = append(keys, fmt.Sprintf("server=%s url=%s", m.Name, m.URL))
	}
	return keys
}

func ingressKeys(pol *policy.Policy) []string {
	if pol == nil {
		return nil
	}
	var keys []string
	for _, in := range pol.Ingress {
		keys = append(keys, fmt.Sprintf("path=%s port=%d", in.Path, in.Port))
	}
	return keys
}