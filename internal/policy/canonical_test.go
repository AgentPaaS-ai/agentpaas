package policy

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

// TestCanonicalize_BasicSorting verifies that the canonicalizer sorts
// maps, slices, and ports deterministically.
func TestCanonicalize_BasicSorting(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test", Description: "test agent"},
		Egress: []EgressRule{
			{Domain: "z.example.com", Ports: []int{443, 80}},
			{Domain: "a.example.com", Ports: []int{443}},
		},
		Credentials: []Credential{
			{ID: "z-key", Type: "header", Header: "X-Z", Value: "z-val"},
			{ID: "a-key", Type: "header", Header: "X-A", Value: "a-val"},
		},
		MCPServers: []MCPServer{
			{Name: "z-server", URL: "https://z.example.com/mcp"},
			{Name: "a-server", URL: "https://a.example.com/mcp"},
		},
		Hooks: []Hook{
			{Name: "z-hook", URL: "https://z.example.com/hook"},
			{Name: "a-hook", URL: "https://a.example.com/hook"},
		},
		Ingress: []IngressRule{
			{Path: "/z", Port: 9090},
			{Path: "/a", Port: 8080},
		},
	}

	cp, warnings := Canonicalize(p)

	// Verify egress sorted by domain
	if len(cp.Egress) != 2 {
		t.Fatalf("expected 2 egress rules, got %d", len(cp.Egress))
	}
	if cp.Egress[0].Domain != "a.example.com" {
		t.Errorf("expected first egress domain 'a.example.com', got %q", cp.Egress[0].Domain)
	}
	if cp.Egress[1].Domain != "z.example.com" {
		t.Errorf("expected second egress domain 'z.example.com', got %q", cp.Egress[1].Domain)
	}

	// Verify ports sorted
	if len(cp.Egress[1].Ports) != 2 || cp.Egress[1].Ports[0] != 80 || cp.Egress[1].Ports[1] != 443 {
		t.Errorf("expected sorted ports [80, 443], got %v", cp.Egress[1].Ports)
	}

	// Verify credentials sorted by ID
	if len(cp.Credentials) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(cp.Credentials))
	}
	if cp.Credentials[0].ID != "a-key" || cp.Credentials[1].ID != "z-key" {
		t.Errorf("credentials not sorted by ID: got %q, %q", cp.Credentials[0].ID, cp.Credentials[1].ID)
	}

	// Verify MCP servers sorted by Name
	if cp.MCPServers[0].Name != "a-server" || cp.MCPServers[1].Name != "z-server" {
		t.Errorf("MCP servers not sorted by name: got %q, %q", cp.MCPServers[0].Name, cp.MCPServers[1].Name)
	}

	// Verify hooks sorted by Name
	if cp.Hooks[0].Name != "a-hook" || cp.Hooks[1].Name != "z-hook" {
		t.Errorf("hooks not sorted by name: got %q, %q", cp.Hooks[0].Name, cp.Hooks[1].Name)
	}

	// Verify ingress sorted by Path
	if cp.Ingress[0].Path != "/a" || cp.Ingress[1].Path != "/z" {
		t.Errorf("ingress not sorted by path: got %q, %q", cp.Ingress[0].Path, cp.Ingress[1].Path)
	}

	// Verify no dedup warnings for unique entries
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings for unique entries, got %d: %v", len(warnings), warnings)
	}
}

// TestCanonicalize_DomainNormalization verifies lowercase + punycode.
func TestCanonicalize_DomainNormalization(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "API.EXAMPLE.COM", Ports: []int{443}},
		},
	}

	cp, _ := Canonicalize(p)
	if cp.Egress[0].Domain != "api.example.com" {
		t.Errorf("expected lowercased domain 'api.example.com', got %q", cp.Egress[0].Domain)
	}
}

// TestCanonicalize_IDNToPunycode verifies punycode normalization.
func TestCanonicalize_IDNToPunycode(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "München.example.com", Ports: []int{443}},
		},
	}

	cp, _ := Canonicalize(p)
	// 'ü' should become xn--mnchen-3ya in punycode
	if !strings.Contains(cp.Egress[0].Domain, "xn--") {
		t.Errorf("expected punycode-encoded domain, got %q", cp.Egress[0].Domain)
	}
}

