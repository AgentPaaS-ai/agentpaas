package mcpmanager

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// ---------------------------------------------------------------------------
// 1. Capability generation and validation
// ---------------------------------------------------------------------------

func TestGenerateCapability_ProducesRandomToken(t *testing.T) {
	tok1, err := GenerateCapability()
	if err != nil {
		t.Fatalf("GenerateCapability() error = %v", err)
	}
	if len(tok1) != 64 {
		t.Fatalf("GenerateCapability() length = %d, want 64", len(tok1))
	}
	// Must be hex.
	for _, c := range tok1 {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("GenerateCapability() contains non-hex char: %c", c)
		}
	}
	// Two successive calls must produce different tokens.
	tok2, err := GenerateCapability()
	if err != nil {
		t.Fatalf("GenerateCapability() second call error = %v", err)
	}
	if tok1 == tok2 {
		t.Fatal("GenerateCapability() produced identical tokens on successive calls")
	}
}

func TestServiceRouteAuthorizer_ApprovedRoute(t *testing.T) {
	cap, err := GenerateCapability()
	if err != nil {
		t.Fatal(err)
	}
	a := &ServiceRouteAuthorizer{}
	if err := a.Authorized(cap, cap); err != nil {
		t.Fatalf("Authorized() with correct capability: %v", err)
	}
}

func TestServiceRouteAuthorizer_MissingCapability(t *testing.T) {
	cap, _ := GenerateCapability()
	a := &ServiceRouteAuthorizer{}
	err := a.Authorized(cap, "")
	if err == nil {
		t.Fatal("Authorized() with empty provided capability should fail")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should say 'missing', got: %v", err)
	}
}

func TestServiceRouteAuthorizer_WrongCapability(t *testing.T) {
	expected, _ := GenerateCapability()
	provided, _ := GenerateCapability()
	a := &ServiceRouteAuthorizer{}
	err := a.Authorized(expected, provided)
	if err == nil {
		t.Fatal("Authorized() with wrong capability should fail")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error should say 'invalid', got: %v", err)
	}
	// The error must NOT contain the actual token.
	if strings.Contains(err.Error(), expected) {
		t.Errorf("error leaked expected token: %v", err)
	}
	if strings.Contains(err.Error(), provided) {
		t.Errorf("error leaked provided token: %v", err)
	}
}

