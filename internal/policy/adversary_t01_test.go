package policy

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// adversarySecret is a distinctive high-entropy string used to detect credential
// leakage via error messages. Must never appear in ParsePolicy errors.
const adversarySecret = "ADVERSARY_LEAK_MARKER_sk-9f3c2a1b8e7d6c5b4a"

func assertErrorDoesNotLeakSecret(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), adversarySecret) {
		t.Errorf("ADVERSARY BREAK [HIGH]: ParsePolicy error leaks credential/secret value from input")
	}
}

// ---------------------------------------------------------------------------
// Unknown field bypass attempts
// ---------------------------------------------------------------------------

func TestAdversaryT01_UnknownTopLevelTypos(t *testing.T) {
	typos := []string{
		`version: "1.0"
agent: {name: x}
mcp_server: []`,
		`version: "1.0"
agent: {name: x}
credential: []`,
		`version: "1.0"
agent: {name: x}
egress_rules: []`,
		`version: "1.0"
agent: {name: x}
allow_wildcards: true`,
	}
	for i, input := range typos {
		t.Run(fmt.Sprintf("typo_%d", i), func(t *testing.T) {
			_, err := ParsePolicy(strings.NewReader(input))
			if err == nil {
				t.Errorf("ADVERSARY BREAK [MEDIUM]: typo/alternate top-level key accepted (bypass strict schema)")
			}
		})
	}
}

func TestAdversaryT01_UnknownNestedAcrossSections(t *testing.T) {
	cases := map[string]string{
		"agent": `version: "1.0"
agent:
  name: x
  owner: admin
`,
		"credential": `version: "1.0"
agent: {name: x}
credentials:
  - id: k
    type: header
    value: x
    provider: aws
`,
		"mcp": `version: "1.0"
agent: {name: x}
mcp_servers:
  - name: s
    url: http://127.0.0.1
    tools: [read]
`,
		"hook": `version: "1.0"
agent: {name: x}
hooks:
  - name: h
    url: https://example.com/h
    token: x
`,
		"ingress": `version: "1.0"
agent: {name: x}
ingress:
  - path: /
    port: 8080
    auth: none
`,
	}
	for section, input := range cases {
		t.Run(section, func(t *testing.T) {
			_, err := ParsePolicy(strings.NewReader(input))
			if err == nil {
				t.Errorf("ADVERSARY BREAK [MEDIUM]: unknown nested field in %s accepted", section)
			}
		})
	}
}

func TestAdversaryT01_AnchorMergeCannotSmuggleUnknown(t *testing.T) {
	input := `version: "1.0"
agent: {name: x}
smuggle: &s
  hidden_field: true
egress:
  - domain: example.com
    ports: [443]
    <<: *s
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Error("ADVERSARY BREAK [HIGH]: YAML merge/anchor smuggled unknown fields into egress")
	}
}

// ---------------------------------------------------------------------------
// Type confusion
// ---------------------------------------------------------------------------

func TestAdversaryT01_CredentialTypeMustBeStringEnum(t *testing.T) {
	// ADVERSARY BREAK: non-string credential.type coerced to string without error.
	input := `version: "1.0"
agent: {name: x}
credentials:
  - id: k
    type: 12345
    header: X
    value: v
`
	p, err := ParsePolicy(strings.NewReader(input))
	if err != nil {
		return // strict rejection is acceptable
	}
	if p.Credentials[0].Type != "12345" {
		t.Fatalf("unexpected type %q", p.Credentials[0].Type)
	}
	t.Error("ADVERSARY BREAK [MEDIUM]: credential.type accepts non-string YAML scalar (int coerced); invalid enum values bypass parse-time rejection")
}

func TestAdversaryT01_PortsRejectStringScalar(t *testing.T) {
	input := `version: "1.0"
agent: {name: x}
egress:
  - domain: example.com
    ports: "443"
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Error("ADVERSARY BREAK [MEDIUM]: ports accepts string scalar instead of int list")
	}
}

