package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

const canaryPII = "canary-pii-m155-t1-ssn-078-05-1120"

func TestPIIBuiltinAllowList(t *testing.T) {
	if PIIBuiltinCreditCard != "CreditCard" || PIIBuiltinSsn != "Ssn" || PIIBuiltinEmail != "Email" {
		t.Fatalf("builtins=%q %q %q", PIIBuiltinCreditCard, PIIBuiltinSsn, PIIBuiltinEmail)
	}
	if !AllowedPIIBuiltin(PIIBuiltinCreditCard) || !AllowedPIIBuiltin(PIIBuiltinSsn) || !AllowedPIIBuiltin(PIIBuiltinEmail) {
		t.Fatal("gateway PII builtins must be allowed")
	}
	for _, bad := range []string{"AWS", "GitHub", "PEM", "aws_key", "private_key"} {
		if AllowedPIIBuiltin(bad) {
			t.Fatalf("D203: builtin %q must be rejected", bad)
		}
	}
}

func TestParsePolicy_PIIMapping(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
guardrails:
  pii:
    action: mask
    builtins: [CreditCard, Ssn, Email]
    patterns:
      - "(?i)employee-id-[0-9]+"
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if len(p.Guardrails) != 0 {
		t.Fatalf("B19 list must stay empty for mapping form, got %+v", p.Guardrails)
	}
	if p.PII == nil {
		t.Fatal("PII is nil")
	}
	if p.PII.Action != PIIActionMask {
		t.Fatalf("action=%q", p.PII.Action)
	}
	if len(p.PII.Builtins) != 3 || p.PII.Builtins[0] != PIIBuiltinCreditCard {
		t.Fatalf("builtins=%v", p.PII.Builtins)
	}
	if len(p.PII.Patterns) != 1 || p.PII.Patterns[0] != "(?i)employee-id-[0-9]+" {
		t.Fatalf("patterns=%v", p.PII.Patterns)
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestParsePolicy_PIIOptionalAbsent_SC8(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if p.PII != nil {
		t.Fatalf("PII must be nil when omitted, got %+v", p.PII)
	}
	if len(p.Guardrails) != 0 {
		t.Fatalf("guardrails=%v", p.Guardrails)
	}
}

func TestParsePolicy_B19SequenceUnchanged(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
guardrails:
  - type: regex
    pattern: "(?i)(password|secret)"
    action: block
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if p.PII != nil {
		t.Fatal("sequence form must not set PII")
	}
	if len(p.Guardrails) != 1 || p.Guardrails[0].Type != "regex" || p.Guardrails[0].Action != "block" {
		t.Fatalf("B19 list broken: %+v", p.Guardrails)
	}
}

func TestParsePolicy_PIIUnknownKeyRejected(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
guardrails:
  pii:
    action: mask
    builtins: [Email]
  webhook: https://evil.example/check
`
	_, err := ParsePolicy(strings.NewReader(yamlStr))
	if err == nil {
		t.Fatal("expected unknown mapping key to fail parse")
	}
}

func TestValidatePII_ActionMustBeMaskOrReject(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII:     &PIIGuardrail{Action: "block", Builtins: []string{PIIBuiltinEmail}},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "mask")
}

func TestValidatePII_BuiltinSecretPackRejected_D203(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII: &PIIGuardrail{
			Action:   PIIActionReject,
			Builtins: []string{"AWS", "GitHub", "PEM"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "builtin")
}

func TestValidatePII_EmptyBuiltinsAndPatterns(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII:     &PIIGuardrail{Action: PIIActionMask},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "builtins")
}

func TestValidatePII_InvalidRegexPattern(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII:     &PIIGuardrail{Action: PIIActionMask, Patterns: []string{"("}},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "pattern")
}

func TestValidatePII_RejectStatusOnlyWithReject(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII: &PIIGuardrail{
			Action:       PIIActionMask,
			Builtins:     []string{PIIBuiltinEmail},
			RejectStatus: 422,
			RejectBody:   "nope",
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "reject")
}

func TestValidatePII_RejectStatusMustBe4xx(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII: &PIIGuardrail{
			Action:       PIIActionReject,
			Builtins:     []string{PIIBuiltinEmail},
			RejectStatus: 500,
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "4xx")
}

func TestCompileGatewayConfig_PIIOmitted_SC6(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII: &PIIGuardrail{
			Action:   PIIActionMask,
			Builtins: []string{PIIBuiltinSsn},
			Patterns: []string{canaryPII},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	outStr := string(out)
	if strings.Contains(outStr, "guardrails") {
		t.Fatalf("route-level guardrails leaked into gateway config:\n%s", outStr)
	}
	if strings.Contains(outStr, canaryPII) {
		t.Fatalf("SC6: canary PII in compiled gateway config:\n%s", outStr)
	}
	if strings.Contains(strings.ToLower(outStr), "backendoauth") {
		t.Fatalf("backendOAuth leaked:\n%s", outStr)
	}
}

func TestPIIRequiresBufferedStream_D202(t *testing.T) {
	if PIIRequiresBufferedStream(nil) || PIIRequiresBufferedStream(&Policy{}) {
		t.Fatal("off when pii absent")
	}
	on := &Policy{PII: &PIIGuardrail{Action: PIIActionReject, Builtins: []string{PIIBuiltinEmail}}}
	if !PIIRequiresBufferedStream(on) {
		t.Fatal("mask|reject must buffer-then-inspect")
	}
}

func TestCanonicalize_PIIOmitempty_SC8(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
	}
	cp, _ := Canonicalize(p)
	b, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "\"pii\"") {
		t.Fatalf("pii must omitempty when unset: %s", b)
	}
}

func TestCanonicalize_PIIPresent(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII: &PIIGuardrail{
			Action:   PIIActionMask,
			Builtins: []string{PIIBuiltinEmail, PIIBuiltinCreditCard},
			Patterns: []string{"b-pat", "a-pat"},
		},
	}
	cp, _ := Canonicalize(p)
	if cp.PII == nil || cp.PII.Action != PIIActionMask {
		t.Fatalf("canonical pii=%+v", cp.PII)
	}
	if len(cp.PII.Builtins) != 2 || cp.PII.Builtins[0] != PIIBuiltinCreditCard {
		t.Fatalf("builtins not sorted: %v", cp.PII.Builtins)
	}
	if len(cp.PII.Patterns) != 2 || cp.PII.Patterns[0] != "a-pat" {
		t.Fatalf("patterns not sorted: %v", cp.PII.Patterns)
	}
}
