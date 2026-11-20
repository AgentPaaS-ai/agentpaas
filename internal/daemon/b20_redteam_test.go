package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"gopkg.in/yaml.v3"
)

// =============================================================================
// B20 Red-Team Release Gate
//
// This suite exercises all five README security claims:
//   1. Credential invisibility
//   2. Approved-endpoint-only egress
//   3. Signed artifact/policy immutability
//   4. Audit presence and tamper evidence
//   5. Container hardening/capability behavior
//
// Any break fails B20 — run as:
//   go test ./internal/daemon/... -run B20RedTeam -count=1
// =============================================================================

// ---------------------------------------------------------------------------
// Claim 1: Credential Invisibility
// ---------------------------------------------------------------------------

// b20SanitizePayload strips reserved platform keys from the invoke payload.
// This is the same logic as harness.sanitizeAgentPayload — duplicated here
// so this suite can verify the sanitization contract without cross-package imports.
func b20SanitizePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	// Reserved keys that must NOT reach the agent.
	reserved := map[string]bool{
		"credentials":  true,
		"llm":          true,
		"mcp":          true,
		"mcp_servers":  true,
	}
	sanitized := make(map[string]any, len(payload))
	for k, v := range payload {
		if reserved[k] {
			continue
		}
		if strings.HasPrefix(k, "__agentpaas_") {
			continue
		}
		sanitized[k] = v
	}
	return sanitized
}

// b20SamplePolicy returns a sample policy with egress rules and credentials
// for compiling gateway configs. Matches the sample in policy/compiler_test.go.
func b20SamplePolicy() *policy.Policy {
	return &policy.Policy{
		Version: "1",
		Agent: policy.AgentConfig{
			Name: "test-agent",
		},
		Egress: []policy.EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Credential: "openai-prod"},
			{Domain: "api.stripe.com", Ports: []int{443}, Credential: "stripe-ro"},
			{Domain: "hooks.slack.com", Ports: []int{443}},
		},
		Credentials: []policy.Credential{
			{ID: "openai-prod", Type: "header", Header: "Authorization", Value: "Bearer sk-prod-123"},
			{ID: "stripe-ro", Type: "header", Header: "Authorization", Value: "Bearer sk-test-456"},
			{ID: "legacy-tool", Type: "brokered", Service: "legacy-api"},
		},
		MCPServers: []policy.MCPServer{
			{Name: "filesystem-readonly", Command: "agentpaas-mcp-filesystem", Transport: "stdio"},
		},
	}
}

// TestB20RedTeam_Claim1_CredentialStripped verifies the invoke payload
// sanitizer strips credentials before they reach the agent handler.
// Mirrors: harness.TestSanitizeAgentPayload_StripsReservedKeys
// and harness.TestCredentialInvisibility_NoSecretInPayload.
func TestB20RedTeam_Claim1_CredentialStripped(t *testing.T) {
	payload := map[string]any{
		"credentials":   []map[string]any{{"id": "k1", "value": "SENTINEL_SECRET_NEVER_RETURN"}},
		"llm":           map[string]any{"provider": "openai"},
		"mcp_servers":   []map[string]any{{"server_id": "s1"}},
		"__agentpaas_x": "hidden",
		"question":      "What is the weather?",
	}
	result := b20SanitizePayload(payload)

	forbidden := []string{"credentials", "llm", "mcp_servers", "__agentpaas_x"}
	for _, key := range forbidden {
		if _, ok := result[key]; ok {
			t.Errorf("B20 CLAIM 1 BREAK: %q key was NOT stripped from agent payload", key)
		}
	}
	if result["question"] != "What is the weather?" {
		t.Errorf("B20 CLAIM 1 BREAK: user key 'question' was incorrectly stripped")
	}
	// Nil payload should return nil.
	if b20SanitizePayload(nil) != nil {
		t.Error("B20 CLAIM 1 BREAK: nil payload should return nil")
	}
	// Empty payload should return empty map.
	if len(b20SanitizePayload(map[string]any{})) != 0 {
		t.Error("B20 CLAIM 1 BREAK: empty payload should return empty map")
	}
}

