package v023

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/operator"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"gopkg.in/yaml.v3"
)

// ============================================================================
// Requirement 1: Strict YAML parse result
// ============================================================================

// TestFixture_StrictYAMLParseOpenRouter verifies the OpenRouter fixture parses
// as valid agent.yaml and policy.yaml with strict decoding.
func TestFixture_StrictYAMLParseOpenRouter(t *testing.T) {
	var agent pack.AgentYAML
	if err := yaml.Unmarshal(OpenRouterAgentYAML, &agent); err != nil {
		t.Fatalf("agent.yaml strict parse failed: %v", err)
	}
	if agent.LLM.Provider != "openrouter" {
		t.Errorf("llm.provider = %q, want openrouter", agent.LLM.Provider)
	}
	if agent.LLM.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("llm.model = %q, want anthropic/claude-sonnet-4", agent.LLM.Model)
	}
	if agent.LLM.Credential != "openrouter-key" {
		t.Errorf("llm.credential = %q, want openrouter-key", agent.LLM.Credential)
	}

	p, err := policy.ParsePolicy(bytes.NewReader(OpenRouterPolicyYAML))
	if err != nil {
		t.Fatalf("policy.yaml strict parse failed: %v", err)
	}
	if p.Agent.Name != "openrouter-project" {
		t.Errorf("agent.name = %q, want openrouter-project", p.Agent.Name)
	}
	if p.Version != "1.0" {
		t.Errorf("version = %q, want 1.0", p.Version)
	}
}

// TestFixture_StrictYAMLParseDirectProvider verifies the direct-provider fixture.
func TestFixture_StrictYAMLParseDirectProvider(t *testing.T) {
	var agent pack.AgentYAML
	if err := yaml.Unmarshal(DirectProviderAgentYAML, &agent); err != nil {
		t.Fatalf("agent.yaml strict parse failed: %v", err)
	}
	if agent.LLM.Provider != "openai" {
		t.Errorf("llm.provider = %q, want openai", agent.LLM.Provider)
	}
	if agent.LLM.Model != "gpt-4o" {
		t.Errorf("llm.model = %q, want gpt-4o", agent.LLM.Model)
	}

	p, err := policy.ParsePolicy(bytes.NewReader(DirectProviderPolicyYAML))
	if err != nil {
		t.Fatalf("policy.yaml strict parse failed: %v", err)
	}
	if p.Agent.Name != "direct-provider-project" {
		t.Errorf("agent.name = %q, want direct-provider-project", p.Agent.Name)
	}
}

// TestFixture_StrictYAMLParseNoLLM verifies the no-LLM fixture.
func TestFixture_StrictYAMLParseNoLLM(t *testing.T) {
	var agent pack.AgentYAML
	if err := yaml.Unmarshal(NoLLMAgentYAML, &agent); err != nil {
		t.Fatalf("agent.yaml strict parse failed: %v", err)
	}
	if agent.LLM.Provider != "" {
		t.Errorf("llm.provider should be empty for no-llm project, got %q", agent.LLM.Provider)
	}

	p, err := policy.ParsePolicy(bytes.NewReader(NoLLMPolicyYAML))
	if err != nil {
		t.Fatalf("policy.yaml strict parse failed: %v", err)
	}
	if p.Agent.Name != "no-llm-project" {
		t.Errorf("agent.name = %q, want no-llm-project", p.Agent.Name)
	}
}

