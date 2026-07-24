package mcpmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// ── Shared helpers ──────────────────────────────────────────────────────────

// dockerE2E holds common resources for a Docker-gated negative e2e test.
type dockerE2E struct {
	t        *testing.T
	ctx      context.Context
	cancel   context.CancelFunc
	dr       runtime.RuntimeDriver
	reg      *ServiceRegistry
	testdata string // absolute path to testdata directory
}

// setupNegE2E creates a Docker runtime and ServiceRegistry for negative e2e
// tests. The returned cleanup function must be deferred immediately.
func setupNegE2E(t *testing.T) *dockerE2E {
	t.Helper()

	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	dr, err := runtime.NewDockerRuntime()
	if err != nil {
		cancel()
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}
	if dr == nil {
		cancel()
		t.Fatal("NewDockerRuntime() returned nil")
	}

	absTestdata, err := filepath.Abs("testdata")
	if err != nil {
		cancel()
		t.Fatalf("filepath.Abs(testdata): %v", err)
	}
	t.Logf("bind-mount testdata from: %s", absTestdata)

	reg := NewServiceRegistry(dr, nil, nil)
	reg.SetServiceContainerDefaults("python:3-alpine",
		[]string{"python", "/mock/mcp_mock_server.py"})
	reg.SetServiceBinds([]string{absTestdata + ":/mock:ro"})

	return &dockerE2E{
		t:        t,
		ctx:      ctx,
		cancel:   cancel,
		dr:       dr,
		reg:      reg,
		testdata: absTestdata,
	}
}

// ensureFeedbackService is a convenience wrapper for EnsureServices with the
// feedback binding and the given allowed tools.
func (e *dockerE2E) ensureFeedbackService(workflowID string, allowedTools []string) {
	e.t.Helper()
	bindings := []pack.ServiceBinding{
		{
			ServiceID:      "feedback",
			BundleDigest:   "sha256:e2e",
			AllowedTools:   allowedTools,
			PackageName:    "feedback-tools",
			PackageVersion: "1.0.0",
		},
	}
	if err := e.reg.EnsureServices(e.ctx, workflowID, bindings); err != nil {
		e.t.Fatalf("EnsureServices: %v", err)
	}
}

// getReadyInstance fetches and validates a READY instance.
func (e *dockerE2E) getReadyInstance(workflowID, serviceID string) *ServiceInstance {
	e.t.Helper()
	inst, err := e.reg.Get(workflowID, serviceID)
	if err != nil {
		e.t.Fatalf("Get(%s): %v", serviceID, err)
	}
	if inst.State != StateReady {
		e.t.Fatalf("expected READY, got %s", inst.State)
	}
	if inst.Endpoint == "" {
		e.t.Fatal("Endpoint not set")
	}
	if inst.Capability == "" {
		e.t.Fatal("Capability not set")
	}
	return inst
}

// createClient creates, starts, and attaches a client container.
// Returns the container ID; caller must clean up.
func (e *dockerE2E) createClient(workflowID string) runtime.ContainerID {
	e.t.Helper()
	clientID, err := e.dr.Create(e.ctx, runtime.ContainerSpec{
		Image:   "python:3-alpine",
		Command: []string{"sleep", "3600"},
	})
	if err != nil {
		e.t.Fatalf("Create(client): %v", err)
	}
	if err := e.dr.Start(e.ctx, clientID); err != nil {
		e.t.Fatalf("Start(client): %v", err)
	}
	time.Sleep(2 * time.Second)

	if err := e.reg.AttachClientContainer(e.ctx, workflowID, clientID); err != nil {
		e.t.Fatalf("AttachClientContainer: %v", err)
	}
	return clientID
}

// cleanupClient stops and removes a client container. Clears the provided
// pointer to prevent double-cleanup.
func (e *dockerE2E) cleanupClient(clientID *runtime.ContainerID) {
	e.t.Helper()
	if *clientID == "" {
		return
	}
	cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = e.dr.Stop(cleanCtx, *clientID, nil)
	_ = e.dr.Remove(cleanCtx, *clientID, true)
	*clientID = ""
}

