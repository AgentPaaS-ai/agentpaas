package policy

import (
	"strings"
	"testing"
)

// ============================================================================
// B4-T01 Adversary Security Tests
// Attack vectors: unknown field bypass, YAML bomb resilience,
//                 error message secret leakage
// ============================================================================

// ---------------------------------------------------------------------------
// 1. UNKNOWN FIELD BYPASS
// ---------------------------------------------------------------------------

// TestAdversaryUnknownFieldTopLevel verifies every possible unknown field
// at the top level is rejected (plural, typo, extra, near-miss).
func TestAdversaryUnknownFieldTopLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "unknown_version_plural",
			input: `versions: "1.0"`,
		},
		{
			name: "unknown_agent_typo",
			input: `version: "1.0"
agnt:
  name: x`,
		},
		{
			name: "unknown_top_extra_field",
			input: `version: "1.0"
agent:
  name: x
metadata:
  created: 2025-01-01`,
		},
		{
			name: "unknown_top_security",
			input: `version: "1.0"
agent:
  name: x
security:
  allow_all: true`,
		},
		{
			name: "unknown_top_rules",
			input: `version: "1.0"
agent:
  name: x
rules:
  - allow: all`,
		},
		{
			name: "unknown_top_typo_egress",
			input: `version: "1.0"
agent:
  name: x
egresss:
  - domain: example.com`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePolicy(strings.NewReader(tt.input))
			if err == nil {
				t.Errorf("ADVERSARY BREAK: unknown field %q was accepted", tt.name)
			} else {
				t.Logf("correctly rejected: %v", err)
			}
		})
	}
}

// TestAdversaryUnknownFieldNested verifies unknown fields nested inside every
// section type are rejected.
func TestAdversaryUnknownFieldNested(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "agent_unknown_field",
			input: `version: "1.0"
agent:
  name: x
  version: 2`,
		},
		{
			name: "egress_unknown_field",
			input: `version: "1.0"
agent:
  name: x
egress:
  - domain: ex.com
    ports: [443]
    protocol: tcp`,
		},
		{
			name: "credentials_unknown_field",
			input: `version: "1.0"
agent:
  name: x
credentials:
  - id: k
    type: header
    value: v
    scope: global`,
		},
		{
			name: "mcp_unknown_field",
			input: `version: "1.0"
agent:
  name: x
mcp_servers:
  - name: s
    url: http://x
    timeout: 30`,
		},
		{
			name: "hooks_unknown_field",
			input: `version: "1.0"
agent:
  name: x
hooks:
  - name: h
    url: http://h
    retry: 3`,
		},
		{
			name: "ingress_unknown_field",
			input: `version: "1.0"
agent:
  name: x
ingress:
  - path: /
    port: 8080
    method: POST`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePolicy(strings.NewReader(tt.input))
			if err == nil {
				t.Errorf("ADVERSARY BREAK: unknown nested field %q was accepted", tt.name)
			} else {
				t.Logf("correctly rejected: %v", err)
			}
		})
	}
}

