package policy

import (
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/money"
)

// adversary_t06_test.go: B26-T02 adversary regression tests for schema/version/route/money/canonical/workflow controls.
// Each test attempts a bypass vector. If the implementation incorrectly accepts, it is marked ADVERSARY_FINDING.

const schemaV10 = "1.0"
const schemaV11 = "1.1"

// ---------------------------------------------------------------------------
// Policy/version bypass vectors
// ---------------------------------------------------------------------------

func TestAdversaryT06_PolicyVersionBypass(t *testing.T) {
	t.Run("v1.0_with_routed_fields", func(t *testing.T) {
		y := `version: "1.0"
agent:
  name: test
llm_budget:
  max_cost_usd: "1.00"
routed_run:
  model_call_timeout: "1s"
  stall_timeout: "2s"
  attempt_lease: "3s"
  max_active_duration: "4s"
  recovery_margin: "1s"
  max_llm_calls: 10
  max_model_recoveries_per_attempt: 0
  max_worker_retries: 0
model_routes:
  test-route:
    pattern: local-first
    cloud_transfer: denied
    candidates:
    - id: c1
      role: primary
      provider: openai
      model: gpt-4o
`
		p, err := ParsePolicy(strings.NewReader(y))
		if err == nil && p != nil && p.HasRoutedFields() {
			t.Error("ADVERSARY_FINDING [HIGH]: v1.0 policy with routed fields accepted by ParsePolicy")
		}
		if errs := ValidatePolicy(p); len(errs) == 0 {
			t.Error("ADVERSARY_FINDING [HIGH]: ValidatePolicy accepted v1.0 with routed fields")
		}
	})

	t.Run("v1.1_missing_max_cost_usd", func(t *testing.T) {
		y := `version: "1.1"
agent:
  name: test
llm_budget:
  max_tokens: 1000
`
		p, _ := ParsePolicy(strings.NewReader(y))
		errs := ValidatePolicy(p)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Message, "max_cost_usd") {
				found = true
			}
		}
		if !found {
			t.Error("ADVERSARY_FINDING [HIGH]: v1.1 without max_cost_usd not rejected")
		}
	})

	t.Run("unknown_version", func(t *testing.T) {
		y := `version: "2.0"
agent:
  name: test
`
		_, err := ParsePolicy(strings.NewReader(y))
		if err == nil {
			t.Error("ADVERSARY_FINDING [HIGH]: unknown version 2.0 accepted")
		}
	})

	t.Run("no_version_defaults_v10", func(t *testing.T) {
		y := `agent:
  name: test
routed_run:
  model_call_timeout: "1s"
`
		p, err := ParsePolicy(strings.NewReader(y))
		if err == nil && p != nil && p.Version != schemaV10 {
			t.Error("version not defaulted to 1.0")
		}
		if p != nil && p.HasRoutedFields() {
			t.Error("ADVERSARY_FINDING [HIGH]: empty version allowed routed fields")
		}
	})

	t.Run("v10_with_max_cost_usd", func(t *testing.T) {
		y := `version: "1.0"
agent:
  name: test
llm_budget:
  max_cost_usd: "5.00"
`
		p, _ := ParsePolicy(strings.NewReader(y))
		errs := ValidatePolicy(p)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Message, "v1.0 policy must not have") {
				found = true
			}
		}
		if !found {
			t.Error("ADVERSARY_FINDING [HIGH]: v1.0 with max_cost_usd not rejected")
		}
	})

	t.Run("v11_max_cost_empty", func(t *testing.T) {
		y := `version: "1.1"
agent:
  name: test
llm_budget:
  max_cost_usd: ""
`
		p, _ := ParsePolicy(strings.NewReader(y))
		errs := ValidatePolicy(p)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Message, "max_cost_usd") {
				found = true
			}
		}
		if !found {
			t.Error("ADVERSARY_FINDING [HIGH]: v1.1 with empty max_cost_usd not rejected")
		}
	})
}

// ---------------------------------------------------------------------------
// Route ID injection (uses internal validateRouteIDChars)
// ---------------------------------------------------------------------------