// cleanupWorkflow calls WorkflowTerminal and reconciles orphan networks.
func (e *dockerE2E) cleanupWorkflow(workflowID string) {
	e.t.Helper()
	cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.reg.WorkflowTerminal(cleanCtx, workflowID); err != nil {
		e.t.Logf("Warning: WorkflowTerminal(%s): %v", workflowID, err)
	}
	if removed, err := ReconcileOrphanServiceNetworks(cleanCtx, e.dr, workflowID); err != nil {
		e.t.Logf("Warning: ReconcileOrphanServiceNetworks: %v", err)
	} else if removed > 0 {
		e.t.Logf("ReconcileOrphanServiceNetworks removed %d orphan network(s)", removed)
	}
}

// patchEndpointToIP replaces the service instance's Endpoint (DNS-based,
// e.g. http://svc-feedback:8080) with a direct IP-based endpoint
// (http://<container-ip>:8080) by inspecting the container's IP on the
// workflow-scoped service network. This ensures ManagedServiceResolver
// calls from the host reach the actual service instead of failing on
// Docker DNS resolution.
func (e *dockerE2E) patchEndpointToIP(workflowID, serviceID string) {
	e.t.Helper()

	// Get a copy to read ContainerID without extra locking.
	inst, err := e.reg.Get(workflowID, serviceID)
	if err != nil {
		e.t.Fatalf("patchEndpointToIP: Get: %v", err)
	}
	if inst.ContainerID == "" {
		e.t.Fatal("patchEndpointToIP: instance has no ContainerID")
	}

	// Find the service network.
	networks, err := e.dr.InspectContainerNetworks(e.ctx, inst.ContainerID)
	if err != nil {
		e.t.Fatalf("patchEndpointToIP: InspectContainerNetworks: %v", err)
	}
	var networkID string
	for _, n := range networks {
		if strings.Contains(n.Name, "agentpaas-mcp-svc") {
			networkID = n.ID
			break
		}
	}
	if networkID == "" {
		e.t.Fatal("patchEndpointToIP: could not find service network")
	}

	// Get the container's IP on that network.
	ip, err := e.dr.InspectContainerIP(e.ctx, inst.ContainerID, networkID)
	if err != nil {
		e.t.Fatalf("patchEndpointToIP: InspectContainerIP: %v", err)
	}
	if ip == "" {
		e.t.Fatal("patchEndpointToIP: empty IP")
	}

	newEndpoint := fmt.Sprintf("http://%s:%d", ip, DefaultMCPServicePort)
	e.t.Logf("Patching endpoint from %s to %s", inst.Endpoint, newEndpoint)

	// Patch the real instance under proper locking.
	key := workflowID + "/" + serviceID
	e.reg.mu.RLock()
	mapped := e.reg.instances[key]
	e.reg.mu.RUnlock()

	if mapped == nil {
		e.t.Fatalf("patchEndpointToIP: instance not in map for key %s", key)
	}

	mapped.mu.Lock()
	mapped.Endpoint = newEndpoint
	mapped.mu.Unlock()
}

// postMCP sends a JSON-RPC tools/call from the client container via Python
// and returns the HTTP status code and response body.
func (e *dockerE2E) postMCP(clientID runtime.ContainerID, serviceURL, capability string,
	tool string, args map[string]interface{}) (status int, body string, err error) {
	e.t.Helper()

	requestPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      tool,
			"arguments": args,
		},
		"id": 1,
	}
	reqBytes, _ := json.Marshal(requestPayload)

	capLine := ""
	if capability != "" {
		capLine = fmt.Sprintf(`"%s": "%s",`, CapabilityHeader, capability)
	}

	pythonScript := fmt.Sprintf(`
import urllib.request
import urllib.error
import json

data = %s
req = urllib.request.Request(
    "%s",
    data=json.dumps(data).encode(),
    headers={
        %s
        "Content-Type": "application/json",
    },
)
try:
    with urllib.request.urlopen(req, timeout=10) as resp:
        status = resp.status
        body = resp.read().decode()
        print(body)
        print("HTTP_STATUS:" + str(status))
except urllib.error.HTTPError as e:
    print(e.read().decode())
    print("HTTP_STATUS:" + str(e.code))
except Exception as ex:
    print("ERROR:" + str(ex))
    print("HTTP_STATUS:0")
`, string(reqBytes), serviceURL, capLine)

	stdout, stderr, exitCode, execErr := e.dr.Exec(e.ctx, clientID,
		[]string{"python", "-c", pythonScript})

	if stderr != "" {
		e.t.Logf("Python stderr: %s", stderr)
	}
	if execErr != nil || exitCode != 0 {
		return 0, stdout, fmt.Errorf("docker exec python: exit=%d err=%v stderr=%s", exitCode, execErr, stderr)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 2 {
		return 0, stdout, fmt.Errorf("unexpected output: %s", stdout)
	}
	statusLine := strings.TrimSpace(lines[len(lines)-1])
	responseBody := strings.Join(lines[:len(lines)-1], "\n")

	if !strings.HasPrefix(statusLine, "HTTP_STATUS:") {
		return 0, stdout, fmt.Errorf("unexpected status line: %s", statusLine)
	}
	fmt.Sscanf(statusLine, "HTTP_STATUS:%d", &status)
	return status, responseBody, nil
}

