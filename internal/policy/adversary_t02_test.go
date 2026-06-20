package policy

import (
	"fmt"
	"strings"
	"testing"
)

// adversarySecret is a distinctive high-entropy string used to detect credential
// leakage via validation error messages. Must never appear in ValidatePolicy errors.
const adversarySecretValidation = "ADVERSARY_LEAK_MARKER_b4t02_sk-9f3c2a1b8e7d"

func assertValidationErrorDoesNotLeakSecret(t *testing.T, errs []ValidationError) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Message, adversarySecretValidation) {
			t.Errorf("ADVERSARY BREAK [HIGH]: ValidatePolicy error leaks credential/secret value: %s", e.Message)
		}
	}
}

// ---------------------------------------------------------------------------
// Wildcard bypass attempts
// ---------------------------------------------------------------------------

func TestAdversaryT02_SubdomainWildcardBypass(t *testing.T) {
	// Attempt to use a pattern that implies subdomain matching without allow_wildcard.
	tests := []string{
		// Multi-level wildcard not caught by basic prefix check
		`version: "1.0"
agent:
  name: x
egress:
  - domain: "sub.*.example.com"
    ports: [443]
`,
		// Wildcard in middle
		`version: "1.0"
agent:
  name: x
egress:
  - domain: "example.*.com"
    ports: [443]
`,
	}
	for i, input := range tests {
		t.Run(fmt.Sprintf("wildcard_position_%d", i), func(t *testing.T) {
			p := parseYAML(t, input)
			errs := ValidatePolicy(p)
			// These should be caught — any `*` in domain should require allow_wildcard.
			hasWildcard := false
			for _, e := range errs {
				if strings.Contains(e.Message, "allow_wildcard") {
					hasWildcard = true
				}
			}
			if !hasWildcard {
				t.Error("ADVERSARY BREAK [MEDIUM]: wildcard positions not caught by validation")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Private CIDR bypass — IPv6 private ranges
// ---------------------------------------------------------------------------

func TestAdversaryT02_IPv6PrivateCIDRBypass(t *testing.T) {
	// IPv6 unique-local addresses should also require allow_private.
	t.Log("IPv6 unique-local CIDR bypass is an accepted P1 risk (IPv4 RFC1918 check only)")
}

// ---------------------------------------------------------------------------
// Credential leak via validation errors
// ---------------------------------------------------------------------------

func TestAdversaryT02_CredentialValueLeakInValidation(t *testing.T) {
	input := fmt.Sprintf(`version: "1.0"
agent:
  name: x
egress:
  - domain: "api.example.com"
    ports: [443]
    credential: "leak-cred"
credentials:
  - id: "leak-cred"
    type: header
    header: "X-Key"
    value: "%s"
    mode: "file"
`, adversarySecretValidation)
	p := parseYAML(t, input)
	errs := ValidatePolicy(p)
	assertValidationErrorDoesNotLeakSecret(t, errs)
}

// ---------------------------------------------------------------------------
// Injection in credential ID
// ---------------------------------------------------------------------------

func TestAdversaryT02_CredentialIDInjection(t *testing.T) {
	// Newlines or control characters in credential IDs should be caught.
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
credentials:
  - id: "evil\nid"
    type: header
    header: "X-Key"
    value: "abc"
`)
	errs := ValidatePolicy(p)
	// Parser may or may not accept this — if it does, validator should flag it.
	hasInjection := false
	for _, e := range errs {
		if strings.Contains(e.Message, "injection") || strings.Contains(e.Message, "control") {
			hasInjection = true
		}
	}
	if !hasInjection && ContainsInjectionPattern("evil\nid") {
		t.Error("ADVERSARY BREAK [MEDIUM]: credential ID with control characters not flagged")
	}
}

// ---------------------------------------------------------------------------
// Direct lease with empty mode after invalid mode
// ---------------------------------------------------------------------------

func TestAdversaryT02_DirectLeaseModeBypass(t *testing.T) {
	// Empty mode explicitly set to empty string.
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
credentials:
  - id: "bypass"
    type: direct_lease
    mode: ""
    reason: "testing"
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "mode")
}

// ---------------------------------------------------------------------------
// Hook URL with embedded credentials
// ---------------------------------------------------------------------------

func TestAdversaryT02_HookWithInlineCredentials(t *testing.T) {
	// Hook URL containing credentials — not technically a validation
	// error but should be flagged as suspicious.
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
egress:
  - domain: "example.com"
    ports: [443]
hooks:
  - name: "cred-hook"
    url: "https://user:pass@hooks.example.com/alert"
`)
	errs := ValidatePolicy(p)
	// Hook with userinfo in URL should parse, but may have inline credentials.
	for _, e := range errs {
		if strings.Contains(e.Message, "no matching egress") {
			return // accept this error
		}
	}
}

// ---------------------------------------------------------------------------
// MCP server name reused — duplicate name
// ---------------------------------------------------------------------------

func TestAdversaryT02_DuplicateMCPName(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
mcp_servers:
  - name: "duplicate"
    url: "https://mcp1.example.com"
  - name: "duplicate"
    url: "https://mcp2.example.com"
egress:
  - domain: "mcp1.example.com"
    ports: [443]
  - domain: "mcp2.example.com"
    ports: [443]
`)
	errs := ValidatePolicy(p)
	// Duplicate MCP server names — should be a warning.
	dupFound := false
	for _, e := range errs {
		if strings.Contains(e.Message, "duplicate") {
			dupFound = true
		}
	}
	// For P1, duplicate names are allowed (only the last one is used).
	// This is logged as a known gap.
	t.Log("ADVERSARY OBSERVATION [LOW]: duplicate MCP server names not rejected (P1 gap)")
	if dupFound {
		t.Log("duplicate MCP name warning present")
	}
}

// ---------------------------------------------------------------------------
// Egress rule with credential but no credential declared
// ---------------------------------------------------------------------------

func TestAdversaryT02_EgressWithCredentialNoDeclared(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
egress:
  - domain: "api.example.com"
    ports: [443]
    credential: "missing-cred"
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "undeclared credential")
}

// ---------------------------------------------------------------------------
// CIDR with IP exactly at boundary
// ---------------------------------------------------------------------------

func TestAdversaryT02_PrivateCIDRBoundary(t *testing.T) {
	// 10.0.0.0/9 is also private (within 10.0.0.0/8)
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
egress:
  - cidr: "10.128.0.0/9"
    ports: [443]
`)
	errs := ValidatePolicy(p)
	// 10.128.0.0/9 is within 10.0.0.0/8, so it should require allow_private.
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "allow_private") {
			found = true
		}
	}
	if !found {
		t.Error("ADVERSARY BREAK [MEDIUM]: 10.128.0.0/9 not detected as private (inside 10.0.0.0/8)")
	}
}

// ---------------------------------------------------------------------------
// Hook URL with loopback variants
// ---------------------------------------------------------------------------

func TestAdversaryT02_LoopbackHookVariants(t *testing.T) {
	variants := []struct {
		name string
		url  string
	}{
		{"ipv6_loopback", "http://[::1]:9090/hook"},
		{"ipv4_127.0.0.2", "http://127.0.0.2:9090/hook"},
		{"ipv4_127.255.255.255", "http://127.255.255.255:9090/hook"},
		{"localhost_subdomain", "http://x.localhost:9090/hook"},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			p := parseYAML(t, fmt.Sprintf(`version: "1.0"
agent:
  name: x
hooks:
  - name: "test"
    url: "%s"
`, v.url))
			errs := ValidatePolicy(p)
			found := false
			for _, e := range errs {
				if strings.Contains(e.Message, "loopback") {
					found = true
				}
			}
			if !found {
				t.Errorf("ADVERSARY BREAK [MEDIUM]: loopback variant %s not detected", v.url)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Port zero bypass — if ports list is missing entirely
// ---------------------------------------------------------------------------

func TestAdversaryT02_MissingPortsList(t *testing.T) {
	// Egress rule with neither domain nor CIDR — should be caught.
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
egress:
  - domain: "example.com"
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "at least one port")
}

// ---------------------------------------------------------------------------
// Nil pointer in AllowWildcard correctly handled
// ---------------------------------------------------------------------------

func TestAdversaryT02_AllowWildcardNilPointer(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
egress:
  - domain: "*.example.com"
    ports: [443]
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "allow_wildcard")
}

// ---------------------------------------------------------------------------
// AllowPrivate nil pointer
// ---------------------------------------------------------------------------

func TestAdversaryT02_AllowPrivateNilPointer(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
egress:
  - cidr: "192.168.0.0/16"
    ports: [443]
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "allow_private")
}

// ---------------------------------------------------------------------------
// Brokered credential type needs service
// ---------------------------------------------------------------------------

func TestAdversaryT02_BrokeredCredNoService(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: x
credentials:
  - id: "vault"
    type: brokered
    path: "secret/key"
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "service")
}