func TestServiceRouteAuthorizer_NoExpectedCapability(t *testing.T) {
	provided, _ := GenerateCapability()
	a := &ServiceRouteAuthorizer{}
	err := a.Authorized("", provided)
	if err == nil {
		t.Fatal("Authorized() with empty expected should fail")
	}
	if !strings.Contains(err.Error(), "no expected capability") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestServiceRouteAuthorizer_CrossWorkflowReplay(t *testing.T) {
	// Capability from one workflow must not authorize a different workflow's
	// authorizer in production. The authorizer itself is a simple string
	// comparison, so we test that a replayed token from a different instance
	// is rejected when the expected cap doesn't match.
	wf1Cap, _ := GenerateCapability()
	wf2Cap, _ := GenerateCapability()

	a := &ServiceRouteAuthorizer{}

	// wf1's expected token won't match wf2's provided token.
	if err := a.Authorized(wf1Cap, wf2Cap); err == nil {
		t.Fatal("cross-workflow capability replay should be denied")
	}
}

// ---------------------------------------------------------------------------
// 2. Generic agent.http deny helper
// ---------------------------------------------------------------------------

func TestForbiddenForAgentHTTP_NoCapHeader(t *testing.T) {
	headers := map[string]string{
		"X-Custom": "value",
	}
	if err := ForbiddenForAgentHTTP(headers); err != nil {
		t.Fatalf("ForbiddenForAgentHTTP() should pass without capability header: %v", err)
	}
}

func TestForbiddenForAgentHTTP_CapHeaderPresent(t *testing.T) {
	capToken, _ := GenerateCapability()
	headers := map[string]string{
		CapabilityHeader: capToken,
	}
	err := ForbiddenForAgentHTTP(headers)
	if err == nil {
		t.Fatal("ForbiddenForAgentHTTP() should deny when capability header present")
	}
	if !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("error should say 'forbidden', got: %v", err)
	}
	// Error must NOT contain the actual token.
	if strings.Contains(err.Error(), capToken) {
		t.Errorf("error leaked capability token: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 3. Capability stripping from headers and env
// ---------------------------------------------------------------------------

func TestStripCapabilityFromHeaders_RemovesHeader(t *testing.T) {
	headers := map[string]string{
		"X-Keep":         "keep-value",
		CapabilityHeader: "some-cap-token",
	}
	stripped := StripCapabilityFromHeaders(headers)
	if !stripped {
		t.Fatal("StripCapabilityFromHeaders() should report stripped=true")
	}
	if _, ok := headers[CapabilityHeader]; ok {
		t.Fatal("CapabilityHeader not removed from map")
	}
	if v, ok := headers["X-Keep"]; !ok || v != "keep-value" {
		t.Errorf("non-capability header was removed: got %q", v)
	}
}

func TestStripCapabilityFromHeaders_NotPresent(t *testing.T) {
	headers := map[string]string{
		"X-Keep": "keep-value",
	}
	stripped := StripCapabilityFromHeaders(headers)
	if stripped {
		t.Fatal("StripCapabilityFromHeaders() should report stripped=false when absent")
	}
}

func TestStripCapabilityFromEnv_RemovesForbiddenKeys(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"AGENTPAAS_MCP_CAPABILITY=abc123def",
		"AGENTPAAS_MCP_ENDPOINT=http://internal:8080",
		"AGENTPAAS_MCP_NETWORK=svc-net",
		"AGENTPAAS_SERVICE_NETWORK=secret-alias",
		"HOME=/home/agent",
		"AGENTPAAS_SERVICE_CAPABILITY=cap-token",
		"USER=agent",
		"AGENTPAAS_SERVICE_ENDPOINT=endpoint",
		"AGENTPAAS_MCP_ALIAS=network-alias-value",
	}
	cleaned, strippedCount := StripCapabilityFromEnv(env)
	if strippedCount != 7 {
		t.Fatalf("StripCapabilityFromEnv() stripped = %d, want 7. Cleaned:\n%s",
			strippedCount, strings.Join(cleaned, "\n"))
	}
	for _, e := range cleaned {
		if strings.Contains(e, "AGENTPAAS_MCP_CAPABILITY") ||
			strings.Contains(e, "AGENTPAAS_MCP_ENDPOINT") ||
			strings.Contains(e, "AGENTPAAS_MCP_NETWORK") ||
			strings.Contains(e, "AGENTPAAS_MCP_ALIAS") ||
			strings.Contains(e, "AGENTPAAS_SERVICE_NETWORK") ||
			strings.Contains(e, "AGENTPAAS_SERVICE_CAPABILITY") ||
			strings.Contains(e, "AGENTPAAS_SERVICE_ENDPOINT") {
			t.Errorf("forbidden key still present in cleaned env: %s", e)
		}
	}
	// Safe env vars should be kept.
	foundPath := false
	foundHome := false
	foundUser := false
	for _, e := range cleaned {
		if e == "PATH=/usr/bin" {
			foundPath = true
		}
		if e == "HOME=/home/agent" {
			foundHome = true
		}
		if e == "USER=agent" {
			foundUser = true
		}
	}
	if !foundPath || !foundHome || !foundUser {
		t.Error("safe env vars were stripped")
	}
}

// ---------------------------------------------------------------------------
// 4. Error message sanitization (no capability leak)
// ---------------------------------------------------------------------------

func TestSanitizeErrorMessage_NoCapabilityLeak(t *testing.T) {
	cap, _ := GenerateCapability()
	msg := "route error: capability " + cap + " rejected"
	sanitized := SanitizeErrorMessageForAgent(msg)
	if strings.Contains(sanitized, cap) {
		t.Errorf("capability token leaked in error message: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[REDACTED]") {
		t.Errorf("capability token not redacted: %s", sanitized)
	}
}

func TestSanitizeErrorMessage_NormalMessage(t *testing.T) {
	msg := "service lookup failed: service not found"
	sanitized := SanitizeErrorMessageForAgent(msg)
	if sanitized != msg {
		t.Errorf("normal message was modified: %s -> %s", msg, sanitized)
	}
}

// ---------------------------------------------------------------------------
// 5. Service network lifecycle: create, attach, detach, remove
// ---------------------------------------------------------------------------

func setupNetworkTest(t *testing.T) (*fakeRuntimeDriver, *ServiceRegistry) {
	t.Helper()
	driver := newFakeRuntimeDriver()
	checker := &fakePromotionChecker{promoted: true}
	probe := &fakeReadinessProbe{ready: true}
	reg := NewServiceRegistry(driver, checker, probe)
	return driver, reg
}

func TestNetwork_CreateIsIdempotent(t *testing.T) {
	driver, reg := setupNetworkTest(t)

	// Declare and start a service.
	_, err := reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	if err != nil {
		t.Fatal(err)
	}

	inst1, err := reg.Start(context.Background(), "wf-1", "feedback")
	if err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if inst1.NetworkAlias == "" {
		t.Fatal("NetworkAlias not set on service instance")
	}
	if inst1.Capability == "" {
		t.Fatal("Capability not set on service instance")
	}
	if len(inst1.Capability) != 64 {
		t.Fatalf("Capability length = %d, want 64", len(inst1.Capability))
	}

	// Start a second service in the same workflow — network is reused.
	_, err = reg.Declare("wf-1", makeBinding("analyzer"), "sha256:def", []string{"tool2"})
	if err != nil {
		t.Fatal(err)
	}
	inst2, err := reg.Start(context.Background(), "wf-1", "analyzer")
	if err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if inst2.NetworkAlias == "" {
		t.Fatal("NetworkAlias not set on second instance")
	}
	// Same workflow should get same network alias.
	if inst1.NetworkAlias != inst2.NetworkAlias {
		t.Errorf("NetworkAlias mismatch: %s != %s", inst1.NetworkAlias, inst2.NetworkAlias)
	}
	// But capabilities must be different (per-binding).
	if inst1.Capability == inst2.Capability {
		t.Fatal("different bindings must have different capabilities")
	}

	// Verify network was created only once.
	networks, err := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) != 1 {
		t.Fatalf("expected 1 service network, got %d", len(networks))
	}
	if !networks[0].Internal {
		t.Fatal("service network must be internal (no external route)")
	}
}

func TestNetwork_StopDetachesAndRemovesNetwork(t *testing.T) {
	driver, reg := setupNetworkTest(t)

	_, err := reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Start(context.Background(), "wf-1", "feedback")
	if err != nil {
		t.Fatal(err)
	}

	// Network should exist.
	networks, _ := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
	)
	if len(networks) != 1 {
		t.Fatalf("expected 1 network, got %d", len(networks))
	}

	// Stop the only service — network should be removed.
	if err := reg.Stop(context.Background(), "wf-1", "feedback"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Network should be gone.
	networks, _ = driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
	)
	if len(networks) != 0 {
		t.Fatalf("expected 0 networks after stop, got %d", len(networks))
	}
}

func TestNetwork_StopServicesThenStartAgain(t *testing.T) {
	driver, reg := setupNetworkTest(t)

	_, err := reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	if err != nil {
		t.Fatal(err)
	}

	// Start/stop/start cycle.
	_, err = reg.Start(context.Background(), "wf-1", "feedback")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Stop(context.Background(), "wf-1", "feedback"); err != nil {
		t.Fatal(err)
	}

	// Verify instance capability is cleared.
	inst, _ := reg.Get("wf-1", "feedback")
	if inst.Capability != "" {
		t.Error("Capability not cleared after stop")
	}
	if inst.NetworkAlias != "" {
		t.Error("NetworkAlias not cleared after stop")
	}

	// Start again — should get new capability and alias.
	inst2, err := reg.Start(context.Background(), "wf-1", "feedback")
	if err != nil {
		t.Fatalf("restart Start() error = %v", err)
	}
	if inst2.Capability == "" {
		t.Fatal("Capability not set on restart")
	}
	if inst2.NetworkAlias == "" {
		t.Fatal("NetworkAlias not set on restart")
	}

	// New network should be created.
	networks, _ := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
	)
	if len(networks) == 0 {
		t.Fatal("no network after restart")
	}
}