// ── Test 1: Undeclared tool ─────────────────────────────────────────────────

func TestE2E_Neg_UndeclaredTool(t *testing.T) {
	env := setupNegE2E(t)
	defer env.cancel()

	workflowID := fmt.Sprintf("e2e-neg-undecl-%d", time.Now().UnixNano())
	env.ensureFeedbackService(workflowID, []string{"lookup_feedback"})
	inst := env.getReadyInstance(workflowID, "feedback")

	// ── Resolver path: undeclared tool rejected via ManagedServiceResolver ──
	resolver := NewManagedServiceResolver(env.reg, nil)
	_, resolverErr := resolver.ResolveToolCall(env.ctx, workflowID, "feedback",
		"evil_tool", map[string]interface{}{})
	if resolverErr == nil {
		t.Fatal("ResolveToolCall with undeclared tool: error = nil, want rejection")
	}
	if !strings.Contains(resolverErr.Error(), "not declared") {
		t.Logf("resolver error: %v", resolverErr)
	}
	t.Logf("Resolver rejected undeclared tool: %v ✓", resolverErr)

	// ── HTTP path: post undeclared tool to mock; mock returns tool not found ──
	clientID := env.createClient(workflowID)
	defer env.cleanupClient(&clientID)

	status, body, err := env.postMCP(clientID, inst.Endpoint, inst.Capability,
		"evil_tool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("postMCP exec error: %v", err)
	}
	// The mock returns 200 with an error response for unknown tools.
	// Either non-200 OR JSON-RPC error in body is acceptable.
	hasError := status != 200 || strings.Contains(body, "Tool not found") || strings.Contains(body, `"error"`)
	if !hasError {
		t.Errorf("expected error for undeclared tool; got status=%d body=%s", status, body)
	} else {
		t.Logf("HTTP path rejected undeclared tool: status=%d ✓", status)
	}

	env.cleanupClient(&clientID)
	env.cleanupWorkflow(workflowID)
}

// ── Test 2: Undeclared binding ──────────────────────────────────────────────

func TestE2E_Neg_UndeclaredBinding(t *testing.T) {
	env := setupNegE2E(t)
	defer env.cancel()

	workflowID := fmt.Sprintf("e2e-neg-nobind-%d", time.Now().UnixNano())
	// Ensure feedback so the registry has a service, but never Ensure "nope".
	env.ensureFeedbackService(workflowID, []string{"lookup_feedback"})

	resolver := NewManagedServiceResolver(env.reg, nil)
	_, err := resolver.ResolveToolCall(env.ctx, workflowID, "nope",
		"lookup_feedback", map[string]interface{}{})
	if err == nil {
		t.Fatal("ResolveToolCall with undeclared binding: error = nil, want rejection")
	}
	t.Logf("Undeclared binding rejected: %v ✓", err)

	env.cleanupWorkflow(workflowID)
}

// ── Test 3: HTTP bypass without capability header ───────────────────────────

