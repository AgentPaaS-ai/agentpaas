package policy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B19-T7: Request/Response Transformations — YAML Parsing
// ---------------------------------------------------------------------------

func TestParsePolicy_WithTransformations(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
transformations:
  request:
    inject_headers:
      X-Agent-ID: "${agent_name}"
    inject_system_prompt: "You are a helpful assistant. Always be concise."
  response:
    remove_headers:
      - X-Internal-Debug
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.Transformations == nil {
		t.Fatal("expected Transformations to be non-nil")
	}
	if p.Transformations.Request == nil {
		t.Fatal("expected Transformations.Request to be non-nil")
	}
	if p.Transformations.Request.InjectHeaders == nil {
		t.Fatal("expected InjectHeaders to be non-nil")
	}
	if p.Transformations.Request.InjectHeaders["X-Agent-ID"] != "${agent_name}" {
		t.Errorf("expected X-Agent-ID header to be ${agent_name}, got %q", p.Transformations.Request.InjectHeaders["X-Agent-ID"])
	}
	if p.Transformations.Request.InjectSystemPrompt != "You are a helpful assistant. Always be concise." {
		t.Errorf("unexpected inject_system_prompt: %q", p.Transformations.Request.InjectSystemPrompt)
	}
	if p.Transformations.Response == nil {
		t.Fatal("expected Transformations.Response to be non-nil")
	}
	if len(p.Transformations.Response.RemoveHeaders) != 1 {
		t.Fatalf("expected 1 remove_header, got %d", len(p.Transformations.Response.RemoveHeaders))
	}
	if p.Transformations.Response.RemoveHeaders[0] != "X-Internal-Debug" {
		t.Errorf("expected X-Internal-Debug, got %q", p.Transformations.Response.RemoveHeaders[0])
	}
}

func TestParsePolicy_RequestOnlyTransformations(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
transformations:
  request:
    inject_system_prompt: "Be concise."
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.Transformations == nil {
		t.Fatal("expected Transformations to be non-nil")
	}
	if p.Transformations.Request == nil {
		t.Fatal("expected Request to be non-nil")
	}
	if p.Transformations.Response != nil {
		t.Error("expected Response to be nil")
	}
}

func TestParsePolicy_ResponseOnlyTransformations(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
transformations:
  response:
    remove_headers:
      - X-Debug
      - X-Trace-Id
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.Transformations == nil {
		t.Fatal("expected Transformations to be non-nil")
	}
	if p.Transformations.Response == nil {
		t.Fatal("expected Response to be non-nil")
	}
	if len(p.Transformations.Response.RemoveHeaders) != 2 {
		t.Fatalf("expected 2 remove_headers, got %d", len(p.Transformations.Response.RemoveHeaders))
	}
}

func TestParsePolicy_WithoutTransformations(t *testing.T) {
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
	if p.Transformations != nil {
		t.Error("expected Transformations to be nil")
	}
}

// ---------------------------------------------------------------------------
// B19-T7: Request/Response Transformations — Validation
// ---------------------------------------------------------------------------

func TestValidateTransformations_EmptyTransformObject(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "neither request nor response is configured")
}

func TestValidateTransformations_OversizedSystemPrompt(t *testing.T) {
	bigPrompt := strings.Repeat("x", 4097)
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Request: &RequestTransform{
				InjectSystemPrompt: bigPrompt,
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "inject_system_prompt must not exceed 4096")
}

