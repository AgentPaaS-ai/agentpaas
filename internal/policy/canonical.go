package policy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/idna"
)

// CanonicalPolicy is the deterministic, canonical form of Policy used for
// digest computation. All slices are sorted by their natural key, maps are
// sorted by key (guaranteed by JSON encoding), secret values are redacted,
// domains are lowercased and punycode-normalized, and duplicate entries are
// removed with warnings.
type CanonicalPolicy struct {
	Version     string                  `json:"version"`
	Agent       CanonicalAgentConfig    `json:"agent"`
	Egress      []CanonicalEgressRule   `json:"egress,omitempty"`
	Credentials []CanonicalCredential   `json:"credentials,omitempty"`
	MCPServers  []CanonicalMCPServer    `json:"mcp_servers,omitempty"`
	Hooks       []CanonicalHook         `json:"hooks,omitempty"`
	Ingress     []CanonicalIngressRule  `json:"ingress,omitempty"`
}

// CanonicalAgentConfig is the canonical form of AgentConfig.
type CanonicalAgentConfig struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CanonicalEgressRule is the canonical form of EgressRule.
type CanonicalEgressRule struct {
	Domain        string `json:"domain,omitempty"`
	CIDR          string `json:"cidr,omitempty"`
	Ports         []int  `json:"ports"`
	AllowWildcard *bool  `json:"allow_wildcard,omitempty"`
	AllowPrivate  *bool  `json:"allow_private,omitempty"`
}

// CanonicalCredential is the canonical form of Credential.
// Secret values (Value) are redacted — only the ID and non-secret metadata remain.
type CanonicalCredential struct {
	ID      string `json:"id"`
	Type    string `json:"type,omitempty"`
	Header  string `json:"header,omitempty"`
	Service string `json:"service,omitempty"`
	Path    string `json:"path,omitempty"`
	// Value is deliberately absent — secret values never enter canonical form.
}