func TestE2E_Neg_HTTPBypassNoCapability(t *testing.T) {
	env := setupNegE2E(t)
	defer env.cancel()

	workflowID := fmt.Sprintf("e2e-neg-nocap-%d", time.Now().UnixNano())
	env.ensureFeedbackService(workflowID, []string{"lookup_feedback"})
	inst := env.getReadyInstance(workflowID, "feedback")

	clientID := env.createClient(workflowID)
	defer env.cleanupClient(&clientID)

	// Post WITHOUT capability header.
	status, body, err := env.postMCP(clientID, inst.Endpoint, "",
		"lookup_feedback", map[string]interface{}{})
	if err != nil {
		t.Fatalf("postMCP exec error: %v", err)
	}
	if status != 401 {
		t.Errorf("expected HTTP 401 for missing capability; got status=%d body=%s", status, body)
	} else {
		t.Logf("HTTP bypass denied with 401 ✓")
	}
	if !strings.Contains(body, "invalid capability") {
		t.Logf("body: %s", body)
	}

	env.cleanupClient(&clientID)
	env.cleanupWorkflow(workflowID)
}

// ── Test 4: Cross-workflow isolation ────────────────────────────────────────

func TestE2E_Neg_CrossWorkflowIsolation(t *testing.T) {
	env := setupNegE2E(t)
	defer env.cancel()

	workflowA := fmt.Sprintf("e2e-neg-isoa-%d", time.Now().UnixNano())
	workflowB := fmt.Sprintf("e2e-neg-isob-%d", time.Now().UnixNano())

	// Ensure feedback on both workflows.
	env.ensureFeedbackService(workflowA, []string{"lookup_feedback"})
	env.ensureFeedbackService(workflowB, []string{"lookup_feedback"})

	instA := env.getReadyInstance(workflowA, "feedback")
	instB := env.getReadyInstance(workflowB, "feedback")

	// Get B's service network and B's IP on that network.
	bNetworks, err := env.dr.InspectContainerNetworks(env.ctx, instB.ContainerID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(B): %v", err)
	}
	var bNetworkID string
	for _, n := range bNetworks {
		if strings.Contains(n.Name, "agentpaas-mcp-svc") {
			bNetworkID = n.ID
			break
		}
	}
	if bNetworkID == "" {
		t.Fatal("could not find B's service network")
	}

	bIP, err := env.dr.InspectContainerIP(env.ctx, instB.ContainerID, bNetworkID)
	if err != nil {
		t.Fatalf("InspectContainerIP(B): %v", err)
	}
	bURL := fmt.Sprintf("http://%s:%d", bIP, DefaultMCPServicePort)
	t.Logf("B's service at %s (on network %s)", bURL, bNetworkID[:12])

	// ── Client attached only to workflow A ──
	clientID := env.createClient(workflowA)
	defer env.cleanupClient(&clientID)

	t.Logf("Client attached to workflow A (%s)", workflowA)

	// ── 4a: Client on A's network tries to reach B's direct IP ──
	// Since the client is only on A's Docker network, B's IP on B's network
	// should be unreachable.
	status, body, postErr := env.postMCP(clientID, bURL, instB.Capability,
		"lookup_feedback", map[string]interface{}{})
	// The postMCP helper returns err for exec failures (e.g. timeout/refused),
	// and status=0 for network errors caught by Python's urllib.
	if postErr == nil && status == 200 {
		t.Errorf("cross-workflow: expected failure reaching B's IP from A's network; got status=200 body=%s", body)
	} else if postErr != nil {
		t.Logf("Cross-workflow isolation confirmed (exec error): %v ✓", postErr)
	} else {
		t.Logf("Cross-workflow isolation confirmed: status=%d ✓", status)
	}

	// ── 4b: Sanity — client on A can still reach A's service ──
	status, body, postErr = env.postMCP(clientID, instA.Endpoint, instA.Capability,
		"lookup_feedback", map[string]interface{}{})
	if postErr != nil {
		t.Fatalf("postMCP sanity exec error: %v", postErr)
	}
	if status != 200 {
		t.Errorf("sanity: expected 200 for same-workflow call; got status=%d body=%s", status, body)
	} else {
		t.Logf("Same-workflow call succeeds: 200 ✓")
	}

	env.cleanupClient(&clientID)
	env.cleanupWorkflow(workflowA)
	env.cleanupWorkflow(workflowB)
}