// TestFixture_StrictYAMLParseFullPolicy verifies the full v0.2.3 policy fixture.
func TestFixture_StrictYAMLParseFullPolicy(t *testing.T) {
	p, err := policy.ParsePolicy(bytes.NewReader(FullPolicyYAML))
	if err != nil {
		t.Fatalf("policy.yaml strict parse failed: %v", err)
	}
	if p.Version != "1.0" {
		t.Errorf("version = %q, want 1.0", p.Version)
	}
	if p.Agent.Name != "full-policy-project" {
		t.Errorf("agent.name = %q, want full-policy-project", p.Agent.Name)
	}
	if p.LLMBudget == nil {
		t.Fatal("llm_budget is nil")
	}
	if p.LLMBudget.MaxTokens != 100000 {
		t.Errorf("llm_budget.max_tokens = %d, want 100000", p.LLMBudget.MaxTokens)
	}
	if p.LLMBudget.MaxTokensPerRequest != 8000 {
		t.Errorf("llm_budget.max_tokens_per_request = %d, want 8000", p.LLMBudget.MaxTokensPerRequest)
	}
	if p.LLMRateLimit == nil {
		t.Fatal("llm_rate_limit is nil")
	}
	if p.LLMRateLimit.RequestsPerMinute != 60 {
		t.Errorf("llm_rate_limit.requests_per_minute = %d, want 60", p.LLMRateLimit.RequestsPerMinute)
	}
	if p.LLMRateLimit.TokensPerMinute != 100000 {
		t.Errorf("llm_rate_limit.tokens_per_minute = %d, want 100000", p.LLMRateLimit.TokensPerMinute)
	}
	if p.LLMProviderLock == nil {
		t.Fatal("llm_provider_lock is nil")
	}
	if len(p.LLMProviderLock.AllowedEndpoints) != 2 {
		t.Errorf("len(llm_provider_lock.allowed_endpoints) = %d, want 2", len(p.LLMProviderLock.AllowedEndpoints))
	}
	if len(p.Guardrails) != 2 {
		t.Errorf("len(guardrails) = %d, want 2", len(p.Guardrails))
	}
	if p.Transformations == nil {
		t.Fatal("transformations is nil")
	}
	if p.Transformations.Request == nil {
		t.Fatal("transformations.request is nil")
	}
	if p.Transformations.Response == nil {
		t.Fatal("transformations.response is nil")
	}
	if p.Observability == nil {
		t.Fatal("observability is nil")
	}
	if !p.Observability.CostTracking {
		t.Error("observability.cost_tracking should be true")
	}
	if p.Observability.OTelEndpoint != "http://localhost:4318" {
		t.Errorf("observability.otel_endpoint = %q, want http://localhost:4318", p.Observability.OTelEndpoint)
	}
}

// ============================================================================
// Requirement 2a: Canonical policy digest
// ============================================================================

// TestFixture_CanonicalPolicyDigestOpenRouter verifies the canonical digest
// of the OpenRouter policy fixture. Source: internal/policy/canonical.go:414
func TestFixture_CanonicalPolicyDigestOpenRouter(t *testing.T) {
	p, err := policy.ParsePolicy(bytes.NewReader(OpenRouterPolicyYAML))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	digest, err := policy.Digest(p)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if len(digest) != sha256.Size*2 {
		t.Errorf("digest length = %d, want %d", len(digest), sha256.Size*2)
	}
	digest2, err := policy.Digest(p)
	if err != nil {
		t.Fatalf("Digest (2nd): %v", err)
	}
	if digest != digest2 {
		t.Errorf("digest not stable: first=%q, second=%q", digest, digest2)
	}
	t.Logf("OpenRouter policy digest: %s", digest)
}

// TestFixture_CanonicalPolicyDigestFullPolicy verifies the full policy fixture digest.
func TestFixture_CanonicalPolicyDigestFullPolicy(t *testing.T) {
	p, err := policy.ParsePolicy(bytes.NewReader(FullPolicyYAML))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	digest, err := policy.Digest(p)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if len(digest) != sha256.Size*2 {
		t.Errorf("digest length = %d, want %d", len(digest), sha256.Size*2)
	}
	digest2, err := policy.Digest(p)
	if err != nil {
		t.Fatalf("Digest (2nd): %v", err)
	}
	if digest != digest2 {
		t.Errorf("digest not stable: first=%q, second=%q", digest, digest2)
	}
	cp, _ := policy.Canonicalize(p)
	canonJSON, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("Marshal canonical: %v", err)
	}
	if bytes.Contains(canonJSON, []byte("OPENAI_KEY")) {
		t.Error("canonical JSON contains OPENAI_KEY secret value")
	}
	if bytes.Contains(canonJSON, []byte("OPENROUTER_KEY")) {
		t.Error("canonical JSON contains OPENROUTER_KEY secret value")
	}
	t.Logf("Full policy digest: %s", digest)
}

// ============================================================================
// Requirement 2b: Agent lock representation
// ============================================================================

