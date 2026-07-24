package mcpmanager

import (
	"context"
	"strings"
	"testing"
)

// TestCreateServiceContainer_BundleDigest tests that when BundleDigest is set
// and no test defaults are active, the container uses a bare sha256:<digest>
// image ref and no forced command (uses image entrypoint).
func TestCreateServiceContainer_BundleDigest(t *testing.T) {
	driver := newFakeRuntimeDriver()
	reg := NewServiceRegistry(driver, nil, nil)

	// Create instance with a bundle digest.
	inst := NewServiceInstance("wf-1", "svc-echo", "echo-tools", "1.0.0",
		"sha256:abc123def456", []string{"echo", "ping"})
	inst.RunID = "run-1"
	inst.State = StateStarting

	containerID, err := reg.createServiceContainer(context.Background(), inst, 1, "cap-test-token")
	if err != nil {
		t.Fatalf("createServiceContainer: %v", err)
	}
	if containerID == "" {
		t.Fatal("empty container ID")
	}

	// Check the spec that was passed to the driver.
	spec := driver.createdSpec(containerID)
	if spec.Image == "" {
		t.Fatal("no container spec captured")
	}
	if !strings.HasPrefix(spec.Image, "sha256:") {
		t.Fatalf("image must be bare sha256: digest, got %q", spec.Image)
	}
	if !strings.Contains(spec.Image, "abc123") {
		t.Fatalf("image must contain bundle digest, got %q", spec.Image)
	}
	if spec.Command != nil && len(spec.Command) > 0 {
		t.Fatalf("command must be nil/empty (use image entrypoint), got %v", spec.Command)
	}

	// Env should contain capability, declared tools, kind, agent path,
	// the invoke API address (non-conflicting with MCP bridge), and the
	// MCP HTTP bridge listen address.
	env := findEnv(spec.Env, "AGENTPAAS_MCP_CAPABILITY")
	if env != "cap-test-token" {
		t.Fatalf("AGENTPAAS_MCP_CAPABILITY = %q", env)
	}
	if f := findEnv(spec.Env, "AGENTPAAS_MCP_DECLARED_TOOLS"); f != "echo,ping" {
		t.Fatalf("AGENTPAAS_MCP_DECLARED_TOOLS = %q", f)
	}
	if f := findEnv(spec.Env, "AGENTPAAS_AGENT_KIND"); f != "mcp_service" {
		t.Fatalf("AGENTPAAS_AGENT_KIND = %q", f)
	}
	if f := findEnv(spec.Env, "AGENTPAAS_AGENT_PATH"); f != "/app/main.py" {
		t.Fatalf("AGENTPAAS_AGENT_PATH = %q, want /app/main.py", f)
	}
	if f := findEnv(spec.Env, "AGENTPAAS_ADDR"); f != "127.0.0.1:8090" {
		t.Fatalf("AGENTPAAS_ADDR = %q, want 127.0.0.1:8090", f)
	}
	if f := findEnv(spec.Env, "AGENTPAAS_MCP_HTTP_ADDR"); f != "0.0.0.0:8080" {
		t.Fatalf("AGENTPAAS_MCP_HTTP_ADDR = %q, want 0.0.0.0:8080", f)
	}
}

// TestCreateServiceContainer_DefaultsFallback tests that when BundleDigest is
// empty and no test defaults are set, the original fallback (agentpaas-mcp-service
// image + sleep infinity) is used.
func TestCreateServiceContainer_DefaultsFallback(t *testing.T) {
	driver := newFakeRuntimeDriver()
	reg := NewServiceRegistry(driver, nil, nil)

	inst := NewServiceInstance("wf-2", "svc-no-digest", "no-digest-tools", "1.0.0",
		"", []string{"echo"})
	inst.RunID = "run-2"
	inst.State = StateStarting

	containerID, err := reg.createServiceContainer(context.Background(), inst, 1, "cap-token")
	if err != nil {
		t.Fatalf("createServiceContainer: %v", err)
	}
	if containerID == "" {
		t.Fatal("empty container ID")
	}

	spec := driver.createdSpec(containerID)
	if spec.Image != "agentpaas-mcp-service:latest" {
		t.Fatalf("image = %q, want agentpaas-mcp-service:latest", spec.Image)
	}
	if len(spec.Command) == 0 || spec.Command[0] != "sleep" {
		t.Fatalf("command = %v, want sleep infinity", spec.Command)
	}
}

// TestCreateServiceContainer_DefaultsOverride tests that SetServiceContainerDefaults
// takes precedence over BundleDigest.
func TestCreateServiceContainer_DefaultsOverride(t *testing.T) {
	driver := newFakeRuntimeDriver()
	reg := NewServiceRegistry(driver, nil, nil)
	reg.SetServiceContainerDefaults("python:3-alpine", []string{"python", "/mock/server.py"})

	inst := NewServiceInstance("wf-3", "svc-override", "override-tools", "1.0.0",
		"sha256:abc123", []string{"echo"})
	inst.RunID = "run-3"
	inst.State = StateStarting

	containerID, err := reg.createServiceContainer(context.Background(), inst, 1, "cap-token")
	if err != nil {
		t.Fatalf("createServiceContainer: %v", err)
	}
	if containerID == "" {
		t.Fatal("empty container ID")
	}

	spec := driver.createdSpec(containerID)
	if spec.Image != "python:3-alpine" {
		t.Fatalf("image = %q, want python:3-alpine (test default)", spec.Image)
	}
	if len(spec.Command) != 2 || spec.Command[0] != "python" {
		t.Fatalf("command = %v, want test default", spec.Command)
	}
}

// findEnv finds the value for a key in an env slice of "KEY=val" entries.
func findEnv(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}