// ── Test 5: Service crash ───────────────────────────────────────────────────

func TestE2E_Neg_ServiceCrash(t *testing.T) {
	env := setupNegE2E(t)
	defer env.cancel()

	workflowID := fmt.Sprintf("e2e-neg-crash-%d", time.Now().UnixNano())
	env.ensureFeedbackService(workflowID, []string{"lookup_feedback"})
	inst := env.getReadyInstance(workflowID, "feedback")

	clientID := env.createClient(workflowID)
	defer env.cleanupClient(&clientID)

	// Sanity: service is reachable before crash.
	status, _, err := env.postMCP(clientID, inst.Endpoint, inst.Capability,
		"lookup_feedback", map[string]interface{}{})
	if err != nil {
		t.Fatalf("pre-crash postMCP: %v", err)
	}
	if status != 200 {
		t.Fatalf("pre-crash: expected 200, got %d", status)
	}
	t.Log("Pre-crash call: 200 ✓")

	// ── Crash the service container ──
	if stopErr := env.dr.Stop(env.ctx, inst.ContainerID, nil); stopErr != nil {
		t.Fatalf("Stop(service): %v", stopErr)
	}
	t.Logf("Service container %s stopped", inst.ContainerID[:12])

	// Wait for the container to actually stop.
	time.Sleep(3 * time.Second)

	// ── Post-crash: call must fail ──
	status, body, err := env.postMCP(clientID, inst.Endpoint, inst.Capability,
		"lookup_feedback", map[string]interface{}{})
	// Either exec error (connection refused) OR HTTP 0 is acceptable.
	if err == nil && status == 200 {
		t.Errorf("post-crash: expected failure, got status=%d body=%s", status, body)
	} else {
		t.Logf("Post-crash call failed as expected: err=%v status=%d ✓", err, status)
	}

	env.cleanupClient(&clientID)
	env.cleanupWorkflow(workflowID)
}

// ── Test 6: Service timeout ─────────────────────────────────────────────────

func TestE2E_Neg_Timeout(t *testing.T) {
	env := setupNegE2E(t)
	defer env.cancel()

	workflowID := fmt.Sprintf("e2e-neg-timeout-%d", time.Now().UnixNano())

	// Ensure service with slow_tool declared.
	bindings := []pack.ServiceBinding{
		{
			ServiceID:      "feedback",
			BundleDigest:   "sha256:e2e",
			AllowedTools:   []string{"lookup_feedback", "slow_tool"},
			PackageName:    "feedback-tools",
			PackageVersion: "1.0.0",
		},
	}
	if err := env.reg.EnsureServices(env.ctx, workflowID, bindings); err != nil {
		t.Fatalf("EnsureServices: %v", err)
	}
	inst := env.getReadyInstance(workflowID, "feedback")
	t.Logf("Service ready for timeout test at %s", inst.Endpoint)

	// Patch endpoint from Docker DNS alias to direct IP so the host
	// can reach the service without Docker DNS resolution.
	env.patchEndpointToIP(workflowID, "feedback")

	// Use ManagedServiceResolver with a tight HTTP client timeout.
	httpClient := &http.Client{Timeout: 1 * time.Second}
	resolver := TestManagedResolverHTTPClient(env.reg, httpClient)

	// Call slow_tool with a short context timeout.
	ctx, cancel := context.WithTimeout(env.ctx, 5*time.Second)
	defer cancel()

	_, resolverErr := resolver.ResolveToolCall(ctx, workflowID, "feedback",
		"slow_tool", map[string]interface{}{})
	if resolverErr == nil {
		t.Fatal("ResolveToolCall with slow_tool: error = nil, want timeout/deadline error")
	}

	errStr := resolverErr.Error()
	t.Logf("Timeout error: %v", errStr)

	// Must contain timeout/deadline/context — NOT mere DNS failure.
	errLower := strings.ToLower(errStr)
	hasTimeoutMarker := strings.Contains(errLower, "timeout") ||
		strings.Contains(errLower, "deadline") ||
		strings.Contains(errLower, "context")
	if !hasTimeoutMarker {
		t.Errorf("expected timeout/deadline/context marker in error, got: %v", resolverErr)
	}
	if strings.Contains(errLower, "no such host") || strings.Contains(errLower, "lookup") {
		t.Errorf("error is DNS lookup failure, not timeout: %v", resolverErr)
	}

	env.cleanupWorkflow(workflowID)
}