// TestAdversaryMCPHeadersArbitraryKeys documents that map[string]string fields
// accept arbitrary keys (expected — YAML strict mode only applies to structs).
// This is not a break but a known constraint.
func TestAdversaryMCPHeadersArbitraryKeys(t *testing.T) {
	input := `version: "1.0"
agent:
  name: x
mcp_servers:
  - name: s
    url: http://x
    headers:
      X-Custom: val
      __proto__: evil
      constructor: bad
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err != nil {
		t.Logf("rejected (stricter than needed): %v", err)
		return
	}
	t.Log("ACCEPTED: map[string]string fields accept arbitrary keys (known constraint, not a break)")
}

// TestAdversaryYAMLAliasBypass tests YAML anchors/aliases (merge keys <<:)
// that may smuggle unknown fields through strict decoding.
func TestAdversaryYAMLAliasBypass(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "merge_key_injects_unknown_field",
			input: `version: "1.0"
agent: &a
  name: x
egress:
  - <<: *a
    domain: ex.com
    ports: [443]
`,
		},
		{
			name: "merge_key_at_top_level",
			input: `defaults: &d
  unknown_top: true
version: "1.0"
agent:
  name: x
<<: *d
`,
		},
		{
			name: "alias_chain_circular",
			input: `version: "1.0"
agent: &a1
  name: x
  next: &a2
    prev: *a1
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePolicy(strings.NewReader(tt.input))
			if err == nil {
				t.Errorf("ADVERSARY BREAK: alias bypass %q was accepted", tt.name)
			} else {
				t.Logf("correctly handled: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. YAML BOMB LIMITS
// ---------------------------------------------------------------------------

// TestAdversaryYAMLBombBillionLaughs verifies the parser doesn't OOM or hang
// on a billion-laughs-style expansion attack.
func TestAdversaryYAMLBombBillionLaughs(t *testing.T) {
	// yaml: a = 1, b = a a, c = b b, d = c c  (3-deep exponential: 8 copies)
	input := `version: "1.0"
agent:
  name: x
a: &a ["x"]
b: &b [*a, *a]
c: &c [*b, *b]
d: &d [*c, *c]
egress:
  - domain: "ex.com"
    ports: [443]
    laugh: *d
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Log("bomb parsed (may indicate alias resolution or limits)")
	} else {
		t.Logf("bomb correctly rejected: %v", err)
	}
}

// TestAdversaryDeepNestingStack verifies deeply nested YAML doesn't cause
// stack exhaustion or OOM.
func TestAdversaryDeepNestingStack(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: \"1.0\"\n")
	b.WriteString("agent:\n  name: x\n")
	b.WriteString("egress:\n")
	// Deep nested YAML: 1000 levels of nesting
	for i := 0; i < 1000; i++ {
		b.WriteString("  - domain: \"d\"\n")
		b.WriteString("    ports: [443]\n")
		b.WriteString("    nested:\n")
	}
	b.WriteString("      end: true\n")

	_, err := ParsePolicy(strings.NewReader(b.String()))
	if err == nil {
		t.Log("deep nest parsed")
	} else {
		t.Logf("deep nest correctly rejected: %v", err)
	}
}

// TestAdversaryLargeDocumentMemory verifies a large but structurally valid
// document doesn't OOM the test process.
func TestAdversaryLargeDocumentMemory(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: \"1.0\"\n")
	b.WriteString("agent:\n  name: x\n")
	b.WriteString("egress:\n")
	for i := 0; i < 10000; i++ {
		b.WriteString("  - domain: \"d")
		b.WriteString(strings.Repeat("x", 100))
		b.WriteString("\"\n    ports: [443]\n")
	}
	b.WriteString("  - domain: \"fin\"\n    ports: [443]\n")

	_, err := ParsePolicy(strings.NewReader(b.String()))
	if err == nil {
		t.Log("large doc parsed successfully")
	} else {
		t.Logf("large doc rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 3. ERROR MESSAGE SECRET LEAKAGE
// ---------------------------------------------------------------------------

// TestAdversaryErrorLeakCredentialValue verifies that when a parsing error
// occurs near a credential value, the error message does not contain the
// secret value.
func TestAdversaryErrorLeakCredentialValue(t *testing.T) {
	secret := "sk-liv...cret!"
	input := `version: "1.0"
agent:
  name: x
credentials:
  - id: leak-test
    type: header
    header: Authorization
    value: "` + secret + `"
    extra_bad_field: true
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error from unknown field")
	}
	errStr := err.Error()
	if strings.Contains(errStr, secret) {
		t.Errorf("ADVERSARY BREAK: error message leaks credential value: %s", errStr)
	} else {
		t.Logf("no secret leakage: err=%q", errStr)
	}
}

// TestAdversaryErrorLeakAPIKeyHeader verifies a credential value in an MCP
// server struct field is not leaked in error messages when a sibling unknown
// field triggers a parse error.
func TestAdversaryErrorLeakAPIKeyHeader(t *testing.T) {
	secret := "Bearer sk-pro...AAAA"
	input := `version: "1.0"
agent:
  name: x
mcp_servers:
  - name: openai
    url: "https://api.openai.com"
    extra_field: evil
    headers:
      Authorization: "` + secret + `"
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error from unknown field in mcp_servers")
	}
	errStr := err.Error()
	if strings.Contains(errStr, secret) {
		t.Errorf("ADVERSARY BREAK: error message leaks API key: %s", errStr)
	} else {
		t.Logf("no secret leakage: err=%q", errStr)
	}
}

// TestAdversaryErrorLeakHookSecret verifies the hook secret value is not
// leaked in error messages.
func TestAdversaryErrorLeakHookSecret(t *testing.T) {
	secret := "whsec_verysecret123"
	input := `version: "1.0"
agent:
  name: x
hooks:
  - name: alert
    url: "https://hooks.example.com"
    secret: "` + secret + `"
    retry: 3
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error from unknown field in hooks")
	}
	errStr := err.Error()
	if strings.Contains(errStr, secret) {
		t.Errorf("ADVERSARY BREAK: error message leaks hook secret: %s", errStr)
	} else {
		t.Logf("no secret leakage: err=%q", errStr)
	}
}

// TestAdversaryErrorLeakCredentialPath verifies credential paths aren't leaked.
func TestAdversaryErrorLeakCredentialPath(t *testing.T) {
	secretPath := "/etc/secrets/db_password"
	input := `version: "1.0"
agent:
  name: x
credentials:
  - id: db
    type: file
    path: "` + secretPath + `"
    extra_bad_field: true
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error from unknown field")
	}
	errStr := err.Error()
	if strings.Contains(errStr, secretPath) {
		t.Errorf("ADVERSARY BREAK: error message leaks credential path: %s", errStr)
	} else {
		t.Logf("no secret leakage: err=%q", errStr)
	}
}

// ---------------------------------------------------------------------------
// 4. TYPE CONFUSION — edge cases that could bypass strict parsing
// ---------------------------------------------------------------------------

// TestAdversaryTypeConfusion tests type mismatches that could be used to
// subvert field validation.
func TestAdversaryTypeConfusion(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "version_as_number",
			input: `version: 1.0
agent:
  name: x`,
		},
		{
			name: "version_as_int",
			input: `version: 1
agent:
  name: x`,
		},
		{
			name: "ports_string_list",
			input: `version: "1.0"
agent:
  name: x
egress:
  - domain: ex.com
    ports: ["443", 80]`,
		},
		{
			name: "egress_domain_int",
			input: `version: "1.0"
agent:
  name: x
egress:
  - domain: 12345
    ports: [443]`,
		},
		{
			name: "ingress_port_string",
			input: `version: "1.0"
agent:
  name: x
ingress:
  - path: /
    port: "8080"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePolicy(strings.NewReader(tt.input))
			if err == nil {
				t.Logf("type confusion %q was accepted (possible if yaml coerces)", tt.name)
			} else {
				t.Logf("type confusion %q correctly rejected: %v", tt.name, err)
			}
		})
	}
}