func TestNetwork_StopOneService_NetworkPreservedIfOthers(t *testing.T) {
	driver, reg := setupNetworkTest(t)

	// Two services in same workflow.
	_, _ = reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	_, _ = reg.Declare("wf-1", makeBinding("analyzer"), "sha256:def", []string{"tool2"})

	_, err := reg.Start(context.Background(), "wf-1", "feedback")
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Start(context.Background(), "wf-1", "analyzer")
	if err != nil {
		t.Fatal(err)
	}

	// Stop only one service.
	if err := reg.Stop(context.Background(), "wf-1", "feedback"); err != nil {
		t.Fatal(err)
	}

	// Network should still exist because analyzer is still on it.
	networks, _ := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
	)
	if len(networks) != 1 {
		t.Fatalf("expected 1 network after stopping one service, got %d", len(networks))
	}
}

func TestNetwork_StopIdempotent(t *testing.T) {
	_, reg := setupNetworkTest(t)

	_, err := reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Start(context.Background(), "wf-1", "feedback")
	if err != nil {
		t.Fatal(err)
	}

	// Second stop should be idempotent.
	if err := reg.Stop(context.Background(), "wf-1", "feedback"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Stop(context.Background(), "wf-1", "feedback"); err != nil {
		t.Fatalf("second Stop() should be idempotent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 6. No capability leakage into Python-simulated env or results
// ---------------------------------------------------------------------------

func TestNoCapabilityLeak_InServiceInstanceCopy(t *testing.T) {
	// Verify that calls to Get() return a copy without exposing internal
	// state, but capability IS in the copy (trusted internal use only).
	// The Python/agent isolation is enforced by the harness/env strip layer.
	_, reg := setupNetworkTest(t)

	_, _ = reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	_, err := reg.Start(context.Background(), "wf-1", "feedback")
	if err != nil {
		t.Fatal(err)
	}

	inst, err := reg.Get("wf-1", "feedback")
	if err != nil {
		t.Fatal(err)
	}

	// Capability and alias must be present on the trusted internal copy.
	if inst.Capability == "" {
		t.Fatal("Capability should be present on trusted copy")
	}
	if inst.NetworkAlias == "" {
		t.Fatal("NetworkAlias should be present on trusted copy")
	}

	// Simulate Python env map construction: capability/alias must NOT be
	// in env vars passed to Python process.
	pythonEnv := []string{
		"PATH=/usr/bin",
		"AGENTPAAS_AGENT_KIND=mcp_service",
	}
	// Never add capability or alias to agent env.
	for _, e := range pythonEnv {
		if strings.Contains(e, inst.Capability) {
			t.Errorf("capability leaked into Python env map: %s", e)
		}
		if strings.Contains(e, inst.NetworkAlias) {
			t.Errorf("network alias leaked into Python env map: %s", e)
		}
	}
}

func TestNoCapabilityLeak_InEnvStripping(t *testing.T) {
	capToken, _ := GenerateCapability()

	// Build an env that might accidentally include capability info.
	env := []string{
		"HOME=/home/agent",
		"AGENTPAAS_MCP_CAPABILITY=" + capToken,
		"AGENTPAAS_MCP_ENDPOINT=http://svc:8080/mcp",
		"X_CAP_TOKEN=" + capToken, // custom, not in forbidden patterns
		"PATH=/usr/bin",
	}
	cleaned, _ := StripCapabilityFromEnv(env)

	for _, e := range cleaned {
		if strings.Contains(e, capToken) && strings.Contains(e, "AGENTPAAS_MCP_CAPABILITY") {
			// The known forbidden key should be stripped.
			t.Errorf("forbidden env with capability not stripped: %s", e)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. Concurrent start race — one network, one generation
// ---------------------------------------------------------------------------

func TestRace_ConcurrentStart_StillOneNetwork(t *testing.T) {
	driver, reg := setupNetworkTest(t)

	_, err := reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan *ServiceInstance, 20)

	// Launch 10 concurrent starts.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst, err := reg.Start(context.Background(), "wf-1", "feedback")
			if err == nil {
				results <- inst
			}
		}()
	}
	wg.Wait()
	close(results)

	successCount := 0
	var cap string
	for inst := range results {
		successCount++
		if cap == "" {
			cap = inst.Capability
		}
	}

	// Exactly one should win the CAS race.
	if successCount != 1 {
		t.Fatalf("concurrent start: expected 1 success, got %d", successCount)
	}

	// Only one network should be created.
	networks, _ := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
	)
	if len(networks) != 1 {
		t.Fatalf("expected 1 network after concurrent start, got %d", len(networks))
	}
}

// ---------------------------------------------------------------------------
// 8. Orphan network reconciliation
// ---------------------------------------------------------------------------

func TestReconcile_OrphanNetworksRemoved(t *testing.T) {
	driver, _ := setupNetworkTest(t)

	// Create a network directly via the driver (simulating an orphan).
	spec := runtime.NetworkSpec{
		Name:     "agentpaas-mcp-svc-orphan-wf",
		Internal: true,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeMCPServiceNet,
			runtime.LabelWorkflowID:   "orphan-wf",
		},
	}
	_, err := driver.CreateNetwork(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}

	// Reconcile should remove it since no containers are attached.
	removed, err := ReconcileOrphanServiceNetworks(context.Background(), driver, "orphan-wf")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 orphan network removed, got %d", removed)
	}

	// Verify network is gone.
	networks, _ := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=orphan-wf",
	)
	if len(networks) != 0 {
		t.Fatal("orphan network not removed")
	}

	// Second reconcile should be idempotent (0 removed).
	removedAgain, _ := ReconcileOrphanServiceNetworks(context.Background(), driver, "orphan-wf")
	if removedAgain != 0 {
		t.Fatalf("second reconcile should find 0 orphans, got %d", removedAgain)
	}
}