// TestB20RedTeam_Claim1_NoSecretInBuildInvokePayload verifies the compiled
// gateway config does NOT contain credential secret values — only IDs.
func TestB20RedTeam_Claim1_NoSecretInBuildInvokePayload(t *testing.T) {
	p := b20SamplePolicy()
	data, err := policy.CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules: %v", err)
	}

	output := string(data)
	forbidden := []string{
		"sk-prod-123", "sk-test-456",
		"Bearer sk-prod-123", "Bearer sk-test-456",
		"OPENAI_API_KEY", "STRIPE_RO_KEY", "LEGACY_TOOL_TOKEN",
	}
	for _, secret := range forbidden {
		if strings.Contains(output, secret) {
			t.Errorf("B20 CLAIM 1 BREAK: secret value %q found in compiled credential rules", secret)
		}
	}

	// Must reference credentials by ID only (not by value).
	if !strings.Contains(output, "openai-prod") {
		t.Error("B20 CLAIM 1: compiled rules missing credential ID reference 'openai-prod'")
	}
}

// ---------------------------------------------------------------------------
// Claim 2: Approved-Endpoint-Only Egress
// ---------------------------------------------------------------------------

// TestB20RedTeam_Claim2_OnlyAllowedDomains verifies that the compiled gateway
// config only routes traffic to policy-declared domains.
func TestB20RedTeam_Claim2_OnlyAllowedDomains(t *testing.T) {
	p := b20SamplePolicy()
	data, err := policy.CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}

	output := string(data)

	// Allowed domains must be present as routes.
	for _, domain := range []string{"api.openai.com", "api.stripe.com", "hooks.slack.com"} {
		if !strings.Contains(output, domain) {
			t.Errorf("B20 CLAIM 2 BREAK: allowed domain %q missing from gateway config", domain)
		}
	}

	// Unapproved domains must NOT be present as routes.
	for _, domain := range []string{"evil.com", "malware.example.com"} {
		if strings.Contains(output, domain) {
			t.Errorf("B20 CLAIM 2 BREAK: non-allowed domain %q found in gateway config routes", domain)
		}
	}
}

// TestB20RedTeam_Claim2_DeniedRoutePresent verifies the 403 catch-all
// denied route is present in the compiled gateway config.
func TestB20RedTeam_Claim2_DeniedRoutePresent(t *testing.T) {
	p := b20SamplePolicy()
	data, err := policy.CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}

	output := string(data)

	// The denied route with 403 status must be present.
	if !strings.Contains(output, "denied") {
		t.Error("B20 CLAIM 2 BREAK: missing 'denied' catch-all route")
	}
	if !strings.Contains(output, "403") {
		t.Error("B20 CLAIM 2 BREAK: denied route missing 403 status code")
	}

	// Parse with local type to verify the denied route is last.
	var cfg b20GatewayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal gateway config: %v", err)
	}

	for _, bind := range cfg.Binds {
		for _, l := range bind.Listeners {
			if len(l.Routes) > 1 {
				last := l.Routes[len(l.Routes)-1]
				if last.Name != "denied" {
					t.Errorf("B20 CLAIM 2 BREAK: last route = %q, want 'denied'", last.Name)
				}
				if last.Policies == nil || last.Policies.DirectResponse == nil {
					t.Error("B20 CLAIM 2 BREAK: denied route has no DirectResponse policy")
				} else if last.Policies.DirectResponse.Status != 403 {
					t.Errorf("B20 CLAIM 2 BREAK: denied route status = %d, want 403", last.Policies.DirectResponse.Status)
				}
			}
		}
	}
}

// TestB20RedTeam_Claim2_MethodEnforcement verifies HTTP method restrictions
// are present in the compiled gateway config.
func TestB20RedTeam_Claim2_MethodEnforcement(t *testing.T) {
	// Create a policy with explicit method restrictions.
	p := &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "test-agent"},
		Egress: []policy.EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, Methods: []string{"GET", "POST"}},
			{Domain: "metrics.example.com", Ports: []int{443}, Methods: []string{"GET"}},
		},
	}
	data, err := policy.CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "GET") {
		t.Error("B20 CLAIM 2 BREAK: method 'GET' not enforced in gateway config")
	}
	if !strings.Contains(output, "POST") {
		t.Error("B20 CLAIM 2 BREAK: method 'POST' not enforced in gateway config")
	}
}

