package policy

import (
	"bytes"
	"testing"
)

// Fixture v1.1 valid local-first policy with one route.
const validLocalFirstYAML = `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: allowed
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
`

// Fixture v1.1 valid cloud-cost-first policy with one route.
const validCloudCostFirstYAML = `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "10.00"
model_routes:
  primary:
    pattern: cloud-cost-first
    cloud_transfer: allowed
    candidates:
      - id: openai
        role: primary
        provider: openai
        model: gpt-4o
        location: cloud
      - id: anthropic
        role: recovery
        provider: anthropic
        model: claude-sonnet-4
        location: cloud
`

// Fixture v1.0 policy (no routed fields).
const v10LegacyYAML = `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: test-key
    type: header
    header: X-Key
    value: secret
`

// TestRouteValidLocalFirst verifies a valid local-first policy parses and validates.
func TestRouteValidLocalFirst(t *testing.T) {
	p, err := ParsePolicy(bytes.NewReader([]byte(validLocalFirstYAML)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := ValidatePolicy(p)
	if HasErrors(errs) {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	errs = validateRouteAndCandidateRules(p, "primary")
	if HasErrors(errs) {
		t.Fatalf("unexpected route validation errors: %v", errs)
	}
	if p.Version != SchemaVersion11 {
		t.Errorf("version = %q, want 1.1", p.Version)
	}
	if p.LLMBudget == nil || p.LLMBudget.MaxCostUSD != "5.00" {
		t.Errorf("max_cost_usd = %q, want 5.00", p.LLMBudget.MaxCostUSD)
	}
	if len(p.ModelRoutes) != 1 {
		t.Fatalf("expected 1 model route, got %d", len(p.ModelRoutes))
	}
	route := p.ModelRoutes["primary"]
	if route.Pattern != PatternLocalFirst {
		t.Errorf("pattern = %q, want local-first", route.Pattern)
	}
	if len(route.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(route.Candidates))
	}
}

// TestRouteValidCloudCostFirst verifies a valid cloud-cost-first policy parses and validates.
func TestRouteValidCloudCostFirst(t *testing.T) {
	p, err := ParsePolicy(bytes.NewReader([]byte(validCloudCostFirstYAML)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := ValidatePolicy(p)
	if HasErrors(errs) {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	errs = validateRouteAndCandidateRules(p, "primary")
	if HasErrors(errs) {
		t.Fatalf("unexpected route validation errors: %v", errs)
	}
	route := p.ModelRoutes["primary"]
	if route.Pattern != PatternCloudCostFirst {
		t.Errorf("pattern = %q, want cloud-cost-first", route.Pattern)
	}
}

// TestRouteMutualExclusion is tested in pack package (LLMConfig mutual exclusion).
func TestRouteMutualExclusion(t *testing.T) {
	t.Skip("LLMConfig mutual exclusion is tested in internal/pack")
}

// TestRouteUnknownEnumNegative tests that unknown enum values fail closed (via route validation).
func TestRouteUnknownEnumNegative(t *testing.T) {
	tests := []struct {
		name    string
		yamlStr string
	}{
		{
			name: "bad pattern",
			yamlStr: makeRouteYAML("primary", func(r *routeTemplate) {
				r.pattern = "bad-pattern"
			}),
		},
		{
			name: "bad role",
			yamlStr: makeRouteYAML("primary", func(r *routeTemplate) {
				r.candidateRoles = []string{"bad-role", "recovery"}
			}),
		},
		{
			name: "bad location",
			yamlStr: makeRouteYAML("primary", func(r *routeTemplate) {
				r.candidateLocations = []string{"bad-location", "cloud"}
			}),
		},
		{
			name: "bad cloud_transfer",
			yamlStr: makeRouteYAML("primary", func(r *routeTemplate) {
				r.cloudTransfer = "bad-value"
			}),
		},
		{
			name: "bad capability_tier",
			yamlStr: makeRouteYAML("primary", func(r *routeTemplate) {
				r.capabilityTier = "bad-tier"
			}),
		},
		{
			name: "bad feature",
			yamlStr: makeRouteYAML("primary", func(r *routeTemplate) {
				r.features = []string{"bad-feature"}
			}),
		},
		{
			name: "bad effort",
			yamlStr: makeRouteYAML("primary", func(r *routeTemplate) {
				r.effort = "bad-effort"
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePolicy(bytes.NewReader([]byte(tc.yamlStr)))
			if err != nil {
				t.Fatalf("ParsePolicy: %v", err)
			}
			errs := validateRouteAndCandidateRules(p, "primary")
			if !HasErrors(errs) {
				t.Fatal("expected route validation errors, got none")
			}
		})
	}
}

// routeTemplate helps build route YAML for testing.
type routeTemplate struct {
	pattern            string
	cloudTransfer      string
	capabilityTier     string
	features           []string
	effort             string
	candidateIDs       []string
	candidateRoles     []string
	candidateProviders []string
	candidateModels    []string
	candidateLocations []string
}

func defaultRouteTemplate() routeTemplate {
	return routeTemplate{
		pattern:            "local-first",
		cloudTransfer:      "allowed",
		candidateIDs:       []string{"c1", "c2"},
		candidateRoles:     []string{"primary", "recovery"},
		candidateProviders: []string{"ollama", "openai"},
		candidateModels:    []string{"llama3", "gpt-4o"},
		candidateLocations: []string{"local", "cloud"},
	}
}

func makeRouteYAML(routeName string, mod func(*routeTemplate)) string {
	tpl := defaultRouteTemplate()
	mod(&tpl)

	yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  ` + routeName + `:
    pattern: ` + tpl.pattern + `
    cloud_transfer: ` + tpl.cloudTransfer + `
`
	if tpl.capabilityTier != "" || len(tpl.features) > 0 || tpl.effort != "" {
		yaml += "    minimum:\n"
		if tpl.capabilityTier != "" {
			yaml += "      capability_tier: " + tpl.capabilityTier + "\n"
		}
		if len(tpl.features) > 0 {
			yaml += "      features:\n"
			for _, f := range tpl.features {
				yaml += "        - " + f + "\n"
			}
		}
		if tpl.effort != "" {
			yaml += "      effort: " + tpl.effort + "\n"
		}
	}
	yaml += "    candidates:\n"
	for i := range tpl.candidateIDs {
		role := tpl.candidateRoles[i]
		loc := tpl.candidateLocations[i]
		prov := tpl.candidateProviders[i]
		model := tpl.candidateModels[i]
		yaml += `      - id: ` + tpl.candidateIDs[i] + `
        role: ` + role + `
        provider: ` + prov + `
        model: ` + model + `
        location: ` + loc + "\n"
	}
	return yaml
}

// TestRouteV10LegacyParse verifies v1.0 policy without routed fields validates.
func TestRouteV10LegacyParse(t *testing.T) {
	p, err := ParsePolicy(bytes.NewReader([]byte(v10LegacyYAML)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := ValidatePolicy(p)
	if HasErrors(errs) {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if p.Version != SchemaVersion10 {
		t.Errorf("version = %q, want 1.0", p.Version)
	}
}

// TestRouteV11RoutedParse verifies v1.1 policy with routed fields validates.
func TestRouteV11RoutedParse(t *testing.T) {
	p, err := ParsePolicy(bytes.NewReader([]byte(validLocalFirstYAML)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := ValidatePolicy(p)
	if HasErrors(errs) {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	errs = validateRouteAndCandidateRules(p, "primary")
	if HasErrors(errs) {
		t.Fatalf("unexpected route validation errors: %v", errs)
	}
}

// TestRouteV10WithRoutedFields verifies v1.0 policy with routed fields fails closed.
func TestRouteV10WithRoutedFields(t *testing.T) {
	yaml := `version: "1.0"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Fatal("expected validation errors for v1.0 with routed fields, got none")
	}
}

// TestRouteV11MissingMaxCostUSD verifies v1.1 without max_cost_usd fails.
func TestRouteV11MissingMaxCostUSD(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.pattern = "local-first"
	})
	// Remove max_cost_usd by using a custom yaml
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	// Override the max_cost_usd to empty
	p.LLMBudget.MaxCostUSD = ""
	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Fatal("expected validation errors for missing max_cost_usd, got none")
	}
}

// TestRouteV11ExplicitZeroMaxCostUSD verifies v1.1 with explicit zero max_cost_usd succeeds.
func TestRouteV11ExplicitZeroMaxCostUSD(t *testing.T) {
	yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "0.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := ValidatePolicy(p)
	if HasErrors(errs) {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

// TestRouteMalformedMaxCostUSD tests malformed max_cost_usd values are rejected.
func TestRouteMalformedMaxCostUSD(t *testing.T) {
	badValues := []string{"1e5", "-5.00", "NaN"}
	for _, val := range badValues {
		t.Run("bad_"+val, func(t *testing.T) {
			yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "` + val + `"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
`
			p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
			if err != nil {
				t.Fatalf("ParsePolicy: %v", err)
			}
			errs := ValidatePolicy(p)
			if !HasErrors(errs) {
				t.Fatalf("expected validation errors for max_cost_usd=%q, got none", val)
			}
		})
	}
}

// TestRouteMissingPrimaryCandidate verifies missing primary candidate fails at route validation.
func TestRouteMissingPrimaryCandidate(t *testing.T) {
	// Without auto-recovery, the role requirements are less strict.
	// But the local-first pattern still requires primary candidates to be local.
	// Use a recovery-only config with auto-recovery to force the check.
	yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: allowed
    candidates:
      - id: ollama
        role: recovery
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
routed_run:
  model_call_timeout: 30s
  stall_timeout: 60s
  attempt_lease: 120s
  max_active_duration: 300s
  recovery_margin: 30s
  max_llm_calls: 10
  max_model_recoveries_per_attempt: 1
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	// This should fail because auto-recovery requires both primary and recovery roles
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors when auto-recovery enabled but no primary candidate, got none")
	}
}

// TestRouteMissingRecoveryCandidateWithAutoRecovery tests recovery required when auto-recovery enabled.
func TestRouteMissingRecoveryCandidateWithAutoRecovery(t *testing.T) {
	yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: primary
        provider: openai
        model: gpt-4o
        location: cloud
routed_run:
  model_call_timeout: 30s
  stall_timeout: 60s
  attempt_lease: 120s
  max_active_duration: 300s
  recovery_margin: 30s
  max_llm_calls: 10
  max_model_recoveries_per_attempt: 1
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for missing recovery with auto-recovery, got none")
	}
}

// TestRouteExtraRoute verifies extra executable routes fail.
func TestRouteExtraRoute(t *testing.T) {
	yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
  secondary:
    pattern: cloud-cost-first
    cloud_transfer: allowed
    candidates:
      - id: openai
        role: primary
        provider: openai
        model: gpt-4o
        location: cloud
      - id: anthropic
        role: recovery
        provider: anthropic
        model: claude-sonnet-4
        location: cloud
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for extra route, got none")
	}
}

// TestRouteTooFewCandidates verifies less than 2 candidates fails.
func TestRouteTooFewCandidates(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.candidateIDs = []string{"c1"}
		r.candidateRoles = []string{"primary"}
		r.candidateProviders = []string{"ollama"}
		r.candidateModels = []string{"llama3"}
		r.candidateLocations = []string{"local"}
	})
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for too few candidates, got none")
	}
}

// TestRouteTooManyCandidates verifies more than 64 candidates fails.
func TestRouteTooManyCandidates(t *testing.T) {
	var cands string
	for i := 0; i < 65; i++ {
		role := "primary"
		if i > 0 {
			role = "recovery"
		}
		cands += `      - id: c` + string(rune('a'+i%26)) + string(rune('0'+i/10)) + `
        role: ` + role + `
        provider: provider` + string(rune('a'+i%26)) + `
        model: model` + string(rune('a'+i%26)) + `
        location: cloud
`
	}
	yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
` + cands
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for too many candidates, got none")
	}
}

// TestRouteDuplicateCandidateID verifies duplicate candidate IDs fail.
func TestRouteDuplicateCandidateID(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.candidateIDs = []string{"dup", "dup"}
	})
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for duplicate candidate ID, got none")
	}
}

// TestRouteUnsafeRouteID verifies unsafe route IDs fail.
func TestRouteUnsafeRouteID(t *testing.T) {
	badIDs := []string{"Primary", "rüte", "-primary", "primary-"}
	for _, id := range badIDs {
		t.Run("bad_"+id, func(t *testing.T) {
			yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  ` + id + `:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
`
			p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
			if err != nil {
				t.Fatalf("ParsePolicy: %v", err)
			}
			errs := validateRouteAndCandidateRules(p, id)
			if !HasErrors(errs) {
				t.Fatalf("expected route validation errors for route ID %q, got none", id)
			}
		})
	}
}

// TestRouteUnsafeCandidateID verifies unsafe candidate IDs fail.
func TestRouteUnsafeCandidateID(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.candidateIDs = []string{"Bad-ID", "good-id"}
	})
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for unsafe candidate ID, got none")
	}
}

// TestRouteCloudCandidateWithDeniedTransfer verifies cloud candidate rejected when cloud_transfer denied.
func TestRouteCloudCandidateWithDeniedTransfer(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.cloudTransfer = "denied"
	})
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for cloud candidate with denied transfer, got none")
	}
}

// TestRouteCredentialNotDeclared is a known gap — cross-reference validation not yet implemented.
func TestRouteCredentialNotDeclared(t *testing.T) {
	t.Skip("route-level credential cross-reference validation not yet implemented")
}

// TestRouteOpenRouterWithoutUpstreamProviders tests OpenRouter requires upstream_providers.
func TestRouteOpenRouterWithoutUpstreamProviders(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.pattern = "cloud-cost-first"
		r.cloudTransfer = "allowed"
		r.candidateIDs = []string{"openrouter", "backup"}
		r.candidateRoles = []string{"primary", "recovery"}
		r.candidateProviders = []string{"openrouter", "openai"}
		r.candidateModels = []string{"gpt-4o", "gpt-4o"}
		r.candidateLocations = []string{"cloud", "cloud"}
	})
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for OpenRouter without upstream_providers, got none")
	}
}

// TestRouteDirectLocalWithUpstreamProviders tests direct/local with upstream_providers fails.
func TestRouteDirectLocalWithUpstreamProviders(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.candidateIDs = []string{"c1", "c2"}
		r.candidateRoles = []string{"primary", "recovery"}
		r.candidateProviders = []string{"ollama", "openai"}
		r.candidateModels = []string{"llama3", "gpt-4o"}
		r.candidateLocations = []string{"local", "cloud"}
	})
	// Add upstream_providers to the local candidate manually
	yaml = yaml + "        upstream_providers:\n          - openai\n"
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for direct/local with upstream_providers, got none")
	}
}

// TestRouteUpstreamProvidersTooMany tests >8 upstream_providers entries.
func TestRouteUpstreamProvidersTooMany(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.pattern = "cloud-cost-first"
		r.cloudTransfer = "allowed"
		r.candidateIDs = []string{"or", "backup"}
		r.candidateRoles = []string{"primary", "recovery"}
		r.candidateProviders = []string{"openrouter", "openai"}
		r.candidateModels = []string{"gpt-4o", "gpt-4o"}
		r.candidateLocations = []string{"cloud", "cloud"}
	})
	// Add 9 upstream_providers manually
	yaml = yaml + `        upstream_providers:
          - a
          - b
          - c
          - d
          - e
          - f
          - g
          - h
          - i
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for too many upstream_providers, got none")
	}
}

// TestRouteUpstreamProvidersDuplicates tests duplicate upstream_providers entries.
func TestRouteUpstreamProvidersDuplicates(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.pattern = "cloud-cost-first"
		r.cloudTransfer = "allowed"
		r.candidateIDs = []string{"or", "backup"}
		r.candidateRoles = []string{"primary", "recovery"}
		r.candidateProviders = []string{"openrouter", "openai"}
		r.candidateModels = []string{"gpt-4o", "gpt-4o"}
		r.candidateLocations = []string{"cloud", "cloud"}
	})
	yaml = yaml + `        upstream_providers:
          - openai
          - openai
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for duplicate upstream_providers, got none")
	}
}

// TestRouteUpstreamProvidersUnsafeChars tests unsafe upstream_provider characters.
func TestRouteUpstreamProvidersUnsafeChars(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.pattern = "cloud-cost-first"
		r.cloudTransfer = "allowed"
		r.candidateIDs = []string{"or", "backup"}
		r.candidateRoles = []string{"primary", "recovery"}
		r.candidateProviders = []string{"openrouter", "openai"}
		r.candidateModels = []string{"gpt-4o", "gpt-4o"}
		r.candidateLocations = []string{"cloud", "cloud"}
	})
	yaml = yaml + `        upstream_providers:
          - "provider with space"
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for unsafe upstream_provider, got none")
	}
}

// TestRouteAuthNoneOnCloudCandidate tests auth: none on cloud candidate fails.
func TestRouteAuthNoneOnCloudCandidate(t *testing.T) {
	yaml := makeRouteYAML("primary", func(r *routeTemplate) {
		r.pattern = "cloud-cost-first"
		r.cloudTransfer = "allowed"
		r.candidateRoles = []string{"primary", "recovery"}
		r.candidateLocations = []string{"cloud", "cloud"}
	})
	// Add auth_mode: none to the first candidate manually
	yaml = `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: cloud-cost-first
    cloud_transfer: allowed
    candidates:
      - id: c1
        role: primary
        provider: openai
        model: gpt-4o
        location: cloud
        auth_mode: none
      - id: c2
        role: recovery
        provider: anthropic
        model: claude-sonnet-4
        location: cloud
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if !HasErrors(errs) {
		t.Fatal("expected route validation errors for auth: none on cloud candidate, got none")
	}
}

// TestRouteAuthNoneOnLocalCustomEndpoint tests auth: none on local endpoint succeeds.
func TestRouteAuthNoneOnLocalCustomEndpoint(t *testing.T) {
	yaml := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: allowed
    candidates:
      - id: local
        role: primary
        provider: ollama
        model: llama3
        location: local
        endpoint: "http://localhost:11434"
        auth_mode: none
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
`
	p, err := ParsePolicy(bytes.NewReader([]byte(yaml)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	errs := validateRouteAndCandidateRules(p, "primary")
	if HasErrors(errs) {
		t.Fatalf("unexpected route validation errors: %v", errs)
	}
}

// TestRouteCustomEndpointWithoutPrivateEgress documents a known gap.
func TestRouteCustomEndpointWithoutPrivateEgress(t *testing.T) {
	t.Skip("custom endpoint private egress check not yet implemented")
}

// Helper to build routed_run limit YAML.
func routedRunLimitYAML(ct, st, al, ma, rm string, maxRecoveries, maxRetries int) string {
	return `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
routed_run:
  model_call_timeout: ` + ct + `
  stall_timeout: ` + st + `
  attempt_lease: ` + al + `
  max_active_duration: ` + ma + `
  recovery_margin: ` + rm + `
  max_llm_calls: 10
  max_model_recoveries_per_attempt: ` + string(rune('0'+maxRecoveries)) + `
  max_worker_retries: ` + string(rune('0'+maxRetries)) + `
`
}

// TestRouteLimitRelationships tests limit relationship violations.
func TestRouteLimitRelationships(t *testing.T) {
	tests := []struct {
		name    string
		yamlStr string
	}{
		{
			name: "model_call_timeout > attempt_lease",
			yamlStr: routedRunLimitYAML(
				"60s", "30s", "30s", "300s", "30s", 0, 0,
			),
		},
		{
			name: "stall_timeout > attempt_lease",
			yamlStr: routedRunLimitYAML(
				"30s", "60s", "30s", "300s", "30s", 0, 0,
			),
		},
		{
			name: "attempt_lease >= max_active_duration",
			yamlStr: routedRunLimitYAML(
				"30s", "30s", "300s", "300s", "30s", 0, 0,
			),
		},
		{
			name: "recovery_margin <= 0",
			yamlStr: routedRunLimitYAML(
				"30s", "30s", "120s", "300s", "0s", 0, 0,
			),
		},
		{
			name: "recovery_margin >= max_active_duration",
			yamlStr: routedRunLimitYAML(
				"30s", "30s", "120s", "300s", "300s", 0, 0,
			),
		},
		{
			name: "attempt_lease + recovery_margin > max_active_duration",
			yamlStr: routedRunLimitYAML(
				"30s", "30s", "200s", "300s", "200s", 0, 0,
			),
		},
		{
			name: "max_model_recoveries_per_attempt > 1",
			yamlStr: routedRunLimitYAML(
				"30s", "30s", "120s", "300s", "30s", 2, 0,
			),
		},
		{
			name: "max_worker_retries > 1",
			yamlStr: routedRunLimitYAML(
				"30s", "30s", "120s", "300s", "30s", 0, 2,
			),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePolicy(bytes.NewReader([]byte(tc.yamlStr)))
			if err != nil {
				t.Fatalf("ParsePolicy: %v", err)
			}
			errs := ValidatePolicy(p)
			if !HasErrors(errs) {
				t.Fatal("expected validation errors, got none")
			}
		})
	}
}

// TestRouteCanonicalizationOrderInvariance verifies order-invariant canonical digest.
func TestRouteCanonicalizationOrderInvariance(t *testing.T) {
	base := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  a:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
  b:
    pattern: cloud-cost-first
    cloud_transfer: allowed
    candidates:
      - id: openai
        role: primary
        provider: openai
        model: gpt-4o
        location: cloud
      - id: anthropic
        role: recovery
        provider: anthropic
        model: claude-sonnet-4
        location: cloud
`
	reversed := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  b:
    pattern: cloud-cost-first
    cloud_transfer: allowed
    candidates:
      - id: openai
        role: primary
        provider: openai
        model: gpt-4o
        location: cloud
      - id: anthropic
        role: recovery
        provider: anthropic
        model: claude-sonnet-4
        location: cloud
  a:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
`
	p1, err := ParsePolicy(bytes.NewReader([]byte(base)))
	if err != nil {
		t.Fatalf("ParsePolicy base: %v", err)
	}
	p2, err := ParsePolicy(bytes.NewReader([]byte(reversed)))
	if err != nil {
		t.Fatalf("ParsePolicy reversed: %v", err)
	}

	d1, err := Digest(p1)
	if err != nil {
		t.Fatalf("Digest base: %v", err)
	}
	d2, err := Digest(p2)
	if err != nil {
		t.Fatalf("Digest reversed: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digests differ for order-invariant routes:\nbase=%s\nreversed=%s", d1, d2)
	}
}

// TestRouteDigestChangesWhenRouteChanges verifies digest changes when routes change.
func TestRouteDigestChangesWhenRouteChanges(t *testing.T) {
	base := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: openai
        role: recovery
        provider: openai
        model: gpt-4o
        location: cloud
`
	changed := `version: "1.1"
agent:
  name: test-agent
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: denied
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
      - id: anthropic
        role: recovery
        provider: anthropic
        model: claude-sonnet-4
        location: cloud
`
	p1, err := ParsePolicy(bytes.NewReader([]byte(base)))
	if err != nil {
		t.Fatalf("ParsePolicy base: %v", err)
	}
	p2, err := ParsePolicy(bytes.NewReader([]byte(changed)))
	if err != nil {
		t.Fatalf("ParsePolicy changed: %v", err)
	}

	d1, _ := Digest(p1)
	d2, _ := Digest(p2)
	if d1 == d2 {
		t.Fatal("digests should differ for different route configurations")
	}
}

// TestRouteLegacyV10DigestUnchanged verifies legacy v1.0 digest is stable.
func TestRouteLegacyV10DigestUnchanged(t *testing.T) {
	p, err := ParsePolicy(bytes.NewReader([]byte(v10LegacyYAML)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	d, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if len(d) != 64 {
		t.Fatalf("expected 64-char hex digest, got %q (len=%d)", d, len(d))
	}
	d2, _ := Digest(p)
	if d != d2 {
		t.Fatal("digest not stable")
	}
}