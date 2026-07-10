package policy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B19-T8: Per-Route Timeouts & Retry — YAML Parsing
// ---------------------------------------------------------------------------

func TestParsePolicy_WithTimeoutAndRetry(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: openrouter.ai
    ports: [443]
    timeout: 30s
    retry:
      max_attempts: 3
      backoff: exponential
      max_backoff: 10s
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if len(p.Egress) != 1 {
		t.Fatalf("expected 1 egress rule, got %d", len(p.Egress))
	}
	e := p.Egress[0]
	if e.Timeout != "30s" {
		t.Errorf("expected Timeout='30s', got %q", e.Timeout)
	}
	if e.Retry == nil {
		t.Fatal("expected Retry to be non-nil")
	}
	if e.Retry.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", e.Retry.MaxAttempts)
	}
	if e.Retry.Backoff != "exponential" {
		t.Errorf("expected Backoff='exponential', got %q", e.Retry.Backoff)
	}
	if e.Retry.MaxBackoff != "10s" {
		t.Errorf("expected MaxBackoff='10s', got %q", e.Retry.MaxBackoff)
	}
}

func TestParsePolicy_WithoutTimeoutOrRetry(t *testing.T) {
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
	if len(p.Egress) != 1 {
		t.Fatalf("expected 1 egress rule, got %d", len(p.Egress))
	}
	e := p.Egress[0]
	if e.Timeout != "" {
		t.Errorf("expected Timeout to be empty, got %q", e.Timeout)
	}
	if e.Retry != nil {
		t.Error("expected Retry to be nil")
	}
}

// ---------------------------------------------------------------------------
// B19-T8: Per-Route Timeouts & Retry — Validation
// ---------------------------------------------------------------------------

func TestValidateTimeout_InvalidDuration(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Timeout: "not-a-duration"},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "invalid duration")
}

func TestValidateTimeout_ValidDuration(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Timeout: "30s"},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateRetry_InvalidMaxAttempts(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Retry: &RetryConfig{
				MaxAttempts: 0,
				Backoff:     "exponential",
			}},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "max_attempts must be >= 1")
}

func TestValidateRetry_InvalidBackoffStrategy(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Retry: &RetryConfig{
				MaxAttempts: 3,
				Backoff:     "jittered",
			}},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "invalid backoff strategy")
}

func TestValidateRetry_InvalidMaxBackoff(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Retry: &RetryConfig{
				MaxAttempts: 3,
				Backoff:     "exponential",
				MaxBackoff:  "xyz",
			}},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "invalid duration")
}

func TestValidateTimeoutAndRetry_ValidConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Timeout: "30s", Retry: &RetryConfig{
				MaxAttempts: 3,
				Backoff:     "exponential",
				MaxBackoff:  "10s",
			}},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateRetry_AllValidBackoffStrategies(t *testing.T) {
	for _, backoff := range []string{"exponential", "linear", "fixed"} {
		p := &Policy{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Egress: []EgressRule{
				{Domain: "api.openai.com", Ports: []int{443}, Retry: &RetryConfig{
					MaxAttempts: 2,
					Backoff:     backoff,
				}},
			},
		}
		errs := ValidatePolicy(p)
		if HasErrors(errs) {
			t.Errorf("backoff %q should be valid, got errors: %v", backoff, errs[0])
		}
	}
}