func TestAdversaryT01_IngressPortRejectsString(t *testing.T) {
	input := `version: "1.0"
agent: {name: x}
ingress:
  - path: /
    port: "443"
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Error("ADVERSARY BREAK [MEDIUM]: ingress.port accepts string instead of int")
	}
}

// ---------------------------------------------------------------------------
// Credential leakage in errors
// ---------------------------------------------------------------------------

func TestAdversaryT01_ErrorNoLeakOnUnknownFieldNearSecret(t *testing.T) {
	input := fmt.Sprintf(`version: "1.0"
agent: {name: x}
credentials:
  - id: leak
    type: header
    header: Authorization
    value: "%s"
    undeclared_field: true
`, adversarySecret)
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	assertErrorDoesNotLeakSecret(t, err)
}

func TestAdversaryT01_ErrorNoLeakOnMalformedYAMLNearSecret(t *testing.T) {
	input := fmt.Sprintf(`version: "1.0"
agent: {name: x}
credentials:
  - id: leak
    type: header
    value: "%s"
    ports: [443
`, adversarySecret)
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected malformed yaml error")
	}
	assertErrorDoesNotLeakSecret(t, err)
}

func TestAdversaryT01_ErrorNoLeakHookSecretOnUnknownField(t *testing.T) {
	input := fmt.Sprintf(`version: "1.0"
agent: {name: x}
hooks:
  - name: h
    url: https://x
    secret: "%s"
    extra: 1
`, adversarySecret)
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error")
	}
	assertErrorDoesNotLeakSecret(t, err)
}

// ---------------------------------------------------------------------------
// YAML bombs / resource limits
// ---------------------------------------------------------------------------

func TestAdversaryT01_LargeDocumentParsesWithinBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: \"1.0\"\nagent:\n  name: x\negress:\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, "  - domain: \"host%d.example.com\"\n    ports: [443]\n", i)
	}
	start := time.Now()
	_, err := ParsePolicy(strings.NewReader(b.String()))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("large but valid policy should parse: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("ADVERSARY BREAK [MEDIUM]: parsing 2000 egress rules took %v (possible DoS via large policy)", elapsed)
	}
}

func TestAdversaryT01_DeepNestingRejectedOrBounded(t *testing.T) {
	// Chain of nested unknown structures should not hang the parser.
	input := `version: "1.0"
agent: {name: x}
` + strings.Repeat("nested: &n\n  ", 200) + `leaf: 1
`
	done := make(chan error, 1)
	go func() {
		_, err := ParsePolicy(strings.NewReader(input))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("ADVERSARY BREAK [MEDIUM]: deeply nested unknown YAML accepted")
		}
	case <-time.After(3 * time.Second):
		t.Error("ADVERSARY BREAK [HIGH]: ParsePolicy hung on deep nested YAML (DoS)")
	}
}

// ---------------------------------------------------------------------------
// Confirmed-safe regression vectors (must stay rejected)
// ---------------------------------------------------------------------------

func TestAdversaryT01_VendorTyposStayRejected(t *testing.T) {
	for _, input := range []string{
		`version: "1.0"
agent: {name: x}
egress:
  - domain: x.com
    ports: [443]
    brokerd: vault
`,
		`version: "1.0"
agent: {name: x}
egress:
  - domain: "*.x.com"
    ports: [443]
    allow_wildcards: true
`,
		`version: "1.0"
agent: {name: x}
egress:
  - domain: x.com
    port: 443
`,
	} {
		_, err := ParsePolicy(strings.NewReader(input))
		if err == nil {
			t.Error("known vendor typo must stay rejected")
		}
	}
}

func TestAdversaryT01_MultipleDocumentsRejected(t *testing.T) {
	input := `version: "1.0"
agent: {name: a}
---
version: "2.0"
agent: {name: b}
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Error("multiple YAML documents must be rejected")
	}
}

func TestAdversaryT01_NilReaderRejected(t *testing.T) {
	_, err := ParsePolicy(nil)
	if err == nil {
		t.Error("nil reader must be rejected")
	}
}

func TestAdversaryT01_AgentNameNoNullByte(t *testing.T) {
	if !utf8.ValidString("ok") {
		t.Skip("utf8 sanity")
	}
	input := "version: \"1.0\"\nagent:\n  name: \"ok\x00evil\"\n"
	_, err := ParsePolicy(strings.NewReader(input))
	// Accept either rejection or parse without retaining null — if parsed, name must not contain NUL.
	if err == nil {
		p, _ := ParsePolicy(strings.NewReader(input))
		if p != nil && strings.ContainsRune(p.Agent.Name, 0) {
			t.Error("ADVERSARY BREAK [MEDIUM]: agent.name retains embedded null byte")
		}
	}
}