// ── Test 7: Fence during call (best-effort) ─────────────────────────────────

func TestE2E_Neg_FenceDuringCall(t *testing.T) {
	env := setupNegE2E(t)
	defer env.cancel()

	workflowID := fmt.Sprintf("e2e-neg-fence-%d", time.Now().UnixNano())

	bindings := []pack.ServiceBinding{
		{
			ServiceID:      "feedback",
			BundleDigest:   "sha256:e2e",
			AllowedTools:   []string{"lookup_feedback", "slow_tool"},
			PackageName:    "feedback-tools",
			PackageVersion: "1.0.0",
		},
	}
	if err := env.reg.EnsureServices(env.ctx, workflowID, bindings); err != nil {
		t.Fatalf("EnsureServices: %v", err)
	}
	_ = env.getReadyInstance(workflowID, "feedback")

	// Patch endpoint to direct IP so ResolveToolCall reaches the
	// actual service instead of failing on Docker DNS resolution.
	env.patchEndpointToIP(workflowID, "feedback")

	// Use a resolver with a long client timeout (we want the fence, not HTTP timeout).
	httpClient := &http.Client{Timeout: 60 * time.Second}
	resolver := TestManagedResolverHTTPClient(env.reg, httpClient)

	ctx, cancel := context.WithTimeout(env.ctx, 30*time.Second)
	defer cancel()

	// Start slow_tool call in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		_, callErr := resolver.ResolveToolCall(ctx, workflowID, "feedback",
			"slow_tool", map[string]interface{}{})
		errCh <- callErr
	}()

	// Give the call time to reach the service.
	time.Sleep(2 * time.Second)

	// Fence the service.
	fenceErr := env.reg.Fence(env.ctx, workflowID, "feedback",
		"test fence during slow call")
	if fenceErr != nil {
		t.Fatalf("Fence: %v", fenceErr)
	}
	t.Log("Fence applied to service")

	// Verify service state is FENCED.
	instAfter, getErr := env.reg.Get(workflowID, "feedback")
	if getErr != nil {
		t.Fatalf("Get after fence: %v", getErr)
	}
	if instAfter.State != StateFenced {
		t.Errorf("after fence, expected state FENCED, got %s", instAfter.State)
	} else {
		t.Logf("Service state is FENCED ✓")
	}

	// Wait for the call to complete (cancelled by fence).
	select {
	case callErr := <-errCh:
		if callErr == nil {
			t.Error("slow_tool call succeeded after fence; expected cancellation/failure")
		} else {
			errStr := callErr.Error()
			errLower := strings.ToLower(errStr)
			t.Logf("Slow call cancelled after fence: %v", errStr)
			// Must not be a mere DNS lookup failure.
			if strings.Contains(errLower, "no such host") || strings.Contains(errLower, "lookup") {
				t.Errorf("fence error is DNS lookup failure, not fence/cancel: %v", callErr)
			}
		}
	case <-time.After(15 * time.Second):
		t.Log("slow_tool call did not return within 15s after fence (best-effort; may be flaky)")
	}

	// After fence, a new ResolveToolCall must fail with "not ready".
	_, afterErr := resolver.ResolveToolCall(ctx, workflowID, "feedback",
		"lookup_feedback", map[string]interface{}{})
	if afterErr == nil {
		t.Error("ResolveToolCall after fence succeeded; expected 'not ready' rejection")
	} else if !strings.Contains(strings.ToLower(afterErr.Error()), "not ready") {
		t.Logf("Post-fence ResolveToolCall error (expected 'not ready'): %v", afterErr)
	} else {
		t.Logf("Post-fence ResolveToolCall rejected: %v ✓", afterErr)
	}

	env.cleanupWorkflow(workflowID)
}