// ---------------------------------------------------------------------------
// B19-T8: Per-Route Timeouts & Retry — Compiler
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_TimeoutOnEgressRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}, Timeout: "30s"},
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

	// Timeout should appear in the gateway config.
	if !strings.Contains(outStr, "timeout") {
		t.Errorf("expected 'timeout' in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "requestTimeout: 30s") {
		t.Errorf("expected requestTimeout: 30s in output, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_RetryOnEgressRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}, Retry: &RetryConfig{
				MaxAttempts: 3,
				Backoff:     "exponential",
			}},
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

	// Retry should appear in the gateway config.
	if !strings.Contains(outStr, "retry") {
		t.Errorf("expected 'retry' in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "attempts: 3") {
		t.Errorf("expected attempts: 3 in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "backoff: exponential") {
		t.Errorf("expected backoff: exponential in output, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_TimeoutAndRetryOnSameRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}, Timeout: "30s", Retry: &RetryConfig{
				MaxAttempts: 3,
				Backoff:     "exponential",
				MaxBackoff:  "10s",
			}},
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

	// Both timeout and retry should appear.
	if !strings.Contains(outStr, "requestTimeout: 30s") {
		t.Errorf("expected requestTimeout: 30s in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "attempts: 3") {
		t.Errorf("expected attempts: 3 in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "backoff: exponential") {
		t.Errorf("expected backoff: exponential in output, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_TimeoutOnlyOnSpecificRoute(t *testing.T) {
	// Timeout on one route should NOT appear on another route.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "openrouter.ai", Ports: []int{443}, Timeout: "30s"},
			{Domain: "api.stripe.com", Ports: []int{443}},
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

	// openrouter route should have timeout.
	openrouterIdx := strings.Index(outStr, "openrouter.ai")
	stripeIdx := strings.Index(outStr, "api.stripe.com")
	if openrouterIdx < 0 || stripeIdx < 0 {
		t.Fatalf("missing expected route names in output:\n%s", outStr)
	}

	// Get the section between openrouter and stripe.
	openrouterSection := outStr[openrouterIdx:stripeIdx]
	if !strings.Contains(openrouterSection, "requestTimeout: 30s") {
		t.Errorf("openrouter.ai route should have requestTimeout, got:\n%s", openrouterSection)
	}

	// Get the section after stripe (until denied route or end).
	stripeSection := outStr[stripeIdx:]
	if strings.Contains(stripeSection, "requestTimeout") {
		t.Errorf("api.stripe.com route should NOT have requestTimeout, got:\n%s", stripeSection)
	}
}

// ---------------------------------------------------------------------------
// B19-T8: Per-Route Timeouts & Retry — Backward Compatibility
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_BackwardCompatNoTimeoutOrRetry(t *testing.T) {
	// Existing policies without timeout or retry must still compile.
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

	// Should not contain timeout or retry policies.
	if strings.Contains(string(out), "requestTimeout") {
		t.Error("samplePolicy should not have requestTimeout")
	}
}

func TestCompileGatewayConfig_LLMRouteGetsBothRateLimitAndTimeout(t *testing.T) {
	// Timeout/retry on an LLM route should coexist with rate limiting.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Timeout: "60s"},
		},
		LLMRateLimit: &LLMRateLimit{
			RequestsPerMinute: 30,
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

	// Should have both localRateLimit and timeout.
	if !strings.Contains(outStr, "localRateLimit") {
		t.Errorf("expected localRateLimit in LLM route, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "requestTimeout: 60s") {
		t.Errorf("expected requestTimeout: 60s in LLM route, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_TimeoutAndRetryWithOAuth(t *testing.T) {
	// Timeout/retry should coexist with OAuth backend config.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Timeout: "45s", Retry: &RetryConfig{
				MaxAttempts: 2,
				Backoff:     "linear",
			}, Credential: "openai-oauth"},
		},
		Credentials: []Credential{
			{
				ID:                    "openai-oauth",
				Type:                  "oauth",
				TokenEndpoint:          "https://auth.example.com/token",
				ClientID:               "client-123",
				RefreshTokenCredential: "openai-refresh",
			},
			{
				ID:    "openai-refresh",
				Type:  "header",
				Header: "Authorization",
				Value: "Bearer refresh-token",
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

	// Should have timeout, retry, and backendOAuth.
	if !strings.Contains(outStr, "requestTimeout: 45s") {
		t.Errorf("expected requestTimeout: 45s, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "attempts: 2") {
		t.Errorf("expected attempts: 2, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "backendOAuth") {
		t.Errorf("backendOAuth must be omitted (not agentgateway v1.3.0 route field), got:\n%s", outStr)
	}
}