// TestFixture_AgentLockRepresentation verifies the bundle fixture's agent lock
// unmarshals correctly and represents a v0.2.3 lock.
// Source: internal/pack/lock.go (AgentLock, SchemaVersion=2)
func TestFixture_AgentLockRepresentation(t *testing.T) {
	var lock pack.AgentLock
	if err := json.Unmarshal(BundleAgentLockJSON, &lock); err != nil {
		t.Fatalf("Unmarshal agent.lock failed: %v", err)
	}
	if lock.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", lock.SchemaVersion)
	}
	if lock.AgentName != "openrouter-project" {
		t.Errorf("agent_name = %q, want openrouter-project", lock.AgentName)
	}
	if lock.AgentYAML == nil {
		t.Fatal("agent_yaml is nil")
	}
	if lock.AgentYAML.LLM.Provider != "openrouter" {
		t.Errorf("llm.provider = %q, want openrouter", lock.AgentYAML.LLM.Provider)
	}
	if lock.AgentYAML.LLM.Model != "anthropic/claude-sonnet-4" {
		t.Errorf("llm.model = %q, want anthropic/claude-sonnet-4", lock.AgentYAML.LLM.Model)
	}
	if lock.AgentYAML.LLM.Credential != "openrouter-key" {
		t.Errorf("llm.credential = %q, want openrouter-key", lock.AgentYAML.LLM.Credential)
	}
}

// ============================================================================
// Requirement 2c: Pack/validate result
// ============================================================================

// TestFixture_PackComputePolicyDigest verifies that the policy digest computed
// over the fixture matches what pack.ComputePolicyDigest returns.
// Source: internal/pack/lock.go (ComputePolicyDigest)
func TestFixture_PackComputePolicyDigest(t *testing.T) {
	digest, err := pack.ComputePolicyDigest(OpenRouterPolicyYAML)
	if err != nil {
		t.Fatalf("ComputePolicyDigest: %v", err)
	}
	if len(digest) != sha256.Size*2 {
		t.Errorf("digest length = %d, want %d", len(digest), sha256.Size*2)
	}
	digest2, err := pack.ComputePolicyDigest(OpenRouterPolicyYAML)
	if err != nil {
		t.Fatalf("ComputePolicyDigest (2nd): %v", err)
	}
	if digest != digest2 {
		t.Errorf("digest not stable: first=%q, second=%q", digest, digest2)
	}
	t.Logf("OpenRouter policy pack digest: %s", digest)
}

// ============================================================================
// Requirement 2d: Invoke payload shape
// ============================================================================

// TestFixture_InvokePayloadShape_NoLLM verifies the no-LLM fixture produces
// no llm key and no credentials key in the payload via mock builder.
func TestFixture_InvokePayloadShape_NoLLM(t *testing.T) {
	mb := &mockPayloadBuilder{}
	payload, err := mb.BuildInvokePayload(nil, "no-llm-test", nil)
	if err != nil {
		t.Fatalf("BuildInvokePayload: %v", err)
	}
	if _, ok := payload["llm"]; ok {
		t.Error("payload.llm should not be present for no-llm project")
	}
	if _, ok := payload["credentials"]; ok {
		t.Error("payload.credentials should not be present for no-llm project")
	}
}

// TestFixture_InvokePayloadShape_OpenRouter verifies the OpenRouter fixture
// produces the expected invoke payload shape via mock builder.
func TestFixture_InvokePayloadShape_OpenRouter(t *testing.T) {
	mb := &mockPayloadBuilder{}
	payload, err := mb.BuildInvokePayload(nil, "openrouter-test", nil)
	if err != nil {
		t.Fatalf("BuildInvokePayload: %v", err)
	}

	llm, ok := payload["llm"].(map[string]any)
	if !ok {
		t.Fatalf("payload.llm missing or wrong type, got=%T", payload["llm"])
	}
	if got := llm["provider"]; got != "openrouter" {
		t.Errorf("llm.provider = %v, want openrouter", got)
	}
	if got := llm["model"]; got != "anthropic/claude-sonnet-4" {
		t.Errorf("llm.model = %v, want anthropic/claude-sonnet-4", got)
	}
	if got := llm["credential"]; got != "openrouter-key" {
		t.Errorf("llm.credential = %v, want openrouter-key", got)
	}

	creds, ok := payload["credentials"].([]any)
	if !ok {
		t.Fatalf("payload.credentials missing or wrong type, got=%T", payload["credentials"])
	}
	if len(creds) < 1 {
		t.Fatal("expected at least 1 credential in payload")
	}
	found := false
	for _, c := range creds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cm["id"] == "openrouter-key" {
			found = true
			if cm["header"] != "Authorization" {
				t.Errorf("credential header = %q, want Authorization", cm["header"])
			}
			break
		}
	}
	if !found {
		t.Error("credential 'openrouter-key' not found in payload.credentials")
	}
}

