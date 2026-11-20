package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"gopkg.in/yaml.v3"
)

// alwaysPromotedChecker is a PromotionChecker that always returns promoted.
// Used in the production daemon path where promotion gates are not yet
// enforced at the registry level (B33 chunk 4 lazy-init).
type alwaysPromotedChecker struct{}

func (c *alwaysPromotedChecker) IsPromoted(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}

// alwaysReadyProbe is a ReadinessProbe that always returns ready.
// Used in the production daemon path where readiness gates are deferred
// to B33 chunk 5 (protocol initialize after container start).
type alwaysReadyProbe struct{}

func (p *alwaysReadyProbe) Check(_ context.Context, _ *mcpmanager.ServiceInstance) (bool, error) {
	return true, nil
}

// ensureMCPRegistry lazily initializes the MCP service registry on the
// controlServer. Safe to call multiple times; returns immediately if the
// registry is already initialized.
//
// Uses the DockerRuntime as the RuntimeDriver and always-promoted /
// always-ready stubs for promotion and readiness checking (deferred to
// later chunks).
func (s *controlServer) ensureMCPRegistry() error {
	if s.mcpRegistry != nil {
		return nil
	}
	rt, err := s.getOrCreateRuntime()
	if err != nil {
		return fmt.Errorf("ensure MCP registry: %w", err)
	}
	s.mcpRegistry = mcpmanager.NewServiceRegistry(rt, &alwaysPromotedChecker{}, &alwaysReadyProbe{})
	return nil
}

// loadWorkflowServices reads and parses workflow.yaml from the deployed
// agent directory. Returns the declared service bindings, or nil if
// workflow.yaml does not exist or has no services section.
func loadWorkflowServices(deployedDir string) ([]pack.ServiceBinding, error) {
	wfPath := filepath.Join(deployedDir, "workflow.yaml")
	data, err := os.ReadFile(wfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read workflow.yaml: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow.yaml: %w", err)
	}
	if len(wf.Services) == 0 {
		return nil, nil
	}
	return wf.Services, nil
}

// prepareMCPBindingsForRun ensures managed MCP services for a workflow run
// and writes the binding sidecar file to the gateway config directory.
//
// Returns:
//   - (path, true, nil)  — services were declared and provisioned successfully
//   - ("", false, nil)   — no services declared (backward compat, no-op)
//   - ("", false, error) — services declared but provisioning failed
//
// The runID is used as the workflowID for per-run service lifecycle isolation.
func (s *controlServer) prepareMCPBindingsForRun(ctx context.Context, runID, deployedDir, gatewayConfigDir string) (string, bool, error) {
	services, err := loadWorkflowServices(deployedDir)
	if err != nil {
		return "", false, fmt.Errorf("load workflow services: %w", err)
	}
	if len(services) == 0 {
		return "", false, nil
	}

	if err := s.ensureMCPRegistry(); err != nil {
		return "", false, fmt.Errorf("init MCP registry: %w", err)
	}

	// Use runID as the workflowID for per-run service lifecycle isolation.
	if err := s.EnsureWorkflowMCPServices(ctx, runID, services); err != nil {
		return "", false, fmt.Errorf("ensure MCP services: %w", err)
	}

	mcpPath := filepath.Join(gatewayConfigDir, "mcp-bindings.json")
	if err := s.mcpRegistry.WriteBindingSidecar(mcpPath, runID); err != nil {
		return "", false, fmt.Errorf("write MCP binding sidecar: %w", err)
	}

	return mcpPath, true, nil
}

// cleanupMCPForRun best-effort cleans up MCP service resources associated
// with the given runID. Safe to call when the MCP registry is nil (no-op).
// Intended to be called from finalizeRun for service lifecycle cleanup.
func (s *controlServer) cleanupMCPForRun(runID string) {
	if s.mcpRegistry == nil {
		return
	}
	if err := s.mcpRegistry.WorkflowTerminal(context.Background(), runID); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: cleanup MCP for run %s: %v\n", runID, err)
	}
}
