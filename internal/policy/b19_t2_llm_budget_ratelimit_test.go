package policy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B19-T2: LLM Token Budget & Rate Limiting
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_LLMRateLimitOnLLMRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
			{Domain: "api.stripe.com", Ports: []int{443}},
		},
		LLMRateLimit: &LLMRateLimit{
			RequestsPerMinute: 30,
			TokensPerMinute:   50000,
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

	// The LLM route (api.openai.com) should have localRateLimit policies.
	if !strings.Contains(outStr, "localRateLimit") {
		t.Errorf("expected localRateLimit in LLM route, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "type: requests") {
		t.Errorf("expected request-based rate limit, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "type: tokens") {
		t.Errorf("expected token-based rate limit, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "maxTokens: 30") {
		t.Errorf("expected maxTokens: 30 for requests_per_minute, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "maxTokens: 50000") {
		t.Errorf("expected maxTokens: 50000 for tokens_per_minute, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "fillInterval: 1m") {
		t.Errorf("expected fillInterval: 1m, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_LLMBudgetPerRequest(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMBudget: &LLMBudget{
			MaxTokens:           10000,
			MaxTokensPerRequest: 2000,
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Per-request budget should appear as a token-based localRateLimit.
	if !strings.Contains(outStr, "localRateLimit") {
		t.Errorf("expected localRateLimit for budget, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "maxTokens: 2000") {
		t.Errorf("expected maxTokens: 2000 for max_tokens_per_request, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_NoLLMPoliciesOnNonLLMRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.stripe.com", Ports: []int{443}},
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

	// Non-LLM route should NOT have localRateLimit.
	if strings.Contains(outStr, "localRateLimit") {
		t.Errorf("localRateLimit should NOT appear on non-LLM route, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_NoLLMPoliciesWhenNotConfigured(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// No LLM governance fields → no localRateLimit.
	if strings.Contains(outStr, "localRateLimit") {
		t.Errorf("localRateLimit should NOT appear when no LLM governance is configured, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_BackwardCompatNoLLMFields(t *testing.T) {
	// Existing policies without llm_budget or llm_rate_limit must still compile.
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

	// Should not contain rate limit policies.
	if strings.Contains(string(out), "localRateLimit") {
		t.Error("samplePolicy should not have localRateLimit")
	}
}

// ---------------------------------------------------------------------------
// B19-T2: LLM Budget & Rate Limit Validation
// ---------------------------------------------------------------------------

func TestValidateLLMBudget_NegativeMaxTokens(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMBudget: &LLMBudget{MaxTokens: -1},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "max_tokens must be non-negative")
}

func TestValidateLLMBudget_PerRequestExceedsTotal(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMBudget: &LLMBudget{
			MaxTokens:           1000,
			MaxTokensPerRequest: 2000,
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "max_tokens_per_request cannot exceed max_tokens")
}

func TestValidateLLMBudget_ValidConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMBudget: &LLMBudget{
			MaxTokens:           10000,
			MaxTokensPerRequest: 2000,
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateLLMRateLimit_NegativeValues(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMRateLimit: &LLMRateLimit{
			RequestsPerMinute: -5,
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "requests_per_minute must be non-negative")
}

func TestValidateLLMRateLimit_BothZero(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMRateLimit: &LLMRateLimit{},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "at least one of requests_per_minute or tokens_per_minute")
}

func TestValidateLLMRateLimit_ValidConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}},
		},
		LLMRateLimit: &LLMRateLimit{
			RequestsPerMinute: 30,
			TokensPerMinute:   50000,
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

// ---------------------------------------------------------------------------
// B19-T2: LLM Budget & Rate Limit YAML Parsing
// ---------------------------------------------------------------------------

func TestParsePolicy_WithLLMBudgetAndRateLimit(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
llm_budget:
  max_tokens: 10000
  max_tokens_per_request: 2000
llm_rate_limit:
  requests_per_minute: 30
  tokens_per_minute: 50000
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.LLMBudget == nil {
		t.Fatal("expected LLMBudget to be non-nil")
	}
	if p.LLMBudget.MaxTokens != 10000 {
		t.Errorf("expected MaxTokens=10000, got %d", p.LLMBudget.MaxTokens)
	}
	if p.LLMBudget.MaxTokensPerRequest != 2000 {
		t.Errorf("expected MaxTokensPerRequest=2000, got %d", p.LLMBudget.MaxTokensPerRequest)
	}
	if p.LLMRateLimit == nil {
		t.Fatal("expected LLMRateLimit to be non-nil")
	}
	if p.LLMRateLimit.RequestsPerMinute != 30 {
		t.Errorf("expected RequestsPerMinute=30, got %d", p.LLMRateLimit.RequestsPerMinute)
	}
	if p.LLMRateLimit.TokensPerMinute != 50000 {
		t.Errorf("expected TokensPerMinute=50000, got %d", p.LLMRateLimit.TokensPerMinute)
	}
}

func TestParsePolicy_WithoutLLMFields(t *testing.T) {
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
	if p.LLMBudget != nil {
		t.Error("expected LLMBudget to be nil")
	}
	if p.LLMRateLimit != nil {
		t.Error("expected LLMRateLimit to be nil")
	}
}