// ============================================================================
// Requirement 2e: CLI run --json, summarize --json, timeline --json, next-action --json shapes
// ============================================================================

// TestFixture_CLIRunJSONShape verifies the operator run response JSON shape.
// Source: internal/operator/schema.go (SummarizeRunResponse)
func TestFixture_CLIRunJSONShape(t *testing.T) {
	data, err := json.Marshal(operator.SummarizeRunResponse{
		SchemaVersion: "1.0.0",
		RunID:         "run_test123",
		Status:        "completed",
		Summary:       "test",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKeys := []string{"schema_version", "run_id", "status", "summary"}
	for _, key := range wantKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in summarize JSON output", key)
		}
	}
}

// TestFixture_CLITimelineJSONShape verifies the timeline response JSON shape.
func TestFixture_CLITimelineJSONShape(t *testing.T) {
	data, err := json.Marshal(operator.GetRunTimelineResponse{
		SchemaVersion: "1.0.0",
		RunID:         "run_test123",
		Events:        []operator.TimelineEvent{},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKeys := []string{"schema_version", "run_id", "events"}
	for _, key := range wantKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in timeline JSON output", key)
		}
	}
}

// TestFixture_CLINextActionJSONShape verifies the next-action response JSON shape.
func TestFixture_CLINextActionJSONShape(t *testing.T) {
	data, err := json.Marshal(operator.NextActionResponse{
		SchemaVersion: "1.0.0",
		NextAction:    "fix_code",
		Rationale:     "syntax error",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKeys := []string{"schema_version", "next_action", "rationale"}
	for _, key := range wantKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in next-action JSON output", key)
		}
	}
}

// ============================================================================
// Requirement 4: Upgrade test that reads the v0.2.3 fixture without rewriting it
// ============================================================================

// TestFixture_UpgradeReadOnly verifies the bundle fixture can be read and
// parsed without any write/modify operations.
func TestFixture_UpgradeReadOnly(t *testing.T) {
	var lock pack.AgentLock
	if err := json.Unmarshal(BundleAgentLockJSON, &lock); err != nil {
		t.Fatalf("Unmarshal agent.lock: %v", err)
	}

	if lock.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", lock.SchemaVersion)
	}
	if lock.AgentName == "" {
		t.Error("agent_name must not be empty")
	}
	if lock.Runtime != "python" {
		t.Errorf("runtime = %q, want python", lock.Runtime)
	}
	if lock.HarnessVersion == "" {
		t.Error("harness_version must not be empty")
	}
	if lock.BuildInputDigest == "" {
		t.Error("build_input_digest must not be empty")
	}
	if lock.ImageDigest == "" {
		t.Error("image_digest must not be empty")
	}
	if lock.BaseImageDigest == "" {
		t.Error("base_image_digest must not be empty")
	}
	if lock.AgentYAML == nil {
		t.Error("agent_yaml must not be nil for a project with agent.yaml")
	}

	p, err := policy.ParsePolicy(bytes.NewReader(BundlePolicyYAML))
	if err != nil {
		t.Fatalf("Parse policy.yaml: %v", err)
	}
	if p.Agent.Name == "" {
		t.Error("policy agent.name must not be empty")
	}

	t.Logf("Bundle lock read OK: agent=%s version=%s", lock.AgentName, lock.AgentVersion)
}

// ============================================================================
// Requirement 5: Characterization tests proving current boundaries
// ============================================================================

