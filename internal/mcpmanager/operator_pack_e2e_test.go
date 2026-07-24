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

// TestE2E_OperatorPack_MCPFeedbackService validates the full pack → deploy →
// invoke cycle for an MCP feedback service WITHOUT Hermes. It builds a Linux
// harness binary, packs both the service and client fixtures into Docker images,
// starts the service via EnsureServices with the packed image digest, attaches
// a client container, and invokes lookup_feedback over HTTP with the capability
// header. The service must return the distinctive S1c fixture marker.
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestE2E_OperatorPack_MCPFeedbackService(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	// ── Find repo root ─────────────────────────────────────────────────────
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	t.Logf("repo root: %s", repoRoot)

	// ── Build Linux harness binary ─────────────────────────────────────────
	harnessPath := filepath.Join(repoRoot, "bin", "agentpaas-harness-linux")
	_ = os.MkdirAll(filepath.Dir(harnessPath), 0o755)
	buildCmd := exec.Command("go", "build", "-o", harnessPath, "./cmd/harness")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v\n%s", err, out)
	}
	t.Logf("harness built: %s", harnessPath)

	// ── Docker runtime ─────────────────────────────────────────────────────
	dr, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime: %v", err)
	}

	// ── Pack MCP feedback service fixture ──────────────────────────────────
	svcTag := fmt.Sprintf("agentpaas/mcp-feedback-service:%d", time.Now().UnixNano())
	svcDigest, err := packMCPFeedbackService(t, ctx, repoRoot, harnessPath, svcTag)
	if err != nil {
		t.Fatalf("pack service: %v", err)
	}
	t.Logf("service image digest: %s", svcDigest)

	// ── Pack MCP feedback client fixture ───────────────────────────────────
	clientTag := fmt.Sprintf("agentpaas/mcp-feedback-client:%d", time.Now().UnixNano())
	clientDigest, err := packMCPFeedbackClient(t, ctx, repoRoot, harnessPath, svcDigest, clientTag)
	if err != nil {
		t.Fatalf("pack client: %v", err)
	}
	t.Logf("client image digest: %s", clientDigest)

	// ── Service Registry ───────────────────────────────────────────────────
	// NO SetServiceContainerDefaults — use the packed image directly so the
	// harness entrypoint runs (which reads AGENTPAAS_AGENT_KIND=mcp_service
	// and starts the HTTP bridge).
	reg := NewServiceRegistry(dr, nil, nil)

	workflowID := fmt.Sprintf("e2e-op-pack-%d", time.Now().UnixNano())

	// Deferred cleanup.
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanCancel()
		if err := reg.WorkflowTerminal(cleanCtx, workflowID); err != nil {
			t.Logf("Warning: WorkflowTerminal: %v", err)
		}
		if removed, err := ReconcileOrphanServiceNetworks(cleanCtx, dr, workflowID); err != nil {
			t.Logf("Warning: ReconcileOrphanServiceNetworks: %v", err)
		} else if removed > 0 {
			t.Logf("ReconcileOrphanServiceNetworks removed %d orphan network(s)", removed)
		}
	}()

	// ── EnsureServices with packed service image ───────────────────────────
	bindings := []pack.ServiceBinding{
		{
			ServiceID:      "feedback",
			BundleDigest:   svcDigest,
			AllowedTools:   []string{"lookup_feedback"},
			PackageName:    "mcp-feedback-service",
			PackageVersion: "0.1.0",
		},
	}

	if err := reg.EnsureServices(ctx, workflowID, bindings); err != nil {
		t.Fatalf("EnsureServices: %v", err)
	}

	// ── Wait for service READY ─────────────────────────────────────────────
	inst, err := waitForServiceReady(ctx, t, reg, workflowID, "feedback", 120*time.Second)
	if err != nil {
		if inst != nil && inst.ContainerID != "" {
			logs, _ := exec.Command("docker", "logs", string(inst.ContainerID)).CombinedOutput()
			t.Logf("service container logs:\n%s", string(logs))
		}
		t.Fatalf("waitForServiceReady: %v", err)
	}

	// ── Verify service is running ──────────────────────────────────────────
	inspectOut, inspectErr := exec.CommandContext(ctx, "docker", "inspect",
		"-f", "{{.State.Running}}",
		string(inst.ContainerID)).CombinedOutput()
	if inspectErr != nil {
		t.Fatalf("docker inspect %s: %v", inst.ContainerID, inspectErr)
	}
	if strings.TrimSpace(string(inspectOut)) != "true" {
		logs, _ := exec.Command("docker", "logs", string(inst.ContainerID)).CombinedOutput()
		t.Fatalf("service container %s is not running. Logs:\n%s",
			inst.ContainerID, string(logs))
	}
	t.Logf("Service container %s is running, endpoint=%s", inst.ContainerID, inst.Endpoint)

	// ── Create client container from packed client image ───────────────────
	clientID, err := dr.Create(ctx, runtime.ContainerSpec{
		Image: "sha256:" + strings.TrimPrefix(clientDigest, "sha256:"),
	})
	if err != nil {
		t.Fatalf("Create(client): %v", err)
	}
	t.Logf("Client container: %s", clientID)

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		_ = dr.Stop(cleanCtx, clientID, nil)
		_ = dr.Remove(cleanCtx, clientID, true)
	}()

	if err := dr.Start(ctx, clientID); err != nil {
		t.Fatalf("Start(client): %v", err)
	}
	time.Sleep(2 * time.Second)

	// ── Attach client to service network ───────────────────────────────────
	if err := reg.AttachClientContainer(ctx, workflowID, clientID); err != nil {
		t.Fatalf("AttachClientContainer: %v", err)
	}

	// ── Get service IP on the service network ──────────────────────────────
	clientNetworks, err := dr.InspectContainerNetworks(ctx, clientID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(client): %v", err)
	}
	var serviceNetworkID string
	for _, n := range clientNetworks {
		if strings.Contains(n.Name, "agentpaas-mcp-svc") {
			serviceNetworkID = n.ID
			break
		}
	}
	if serviceNetworkID == "" {
		t.Fatal("could not find service network among client's networks")
	}

	svcIP, err := dr.InspectContainerIP(ctx, inst.ContainerID, serviceNetworkID)
	if err != nil {
		t.Logf("Warning: InspectContainerIP(service): %v", err)
	}
	if svcIP == "" {
		t.Fatal("could not resolve service container IP")
	}
	serviceURL := fmt.Sprintf("http://%s:%d", svcIP, DefaultMCPServicePort)
	t.Logf("Service URL: %s", serviceURL)

	// ── Invoke lookup_feedback via HTTP ────────────────────────────────────
	time.Sleep(2 * time.Second) // wait for DNS/network propagation

	requestPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "lookup_feedback",
			"arguments": map[string]interface{}{"query": "test"},
		},
		"id": 1,
	}
	reqBytes, _ := json.Marshal(requestPayload)

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
        body = resp.read().decode()
        print(body)
        print("HTTP_STATUS:" + str(resp.status))