// CanonicalMCPServer is the canonical form of MCPServer.
type CanonicalMCPServer struct {
	Name    string            `json:"name"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// CanonicalHook is the canonical form of Hook.
// Secret values (Secret) are redacted.
type CanonicalHook struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Secret is deliberately absent — secret values never enter canonical form.
}

// CanonicalIngressRule is the canonical form of IngressRule.
type CanonicalIngressRule struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// egressRuleKey returns a sortable key for deduplication + ordering.
// Uses normalized domain (lowercased, trailing-dot-stripped), sorted ports,
// and security flags (AllowWildcard, AllowPrivate) to ensure rules with
// different semantics are not wrongly deduplicated.
func egressRuleKey(e EgressRule) string {
	// Normalize domain for dedup (lowercase, IDNA, strip trailing dot)
	domain := normalizeDomain(e.Domain)
	ports := sortedInts(e.Ports)
	aw := formatBoolPtr(e.AllowWildcard)
	ap := formatBoolPtr(e.AllowPrivate)
	return fmt.Sprintf("%s|%s|%v|%s|%s", domain, e.CIDR, ports, aw, ap)
}

// formatBoolPtr formats a *bool for use in dedup keys.
func formatBoolPtr(b *bool) string {
	if b == nil {
		return "nil"
	}
	return fmt.Sprintf("%t", *b)
}

// normalizeDomain lowercases the domain and applies IDNA punycode conversion.
// Returns the normalized domain. On punycode error, returns the lowercased
// original — the domain will be rejected by downstream validation if it's
// non-normalizable.
func normalizeDomain(domain string) string {
	if domain == "" {
		return ""
	}
	// Strip trailing dot (RFC 1034 — fully-qualified domain name indicator)
	domain = strings.TrimSuffix(domain, ".")
	// First lowercase
	lower := strings.ToLower(domain)

	// Only attempt punycode if there are non-ASCII characters (possible IDN)
	hasNonASCII := false
	for i := 0; i < len(lower); i++ {
		if lower[i] > 127 {
			hasNonASCII = true
			break
		}
	}
	if !hasNonASCII {
		return lower
	}

	// Split labels, normalize each, rejoin
	labels := strings.Split(lower, ".")
	for i, label := range labels {
		if !isASCII(label) {
			puny, err := idna.ToASCII(label)
			if err == nil {
				labels[i] = puny
			}
			// On error, leave the original — fails closed at validation
		}
	}
	return strings.Join(labels, ".")
}

// isASCII checks whether a string contains only ASCII characters.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

// Canonicalize converts a parsed Policy to its canonical form.
// It returns the canonical policy and any deduplication warnings.
// The canonical form:
//   - Sorts all slices by their natural key
//   - Lowercases and punycode-normalizes domains
//   - Redacts secret values (credential.value, hook.secret)
//   - Deduplicates equivalent entries with warnings
//   - Removes comments (comments are already absent from the parsed struct)
//   - Expands defaults (inferred transport, etc.)
func Canonicalize(p *Policy) (*CanonicalPolicy, []string) {
	if p == nil {
		return nil, nil
	}
	var warnings []string

	cp := &CanonicalPolicy{
		Version: p.Version,
		Agent: CanonicalAgentConfig{
			Name:        p.Agent.Name,
			Description: p.Agent.Description,
		},
	}

	// --- Egress: deduplicate and sort ---
	cp.Egress = canonicalizeEgress(p.Egress, &warnings)

	// --- Credentials: deduplicate by ID and sort ---
	cp.Credentials = canonicalizeCredentials(p.Credentials, &warnings)

	// --- MCP Servers: deduplicate by Name and sort ---
	cp.MCPServers = canonicalizeMCPServers(p.MCPServers, &warnings)

	// --- Hooks: deduplicate by Name and sort ---
	cp.Hooks = canonicalizeHooks(p.Hooks, &warnings)

	// --- Ingress: deduplicate by Path and sort ---
	cp.Ingress = canonicalizeIngress(p.Ingress, &warnings)

	return cp, warnings
}

func canonicalizeEgress(rules []EgressRule, warnings *[]string) []CanonicalEgressRule {
	if len(rules) == 0 {
		return nil
	}

	// Build a sorted copy
	sorted := make([]EgressRule, len(rules))
	copy(sorted, rules)

	// Sort by domain+cidr+ports key
	sort.SliceStable(sorted, func(i, j int) bool {
		return egressRuleKey(sorted[i]) < egressRuleKey(sorted[j])
	})

	// Deduplicate using a set of seen keys
	seen := make(map[string]bool)
	var result []CanonicalEgressRule

	for _, r := range sorted {
		key := egressRuleKey(r)
		if seen[key] {
			domainStr := r.Domain
			if domainStr == "" {
				domainStr = r.CIDR
			}
			*warnings = append(*warnings,
				fmt.Sprintf("dedup: duplicate egress rule %q (ports %v) removed", domainStr, r.Ports))
			continue
		}
		seen[key] = true

		// Normalize domain
		domain := normalizeDomain(r.Domain)

		cr := CanonicalEgressRule{
			Domain:        domain,
			CIDR:          r.CIDR,
			Ports:         sortedInts(r.Ports),
			AllowWildcard: r.AllowWildcard,
			AllowPrivate:  r.AllowPrivate,
		}
		result = append(result, cr)
	}
	return result
}

func canonicalizeCredentials(creds []Credential, warnings *[]string) []CanonicalCredential {
	if len(creds) == 0 {
		return nil
	}

	// Sort by ID
	sorted := make([]Credential, len(creds))
	copy(sorted, creds)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})

	// Deduplicate by ID
	seen := make(map[string]bool)
	var result []CanonicalCredential

	for _, c := range sorted {
		if seen[c.ID] {
			*warnings = append(*warnings,
				fmt.Sprintf("dedup: duplicate credential id %q removed", c.ID))
			continue
		}
		seen[c.ID] = true

		cr := CanonicalCredential{
			ID:      c.ID,
			Type:    c.Type,
			Header:  c.Header,
			Service: c.Service,
			Path:    c.Path,
			// Value deliberately omitted — no secret values in canonical form.
		}
		result = append(result, cr)
	}
	return result
}

func canonicalizeMCPServers(servers []MCPServer, warnings *[]string) []CanonicalMCPServer {
	if len(servers) == 0 {
		return nil
	}

	// Sort by Name
	sorted := make([]MCPServer, len(servers))
	copy(sorted, servers)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	// Deduplicate by Name
	seen := make(map[string]bool)
	var result []CanonicalMCPServer

	for _, m := range sorted {
		if seen[m.Name] {
			*warnings = append(*warnings,
				fmt.Sprintf("dedup: duplicate MCP server name %q removed", m.Name))
			continue
		}
		seen[m.Name] = true

		// Redact header values — header names are structural (determine behavior)
		// but values are secrets (Bearer tokens, API keys) that must never appear
		// in the canonical form or be fed to the digest.
		headers := make(map[string]string, len(m.Headers))
		for k := range m.Headers {
			headers[k] = "" // redacted — key preserved for structural identity
		}

		cr := CanonicalMCPServer{
			Name:    m.Name,
			URL:     stripURLUserinfo(m.URL),
			Headers: headers,
		}
		result = append(result, cr)
	}
	return result
}

func canonicalizeHooks(hooks []Hook, warnings *[]string) []CanonicalHook {
	if len(hooks) == 0 {
		return nil
	}

	// Sort by Name
	sorted := make([]Hook, len(hooks))
	copy(sorted, hooks)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	// Deduplicate by Name
	seen := make(map[string]bool)
	var result []CanonicalHook

	for _, h := range sorted {
		if seen[h.Name] {
			*warnings = append(*warnings,
				fmt.Sprintf("dedup: duplicate hook name %q removed", h.Name))
			continue
		}
		seen[h.Name] = true

		// Secret is deliberately omitted — no secret values in canonical form.
		cr := CanonicalHook{
			Name: h.Name,
			URL:  stripURLUserinfo(h.URL),
		}
		result = append(result, cr)
	}
	return result
}

func canonicalizeIngress(rules []IngressRule, warnings *[]string) []CanonicalIngressRule {
	if len(rules) == 0 {
		return nil
	}

	// Sort by Path
	sorted := make([]IngressRule, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	// Deduplicate by Path
	seen := make(map[string]bool)
	var result []CanonicalIngressRule

	for _, r := range sorted {
		if seen[r.Path] {
			*warnings = append(*warnings,
				fmt.Sprintf("dedup: duplicate ingress path %q removed", r.Path))
			continue
		}
		seen[r.Path] = true

		cr := CanonicalIngressRule(r)
		result = append(result, cr)
	}
	return result
}

// stripURLUserinfo removes userinfo (user:password@) from a URL string.
// If the URL has no userinfo or cannot be parsed, returns the original URL.
func stripURLUserinfo(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}

// sortedInts returns a sorted copy of the integer slice.
func sortedInts(s []int) []int {
	if len(s) == 0 {
		return nil
	}
	out := make([]int, len(s))
	copy(out, s)
	sort.Ints(out)
	return out
}

// marshalCanonicalJSON marshals the CanonicalPolicy to deterministic JSON.
// Map keys are sorted by encoding/json by default in Go 1.8+.
func marshalCanonicalJSON(cp *CanonicalPolicy) ([]byte, error) {
	return json.Marshal(cp)
}

// Digest computes the stable sha256 hex digest of a Policy.
// The digest is computed over the canonical JSON representation of the policy,
// which ensures that comments, key order, and white space do not affect the
// digest, while semantically meaningful changes do.
func Digest(p *Policy) (string, error) {
	if p == nil {
		return "", fmt.Errorf("policy digest: nil policy")
	}
	cp, _ := Canonicalize(p)
	data, err := marshalCanonicalJSON(cp)
	if err != nil {
		return "", fmt.Errorf("policy digest: failed to marshal canonical form: %w", err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), nil
}

// MustDigest computes the digest or panics. Useful for test helpers.
func MustDigest(p *Policy) string {
	d, err := Digest(p)
	if err != nil {
		panic(err)
	}
	return d
}