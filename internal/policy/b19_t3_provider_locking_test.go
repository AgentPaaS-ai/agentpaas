package policy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B19-T3: LLM Provider Locking — Parser Tests
// ---------------------------------------------------------------------------

func TestParsePolicy_WithLLMProviderLock(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: openrouter.ai
    ports: [443]
llm_provider_lock:
  allowed_endpoints:
    - https://openrouter.ai/api/v1/chat/completions
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.LLMProviderLock == nil {
		t.Fatal("expected LLMProviderLock to be non-nil")
	}
	if len(p.LLMProviderLock.AllowedEndpoints) != 1 {
		t.Fatalf("expected 1 allowed endpoint, got %d", len(p.LLMProviderLock.AllowedEndpoints))
	}
	if p.LLMProviderLock.AllowedEndpoints[0] != "https://openrouter.ai/api/v1/chat/completions" {
		t.Errorf("unexpected endpoint: %s", p.LLMProviderLock.AllowedEndpoints[0])
	}
}

func TestParsePolicy_MultipleEndpointsInProviderLock(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: openrouter.ai
    ports: [443]
  - domain: api.openai.com
    ports: [443]
llm_provider_lock:
  allowed_endpoints:
    - https://openrouter.ai/api/v1/chat/completions
    - https://api.openai.com/v1/chat/completions
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if len(p.LLMProviderLock.AllowedEndpoints) != 2 {
		t.Fatalf("expected 2 allowed endpoints, got %d", len(p.LLMProviderLock.AllowedEndpoints))
	}
}

func TestParsePolicy_WithoutLLMProviderLock(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: openrouter.ai
    ports: [443]
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.LLMProviderLock != nil {
		t.Error("expected LLMProviderLock to be nil when not configured")
	}
}

// ---------------------------------------------------------------------------
// B19-T3: LLM Provider Locking — Validation Tests
// ---------------------------------------------------------------------------

func TestValidateLLMProviderLock_ValidHTTPS(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"https://openrouter.ai/api/v1/chat/completions",
			},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateLLMProviderLock_EmptyEndpoints(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "at least one endpoint is required")
}

func TestValidateLLMProviderLock_RejectsHTTP(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"http://openrouter.ai/api/v1/chat/completions",
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "must use https scheme")
}

func TestValidateLLMProviderLock_InvalidURL(t *testing.T) {
	// Go's url.Parse is lenient and rarely returns errors for simple strings.
	// Test that endpoints with no hostname or path are caught.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"https://", // no host, no path
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "must include a hostname")
}

func TestValidateLLMProviderLock_MissingPath(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"https://openrouter.ai",
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "must include a path")
}

// ---------------------------------------------------------------------------
// B19-T3: LLM Provider Locking — Compiler Tests
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_LLMProviderLockAddsPathRestriction(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"https://openrouter.ai/api/v1/chat/completions",
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

	// The LLM route should have path restrictions.
	if !strings.Contains(outStr, "path: /api/v1/chat/completions") {
		t.Errorf("expected path restriction in LLM route, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_LLMProviderLockMultiplePaths(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"https://api.openai.com/v1/chat/completions",
				"https://api.openai.com/v1/embeddings",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if !strings.Contains(outStr, "path: /v1/chat/completions") {
		t.Errorf("expected path for chat completions, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "path: /v1/embeddings") {
		t.Errorf("expected path for embeddings, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_LLMProviderLockOnlyAffectsLLMRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
			{Domain: "api.stripe.com", Ports: []int{443}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"https://api.openai.com/v1/chat/completions",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Stripe route should NOT have path restrictions.
	// Check that the stripe route exists without path restrictions.
	if !strings.Contains(outStr, "api.stripe.com") {
		t.Error("expected stripe route to exist")
	}

	// OpenAI route should have path restrictions.
	if !strings.Contains(outStr, "path: /v1/chat/completions") {
		t.Errorf("expected path restriction on OpenAI route, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_NonLLMRoutesUnaffected(t *testing.T) {
	// Non-LLM routes should be completely unaffected by provider locking.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.stripe.com", Ports: []int{443}, Methods: []string{"GET", "POST"}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"https://openrouter.ai/api/v1/chat/completions",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Stripe route should have no path restrictions from provider lock.
	if strings.Contains(outStr, "path:") {
		t.Errorf("non-LLM routes should not have path restrictions, got:\n%s", outStr)
	}

	// But should still have its method matches.
	if !strings.Contains(outStr, "method: GET") {
		t.Error("expected method: GET on non-LLM route")
	}
	if !strings.Contains(outStr, "method: POST") {
		t.Error("expected method: POST on non-LLM route")
	}
}

func TestCompileGatewayConfig_LLMProviderLockWithMethodRestrictions(t *testing.T) {
	// When both method restrictions and provider lock are active, the route
	// should have combined method+path matches.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Methods: []string{"POST"}},
		},
		LLMProviderLock: &LLMProviderLock{
			AllowedEndpoints: []string{
				"https://api.openai.com/v1/chat/completions",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Should have both method and path in the same match.
	if !strings.Contains(outStr, "method: POST") {
		t.Errorf("expected method: POST, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "path: /v1/chat/completions") {
		t.Errorf("expected path: /v1/chat/completions, got:\n%s", outStr)
	}
}

// ---------------------------------------------------------------------------
// B19-T3: LLM Provider Locking — Backward Compatibility
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_BackwardCompatNoProviderLock(t *testing.T) {
	// Existing policies without llm_provider_lock must still compile.
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

	// Should not contain path restrictions.
	if strings.Contains(string(out), "path:") {
		t.Error("samplePolicy should not have path restrictions")
	}
}