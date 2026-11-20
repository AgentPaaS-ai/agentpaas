package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// testPromotionChecker always returns promoted.
type testPromotionChecker struct{}

func (c *testPromotionChecker) IsPromoted(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}

// testReadinessProbe always returns ready.
type testReadinessProbe struct{}

func (p *testReadinessProbe) Check(_ context.Context, _ *mcpmanager.ServiceInstance) (bool, error) {
	return true, nil
}

// newTestMCPRegistry creates a ServiceRegistry with a mock runtime driver
// that supports the full container/network lifecycle for unit tests.
func newTestMCPRegistry() *mcpmanager.ServiceRegistry {
	mock := &mockRuntimeDriver{
		createFunc: func(_ context.Context, _ runtime.ContainerSpec) (runtime.ContainerID, error) {
			return runtime.ContainerID("mcp-cid-1"), nil
		},
		startFunc: func(_ context.Context, _ runtime.ContainerID) error {
			return nil
		},
		stopFunc: func(_ context.Context, _ runtime.ContainerID, _ *time.Duration) error {
			return nil
		},
		statusFunc: func(_ context.Context, _ runtime.ContainerID) (runtime.ContainerStatus, error) {
			return runtime.ContainerStatusRunning, nil
		},
		createNetworkFunc: func(_ context.Context, _ runtime.NetworkSpec) (runtime.NetworkID, error) {
			return runtime.NetworkID("mcp-net-1"), nil
		},
		removeNetworkFunc: func(_ context.Context, _ runtime.NetworkID) error {
			return nil
		},
		attachNetworkFunc: func(_ context.Context, _ runtime.ContainerID, _ runtime.NetworkID) error {
			return nil
		},
		inspectNetworkFunc: func(_ context.Context, _ runtime.NetworkID) (runtime.NetworkInfo, error) {
			return runtime.NetworkInfo{ID: "mcp-net-1", Name: "mcp-svc-net", Internal: true}, nil
		},
	}
	return mcpmanager.NewServiceRegistry(mock, &testPromotionChecker{}, &testReadinessProbe{})
}

// ---------------------------------------------------------------------------
// Tests: EnsureWorkflowMCPServices
// ---------------------------------------------------------------------------

func TestEnsureWorkflowMCPServices_NoRegistry(t *testing.T) {
	s := &controlServer{}
	err := s.EnsureWorkflowMCPServices(context.Background(), "wf-1", nil)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if !strings.Contains(err.Error(), "mcp service not enabled") {
		t.Fatalf("expected 'mcp service not enabled' error, got %v", err)
	}
}

func TestEnsureWorkflowMCPServices_DelegatesToRegistry(t *testing.T) {
	s := &controlServer{}
	reg := newTestMCPRegistry()
	s.SetMCPServiceRegistry(reg)

	ctx := context.Background()
	binding := pack.ServiceBinding{
		ServiceID:      "feedback",
		PackageName:    "feedback-tools",
		PackageVersion: "1.0.0",
		BundleDigest:   "sha256:abc123",
		AllowedTools:   []string{"tool_a", "tool_b"},
	}

	err := s.EnsureWorkflowMCPServices(ctx, "wf-1", []pack.ServiceBinding{binding})
	if err != nil {
		t.Fatalf("EnsureWorkflowMCPServices() error = %v", err)
	}

	// Verify the service was created and is READY.
	inst, getErr := reg.Get("wf-1", "feedback")
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if inst.State != mcpmanager.StateReady {
		t.Fatalf("expected READY, got %s", inst.State)
	}
}
