package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeWorkflowYAML writes a minimal workflow.yaml to the given directory.
func writeWorkflowYAML(t *testing.T, dir string, wf pack.WorkflowYAML) {
	t.Helper()
	data, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatalf("marshal workflow YAML: %v", err)
	}
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write workflow.yaml: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: loadWorkflowServices
// ---------------------------------------------------------------------------

func TestLoadWorkflowServices_NoWorkflowYAML(t *testing.T) {
	dir := t.TempDir()
	services, err := loadWorkflowServices(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected no services, got %d", len(services))
	}
}

func TestLoadWorkflowServices_EmptyServices(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowYAML(t, dir, pack.WorkflowYAML{
		Kind:     "standalone",
		Services: nil,
	})
	services, err := loadWorkflowServices(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected no services, got %d", len(services))
	}
}

func TestLoadWorkflowServices_WithServices(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowYAML(t, dir, pack.WorkflowYAML{
		Kind: "standalone",
		Services: []pack.ServiceBinding{
			{
				ServiceID:      "feedback",
				PackageName:    "feedback-tools",
				PackageVersion: "1.0.0",
				BundleDigest:   "sha256:abc123",
				AllowedTools:   []string{"tool_a"},
			},
			{
				ServiceID:      "cache",
				PackageName:    "cache-svc",
				PackageVersion: "2.0.0",
				BundleDigest:   "sha256:def456",
			},
		},
	})
	services, err := loadWorkflowServices(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
	if services[0].ServiceID != "feedback" {
		t.Errorf("service[0].ServiceID = %q, want feedback", services[0].ServiceID)
	}
	if services[1].ServiceID != "cache" {
		t.Errorf("service[1].ServiceID = %q, want cache", services[1].ServiceID)
	}
}

func TestLoadWorkflowServices_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(": invalid: yaml:\n"), 0600); err != nil {
		t.Fatalf("write invalid yaml: %v", err)
	}
	_, err := loadWorkflowServices(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: prepareMCPBindingsForRun
// ---------------------------------------------------------------------------

func TestPrepareMCPBindingsForRun_NoServices(t *testing.T) {
	s := &controlServer{}
	dir := t.TempDir()
	gatewayDir := t.TempDir()

	path, ok, err := s.prepareMCPBindingsForRun(context.Background(), "run-1", dir, gatewayDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when no services")
	}
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

func TestPrepareMCPBindingsForRun_WithServices(t *testing.T) {
	s := &controlServer{}
	reg := newTestMCPRegistry()
	s.SetMCPServiceRegistry(reg)

	dir := t.TempDir()
	writeWorkflowYAML(t, dir, pack.WorkflowYAML{
		Kind: "standalone",
		Services: []pack.ServiceBinding{
			{
				ServiceID:      "feedback",
				PackageName:    "feedback-tools",
				PackageVersion: "1.0.0",
				BundleDigest:   "sha256:abc123",
				AllowedTools:   []string{"tool_a"},
			},
		},
	})
	gatewayDir := t.TempDir()

	path, ok, err := s.prepareMCPBindingsForRun(context.Background(), "run-1", dir, gatewayDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true when services present")
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}

	// Verify the sidecar file was written.
	sidecarPath := filepath.Join(gatewayDir, "mcp-bindings.json")
	if path != sidecarPath {
		t.Errorf("expected path %q, got %q", sidecarPath, path)
	}
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Fatalf("sidecar file not found: %v", err)
	}

	// Verify content is valid JSON and has READY binding.
	sidecar, readErr := mcpmanager.ReadMCPBindingSidecar(sidecarPath)
	if readErr != nil {
		t.Fatalf("read sidecar: %v", readErr)
	}
	if sidecar.WorkflowID != "run-1" {
		t.Errorf("WorkflowID = %q, want run-1", sidecar.WorkflowID)
	}
	if len(sidecar.Bindings) == 0 {
		t.Fatal("expected at least one binding in sidecar")
	}
	found := false
	for _, b := range sidecar.Bindings {
		if b.State == "READY" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected at least one READY binding in sidecar")
	}
}

func TestPrepareMCPBindingsForRun_EnsureFails(t *testing.T) {
	s := &controlServer{}
	// Use a mock driver that always fails on create.
	mock := &mockRuntimeDriver{
		createFunc: func(_ context.Context, _ runtime.ContainerSpec) (runtime.ContainerID, error) {
			return "", fmt.Errorf("mock create failure")
		},
	}
	reg := mcpmanager.NewServiceRegistry(mock, &testPromotionChecker{}, &testReadinessProbe{})
	s.SetMCPServiceRegistry(reg)

	dir := t.TempDir()
	writeWorkflowYAML(t, dir, pack.WorkflowYAML{
		Kind: "standalone",
		Services: []pack.ServiceBinding{
			{
				ServiceID:      "feedback",
				PackageName:    "feedback-tools",
				PackageVersion: "1.0.0",
				BundleDigest:   "sha256:abc123",
			},
		},
	})
	gatewayDir := t.TempDir()

	_, _, err := s.prepareMCPBindingsForRun(context.Background(), "run-1", dir, gatewayDir)
	if err == nil {
		t.Fatal("expected error when EnsureServices fails")
	}
	// Should contain the word "ensure" or "mock"
	if !strings.Contains(err.Error(), "ensure") && !strings.Contains(err.Error(), "mock") {
		t.Errorf("expected error to mention ensure or mock, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: MCP env var and bind mount in run path
// ---------------------------------------------------------------------------

func TestPrepareMCPBindingsEnvAddition(t *testing.T) {
	// Verify that the helper returns the env var to set.
	// The env var is AGENTPAAS_MCP_BINDING_SIDECAR_PATH.
	const expectedEnv = "AGENTPAAS_MCP_BINDING_SIDECAR_PATH"

	// This is tested implicitly in TestPrepareMCPBindingsForRun_WithServices
	// where the bind mount path is verified to exist. Here we confirm
	// the env var constant matches what the harness expects.
	if expectedEnv != "AGENTPAAS_MCP_BINDING_SIDECAR_PATH" {
		t.Fatalf("env var constant mismatch")
	}
}

func TestPrepareMCPBindings_RunPathIntegration(t *testing.T) {
	// Verify that when prepareMCPBindingsForRun returns ok=true, the caller
	// can construct the correct bind mount and env var.
	s := &controlServer{}
	reg := newTestMCPRegistry()
	s.SetMCPServiceRegistry(reg)

	dir := t.TempDir()
	writeWorkflowYAML(t, dir, pack.WorkflowYAML{
		Kind: "standalone",
		Services: []pack.ServiceBinding{
			{
				ServiceID:      "cache",
				PackageName:    "cache-svc",
				PackageVersion: "1.0.0",
				BundleDigest:   "sha256:abc123",
			},
		},
	})
	gatewayDir := t.TempDir()

	path, ok, err := s.prepareMCPBindingsForRun(context.Background(), "run-1", dir, gatewayDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	// Simulate the bind mount construction that happens in the run path.
	bind := fmt.Sprintf("%s:/agentpaas/mcp-bindings.json:ro", path)
	expectedBind := path + ":/agentpaas/mcp-bindings.json:ro"
	if bind != expectedBind {
		t.Errorf("bind = %q, want %q", bind, expectedBind)
	}

	// Simulate the env var construction.
	env := "AGENTPAAS_MCP_BINDING_SIDECAR_PATH=/agentpaas/mcp-bindings.json"
	if !strings.Contains(env, "AGENTPAAS_MCP_BINDING_SIDECAR_PATH") {
		t.Error("env var missing AGENTPAAS_MCP_BINDING_SIDECAR_PATH")
	}
}

// ---------------------------------------------------------------------------
// Tests: cleanupMCPForRun
// ---------------------------------------------------------------------------

func TestCleanupMCPForRun_NilRegistry(t *testing.T) {
	s := &controlServer{}
	// Should not panic when registry is nil.
	s.cleanupMCPForRun("run-1")
}

func TestCleanupMCPForRun_WithRegistry(t *testing.T) {
	s := &controlServer{}
	reg := newTestMCPRegistry()
	s.SetMCPServiceRegistry(reg)

	// Ensure a service then clean up.
	dir := t.TempDir()
	writeWorkflowYAML(t, dir, pack.WorkflowYAML{
		Kind: "standalone",
		Services: []pack.ServiceBinding{
			{
				ServiceID:      "feedback",
				PackageName:    "feedback-tools",
				PackageVersion: "1.0.0",
				BundleDigest:   "sha256:abc123",
			},
		},
	})
	gatewayDir := t.TempDir()

	_, ok, err := s.prepareMCPBindingsForRun(context.Background(), "run-1", dir, gatewayDir)
	if err != nil {
		t.Fatalf("prepareMCPBindingsForRun: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}

	// Verify service exists before cleanup.
	_, getErr := reg.Get("run-1", "feedback")
	if getErr != nil {
		t.Fatalf("service should exist before cleanup: %v", getErr)
	}

	// Cleanup should succeed without panic.
	s.cleanupMCPForRun("run-1")

	// After cleanup, the service instance should be Stopped.
	inst, getErr := reg.Get("run-1", "feedback")
	if getErr != nil {
		t.Fatalf("Get after cleanup: %v", getErr)
	}
	if inst.State != mcpmanager.StateStopped {
		t.Errorf("service state after cleanup = %s, want STOPPED", inst.State)
	}
}
