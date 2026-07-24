package mcpmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// TestE2E_CrossContainer_LookupFeedback validates the Docker cross-container MCP
// call path end-to-end: a client container on the internal MCP service network
// reaches a service container by its DNS alias, sends a JSON-RPC tools/call
// with the capability header, and receives a distinctive tool result. After
// cleanup, zero service network or MCP container orphans remain.
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestE2E_CrossContainer_LookupFeedback(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// ── Setup real Docker runtime ──────────────────────────────────────────
	dr, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}
	if dr == nil {
		t.Fatal("NewDockerRuntime() returned nil")
	}

	if ver, verr := dr.ServerVersion(ctx); verr != nil {
		t.Logf("Warning: ServerVersion: %v (Docker may be slow to start)", verr)
	} else {
		t.Logf("Docker Engine version: %s", ver)
	}

	workflowID := fmt.Sprintf("e2e-wf-c5b-%d", time.Now().UnixNano())

	// ── Copy mock server script to a temp directory for bind mount ─────────
	mockSrc := filepath.Join("testdata", "mcp_mock_server.py")
	tmpDir, err := os.MkdirTemp("", "mcp-e2e-c5b-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	copyCmd := exec.CommandContext(ctx, "cp", mockSrc, tmpDir)
	if out, err := copyCmd.CombinedOutput(); err != nil {
		t.Fatalf("cp mock server: %v: %s", err, out)
	}
	t.Logf("mock server copied to %s", tmpDir)

	// ── Create ServiceRegistry backed by real Docker runtime ───────────────
	// No promotion checker, no readiness probe (service is mock).
	reg := NewServiceRegistry(dr, nil, nil)

	// Inject service container defaults: python:3-alpine running the mock.
	reg.SetServiceContainerDefaults("python:3-alpine",
		[]string{"python", "/mock/mcp_mock_server.py"})
	reg.SetServiceBinds([]string{tmpDir + ":/mock:ro"})

	// Tracking for cleanup.
	clientContainerID := runtime.ContainerID("")

	// Deferred cleanup: always tear down, even on panic.
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()

		t.Log("cleanup: stopping services...")
		if err := reg.WorkflowTerminal(cleanCtx, workflowID); err != nil {
			t.Logf("Warning: WorkflowTerminal: %v", err)
		}

		if clientContainerID != "" {
			t.Logf("cleanup: removing client container %s", clientContainerID)
			_ = dr.Stop(cleanCtx, clientContainerID, nil)
			_ = dr.Remove(cleanCtx, clientContainerID, true)
		}
	}()

	// ── Ensure the feedback MCP service ────────────────────────────────────
	bindings := []pack.ServiceBinding{
		{
			ServiceID:      "feedback",
			BundleDigest:   "sha256:e2e",
			AllowedTools:   []string{"lookup_feedback"},
			PackageName:    "feedback-tools",
			PackageVersion: "1.0.0",
		},
	}

	if err := reg.EnsureServices(ctx, workflowID, bindings); err != nil {
		t.Fatalf("EnsureServices: %v", err)
	}

	// ── Get the instance and verify endpoint ───────────────────────────────
	inst, err := reg.Get(workflowID, "feedback")
	if err != nil {
		t.Fatalf("Get(feedback): %v", err)
	}
	if inst.State != StateReady {
		t.Fatalf("expected READY, got %s", inst.State)
	}
	if inst.Endpoint == "" {
		t.Fatal("Endpoint not set")
	}
	if !strings.Contains(inst.Endpoint, "svc-feedback") {
		t.Errorf("Endpoint %q does not contain svc-feedback", inst.Endpoint)
	}
	if inst.Capability == "" {
		t.Fatal("Capability not set")
	}
	t.Logf("Service endpoint: %s", inst.Endpoint)
	t.Logf("Service capability: %s (len=%d)", inst.Capability[:12]+"...", len(inst.Capability))

	// ── Create client container ────────────────────────────────────────────
	clientID, err := dr.Create(ctx, runtime.ContainerSpec{
		Image:   "python:3-alpine",
		Command: []string{"sleep", "3600"},
	})
	if err != nil {
		t.Fatalf("Create(client): %v", err)
	}
	clientContainerID = clientID
	t.Logf("Client container: %s", clientID)

	if err := dr.Start(ctx, clientID); err != nil {
		t.Fatalf("Start(client): %v", err)
	}

	// Wait for container to be ready.
	time.Sleep(2 * time.Second)

	// ── Attach client to the service network ──────────────────────────────
	if err := reg.AttachClientContainer(ctx, workflowID, clientID); err != nil {
		t.Fatalf("AttachClientContainer: %v", err)
	}
	t.Log("Client attached to service network")

	// ── Debug: verify network connectivity ────────────────────────────────
	// Check service container networks
	svcNetworks, err := dr.InspectContainerNetworks(ctx, inst.ContainerID)
	if err != nil {
		t.Logf("Warning: InspectContainerNetworks(service): %v", err)
	} else {
		for _, n := range svcNetworks {
			t.Logf("Service network: ID=%s IP=%s", n.ID, n.IPAddress)
		}
	}

	// Check client container networks
	clientNetworks, err := dr.InspectContainerNetworks(ctx, clientID)
	if err != nil {
		t.Logf("Warning: InspectContainerNetworks(client): %v", err)
	} else {
		for _, n := range clientNetworks {
			t.Logf("Client network: ID=%s IP=%s", n.ID, n.IPAddress)
		}
	}

	// Try nslookup from client
	nsOut, nsStderr, _, _ := dr.Exec(ctx, clientID,
		[]string{"nslookup", "svc-feedback"})
	t.Logf("nslookup svc-feedback: %s (stderr: %s)", nsOut, nsStderr)

	// Wait for DNS to propagate on Docker embedded DNS server.
	time.Sleep(3 * time.Second)

	// Retry nslookup after wait
	nsOut2, nsStderr2, _, _ := dr.Exec(ctx, clientID,
		[]string{"nslookup", "svc-feedback"})
	t.Logf("nslookup (after wait) svc-feedback: %s (stderr: %s)", nsOut2, nsStderr2)

	// ── Build JSON-RPC request payload ─────────────────────────────────────
	requestPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "lookup_feedback",
			"arguments": map[string]interface{}{},
		},
		"id": 1,
	}
	reqBytes, _ := json.Marshal(requestPayload)

	// ── Use Python from the client container to POST to the service ────────
	pythonScript := fmt.Sprintf(`
import urllib.request
import json

data = %s
req = urllib.request.Request(
    "%s",
    data=json.dumps(data).encode(),
    headers={
        "Content-Type": "application/json",
        "%s": "%s",
    },
)
try:
    with urllib.request.urlopen(req, timeout=10) as resp:
        status = resp.status
        body = resp.read().decode()
        print(body)
        print("HTTP_STATUS:" + str(status))
except Exception as e:
    print("ERROR:" + str(e))
    print("HTTP_STATUS:0")
`, string(reqBytes), inst.Endpoint, CapabilityHeader, inst.Capability)

	stdout, stderr, exitCode, err := dr.Exec(ctx, clientID,
		[]string{"python", "-c", pythonScript})
	t.Logf("Python HTTP response stdout: %s", stdout)
	if stderr != "" {
		t.Logf("Python stderr: %s", stderr)
	}

	if err != nil || exitCode != 0 {
		// Docker exec itself failing (not HTTP code)
		t.Fatalf("docker exec python: exit=%d err=%v stderr=%s", exitCode, err, stderr)
	}

	// Parse response: last line is HTTP_STATUS:<code>
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("unexpected output (expected body + status line): %s", stdout)
	}
	httpStatusLine := strings.TrimSpace(lines[len(lines)-1])
	body := strings.Join(lines[:len(lines)-1], "\n")

	if !strings.HasPrefix(httpStatusLine, "HTTP_STATUS:") {
		t.Fatalf("unexpected status line: %s", httpStatusLine)
	}
	httpStatus := strings.TrimPrefix(httpStatusLine, "HTTP_STATUS:")

	if httpStatus != "200" {
		t.Fatalf("HTTP status %s, body: %s", httpStatus, body)
	}

	t.Logf("Response body: %s", body)

	// ── Assert the distinctive marker ──────────────────────────────────────
	if !strings.Contains(body, "b33-t08-docker-e2e") {
		t.Errorf("response missing marker 'b33-t08-docker-e2e': %s", body)
	}
	if !strings.Contains(body, "Cross-container works") {
		t.Errorf("response missing 'Cross-container works': %s", body)
	}

	// ── Verify capability was provided (from mock response) ────────────────
	if !strings.Contains(body, `"capability_provided": true`) {
		t.Errorf("response capability_provided not true: %s", body)
	}

	// ── Optional: try ManagedServiceResolver from host (may fail on internal net) ──
	t.Log("Testing ManagedServiceResolver (from host — may fail if not on internal net)...")
	resolver := NewManagedServiceResolver(reg, nil)
	resolverResult, resolverErr := resolver.ResolveToolCall(ctx, workflowID, "feedback", "lookup_feedback", map[string]interface{}{})
	if resolverErr != nil {
		t.Logf("ManagedServiceResolver.ResolveToolCall from host: %v (expected if host not on internal network)", resolverErr)
	} else {
		t.Logf("ManagedServiceResolver result: %v", resolverResult)
	}

	t.Log("SUCCESS: cross-container e2e lookup_feedback completed")

	// ── Cleanup phase ──────────────────────────────────────────────────────
	// Stop client container first.
	if err := dr.Stop(ctx, clientID, nil); err != nil {
		t.Logf("Warning: Stop(client): %v", err)
	}
	if err := dr.Remove(ctx, clientID, true); err != nil {
		t.Logf("Warning: Remove(client): %v", err)
	}
	clientContainerID = "" // prevent double-cleanup in defer

	// Stop all services via WorkflowTerminal.
	if err := reg.WorkflowTerminal(ctx, workflowID); err != nil {
		t.Fatalf("WorkflowTerminal: %v", err)
	}

	// ── Zero-orphans check ─────────────────────────────────────────────────
	time.Sleep(2 * time.Second) // give Docker time to process removals

	containers, err := dr.ListContainers(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelWorkflowID+"="+workflowID,
	)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) > 0 {
		ids := make([]string, len(containers))
		for i, c := range containers {
			ids[i] = c.ID[:12]
		}
		t.Errorf("orphan containers remaining for workflow: %v", ids)
	} else {
		t.Log("Zero MCP orphan containers ✓")
	}

	networks, err := dr.ListNetworks(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
		runtime.LabelWorkflowID+"="+workflowID,
	)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(networks) > 0 {
		names := make([]string, len(networks))
		for i, n := range networks {
			names[i] = n.Name
		}
		t.Errorf("orphan networks remaining for workflow: %v", names)
	} else {
		t.Log("Zero service network orphans ✓")
	}
}