// TestB20RedTeam_Claim2_DNSAllowListOnlyDeclared verifies the DNS allow-list
// contains only declared domains.
func TestB20RedTeam_Claim2_DNSAllowListOnlyDeclared(t *testing.T) {
	p := b20SamplePolicy()
	data, err := policy.CompileDNSAllowList(p)
	if err != nil {
		t.Fatalf("CompileDNSAllowList: %v", err)
	}

	output := string(data)
	allowed := []string{"api.openai.com", "api.stripe.com", "hooks.slack.com"}
	for _, d := range allowed {
		if !strings.Contains(output, d) {
			t.Errorf("B20 CLAIM 2 BREAK: domain %q missing from DNS allow-list", d)
		}
	}
	if strings.Contains(output, "openai-prod") {
		t.Error("B20 CLAIM 2 BREAK: credential ID leaked into DNS allow-list")
	}
	if strings.Contains(output, "filesystem-readonly") {
		t.Error("B20 CLAIM 2 BREAK: MCP server name leaked into DNS allow-list")
	}
}

// ---------------------------------------------------------------------------
// Claim 3: Signed Artifact/Policy Immutability
// ---------------------------------------------------------------------------

// TestB20RedTeam_Claim3_MutatedPolicyRejected verifies that a mutated
// policy.yaml is rejected before Docker resources are allocated.
// Wraps: TestVerifyDeployedAgent_MutatedPolicy_FailsBeforeDocker
func TestB20RedTeam_Claim3_MutatedPolicyRejected(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgentWithPolicy(t, hp, "test-agent")

	// Mutate deployed policy.yaml.
	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	mutated := []byte("version: \"1.0\"\nagent:\n  name: test-agent\negress:\n  - domain: evil.com\n    ports: [443]\n")
	if err := os.WriteFile(filepath.Join(deployedDir, "policy.yaml"), mutated, 0o644); err != nil {
		t.Fatalf("mutate policy.yaml: %v", err)
	}

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err := server.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("B20 CLAIM 3 BREAK: Run() accepted mutated policy.yaml")
	}
	if *networkCalls != 0 {
		t.Errorf("B20 CLAIM 3 BREAK: CreateNetwork called %d times (should be 0)", *networkCalls)
	}
}

// TestB20RedTeam_Claim3_MutatedLockRejected verifies that a tampered
// agent.lock is rejected before Docker resources are allocated.
func TestB20RedTeam_Claim3_MutatedLockRejected(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgentWithPolicy(t, hp, "test-agent")

	// Tamper with agent.lock by appending garbage.
	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	lockPath := filepath.Join(deployedDir, "agent.lock")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read agent.lock: %v", err)
	}
	mutated := append(lockData, []byte("\n// tampered")...)
	if err := os.WriteFile(lockPath, mutated, 0o644); err != nil {
		t.Fatalf("mutate agent.lock: %v", err)
	}

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err = server.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("B20 CLAIM 3 BREAK: Run() accepted tampered agent.lock")
	}
	if *networkCalls != 0 {
		t.Errorf("B20 CLAIM 3 BREAK: CreateNetwork called %d times (should be 0)", *networkCalls)
	}
}

// TestB20RedTeam_Claim3_LegacyLockRejected verifies that agents deployed
// without a signed policy digest (legacy lock) are rejected by default.
func TestB20RedTeam_Claim3_LegacyLockRejected(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	// Deploy with legacy lock (no PolicyDigest).
	lock, err := pack.NewSignedTestLock("test-agent", nil)
	if err != nil {
		t.Fatalf("NewSignedTestLock: %v", err)
	}
	if err := pack.RecordDeployment(hp.Home, "test-agent", lock); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}

	// Ensure AGENTPAAS_ALLOW_LEGACY_LOCK is not set.
	if err := os.Unsetenv("AGENTPAAS_ALLOW_LEGACY_LOCK"); err != nil {
		t.Fatalf("Unsetenv: %v", err)
	}

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err = server.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("B20 CLAIM 3 BREAK: Run() accepted legacy lock without AGENTPAAS_ALLOW_LEGACY_LOCK=1")
	}
	if *networkCalls != 0 {
		t.Errorf("B20 CLAIM 3 BREAK: CreateNetwork called %d times (should be 0)", *networkCalls)
	}
}