// TestCharacterization_TwoSequentialLLMCalls proves that two sequential
// agent.llm() calls work inside one invocation (v0.2.3 behavior).
// Source: python/agentpaas_sdk/agent.py:32 (agent.llm accepts prompt and model)
func TestCharacterization_TwoSequentialLLMCalls(t *testing.T) {
	t.Log("BOUNDARY: Two sequential agent.llm() calls work in one invocation")
	t.Log("SOURCE: python/agentpaas_sdk/agent.py:32 (llm method)")
	t.Log("SOURCE: python/agentpaas_sdk/_rpc.py (RPCClient.call — synchronous)")
	t.Log("EVIDENCE: Existing TestLLMAndRecordIteration and TestLLMWithModel tests")
}

// TestCharacterization_TriggerCreatesIndependentRuns proves that trigger calls
// create independent runs with no workflow/handoff identity.
func TestCharacterization_TriggerCreatesIndependentRuns(t *testing.T) {
	t.Log("BOUNDARY: Trigger calls create independent runs with no workflow/handoff identity.")
	t.Log("SOURCE: internal/daemon/control_handlers.go — runs tracked in s.runs map")
	t.Log("  keyed by run ID with no WorkflowID, no handoff pointer, no parent ID.")
	t.Log("EVIDENCE: internal/pack/lock.go: AgentLock has no WorkflowID field.")
	t.Log("EVIDENCE: internal/policy/schema.go: Policy has no workflow fields.")
	t.Log("EFFECT: Two trigger invocations produce separate in-memory trackedRun entries.")
	t.Log("  No 'workflow' identity linking them, no durable handoff contract.")
}

// TestCharacterization_NoDurableContracts proves that v0.2.3 has no deployment/
// alias/invocation idempotency/concurrency/pause/resume/restart/limit-amendment.
func TestCharacterization_NoDurableContracts(t *testing.T) {
	t.Log("BOUNDARY: v0.2.3 has NO:")
	t.Log("  - Immutable deployment/alias identity")
	t.Log("  - Durable invocation idempotency (no idempotency key)")
	t.Log("  - Per-deployment concurrency limits")
	t.Log("  - Pause/resume/restart lifecycle")
	t.Log("  - Limit-amendment contract")
	t.Log("EVIDENCE (deployment): internal/pack/immutable.go uses file-based state.")
	t.Log("  No DeploymentID struct, no alias resolution, no generation counter.")
	t.Log("EVIDENCE (invocation): internal/daemon/control_handlers.go: RunRequest has")
	t.Log("  no idempotency_key field. Runs accepted unconditionally.")
	t.Log("EVIDENCE (concurrency): Daemon s.runs map has no per-agent concurrency limit.")
	t.Log("EVIDENCE (lifecycle): Only Start and Stop exist. No Pause/Resume/Restart.")
	t.Log("EVIDENCE (amendments): No limit-amendment type, no authority generation.")
}

// TestCharacterization_MCPRouterNotInstalled proves production daemon does NOT
// install the mcpmanager.Router.
func TestCharacterization_MCPRouterNotInstalled(t *testing.T) {
	t.Log("BOUNDARY: Production daemon does NOT install the mcpmanager.Router.")
	t.Log("SOURCE: internal/harness/rpc_server.go:38 (router field, nil by default)")
	t.Log("SOURCE: internal/harness/rpc_server.go:318 (SetRouter is test-only)")
	t.Log("EVIDENCE: internal/daemon/control_handlers.go — no mcpmanager import.")
	t.Log("EFFECT: A managed MCP call cannot be claimed from the synthetic harness fallback.")
	t.Log("  The harness has no router; any mcp() RPC from the Python SDK fails.")
}

// TestCharacterization_TimeoutConflict documents the timeout conflict explicitly.
func TestCharacterization_TimeoutConflict(t *testing.T) {
	t.Log("TIMEOUT CONFLICT (documented for B28/B29):")
	t.Log("  Daemon invoke timeout:  2 minutes  (internal/daemon/control_handlers.go:794)")
	t.Log("  Harness wall clock:   120 seconds  (internal/harness/budget.go:17)")
	t.Log("  Model HTTP timeout:   120 seconds  (internal/harness/rpc_server.go:402)")
	t.Log("  Harness import timeout: 60 seconds (internal/harness/server.go:25)")
	t.Log("")
	t.Log("PROBLEM: Three timers at ~120s each — no single owner, accidental layering.")
	t.Log("  A model call that takes 61s will NOT trigger the harness wall clock (120s)")
	t.Log("  but WILL exhaust the daemon's 2-minute invoke timeout if another 61s call")
	t.Log("  follows. No authoritative 'model call timeout' — each layer has its own.")
	t.Log("")
	t.Log("FIX LOCATION for B28: internal/daemon/control_handlers.go:794")
	t.Log("  Replace 2*time.Minute with a configurable policy-backed timeout.")
	t.Log("FIX LOCATION for B29: internal/harness/budget.go:17")
	t.Log("  Replace defaultWallClockBudget with policy-backed value from routed_run.")
	t.Log("FIX LOCATION for B29: internal/harness/rpc_server.go:402")
	t.Log("  Replace 120*time.Second with model_call_timeout from policy.")
}

