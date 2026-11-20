package policy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B19-T6: Gateway-Level Guardrails
// ---------------------------------------------------------------------------

func TestParsePolicy_WithGuardrails(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
credentials:
  - id: openai-key
    type: header
    header: Authorization
guardrails:
  - type: regex
    pattern: "(?i)(password|secret)"
    action: block
  - type: moderation
    provider: openai
    credential: openai-key
  - type: webhook
    url: https://guardrails.example.com/check
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if len(p.Guardrails) != 3 {
		t.Fatalf("expected 3 guardrails, got %d", len(p.Guardrails))
	}
	if p.Guardrails[0].Type != "regex" || p.Guardrails[0].Pattern != "(?i)(password|secret)" || p.Guardrails[0].Action != "block" {
		t.Errorf("guardrail 0 mismatch: %+v", p.Guardrails[0])
	}
	if p.Guardrails[1].Type != "moderation" || p.Guardrails[1].Provider != "openai" || p.Guardrails[1].Credential != "openai-key" {
		t.Errorf("guardrail 1 mismatch: %+v", p.Guardrails[1])
	}
	if p.Guardrails[2].Type != "webhook" || p.Guardrails[2].URL != "https://guardrails.example.com/check" {
		t.Errorf("guardrail 2 mismatch: %+v", p.Guardrails[2])
	}
}

func TestParsePolicy_WithoutGuardrails(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if len(p.Guardrails) != 0 {
		t.Errorf("expected 0 guardrails, got %d", len(p.Guardrails))
	}
}

func TestValidateGuardrails_MissingType(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Pattern: "test", Action: "block"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "guardrail type is required")
}

func TestValidateGuardrails_InvalidType(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "invalid"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "invalid guardrail type")
}

func TestValidateGuardrails_RegexMissingPattern(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "regex", Action: "block"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "pattern is required for regex guardrail")
}

func TestValidateGuardrails_RegexInvalidAction(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "regex", Pattern: "test", Action: "invalid"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "action must be 'block' or 'mask'")
}

func TestValidateGuardrails_ModerationMissingProvider(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "moderation", Credential: "openai-key"},
		},
		Credentials: []Credential{
			{ID: "openai-key", Type: "header", Header: "Authorization"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "provider is required for moderation guardrail")
}

func TestValidateGuardrails_ModerationUndeclaredCredential(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "moderation", Provider: "openai", Credential: "missing-key"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "references undeclared credential")
}

func TestValidateGuardrails_WebhookMissingURL(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "webhook"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "url is required for webhook guardrail")
}

func TestValidateGuardrails_WebhookNonHTTPS(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "webhook", URL: "http://guardrails.example.com/check"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "url must be a valid https URL")
}

func TestValidateGuardrails_ValidConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "regex", Pattern: "test", Action: "block"},
			{Type: "moderation", Provider: "openai", Credential: "openai-key"},
		},
		Credentials: []Credential{
			{ID: "openai-key", Type: "header", Header: "Authorization"},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestCompileGatewayConfig_GuardrailsOnLLMRoute(t *testing.T) {
	// agentgateway v1.3.0 does not support guardrails as a route-level policy
	// field (requires AI backend type or extProc). Guardrails are parsed and
	// validated at the policy level but omitted from compiled gateway config.
	// This test verifies the config compiles without error when guardrails
	// are declared — enforcement is harness-level (same pattern as Bug 019).
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "regex", Pattern: "password", Action: "block"},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, outStr)
	}

	// Guardrails should NOT appear in gateway config (agentgateway v1.3.0
	// doesn't support route-level guardrails). The config must still be valid.
	if strings.Contains(outStr, "guardrails") {
		t.Errorf("guardrails should NOT appear in compiled gateway config (agentgateway v1.3.0), got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_NoGuardrailsOnNonLLMRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.stripe.com", Ports: []int{443}}},
		Guardrails: []Guardrail{
			{Type: "regex", Pattern: "test", Action: "block"},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if strings.Contains(outStr, "guardrails") {
		t.Errorf("guardrails should NOT appear on non-LLM route, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_BackwardCompatNoGuardrails(t *testing.T) {
	p := samplePolicy()
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if strings.Contains(outStr, "guardrails") {
		t.Errorf("samplePolicy should not have guardrails, got:\n%s", outStr)
	}
}
