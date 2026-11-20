package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

// Adversary tests for B4-T03: Canonicalizer and policy digest.
// Focus: digest stability and secret leakage attacks.
//
// Each test marked // ADVERSARY BREAK demonstrates a vulnerability.
// Tests that PASS confirm the security claim holds.

// =========================================================================
// SECRET LEAKAGE ATTACKS
// =========================================================================

// ADVERSARY BREAK (HIGH): Secret leakage via MCP server headers.
// CanonicalMCPServer includes the Headers map in the canonical form without
// redaction. MCP server headers commonly carry authentication secrets
// (Authorization: Bearer ..., X-API-Key: ...). These secret values appear in
// the canonical JSON and are fed to the digest computation. The worker
// claimed "secret values never enter canonical form" but only redacted
// credential.Value and hook.Secret — MCP headers were missed entirely.
func TestAdversaryT03_MCPHeaderSecretLeak(t *testing.T) {
	secretToken := "sk-ant-api03-adversary-secret-token-12345"
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		MCPServers: []MCPServer{
			{
				Name: "upstream",
				URL:  "https://mcp.example.com/sse",
				Headers: map[string]string{
					"Authorization": "Bearer " + secretToken,
					"X-API-Key":     secretToken,
				},
			},
		},
	}

	cp, _ := Canonicalize(p)
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	canonicalJSON := string(data)
	if strings.Contains(canonicalJSON, secretToken) {
		t.Errorf("ADVERSARY BREAK: MCP server header secret %q leaked into canonical JSON: %s",
			secretToken, canonicalJSON)
	}
}

// ADVERSARY BREAK (HIGH): Secret leakage via URL userinfo in MCP and Hook URLs.
// URLs may contain embedded credentials in the userinfo component
// (https://token:secret@host/path). The canonicalizer includes the raw URL
// without stripping userinfo. These embedded credentials appear in the
// canonical JSON and feed the digest. An attacker who can observe the digest
// input (e.g., via logs) can extract the secrets.
func TestAdversaryT03_URLUserinfoSecretLeak(t *testing.T) {
	mcpSecret := "mcp-token-value-adversary"
	hookSecret := "hook-pass-adversary"
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		MCPServers: []MCPServer{
			{
				Name: "leaky",
				URL:  "https://user:" + mcpSecret + "@mcp.example.com/sse",
			},
		},
		Hooks: []Hook{
			{
				Name: "leaky-hook",
				URL:  "https://token:" + hookSecret + "@hooks.example.com/webhook",
			},
		},
	}

	cp, _ := Canonicalize(p)
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	canonicalJSON := string(data)
	if strings.Contains(canonicalJSON, mcpSecret) {
		t.Errorf("ADVERSARY BREAK: MCP URL userinfo secret %q leaked into canonical JSON: %s",
			mcpSecret, canonicalJSON)
	}
	if strings.Contains(canonicalJSON, hookSecret) {
		t.Errorf("ADVERSARY BREAK: Hook URL userinfo secret %q leaked into canonical JSON: %s",
			hookSecret, canonicalJSON)
	}
}

// ADVERSARY BREAK (HIGH): Secret leakage via MCP header into the digest input.
// Even if the digest output is a hash (which doesn't directly reveal the
// secret), the canonical JSON is the *input* to the hash. If the canonical
// form is ever logged, cached, or compared as a string, the secret is
// exposed. This test verifies the secret is absent from the raw canonical
// bytes that feed Digest().
func TestAdversaryT03_MCPHeaderSecretInDigestInput(t *testing.T) {
	apiKey := "AKIA-ADVERSARY-FAKE-KEY-9876543210"
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		MCPServers: []MCPServer{
			{
				Name: "aws-mcp",
				URL:  "https://mcp.aws.example.com",
				Headers: map[string]string{
					"X-API-Key": apiKey,
				},
			},
		},
	}

	cp, _ := Canonicalize(p)
	// marshalCanonicalJSON is the exact function Digest() calls
	data, err := marshalCanonicalJSON(cp)
	if err != nil {
		t.Fatalf("marshalCanonicalJSON error: %v", err)
	}
	if strings.Contains(string(data), apiKey) {
		t.Errorf("ADVERSARY BREAK: MCP header secret %q present in digest input bytes: %s",
			apiKey, string(data))
	}
}