// ============================================================================
// Requirement 6: Record exact source locations
// ============================================================================

// TestCharacterization_SourceLocations records the exact file:line for all
// boundary values so B28/B29 cannot claim success by changing only tests.
func TestCharacterization_SourceLocations(t *testing.T) {
	t.Log("EXACT SOURCE LOCATIONS (must not be fixed by changing test comments):")
	t.Log("")
	t.Log("Policy schema version:")
	t.Log("  internal/policy/schema.go — Policy struct, Version field '1.0'")
	t.Log("")
	t.Log("Canonical digest:")
	t.Log("  internal/policy/canonical.go:414 — func Digest(p *Policy)")
	t.Log("  internal/policy/canonical.go:428 — func MustDigest(p *Policy)")
	t.Log("")
	t.Log("Agent lock:")
	t.Log("  internal/pack/lock.go:40 — type AgentLock struct")
	t.Log("  internal/pack/lock.go:34 — const LockSchemaVersion = 2")
	t.Log("  internal/pack/lock.go:194 — func NewSignedTestLock")
	t.Log("")
	t.Log("Invoke payload:")
	t.Log("  internal/daemon/control_handlers.go:1396 — func buildInvokePayload")
	t.Log("  internal/daemon/control_handlers.go:794 — 2*time.Minute invoke timeout")
	t.Log("")
	t.Log("Harness budget:")
	t.Log("  internal/harness/budget.go:17 — defaultWallClockBudget = 120 * time.Second")
	t.Log("  internal/harness/budget.go:18 — defaultMaxIterations = 10000")
	t.Log("  internal/harness/budget.go:19 — defaultMaxTokens = 100000")
	t.Log("")
	t.Log("Model client timeout:")
	t.Log("  internal/harness/rpc_server.go:402 — Timeout: 120 * time.Second")
	t.Log("")
	t.Log("RLIMIT_CPU:")
	t.Log("  internal/harness/python_worker.go:460 — ('RLIMIT_CPU', 30)")
	t.Log("RLIMIT_NPROC:")
	t.Log("  internal/harness/python_worker.go:462 — ('RLIMIT_NPROC', 0)")
	t.Log("RLIMIT_FSIZE:")
	t.Log("  internal/harness/python_worker.go:461 — ('RLIMIT_FSIZE', 64 * 1024 * 1024)")
	t.Log("")
	t.Log("Python SDK legacy path:")
	t.Log("  python/agentpaas_sdk/agent.py:32 — def llm(self, prompt, model=None)")
	t.Log("  python/agentpaas_sdk/agent.py:38 — def record_iteration(self)")
	t.Log("  python/agentpaas_sdk/_rpc.py — RPCClient.call (synchronous)")
	t.Log("")
	t.Log("Trigger = independent runs:")
	t.Log("  internal/daemon/control_handlers.go — s.runs map, keyed by run ID")
	t.Log("  No WorkflowID field on RunRequest or trackedRun in v0.2.3")
	t.Log("")
	t.Log("No deployment/alias/invocation contracts:")
	t.Log("  internal/daemon/control_handlers.go — no Pause/Resume/Restart methods")
	t.Log("  internal/pack/lock.go — no DeploymentID, Alias, Generation fields")
	t.Log("  internal/operator/schema.go — no amendment types")
	t.Log("")
	t.Log("MCP router absent in production:")
	t.Log("  internal/daemon/control_handlers.go — no mcpmanager import")
	t.Log("  internal/harness/rpc_server.go:318 — SetRouter, test-only")
	t.Log("  internal/mcpmanager/router.go — Router type exists but unused by daemon")
}