except Exception as e:
    print("ERROR:" + str(e))
    print("HTTP_STATUS:0")
`, string(reqBytes), serviceURL, CapabilityHeader, inst.Capability)

	stdout, stderr, exitCode, err := dr.Exec(ctx, clientID,
		[]string{"python", "-c", pythonScript})
	t.Logf("Python HTTP response stdout: %s", stdout)
	if stderr != "" {
		t.Logf("Python stderr: %s", stderr)
	}

	if err != nil || exitCode != 0 {
		t.Fatalf("docker exec python: exit=%d err=%v stderr=%s", exitCode, err, stderr)
	}

	// Parse response.
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 2 {
		t.Fatalf("unexpected output: %s", stdout)
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

	// ── Assert the distinctive S1c fixture marker ──────────────────────────
	if !strings.Contains(body, "mcp-feedback-service-S1c-fixture") {
		t.Errorf("response missing marker 'mcp-feedback-service-S1c-fixture': %s", body)
	}

	t.Log("SUCCESS: operator pack e2e — MCP feedback service returned fixture marker")

	// ── Negative: invoke with undeclared tool should fail ──────────────────
	t.Run("undeclared-tool-denied", func(t *testing.T) {
		requestPayload2 := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name":      "secret_backdoor",
				"arguments": map[string]interface{}{},
			},
			"id": 2,
		}
		reqBytes2, _ := json.Marshal(requestPayload2)

		pythonScript2 := fmt.Sprintf(`
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
        body = resp.read().decode()
        print(body)
        print("HTTP_STATUS:" + str(resp.status))
except Exception as e:
    print("ERROR:" + str(e))
    print("HTTP_STATUS:0")