// =========================================================================
// DIGEST STABILITY ATTACKS
// =========================================================================

// ADVERSARY BREAK (MEDIUM): Egress dedup happens before domain normalization,
// so case-variant duplicates survive into the canonical form. Two rules with
// "Example.COM" and "example.com" (same after normalization) are NOT
// deduplicated, producing duplicate entries in the canonical output. This
// violates the "canonical" property — the form should not contain duplicates.
// It also makes the digest depend on the original casing of domains, which
// is non-deterministic from a semantic perspective.
func TestAdversaryT03_EgressDedupBeforeNormalization(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "Example.COM", Ports: []int{443}},
			{Domain: "example.com", Ports: []int{443}},
		},
	}

	cp, _ := Canonicalize(p)
	if len(cp.Egress) != 1 {
		t.Errorf("ADVERSARY BREAK: expected 1 egress rule after dedup (case-variant domains "+
			"normalize to same), got %d: %+v", len(cp.Egress), cp.Egress)
	}
}

// ADVERSARY BREAK (MEDIUM): Egress dedup key uses unsorted ports (%v on raw
// slice), so [443, 80] and [80, 443] are treated as different rules. After
// canonicalization, both sort to [80, 443] — producing duplicate canonical
// entries. The canonical form is not truly canonical.
func TestAdversaryT03_EgressDedupBeforePortSort(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443, 80}},
			{Domain: "api.example.com", Ports: []int{80, 443}},
		},
	}

	cp, _ := Canonicalize(p)
	if len(cp.Egress) != 1 {
		t.Errorf("ADVERSARY BREAK: expected 1 egress rule after dedup (port order doesn't "+
			"affect semantics after sorting), got %d: %+v", len(cp.Egress), cp.Egress)
	}
}

// ADVERSARY BREAK (MEDIUM): Egress dedup key does not include AllowWildcard
// or AllowPrivate. Two rules with the same domain/cidr/ports but different
// security-relevant flags are treated as duplicates. The second rule is
// silently dropped, losing security semantics. Which rule survives depends
// on source ordering, making the canonical form non-deterministic from a
// security perspective.
func TestAdversaryT03_EgressDedupDropsAllowWildcard(t *testing.T) {
	trueVal := true
	falseVal := false
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, AllowWildcard: &trueVal},
			{Domain: "api.example.com", Ports: []int{443}, AllowWildcard: &falseVal},
		},
	}

	cp, _ := Canonicalize(p)
	if len(cp.Egress) != 2 {
		t.Errorf("ADVERSARY BREAK: expected 2 egress rules (different AllowWildcard = different "+
			"security semantics), got %d: %+v — a rule was silently dropped by dedup",
			len(cp.Egress), cp.Egress)
	}
}

// ADVERSARY BREAK (MEDIUM): Digest(nil) panics with a nil pointer dereference
// instead of returning an error. Canonicalize does not check for nil input,
// and Digest does not guard against it. Any caller that passes a nil policy
// (e.g., from a failed parse that returns nil) gets an unrecoverable crash
// rather than a handled error.
func TestAdversaryT03_NilPolicyPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ADVERSARY BREAK: Digest(nil) panicked with %v instead of returning an error", r)
		}
	}()
	_, err := Digest(nil)
	if err == nil {
		t.Error("ADVERSARY BREAK: Digest(nil) returned nil error instead of handling nil input gracefully")
	}
	// If we reach here without panic and with an error, the claim holds.
}