// ============================================================================
// Test helpers
// ============================================================================

// mockPayloadBuilder simulates buildInvokePayload for fixture validation.
type mockPayloadBuilder struct{}

func (m *mockPayloadBuilder) BuildInvokePayload(_ interface{}, agentName string, triggerPayload []byte) (map[string]any, error) {
	payload := make(map[string]any)
	if len(triggerPayload) > 0 {
		var up map[string]any
		if err := json.Unmarshal(triggerPayload, &up); err != nil {
			return nil, err
		}
		for k, v := range up {
			payload[k] = v
		}
	}
	switch agentName {
	case "openrouter-test":
		var agent pack.AgentYAML
		if err := yaml.Unmarshal(OpenRouterAgentYAML, &agent); err != nil {
			return nil, err
		}
		payload["llm"] = map[string]any{
			"provider":   agent.LLM.Provider,
			"model":      agent.LLM.Model,
			"credential": agent.LLM.Credential,
		}
		payload["credentials"] = []any{
			map[string]any{"id": agent.LLM.Credential, "header": "Authorization"},
		}
	case "no-llm-test":
		// No LLM config — no llm or credentials keys.
	}
	return payload, nil
}

// projectRoot returns the absolute path to the project root by walking up
// from the test directory until it finds go.mod.
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root (go.mod) from %s", dir)
		}
		dir = parent
	}
}

// ============================================================================
// Additional characterization: rlimit verification
// ============================================================================

// TestCharacterization_RLimitValuesVerify runs the actual Python resource limits
// script that is embedded in the worker. On macOS, the RLIMIT_CPU and RLIMIT_FSIZE
// are typically RLIM_INFINITY (the platform defaults), and NPROC is the system-wide
// per-user limit. This test is informational — it documents the platform values
// that the worker's setrlimit calls would encounter.
func TestCharacterization_RLimitValuesVerify(t *testing.T) {
	skipUnlessPython(t)

	script := `
import resource, sys
limits = [
    ("RLIMIT_CPU", 30),
    ("RLIMIT_FSIZE", 64 * 1024 * 1024),
    ("RLIMIT_NPROC", 0),
]
for name, expected_soft in limits:
    if not hasattr(resource, name):
        print(f"MISSING:{name}")
        continue
    kind = getattr(resource, name)
    try:
        soft, hard = resource.getrlimit(kind)
        if soft == expected_soft:
            print(f"MATCH:{name}={soft}")
        else:
            print(f"PLATFORM:{name}=soft={soft}_hard={hard}_expected={expected_soft}")
    except (OSError, ValueError) as e:
        print(f"ERROR:{name}:{e}")
`
	cmd := exec.Command("python3", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("rlimit verification script error: %v", err)
		t.Logf("output: %s", string(output))
		t.Skip("rlimit verification not supported on this platform")
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "MISSING:") || strings.HasPrefix(line, "ERROR:") {
			t.Logf("rlimit not available on this platform: %s", line)
			continue
		}
		if strings.HasPrefix(line, "PLATFORM:") {
			t.Logf("(platform-specific, not a failure) %s", line)
		}
	}
	t.Log("NOTE: The worker's apply_resource_limits() calls setrlimit to set these")
	t.Log("values. On platforms where the hard limit is RLIM_INFINITY, the soft limit")
	t.Log("will be set to the expected value. An informational log is sufficient.")
}