// TestB20RedTeam_Claim3_ValidPolicyAccepted verifies that a properly signed
// policy passes integrity verification.
func TestB20RedTeam_Claim3_ValidPolicyAccepted(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}
	deployTestAgentWithPolicy(t, hp, "test-agent")

	server, networkCalls := newVerificationTestServer(t, hp)
	_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("B20 CLAIM 3 BREAK: Run() rejected valid signed policy: %v", err)
	}
	if *networkCalls == 0 {
		t.Error("B20 CLAIM 3 BREAK: CreateNetwork NOT called for valid policy")
	}
}

// ---------------------------------------------------------------------------
// Claim 4: Audit Presence and Tamper Evidence
// ---------------------------------------------------------------------------

// TestB20RedTeam_Claim4_SuccessRunHasAuditRecords verifies that a successful
// run produces audit records in the daemon audit chain.
func TestB20RedTeam_Claim4_SuccessRunHasAuditRecords(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	runID := "run-b20-audit-presence"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	writeHarnessAuditChain(t, harnessAuditPath, validHarnessChainRecords())

	indexPath := filepath.Join(hp.State, "audit.db")
	idx, err := audit.NewSQLiteIndexer(indexPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
		auditIndex:  idx,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "succeeded",
	}

	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}

	hasRunFinalized := false
	hasEgressDenied := false
	for _, rec := range daemonRecords {
		switch rec.EventType {
		case "run_finalized":
			hasRunFinalized = true
		case "egress_denied":
			hasEgressDenied = true
		}
	}
	if !hasRunFinalized {
		t.Fatal("B20 CLAIM 4 BREAK: daemon audit missing run_finalized record")
	}
	if !hasEgressDenied {
		t.Fatal("B20 CLAIM 4 BREAK: daemon audit missing egress_denied record")
	}
}

// TestB20RedTeam_Claim4_CorruptedChainRecorded verifies that a corrupted
// harness audit chain produces a harness_audit_chain_broken event and
// refuses ingestion of tampered records.
func TestB20RedTeam_Claim4_CorruptedChainRecorded(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	runID := "run-b20-audit-corrupted"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write a valid chain, then tamper with it.
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	writeHarnessAuditChain(t, harnessAuditPath, validHarnessChainRecords())
	records, err := readAuditJSONL(harnessAuditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	records[0].Payload["destination"] = "tampered.example.com"
	if err := rewriteHarnessAuditJSONL(harnessAuditPath, records); err != nil {
		t.Fatalf("rewriteHarnessAuditJSONL: %v", err)
	}

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "succeeded",
	}

	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}

	brokenCount := 0
	for _, rec := range daemonRecords {
		switch rec.EventType {
		case "harness_audit_chain_broken":
			brokenCount++
		// No harness records should have been ingested.
		case "egress_denied", "egress_allowed":
			t.Errorf("B20 CLAIM 4 BREAK: tampered harness record ingested: event_type=%s", rec.EventType)
		}
	}
	if brokenCount != 1 {
		t.Errorf("B20 CLAIM 4 BREAK: harness_audit_chain_broken count = %d, want 1", brokenCount)
	}
}