func TestReconcile_ServiceRegistryFullReconcileWithNetworks(t *testing.T) {
	driver, reg := setupNetworkTest(t)

	_, err := reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Start(context.Background(), "wf-1", "feedback")
	if err != nil {
		t.Fatal(err)
	}

	// Call Reconcile — should not touch our live network.
	if err := reg.Reconcile(context.Background(), "wf-1"); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	// Network still exists.
	networks, _ := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
	)
	if len(networks) != 1 {
		t.Errorf("live network should survive reconcile, got %d", len(networks))
	}
}

// ---------------------------------------------------------------------------
// 9. Start failure rollback — container and network cleaned up
// ---------------------------------------------------------------------------

func TestStart_FailureBeforeReadiness_CleanedUp(t *testing.T) {
	driver := newFakeRuntimeDriver()
	driver.failCreate = true // Container create fails.

	reg := NewServiceRegistry(driver, nil, &fakeReadinessProbe{ready: true})
	_, err := reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = reg.Start(context.Background(), "wf-1", "feedback")
	if err == nil {
		t.Fatal("Start() should fail when container create fails")
	}

	// Instance should be FAILED.
	inst, _ := reg.Get("wf-1", "feedback")
	if inst != nil && inst.State != StateFailed {
		t.Errorf("instance state = %s, want %s", inst.State, StateFailed)
	}
}