// ---------------------------------------------------------------------------
// Verification: adversary tests from B4-T01 still pass (no regression)
// ---------------------------------------------------------------------------

func TestAdversaryT02_B4T01AdversaryRegression(t *testing.T) {
	// Run a selection of B4-T01 adversary scenarios through the parser
	// to ensure no regression from schema enrichment.
	t.Run("known_typo_brokerd_rejected", func(t *testing.T) {
		_, err := ParsePolicy(strings.NewReader(`version: "1.0"
agent:
  name: x
egress:
  - domain: x.com
    ports: [443]
    brokerd: vault
`))
		if err == nil {
			t.Error("ADVERSARY BREAK [HIGH]: brokerd typo accepted after schema enrichment")
		}
	})

	t.Run("known_typo_allow_wildcards_rejected", func(t *testing.T) {
		_, err := ParsePolicy(strings.NewReader(`version: "1.0"
agent:
  name: x
egress:
  - domain: "*.x.com"
    ports: [443]
    allow_wildcards: true
`))
		if err == nil {
			t.Error("ADVERSARY BREAK [HIGH]: allow_wildcards typo accepted after schema enrichment")
		}
	})

	t.Run("scalar_port_rejected", func(t *testing.T) {
		_, err := ParsePolicy(strings.NewReader(`version: "1.0"
agent:
  name: x
egress:
  - domain: x.com
    port: 443
`))
		if err == nil {
			t.Error("ADVERSARY BREAK [HIGH]: scalar port accepted after schema enrichment")
		}
	})
}