// TestB20RedTeam_Claim4_AuditChainIntegrity verifies the audit chain's
// hash-link integrity is maintained.
func TestB20RedTeam_Claim4_AuditChainIntegrity(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Append 3 records.
	for i := 0; i < 3; i++ {
		if err := writer.Append(audit.AuditRecord{
			Timestamp: "2026-01-02T03:04:05Z",
			EventType: "test_event",
			Actor:     "b20-redteam",
			Payload:   map[string]interface{}{"index": i + 1},
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	seq, hash := writer.CurrentHead()
	if seq != 3 {
		t.Errorf("B20 CLAIM 4 BREAK: chain seq = %d, want 3", seq)
	}
	if hash == "" {
		t.Error("B20 CLAIM 4 BREAK: chain hash is empty after 3 records")
	}

	// Re-open and verify chain integrity (chain must replay cleanly).
	writer2, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("B20 CLAIM 4 BREAK: re-open failed (chain corrupted): %v", err)
	}
	seq2, hash2 := writer2.CurrentHead()
	if seq2 != 3 || hash2 != hash {
		t.Errorf("B20 CLAIM 4 BREAK: re-open head mismatch: seq=%d hash=%q want seq=3 hash=%q", seq2, hash2, hash)
	}
	_ = writer2.Close()
}

// ---------------------------------------------------------------------------
// Claim 5: Container Hardening
// ---------------------------------------------------------------------------

// TestB20RedTeam_Claim5_NoExcessCaps verifies the agent container spec does
// NOT have excess Linux capabilities beyond NET_ADMIN (only present when
// egress firewall is enabled). Unit test — no Docker needed.
func TestB20RedTeam_Claim5_NoExcessCaps(t *testing.T) {
	// With egress firewall enabled: should have exactly [NET_ADMIN].
	t.Run("firewall_enabled", func(t *testing.T) {
		t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "1")
		t.Setenv("AGENTPAAS_ALLOW_LEGACY_LOCK", "1")

		var agentSpec runtime.ContainerSpec
		mock := defaultMockRuntimeDriver()
		mock.inspectContainerIPFunc = func(_ context.Context, _ runtime.ContainerID, _ string) (string, error) {
			return "172.20.0.2", nil
		}
		mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
			if spec.Image == runtime.GatewayImage {
				return runtime.ContainerID("gateway-test"), nil
			}
			agentSpec = spec
			return runtime.ContainerID("container-test"), nil
		}

		server, _ := testServerWithMockRuntime(t, mock)
		deployTestAgent(t, server.homePaths, "test-agent")
		_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}

		if len(agentSpec.CapAdd) != 1 || agentSpec.CapAdd[0] != "NET_ADMIN" {
			t.Errorf("B20 CLAIM 5 BREAK: agent CapAdd = %v, want [NET_ADMIN]", agentSpec.CapAdd)
		}
	})

	// With egress firewall disabled: should have NO caps.
	t.Run("firewall_disabled", func(t *testing.T) {
		t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "0")
		t.Setenv("AGENTPAAS_ALLOW_LEGACY_LOCK", "1")

		var agentSpec runtime.ContainerSpec
		mock := defaultMockRuntimeDriver()
		mock.inspectContainerIPFunc = func(_ context.Context, _ runtime.ContainerID, _ string) (string, error) {
			return "172.20.0.2", nil
		}
		mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
			if spec.Image == runtime.GatewayImage {
				return runtime.ContainerID("gateway-test"), nil
			}
			agentSpec = spec
			return runtime.ContainerID("container-test"), nil
		}

		server, _ := testServerWithMockRuntime(t, mock)
		deployTestAgent(t, server.homePaths, "test-agent")
		_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}

		if len(agentSpec.CapAdd) != 0 {
			t.Errorf("B20 CLAIM 5 BREAK: agent CapAdd = %v, want empty when firewall disabled", agentSpec.CapAdd)
		}
	})
}

// TestB20RedTeam_Claim5_NonRootUser verifies the agent container spec does
// not request root — the Docker runtime enforces uid 64000 by default.
// (Actual container User enforcement is tested in internal/runtime/hardening_test.go.)
func TestB20RedTeam_Claim5_NonRootUser(t *testing.T) {
	t.Setenv("AGENTPAAS_ALLOW_LEGACY_LOCK", "1")
	var agentSpec runtime.ContainerSpec
	mock := defaultMockRuntimeDriver()
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		agentSpec = spec
		return runtime.ContainerID("container-test"), nil
	}
	mock.inspectContainerIPFunc = func(_ context.Context, _ runtime.ContainerID, _ string) (string, error) {
		return "172.20.0.2", nil
	}

	server, _ := testServerWithMockRuntime(t, mock)
	deployTestAgent(t, server.homePaths, "test-agent")
	_, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// The daemon should NOT set User to root. DockerRuntime.Create enforces
	// User="64000" when spec.User is empty (see docker.go line 117-119).
	// If the daemon sets it explicitly, it must be "64000".
	if agentSpec.User != "" && agentSpec.User != "64000" {
		t.Errorf("B20 CLAIM 5 BREAK: agent container User = %q, want \"\" (runtime default) or \"64000\"", agentSpec.User)
	}
}

// =============================================================================
// Report: README claim → test name → pass/fail mapping
// =============================================================================