func TestValidateTransformations_SystemPromptExactly4096(t *testing.T) {
	prompt := strings.Repeat("x", 4096)
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Request: &RequestTransform{
				InjectSystemPrompt: prompt,
			},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateTransformations_ControlCharsInHeader(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Response: &ResponseTransform{
				RemoveHeaders: []string{"X-Bad\x00Header"},
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "invalid HTTP header name")
}

func TestValidateTransformations_EmptyHeaderName(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Response: &ResponseTransform{
				RemoveHeaders: []string{""},
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "invalid HTTP header name")
}

func TestValidateTransformations_NewlineInHeader(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Response: &ResponseTransform{
				RemoveHeaders: []string{"X-Evil\nHeader-Injection"},
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "invalid HTTP header name")
}

func TestValidateTransformations_ValidConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Request: &RequestTransform{
				InjectHeaders: map[string]string{
					"X-Agent-ID": "${agent_name}",
				},
				InjectSystemPrompt: "Be helpful.",
			},
			Response: &ResponseTransform{
				RemoveHeaders: []string{"X-Internal-Debug", "X-Trace-Id"},
			},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateTransformations_ValidHeadersWithHyphens(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Response: &ResponseTransform{
				RemoveHeaders: []string{"X-Custom-Header", "X-Request-ID", "Content-Type"},
			},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

// ---------------------------------------------------------------------------
// B19-T7: Request/Response Transformations — Compiler
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_TransformationsOnLLMRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
			{Domain: "api.stripe.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Request: &RequestTransform{
				InjectHeaders: map[string]string{
					"X-Agent-ID": "${agent_name}",
				},
				InjectSystemPrompt: "You are a helpful assistant. Always be concise.",
			},
			Response: &ResponseTransform{
				RemoveHeaders: []string{"X-Internal-Debug"},
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Must be valid YAML.
	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, outStr)
	}

	// agentgateway transformations: request.set + response.remove.
	if !strings.Contains(outStr, "transformations:") {
		t.Errorf("expected transformations in compiled config, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "set:") {
		t.Errorf("expected request set in compiled config, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "X-Agent-ID") {
		t.Errorf("expected X-Agent-ID in compiled config, got:\n%s", outStr)
	}
	// inject_system_prompt is omitted for host backends (no gateway field).
	if strings.Contains(outStr, "injectSystemPrompt") || strings.Contains(outStr, "Always be concise") {
		t.Errorf("inject_system_prompt must not appear in host-backend gateway config, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "remove:") {
		t.Errorf("expected response remove in compiled config, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "X-Internal-Debug") {
		t.Errorf("expected X-Internal-Debug in compiled config, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_NoTransformationsOnNonLLMRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.stripe.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Request: &RequestTransform{
				InjectSystemPrompt: "Be helpful.",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Non-LLM route should NOT have transformations.
	if strings.Contains(outStr, "transformation") {
		t.Errorf("transformation should NOT appear on non-LLM route, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_TransformationsWithRateLimits(t *testing.T) {
	// When both transformations and rate limits are set, both should appear.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMRateLimit: &LLMRateLimit{
			RequestsPerMinute: 30,
		},
		Transformations: &Transformation{
			Request: &RequestTransform{
				InjectHeaders: map[string]string{"X-Mode": "concise"},
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if !strings.Contains(outStr, "transformations:") {
		t.Errorf("expected transformations in compiled config, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "localRateLimit") {
		t.Errorf("expected localRateLimit in compiled config, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_RequestOnlyTransformation(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Request: &RequestTransform{
				InjectHeaders: map[string]string{
					"X-Custom": "value",
				},
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if !strings.Contains(outStr, "set:") {
		t.Errorf("expected request set in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "X-Custom") {
		t.Errorf("expected X-Custom in output, got:\n%s", outStr)
	}
	// No response path.
	if strings.Contains(outStr, "remove:") && strings.Contains(outStr, "response:") {
		t.Errorf("response remove should not appear when only request transforms are set, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_ResponseOnlyTransformation(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		Transformations: &Transformation{
			Response: &ResponseTransform{
				RemoveHeaders: []string{"X-Debug"},
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if !strings.Contains(outStr, "remove:") {
		t.Errorf("expected response remove in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "X-Debug") {
		t.Errorf("expected X-Debug in output, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "set:") && strings.Contains(outStr, "request:") {
		t.Errorf("request set should not appear when only response transforms are set, got:\n%s", outStr)
	}
}

// ---------------------------------------------------------------------------
// B19-T7: Backward Compatibility
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_BackwardCompatNoTransformations(t *testing.T) {
	// Existing policies without transformations must still compile.
	p := samplePolicy()
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}

	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, string(out))
	}

	// Should not contain transformation policies.
	if strings.Contains(string(out), "transformation") {
		t.Error("samplePolicy should not have transformation")
	}
}

func TestValidateTransformations_NoTransformationsInSamplePolicy(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}