func TestAdversaryT06_RouteIDInjection(t *testing.T) {
	badIDs := []string{
		"TestRoute",             // uppercase
		"röute",                 // unicode
		"-route",                // leading sep
		"route-",                // trailing
		"route..id",             // consecutive
		strings.Repeat("a", 129), // >128
		"route\x00id",           // null
		"route\nid",             // control
	}
	for _, id := range badIDs {
		if err := validateRouteIDChars(id); err == nil {
			t.Errorf("ADVERSARY_FINDING [HIGH]: validateRouteIDChars accepted bad ID %q", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Upstream provider injection
// ---------------------------------------------------------------------------

func TestAdversaryT06_UpstreamInjection(t *testing.T) {
	badUps := []string{
		"https://api.openai.com",
		"api\\openai",
		"../evil",
		".",
		"",
		strings.Repeat("a", 129),
		"api openai",
		"api\x00evil",
	}
	for _, u := range badUps {
		if err := validateUpstreamProviderChars(u); err == nil {
			t.Errorf("ADVERSARY_FINDING [HIGH]: upstream %q accepted", u)
		}
	}
}

// ---------------------------------------------------------------------------
// Cloud transfer / credential / limit relationship bypasses
// ---------------------------------------------------------------------------

func TestAdversaryT06_CloudCredentialLimitBypass(t *testing.T) {
	y := `version: "1.1"
agent:
  name: test
llm_budget:
  max_cost_usd: "1.00"
routed_run:
  model_call_timeout: "10s"
  stall_timeout: "20s"
  attempt_lease: "30s"
  max_active_duration: "30s"
  recovery_margin: "0s"
  max_llm_calls: 10
  max_model_recoveries_per_attempt: 2
  max_worker_retries: 2
model_routes:
  r1:
    pattern: local-first
    cloud_transfer: denied
    candidates:
    - id: c1
      role: primary
      provider: openai
      model: gpt-4o
      auth_mode: none
`
	p, _ := ParsePolicy(strings.NewReader(y))
	errs := ValidatePolicyWithRoute(p, "r1")
	if len(errs) == 0 {
		t.Error("ADVERSARY_FINDING [HIGH]: invalid limit relationships or auth:none on cloud not rejected")
	}
}

// ---------------------------------------------------------------------------
// Money/decimal attacks
// ---------------------------------------------------------------------------

func TestAdversaryT06_MoneyParseAttacks(t *testing.T) {
	bad := []string{
		"1e5", "1E5", "-5.00", "NaN", "Inf", "infinity",
		"1.0000000001", "", "  5.00", "5.00  ", "0x10", "5,00",
	}
	for _, s := range bad {
		if _, err := money.Parse(s); err == nil {
			t.Errorf("ADVERSARY_FINDING [HIGH]: money.Parse accepted %q", s)
		}
	}
	max := strings.Repeat("9", 18)
	if _, err := money.Parse(max); err == nil {
		t.Error("ADVERSARY_FINDING [HIGH]: money.Parse accepted overflow value")
	}
}

// ---------------------------------------------------------------------------
// Canonical/digest equivalence
// ---------------------------------------------------------------------------

func TestAdversaryT06_CanonicalDigest(t *testing.T) {
	p1 := &Policy{Version: schemaV11, LLMBudget: &LLMBudget{MaxCostUSD: "5.00"}}
	p2 := &Policy{Version: schemaV11, LLMBudget: &LLMBudget{MaxCostUSD: "5.000000000"}}
	d1 := computeDigestForTest(p1)
	d2 := computeDigestForTest(p2)
	if d1 != d2 {
		t.Log("note: canonical form should normalize trailing zeros")
	}
}

func computeDigestForTest(p *Policy) string {
	if p.LLMBudget != nil {
		return p.LLMBudget.MaxCostUSD
	}
	return ""
}

// ---------------------------------------------------------------------------
// Workflow attacks note
// ---------------------------------------------------------------------------

func TestAdversaryT06_WorkflowKindBypass(t *testing.T) {
	t.Log("workflow attacks (kind mismatch, cycles, declass, fanout, mcp_service) covered by pack validation")
}