// TestCanonicalize_RedactSecrets verifies secret values never appear in digest.
func TestCanonicalize_RedactSecrets(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "my-key", Type: "header", Header: "X-API-Key", Value: "super-secret-value"},
		},
		Hooks: []Hook{
			{Name: "alert", URL: "https://hooks.example.com/alert", Secret: "hook-secret-value"},
		},
	}

	cp, _ := Canonicalize(p)
	// Credential value should be redacted — the field is absent from CanonicalCredential.
	_ = cp.Credentials[0].ID // just access to confirm the credential exists
	// Verify the canonical JSON does not contain the secret value
	canonStr := fmt.Sprintf("%+v", cp)
	if strings.Contains(canonStr, "super-secret-value") {
		t.Error("credential value appears in canonical output")
	}
	// Hook secret should be redacted — the field is absent from CanonicalHook.
	if strings.Contains(canonStr, "hook-secret-value") {
		t.Error("hook secret appears in canonical output")
	}
}

// TestCanonicalize_DeduplicateEgress verifies duplicate egress rules are deduplicated.
func TestCanonicalize_DeduplicateEgress(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}},
			{Domain: "api.example.com", Ports: []int{443}}, // duplicate
			{Domain: "other.example.com", Ports: []int{80}},
		},
	}

	cp, warnings := Canonicalize(p)
	if len(cp.Egress) != 2 {
		t.Errorf("expected 2 egress rules after dedup, got %d", len(cp.Egress))
	}
	// Should have at least one dedup warning
	if len(warnings) == 0 {
		t.Error("expected at least one dedup warning for duplicate egress rules")
	}
}

// TestCanonicalize_DeduplicateCredentials verifies duplicate credential IDs are deduplicated.
func TestCanonicalize_DeduplicateCredentials(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "dup-key", Type: "header", Header: "X-Key-1"},
			{ID: "dup-key", Type: "header", Header: "X-Key-2"}, // duplicate ID
		},
	}

	cp, warnings := Canonicalize(p)
	if len(cp.Credentials) != 1 {
		t.Errorf("expected 1 credential after dedup, got %d", len(cp.Credentials))
	}
	if len(warnings) == 0 {
		t.Error("expected dedup warning for duplicate credential ID")
	}
}

// TestDigest_Stable verifies digest is deterministic.
func TestDigest_Stable(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}},
		},
	}

	d1, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest() error: %v", err)
	}
	d2, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest() error: %v", err)
	}
	if d1 != d2 {
		t.Errorf("digest not stable: got %q then %q", d1, d2)
	}
}

// TestDigest_MeaningfulChange verifies that semantically meaningful changes DO change the digest.
func TestDigest_MeaningfulChange(t *testing.T) {
	base := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.example.com", Ports: []int{443}}},
	}
	changed := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.example.com", Ports: []int{80}}}, // changed port
	}

	d1, _ := Digest(base)
	d2, _ := Digest(changed)
	if d1 == d2 {
		t.Errorf("digest should differ for different policies, but both are %q", d1)
	}
}

// TestDigest_GoldenFormat verifies the digest format is sha256 hex.
func TestDigest_GoldenFormat(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
	}

	d, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest() error: %v", err)
	}
	if len(d) != sha256.Size*2 {
		t.Errorf("expected sha256 hex of length %d, got %q (len=%d)", sha256.Size*2, d, len(d))
	}
	// Must be valid hex
	for _, c := range d {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("digest contains non-hex character %c", c)
		}
	}
}

// TestCanonicalize_DigestNoSecrets verifies secret values never appear in digest input.
func TestCanonicalize_DigestNoSecrets(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "my-key", Type: "header", Header: "X-Key", Value: "should-not-appear"},
		},
		Hooks: []Hook{
			{Name: "alert", URL: "https://hooks.example.com/alert", Secret: "secret-hook-value"},
		},
	}

	cp, _ := Canonicalize(p)
	canonStr := fmt.Sprintf("%+v", cp)
	if strings.Contains(canonStr, "should-not-appear") {
		t.Error("credential value appears in canonical output")
	}
	if strings.Contains(canonStr, "secret-hook-value") {
		t.Error("hook secret appears in canonical output")
	}
}