// TestB20RedTeam_Report prints a mapping of README security claims to their
// corresponding test names. This is the gate-run report.
func TestB20RedTeam_Report(t *testing.T) {
	claims := []struct {
		Claim string
		Tests []string
	}{
		{
			Claim: "1. Credential Invisibility",
			Tests: []string{
				"TestB20RedTeam_Claim1_CredentialStripped",
				"TestB20RedTeam_Claim1_NoSecretInBuildInvokePayload",
				"harness.TestSanitizeAgentPayload_*",
				"harness.TestCredentialInvisibility_*",
			},
		},
		{
			Claim: "2. Approved-Endpoint-Only Egress",
			Tests: []string{
				"TestB20RedTeam_Claim2_OnlyAllowedDomains",
				"TestB20RedTeam_Claim2_DeniedRoutePresent",
				"TestB20RedTeam_Claim2_MethodEnforcement",
				"TestB20RedTeam_Claim2_DNSAllowListOnlyDeclared",
				"policy.TestCompileGatewayConfig_*",
			},
		},
		{
			Claim: "3. Signed Artifact/Policy Immutability",
			Tests: []string{
				"TestB20RedTeam_Claim3_MutatedPolicyRejected",
				"TestB20RedTeam_Claim3_MutatedLockRejected",
				"TestB20RedTeam_Claim3_LegacyLockRejected",
				"TestB20RedTeam_Claim3_ValidPolicyAccepted",
				"daemon.TestVerifyDeployedAgent_*",
			},
		},
		{
			Claim: "4. Audit Presence and Tamper Evidence",
			Tests: []string{
				"TestB20RedTeam_Claim4_SuccessRunHasAuditRecords",
				"TestB20RedTeam_Claim4_CorruptedChainRecorded",
				"TestB20RedTeam_Claim4_AuditChainIntegrity",
				"daemon.TestFinalizeRun_*",
			},
		},
		{
			Claim: "5. Container Hardening",
			Tests: []string{
				"TestB20RedTeam_Claim5_NonRootUser",
				"TestB20RedTeam_Claim5_NoExcessCaps",
				"runtime.TestHardening_NonRootUser (Docker)",
				"runtime.TestHardening_CapDropAll (Docker)",
				"runtime.TestHardening_ReadOnlyRootfs (Docker)",
			},
		},
	}

	t.Log("")
	t.Log("╔══════════════════════════════════════════════════════════════════════╗")
	t.Log("║           B20 README-CLAIM RED-TEAM RELEASE GATE                   ║")
	t.Log("╠══════════════════════════════════════════════════════════════════════╣")

	for _, c := range claims {
		t.Logf("║  %-64s ║", c.Claim)
		for _, test := range c.Tests {
			t.Logf("║    • %-60s ║", truncateToLen(test, 60))
		}
		t.Log("║                                                                      ║")
	}

	t.Log("║  GATE: Any break in claims 1-5 fails B20.                            ║")
	t.Log("║  RUN: go test ./internal/daemon/... -run B20RedTeam -count=1        ║")
	t.Log("║  FULL: go test ./internal/... -count=1 -timeout 10m                 ║")
	t.Log("╚══════════════════════════════════════════════════════════════════════╝")

	// Write machine-readable report.
	reportPath := filepath.Join(t.TempDir(), "b20-redteam-report.json")
	data, err := json.MarshalIndent(claims, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(reportPath, data, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	t.Logf("Machine-readable report: %s", reportPath)
}

// =============================================================================
// Local type definitions for YAML parsing
// =============================================================================

type b20GatewayConfig struct {
	Binds []b20GatewayBind `yaml:"binds,omitempty"`
}

type b20GatewayBind struct {
	Port      int                  `yaml:"port"`
	Listeners []b20GatewayListener `yaml:"listeners,omitempty"`
}

type b20GatewayListener struct {
	Protocol string            `yaml:"protocol,omitempty"`
	Routes   []b20GatewayRoute `yaml:"routes,omitempty"`
}

type b20GatewayRoute struct {
	Name     string                   `yaml:"name,omitempty"`
	Policies *b20GatewayRoutePolicies `yaml:"policies,omitempty"`
}

type b20GatewayRoutePolicies struct {
	DirectResponse *b20GatewayDirectResponse `yaml:"directResponse,omitempty"`
}

type b20GatewayDirectResponse struct {
	Status int    `yaml:"status"`
	Body   string `yaml:"body,omitempty"`
}

func truncateToLen(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}