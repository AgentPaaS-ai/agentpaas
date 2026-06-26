package daemon

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/parvezsyed/agentpaas/internal/dashboard"
	"github.com/parvezsyed/agentpaas/internal/runtime"
)

func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker client unavailable: %v", err)
	}
	defer func() { _ = cli.Close() }()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("Docker daemon unavailable: %v", err)
	}
}

func TestDockerResourceManager_EmptyFilter(t *testing.T) {
	mgr := NewDockerResourceManager(nil)
	agents, err := mgr.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if agents == nil {
		t.Fatal("ListAgents() = nil, want empty slice")
	}
	if len(agents) != 0 {
		t.Fatalf("len(agents) = %d, want 0", len(agents))
	}
}

func TestDockerResourceManager_ListAgents(t *testing.T) {
	requireDocker(t)

	ctx := context.Background()
	dr, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "test-run-123"
	cleanupContainers := []runtime.ContainerID{}
	cleanupNetworks := []runtime.NetworkID{}
	defer func() {
		for _, id := range cleanupContainers {
			_ = dr.Remove(ctx, id, true)
		}
		for _, nid := range cleanupNetworks {
			_ = dr.RemoveNetwork(ctx, nid)
		}
	}()

	internalNetName := runtime.NetworkName("internal", runID)
	internalNetID, err := dr.CreateNetwork(ctx, runtime.NetworkSpec{
		Name:     internalNetName,
		Internal: true,
		Labels:   runtime.Labels(runtime.ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork() failed: %v", err)
	}
	cleanupNetworks = append(cleanupNetworks, internalNetID)

	agentID, err := dr.Create(ctx, runtime.ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     runtime.Labels(runtime.ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	cleanupContainers = append(cleanupContainers, agentID)

	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}

	time.Sleep(2 * time.Second)

	mgr := NewDockerResourceManager(dr)
	agents, err := mgr.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if agents == nil {
		t.Fatal("ListAgents() = nil, want non-nil slice")
	}

	var found bool
	for _, a := range agents {
		if a.ID != runID {
			continue
		}
		found = true
		if a.Status != "running" {
			t.Errorf("agent status = %q, want %q", a.Status, "running")
		}
		if a.ContainerID == "" {
			t.Error("agent ContainerID is empty")
		}
	}
	if !found {
		t.Fatalf("ListAgents() missing agent with ID %q; got %d agents: %v", runID, len(agents), formatAgentIDs(agents))
	}
}

func formatAgentIDs(agents []dashboard.AgentResource) string {
	ids := make([]string, len(agents))
	for i, a := range agents {
		ids[i] = fmt.Sprintf("%q", a.ID)
	}
	return fmt.Sprintf("[%s]", fmt.Sprint(ids))
}