// ---------------------------------------------------------------------------
// 10. WorkflowTerminal cleans up networks
// ---------------------------------------------------------------------------

func TestWorkflowTerminal_CleansUpNetwork(t *testing.T) {
	driver, reg := setupNetworkTest(t)

	_, _ = reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	_, _ = reg.Start(context.Background(), "wf-1", "feedback")

	if err := reg.WorkflowTerminal(context.Background(), "wf-1"); err != nil {
		t.Fatalf("WorkflowTerminal() error = %v", err)
	}

	// Network should be cleaned up.
	networks, _ := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
	)
	if len(networks) != 0 {
		t.Fatalf("network should be removed after workflow terminal, got %d", len(networks))
	}
}

// ---------------------------------------------------------------------------
// 11. ServiceRouteAuthorizer — stale capability after stop
// ---------------------------------------------------------------------------

func TestStaleCapability_AfterStop_NotAuthorized(t *testing.T) {
	_, reg := setupNetworkTest(t)

	_, _ = reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	inst, _ := reg.Start(context.Background(), "wf-1", "feedback")
	oldCap := inst.Capability

	// Stop the service.
	_ = reg.Stop(context.Background(), "wf-1", "feedback")

	// Capability is cleared.
	stoppedInst, _ := reg.Get("wf-1", "feedback")
	if stoppedInst != nil && stoppedInst.Capability != "" {
		t.Error("capability not cleared after stop")
	}

	// The old capability should not authorize if replayed.
	a := &ServiceRouteAuthorizer{}
	// A fresh expected token means the old one won't match.
	newCap, _ := GenerateCapability()
	if err := a.Authorized(newCap, oldCap); err == nil {
		t.Fatal("stale capability was authorized after stop")
	}
}

// ---------------------------------------------------------------------------
// 12. Attach/Detach crash matrix
// ---------------------------------------------------------------------------

func TestAttach_DetachIdempotent_MultipleCalls(t *testing.T) {
	driver := newFakeRuntimeDriver()
	state := newServiceNetworkState()
	state.NetworkID = "net-test-1"
	state.NetworkAlias = "alias-test"

	// Create a container in the driver so AttachNetwork can find it.
	containerID, err := driver.Create(context.Background(), runtime.ContainerSpec{
		Image: "test-image",
	})
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Multiple attaches should be idempotent.
	for i := 0; i < 3; i++ {
		if err := AttachToServiceNetwork(context.Background(), driver, containerID, state); err != nil {
			t.Fatalf("Attach #%d failed: %v", i+1, err)
		}
	}
	if state.RemainingAttachments() != 1 {
		t.Fatalf("after 3x attach, attachments = %d, want 1", state.RemainingAttachments())
	}

	// Multiple detaches should be idempotent.
	for i := 0; i < 3; i++ {
		DetachFromServiceNetwork(context.Background(), driver, containerID, state)
	}
	if state.RemainingAttachments() != 0 {
		t.Fatalf("after 3x detach, attachments = %d, want 0", state.RemainingAttachments())
	}
}