`, string(reqBytes2), serviceURL, CapabilityHeader, inst.Capability)

		stdout2, stderr2, exitCode2, err2 := dr.Exec(ctx, clientID,
			[]string{"python", "-c", pythonScript2})
		if err2 != nil || exitCode2 != 0 {
			t.Fatalf("docker exec python: exit=%d err=%v stderr=%s", exitCode2, err2, stderr2)
		}

		lines2 := strings.Split(strings.TrimSpace(stdout2), "\n")
		body2 := strings.Join(lines2[:len(lines2)-1], "\n")

		if stderr2 != "" {
			t.Logf("Python stderr: %s", stderr2)
		}
		t.Logf("Undeclared tool response body: %s", body2)

		// The handler should return an error for the undeclared tool.
		if !strings.Contains(body2, "error") {
			t.Errorf("expected error for undeclared tool, got body: %s", body2)
		}
	})

	// ── Zero-orphans check after cleanup ───────────────────────────────────
	_ = dr.Stop(ctx, clientID, nil)
	_ = dr.Remove(ctx, clientID, true)

	if err := reg.WorkflowTerminal(ctx, workflowID); err != nil {
		t.Fatalf("WorkflowTerminal: %v", err)
	}
	if removed, err := ReconcileOrphanServiceNetworks(ctx, dr, workflowID); err != nil {
		t.Logf("Warning: ReconcileOrphanServiceNetworks: %v", err)
	} else if removed > 0 {
		t.Logf("ReconcileOrphanServiceNetworks removed %d orphan network(s)", removed)
	}

	time.Sleep(2 * time.Second)

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

// packMCPFeedbackService packs the service fixture into a Docker image and
// returns the image digest.
func packMCPFeedbackService(t *testing.T, ctx context.Context, repoRoot, harnessPath, tag string) (string, error) {
	t.Helper()

	fixtureDir := filepath.Join(repoRoot, "test", "e2e", "fixtures", "mcp-feedback-service")
	if _, err := os.Stat(filepath.Join(fixtureDir, "agent.yaml")); os.IsNotExist(err) {
		return "", fmt.Errorf("service fixture not found at %s", fixtureDir)
	}

	projectDir := t.TempDir()
	if err := copyDir(fixtureDir, projectDir); err != nil {
		return "", fmt.Errorf("copy fixture: %w", err)
	}

	cfg := pack.BuildConfig{
		ProjectDir:      projectDir,
		Runtime:         pack.RuntimePython,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		HarnessPath:     harnessPath,
		SDKDir:          filepath.Join(repoRoot, "python", "agentpaas_sdk"),
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
		ImageTag:        tag,
	}

	result, err := pack.BuildImage(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("BuildImage(service): %w", err)
	}

	return result.ImageDigest, nil
}

// packMCPFeedbackClient packs the client fixture into a Docker image and
// returns the image digest. The client workflow.yaml references the service
// by bundle_digest.
func packMCPFeedbackClient(t *testing.T, ctx context.Context, repoRoot, harnessPath, svcDigest, tag string) (string, error) {
	t.Helper()

	fixtureDir := filepath.Join(repoRoot, "test", "e2e", "fixtures", "mcp-feedback-client")
	if _, err := os.Stat(filepath.Join(fixtureDir, "agent.yaml")); os.IsNotExist(err) {
		return "", fmt.Errorf("client fixture not found at %s", fixtureDir)
	}

	projectDir := t.TempDir()
	if err := copyDir(fixtureDir, projectDir); err != nil {
		return "", fmt.Errorf("copy fixture: %w", err)
	}

	// Update workflow.yaml with the actual service digest.
	wfPath := filepath.Join(projectDir, "workflow.yaml")
	wfContent, err := os.ReadFile(wfPath)
	if err != nil {
		return "", fmt.Errorf("read workflow.yaml: %w", err)
	}
	updated := strings.Replace(string(wfContent),
		"bundle_digest: sha256:placeholder",
		"bundle_digest: "+svcDigest, 1)
	if err := os.WriteFile(wfPath, []byte(updated), 0600); err != nil {
		return "", fmt.Errorf("write workflow.yaml: %w", err)
	}

	cfg := pack.BuildConfig{
		ProjectDir:      projectDir,
		Runtime:         pack.RuntimePython,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		HarnessPath:     harnessPath,
		SDKDir:          filepath.Join(repoRoot, "python", "agentpaas_sdk"),
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
		ImageTag:        tag,
	}

	result, err := pack.BuildImage(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("BuildImage(client): %w", err)
	}

	return result.ImageDigest, nil
}

// waitForServiceReady polls until the service reaches StateReady.
func waitForServiceReady(ctx context.Context, t *testing.T, reg *ServiceRegistry, workflowID, serviceID string, timeout time.Duration) (*ServiceInstance, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var inst *ServiceInstance
	for time.Now().Before(deadline) {
		var err error
		inst, err = reg.Get(workflowID, serviceID)
		if err != nil {
			return nil, err
		}
		if inst.State == StateReady {
			return inst, nil
		}
		if inst.State == StateFailed {
			return inst, fmt.Errorf("service entered FAILED state: %s", inst.LastError)
		}
		time.Sleep(2 * time.Second)
	}
	return inst, fmt.Errorf("timed out waiting for service %s (state=%s)", serviceID, inst.State)
}

// findRepoRoot walks up from the working directory to find go.mod.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	cmd := exec.Command("cp", "-R", src, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cp -R %s %s: %w\n%s", src, dst, err, out)
	}
	return nil
}