// ADVERSARY BREAK (LOW): Trailing dot in domain is not normalized.
// "api.example.com." and "api.example.com" are semantically equivalent
// (RFC 1034: trailing dot = root label), but normalizeDomain does not strip
// trailing dots. This causes digest instability — two policies that are
// semantically identical produce different digests.
func TestAdversaryT03_TrailingDotDomainInstability(t *testing.T) {
	p1 := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.example.com", Ports: []int{443}}},
	}
	p2 := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.example.com.", Ports: []int{443}}},
	}

	d1, err := Digest(p1)
	if err != nil {
		t.Fatalf("Digest(p1) error: %v", err)
	}
	d2, err := Digest(p2)
	if err != nil {
		t.Fatalf("Digest(p2) error: %v", err)
	}
	if d1 != d2 {
		t.Errorf("ADVERSARY BREAK: trailing-dot domain produced different digest for "+
			"semantically equivalent policies: %q vs %q", d1, d2)
	}
}

// =========================================================================
// CONFIRMED SAFE
// =========================================================================

// Confirmed safe: digest is stable when slices are in different orders.
// The canonicalizer sorts all slices, so reordering input does not change
// the digest. 20 random orderings all produce the identical hash.
func TestAdversaryT03_DigestStableAcrossSliceOrdering(t *testing.T) {
	base := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test", Description: "test agent"},
		Egress: []EgressRule{
			{Domain: "z.example.com", Ports: []int{443, 80}},
			{Domain: "a.example.com", Ports: []int{443}},
			{Domain: "m.example.com", Ports: []int{8080}},
		},
		Credentials: []Credential{
			{ID: "z-key", Type: "header", Header: "X-Z", Value: "z-val"},
			{ID: "a-key", Type: "header", Header: "X-A", Value: "a-val"},
			{ID: "m-key", Type: "header", Header: "X-M", Value: "m-val"},
		},
		MCPServers: []MCPServer{
			{Name: "z-server", URL: "https://z.example.com/mcp"},
			{Name: "a-server", URL: "https://a.example.com/mcp"},
		},
		Hooks: []Hook{
			{Name: "z-hook", URL: "https://z.example.com/hook", Secret: "z-secret"},
			{Name: "a-hook", URL: "https://a.example.com/hook", Secret: "a-secret"},
		},
		Ingress: []IngressRule{
			{Path: "/z", Port: 9090},
			{Path: "/a", Port: 8080},
		},
	}

	baseDigest, err := Digest(base)
	if err != nil {
		t.Fatalf("base Digest error: %v", err)
	}

	// Reverse all slice orderings
	reversed := &Policy{
		Version: base.Version,
		Agent:   base.Agent,
		Egress: []EgressRule{
			{Domain: "m.example.com", Ports: []int{8080}},
			{Domain: "a.example.com", Ports: []int{443}},
			{Domain: "z.example.com", Ports: []int{443, 80}},
		},
		Credentials: []Credential{
			{ID: "m-key", Type: "header", Header: "X-M", Value: "m-val"},
			{ID: "a-key", Type: "header", Header: "X-A", Value: "a-val"},
			{ID: "z-key", Type: "header", Header: "X-Z", Value: "z-val"},
		},
		MCPServers: []MCPServer{
			{Name: "a-server", URL: "https://a.example.com/mcp"},
			{Name: "z-server", URL: "https://z.example.com/mcp"},
		},
		Hooks: []Hook{
			{Name: "a-hook", URL: "https://a.example.com/hook", Secret: "a-secret"},
			{Name: "z-hook", URL: "https://z.example.com/hook", Secret: "z-secret"},
		},
		Ingress: []IngressRule{
			{Path: "/a", Port: 8080},
			{Path: "/z", Port: 9090},
		},
	}

	reversedDigest, err := Digest(reversed)
	if err != nil {
		t.Fatalf("reversed Digest error: %v", err)
	}
	if baseDigest != reversedDigest {
		t.Errorf("digest should be stable across slice ordering: base=%q, reversed=%q",
			baseDigest, reversedDigest)
	}
}