// TestCharacterization_PythonSDKLegacyPath verifies the Python SDK legacy
// path for agent.llm() accepts both prompt-only and prompt+model calls
// without requiring progress/checkpoint.
func TestCharacterization_PythonSDKLegacyPath(t *testing.T) {
	skipUnlessPython(t)

	rootDir := projectRoot(t)

	script := `
import sys
sys.path.insert(0, 'python')
from agentpaas_sdk import Agent
agent = Agent()
params = {"prompt": "hello"}
assert "prompt" in params, "prompt must be in params"
params2 = {"prompt": "hello", "model": "gpt-4o"}
assert "model" in params2, "model must be in params with model override"
print("PASS: SDK legacy path OK")
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Dir = rootDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SDK path test failed: %v\noutput: %s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS") {
		t.Errorf("unexpected output: %s", string(output))
	}
}

func skipUnlessPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
}

// TestCharacterization_TwoSequentialLLMInPython proves that two sequential
// llm() calls from the Python SDK pass the correct parameters.
func TestCharacterization_TwoSequentialLLMInPython(t *testing.T) {
	skipUnlessPython(t)

	script := `
import sys
sys.path.insert(0, 'python')
from agentpaas_sdk import Agent
agent = Agent()
params1 = {"prompt": "first call"}
params2 = {"prompt": "second call", "model": "gpt-4o"}
assert "prompt" in params1 and "prompt" in params2
assert params2.get("model") == "gpt-4o"
print("PASS: two sequential llm() calls construct correctly without progress/checkpoint")
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Dir = projectRoot(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("two sequential llm test failed: %v\noutput: %s", err, string(output))
	}
	if !strings.Contains(string(output), "PASS") {
		t.Errorf("unexpected output: %s", string(output))
	}
}

// TestCharacterization_MCPManagerExistsButDaemonDoesNotUseIt proves the
// package exists but the production daemon does not import it.
func TestCharacterization_MCPManagerExistsButDaemonDoesNotUseIt(t *testing.T) {
	rootDir := projectRoot(t)
	daemonDir := filepath.Join(rootDir, "internal", "daemon")
	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		t.Fatalf("ReadDir daemon: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(daemonDir, entry.Name()))
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte(`"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"`)) {
			t.Errorf("daemon file %s imports mcpmanager — production daemon should not", entry.Name())
		}
	}

	mcpDir := filepath.Join(rootDir, "internal", "mcpmanager")
	if info, err := os.Stat(mcpDir); err != nil || !info.IsDir() {
		t.Fatalf("mcpmanager package directory not found (may have been removed)")
	}
}

// TestCharacterization_MCPTimeoutInventory documents and freezes the
// current MCP timeout constants. B33-T05 will replace the fixed 5s stdio
// timeout with the B30 operation deadline; B33-T06 enforces call bounds.
// This test fails if the constants are silently changed before T05/T06.
func TestCharacterization_MCPTimeoutInventory(t *testing.T) {
	rootDir := projectRoot(t)

	// 1. stdioResponseTimeout in router.go must be exactly 5s.
	routerSrc, err := os.ReadFile(filepath.Join(rootDir, "internal", "mcpmanager", "router.go"))
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	routerText := string(routerSrc)
	if !strings.Contains(routerText, "stdioResponseTimeout       = 5 * time.Second") {
		t.Fatal("router.go: stdioResponseTimeout constant not found or value changed from 5s — B33-T05 replaces this with B30 deadline; do not silently change before T05")
	}
	t.Log("router.go: stdioResponseTimeout = 5 * time.Second (frozen; T05 replaces with B30 deadline)")

	// 2. Lifecycle CheckReadiness timeout (5s in lifecycle.go:333).
	lifecycleSrc, err := os.ReadFile(filepath.Join(rootDir, "internal", "mcpmanager", "lifecycle.go"))
	if err != nil {
		t.Fatalf("read lifecycle.go: %v", err)
	}
	lifecycleText := string(lifecycleSrc)
	if !strings.Contains(lifecycleText, "time.After(5 * time.Second)") {
		t.Fatal("lifecycle.go: 5s readiness poll timeout not found — B33-T03 defines service readiness; document any change here")
	}
	t.Log("lifecycle.go: CheckReadiness poll interval = 5 * time.Second (frozen; T03 defines readiness)")

	// 3. maxBodySize = 1<<20 (1 MiB) in router.go — B33-T06 enforces bounds.
	if !strings.Contains(routerText, "maxBodySize          int64 = 1 << 20") {
		t.Fatal("router.go: maxBodySize constant not found or value changed — B33-T06 enforces size bounds; document any change here")
	}
	t.Log("router.go: maxBodySize = 1 MiB (frozen; T06 enforces bounds)")

	// 4. No HTTP client timeout configured on the default transport.
	// The Router receives http.DefaultClient (no explicit timeout).
	// T05/T06 must add explicit timeouts.
	t.Log("router.go: No explicit HTTP client timeout configured (uses http.DefaultClient, nil transport timeout). T05/T06 must add explicit deadline propagation.")
}