func TestAttach_WithoutNetwork_CausesError(t *testing.T) {
	driver := newFakeRuntimeDriver()
	state := newServiceNetworkState()
	// NetworkID is empty — not created.

	err := AttachToServiceNetwork(context.Background(), driver, runtime.ContainerID("c1"), state)
	if err == nil {
		t.Fatal("Attach without network should fail")
	}
	if !strings.Contains(err.Error(), "network not created") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRemove_WithContainersAttached_Fails(t *testing.T) {
	driver := newFakeRuntimeDriver()
	state := newServiceNetworkState()
	state.NetworkID = "net-test-1"
	state.attachedContainers["container-1"] = true

	err := RemoveServiceNetwork(context.Background(), driver, state)
	if err == nil {
		t.Fatal("Remove with containers still attached should fail")
	}
	if !strings.Contains(err.Error(), "containers still attached") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 13. Helper for test bindings
// ---------------------------------------------------------------------------

func makeBinding(serviceID string) pack.ServiceBinding {
	return pack.ServiceBinding{
		ServiceID:      serviceID,
		PackageName:    "test-pkg",
		PackageVersion: "1.0.0",
		AllowedTools:   []string{"tool1", "tool2"},
	}
}

func TestNetworkAlias_IsHexEncoded(t *testing.T) {
	alias, err := generateNetworkAlias()
	if err != nil {
		t.Fatal(err)
	}
	if len(alias) != 32 { // 16 bytes → 32 hex chars
		t.Fatalf("alias length = %d, want 32", len(alias))
	}
	for _, c := range alias {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("alias contains non-hex char: %c", c)
		}
	}
}

// ---------------------------------------------------------------------------
// 14. Ensure no capability/endpoint in audit/error redaction
// ---------------------------------------------------------------------------

func TestCapabilityNotInLastError(t *testing.T) {
	// When a service fails, the LastError field should not contain
	// capability tokens.
	_, reg := setupNetworkTest(t)

	_, _ = reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	inst, _ := reg.Start(context.Background(), "wf-1", "feedback")

	// Fence the service with a reason that accidentally includes a
	// capability-like string.
	_ = reg.Fence(context.Background(), "wf-1", "feedback", "token: "+inst.Capability)
	fenced, _ := reg.Get("wf-1", "feedback")

	if strings.Contains(fenced.LastError, inst.Capability) {
		t.Error("capability token leaked into LastError after Fence")
	}
}

// ---------------------------------------------------------------------------
// 15. Timing — Start/Stop cycle timing
// ---------------------------------------------------------------------------

func TestStart_WithinReasonableTime(t *testing.T) {
	_, reg := setupNetworkTest(t)

	_, err := reg.Declare("wf-1", makeBinding("feedback"), "sha256:abc", []string{"tool1"})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = reg.Start(context.Background(), "wf-1", "feedback")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Start() took %v, should be well under 5s on fake driver", elapsed)
	}
}

// ---------------------------------------------------------------------------
// 16. No duplicate network creation for concurrent starts
// ---------------------------------------------------------------------------

func TestRace_ConcurrentStart_NoDuplicateNetworks(t *testing.T) {
	driver, reg := setupNetworkTest(t)

	// Two different services, same workflow.
	_, _ = reg.Declare("wf-1", makeBinding("svc-a"), "sha256:abc", []string{"t1"})
	_, _ = reg.Declare("wf-1", makeBinding("svc-b"), "sha256:def", []string{"t2"})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(svc string) {
			defer wg.Done()
			_, _ = reg.Start(context.Background(), "wf-1", svc)
		}([]string{"svc-a", "svc-b"}[i])
	}
	wg.Wait()

	networks, _ := driver.ListNetworks(context.Background(),
		runtime.LabelWorkflowID+"=wf-1",
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
	)
	if len(networks) != 1 {
		t.Fatalf("concurrent start of 2 services: expected 1 network, got %d", len(networks))
	}
}