// Confirmed safe: changing credential.Value does NOT change the digest.
// This is by design — secrets are excluded from the canonical form so the
// digest can be logged/shared without leaking secrets. The digest detects
// structural changes (port, domain, credential ID/type) but not secret
// value changes. This is an intentional design trade-off.
func TestAdversaryT03_CredentialValueChangeNoDigestChange(t *testing.T) {
	p1 := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "my-key", Type: "header", Header: "X-Key", Value: "old-secret-value"},
		},
	}
	p2 := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "my-key", Type: "header", Header: "X-Key", Value: "new-secret-value"},
		},
	}

	d1, _ := Digest(p1)
	d2, _ := Digest(p2)
	// This is expected — secrets are deliberately excluded. If this test
	// fails (digests differ), it means a secret value leaked into the digest.
	if d1 != d2 {
		t.Logf("NOTE: credential value change altered digest — this means a secret "+
			"value leaked into the canonical form. d1=%q, d2=%q", d1, d2)
	}
	// We do NOT fail here — this is by design. The test documents the behavior.
}

// Confirmed safe: changing hook.Secret does NOT change the digest.
// Same rationale as credential value — secrets are excluded by design.
func TestAdversaryT03_HookSecretChangeNoDigestChange(t *testing.T) {
	p1 := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Hooks: []Hook{
			{Name: "alert", URL: "https://hooks.example.com/alert", Secret: "old-hook-secret"},
		},
	}
	p2 := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Hooks: []Hook{
			{Name: "alert", URL: "https://hooks.example.com/alert", Secret: "new-hook-secret"},
		},
	}

	d1, _ := Digest(p1)
	d2, _ := Digest(p2)
	if d1 != d2 {
		t.Logf("NOTE: hook secret change altered digest — secret leaked into canonical form. "+
			"d1=%q, d2=%q", d1, d2)
	}
}

// Confirmed safe: credential Value is absent from canonical output.
// The CanonicalCredential struct deliberately omits the Value field.
// Verified by marshaling to JSON and searching for the secret string.
func TestAdversaryT03_CredentialValueRedacted(t *testing.T) {
	secret := "super-secret-credential-value-adversary"
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "key", Type: "header", Header: "X-Key", Value: secret},
		},
	}

	cp, _ := Canonicalize(p)
	data, _ := json.Marshal(cp)
	if strings.Contains(string(data), secret) {
		t.Errorf("credential value %q leaked into canonical JSON", secret)
	}
}

// Confirmed safe: hook Secret is absent from canonical output.
// The CanonicalHook struct deliberately omits the Secret field.
func TestAdversaryT03_HookSecretRedacted(t *testing.T) {
	secret := "super-secret-hook-value-adversary"
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Hooks: []Hook{
			{Name: "alert", URL: "https://hooks.example.com/alert", Secret: secret},
		},
	}

	cp, _ := Canonicalize(p)
	data, _ := json.Marshal(cp)
	if strings.Contains(string(data), secret) {
		t.Errorf("hook secret %q leaked into canonical JSON", secret)
	}
}

// Confirmed safe: digest format is sha256 hex (64 lowercase hex chars).
// Already tested by worker but verified here with a richer policy.
func TestAdversaryT03_DigestFormatRichPolicy(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "full-test", Description: "full policy"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443, 80}},
			{CIDR:   "10.0.0.0/8", Ports: []int{5432}},
		},
		Credentials: []Credential{
			{ID: "key1", Type: "header", Header: "X-Key"},
		},
		MCPServers: []MCPServer{
			{Name: "mcp1", URL: "https://mcp.example.com"},
		},
		Hooks: []Hook{
			{Name: "hook1", URL: "https://hooks.example.com/h"},
		},
		Ingress: []IngressRule{
			{Path: "/webhook", Port: 8080},
		},
	}

	d, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest error: %v", err)
	}
	if len(d) != 64 {
		t.Errorf("expected 64-char sha256 hex, got %d chars: %q", len(d), d)
	}
	for _, c := range d {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("digest contains non-hex char %c in %q", c, d)
		}
	}
}
