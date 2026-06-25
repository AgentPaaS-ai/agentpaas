package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/parvezsyed/agentpaas/internal/dashboard"
	"github.com/parvezsyed/agentpaas/internal/runtime"
)

// dockerResourceManager implements dashboard.ResourceManager by querying
// the Docker runtime for AgentPaaS-managed containers.
type dockerResourceManager struct {
	rt *runtime.DockerRuntime
}

// NewDockerResourceManager creates a ResourceManager backed by the given
// Docker runtime. The runtime must already be initialized.
func NewDockerResourceManager(rt *runtime.DockerRuntime) *dockerResourceManager {
	return &dockerResourceManager{rt: rt}
}

func (m *dockerResourceManager) ListAgents(ctx context.Context) ([]dashboard.AgentResource, error) {
	if m.rt == nil {
		return []dashboard.AgentResource{}, nil
	}
	containers, err := m.rt.ListContainers(ctx, runtime.LabelResourceType)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	agents := []dashboard.AgentResource{}
	for _, c := range containers {
		if c.ResourceType != runtime.ResourceTypeAgent {
			continue
		}
		agents = append(agents, dashboard.AgentResource{
			ID:          c.RunID,
			Name:        c.Name,
			Status:      c.Status.String(),
			ContainerID: c.ID,
			Health:      c.Status.String(),
			Labels:      c.Labels,
			// approx; Docker doesn't expose Created in ContainerInfo
			CreatedAt: time.Now().UTC(),
		})
	}
	return agents, nil
}

func (m *dockerResourceManager) ListGateways(ctx context.Context) ([]dashboard.GatewayResource, error) {
	if m.rt == nil {
		return []dashboard.GatewayResource{}, nil
	}
	containers, err := m.rt.ListContainers(ctx, runtime.LabelResourceType)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	gateways := []dashboard.GatewayResource{}
	for _, c := range containers {
		if c.ResourceType != runtime.ResourceTypeGateway {
			continue
		}
		gateways = append(gateways, dashboard.GatewayResource{
			ID:          c.RunID,
			AgentID:     c.RunID,
			Status:      c.Status.String(),
			ContainerID: c.ID,
			Health:      c.Status.String(),
			// approx; Docker doesn't expose Created in ContainerInfo
			CreatedAt: time.Now().UTC(),
		})
	}
	return gateways, nil
}

func (m *dockerResourceManager) ListMCPServers(ctx context.Context) ([]dashboard.MCPServerResource, error) {
	if m.rt == nil {
		return []dashboard.MCPServerResource{}, nil
	}
	containers, err := m.rt.ListContainers(ctx, runtime.LabelResourceType)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	servers := []dashboard.MCPServerResource{}
	for _, c := range containers {
		if c.ResourceType != runtime.ResourceTypeMCP {
			continue
		}
		servers = append(servers, dashboard.MCPServerResource{
			ID:          c.RunID,
			AgentID:     c.RunID,
			Status:      c.Status.String(),
			ContainerID: c.ID,
			Health:      c.Status.String(),
			// approx; Docker doesn't expose Created in ContainerInfo
			CreatedAt: time.Now().UTC(),
		})
	}
	return servers, nil
}

var _ dashboard.ResourceManager = (*dockerResourceManager)(nil)