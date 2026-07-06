package runtime

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotOwned is returned when an operation targets a resource that is not
// owned by AgentPaaS.
var ErrNotOwned = errors.New("resource is not owned by AgentPaaS")

// ReconcileResult summarizes the actions taken during crash reconciliation.
type ReconcileResult struct {
	// RemovedContainers is the list of container IDs that were removed.
	RemovedContainers []ContainerID
	// RemovedNetworks is the list of network IDs that were removed.
	RemovedNetworks []NetworkID
}

// ReconcileAfterCrash performs startup reconciliation after a daemon crash.
// It discovers all AgentPaaS-owned containers and networks via the managed-by
// label and removes orphaned resources whose peer is absent.
//
// The reconciliation respects the following rules:
//   - Only resources with agentpaas.managed-by=agentpaas are considered
//   - Agent, gateway, and MCP containers are cleaned up
//   - An agent container is removed only if NO running gateway container
//     (agentpaas.resource-type=gateway) exists for the same run ID
//   - A gateway container is removed only if NO running agent container
//     (agentpaas.resource-type=agent) exists for the same run ID
//   - An MCP container is removed only if NO running agent container exists
//     for the same run ID
//   - Per-run networks (internal/egress) are removed when their run has no
//     remaining containers
//   - Audit directories (state/runs/<runID>/) are NOT removed — they are
//     forensic evidence on the filesystem, not Docker resources
//   - Unrelated Docker resources are never touched
//
// ReconcileAfterCrash returns a summary of actions taken.
func ReconcileAfterCrash(ctx context.Context, driver RuntimeDriver) (ReconcileResult, error) {
	if driver == nil {
		return ReconcileResult{}, errors.New("reconcile: runtime driver is nil")
	}

	// List all AgentPaaS-owned containers.
	containers, err := driver.ListContainers(ctx, LabelManagedBy+"="+ManagedByValue)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile: list owned containers: %w", err)
	}

	// Group containers by run ID.
	type runGroup struct {
		agents   []ContainerInfo
		gateways []ContainerInfo
		mcps     []ContainerInfo
	}
	groups := make(map[string]*runGroup)

	for _, c := range containers {
		if c.RunID == "" {
			continue
		}
		g, ok := groups[c.RunID]
		if !ok {
			g = &runGroup{}
			groups[c.RunID] = g
		}
		switch c.ResourceType {
		case ResourceTypeAgent:
			g.agents = append(g.agents, c)
		case ResourceTypeGateway:
			g.gateways = append(g.gateways, c)
		case ResourceTypeMCP:
			g.mcps = append(g.mcps, c)
		}
	}

	var result ReconcileResult

	// Track which run IDs have no remaining containers after cleanup.
	emptyRuns := make(map[string]bool)

	// Phase 1: Remove orphaned containers.
	for runID, g := range groups {
		hasRunningGateway := false
		for _, gw := range g.gateways {
			if gw.Status == ContainerStatusRunning {
				hasRunningGateway = true
				break
			}
		}

		if !hasRunningGateway {
			for _, agent := range g.agents {
				if agent.Status == ContainerStatusRunning || agent.Status == ContainerStatusStopped {
					cid := ContainerID(agent.ID)
					if err := driver.Remove(ctx, cid, true); err != nil {
						shortID := agent.ID
						if len(shortID) > 12 {
							shortID = shortID[:12]
						}
						return result, fmt.Errorf("reconcile: remove agent %s (run %s): %w", shortID, runID, err)
					}
					result.RemovedContainers = append(result.RemovedContainers, cid)
				}
			}
			g.agents = nil
		}

		hasRunningAgent := false
		for _, agent := range g.agents {
			if agent.Status == ContainerStatusRunning {
				hasRunningAgent = true
				break
			}
		}

		if !hasRunningAgent {
			for _, mcp := range g.mcps {
				if mcp.Status == ContainerStatusRunning || mcp.Status == ContainerStatusStopped {
					cid := ContainerID(mcp.ID)
					if err := driver.Remove(ctx, cid, true); err != nil {
						shortID := mcp.ID
						if len(shortID) > 12 {
							shortID = shortID[:12]
						}
						return result, fmt.Errorf("reconcile: remove MCP sidecar %s (run %s): %w", shortID, runID, err)
					}
					result.RemovedContainers = append(result.RemovedContainers, cid)
				}
			}
			g.mcps = nil

			// Remove orphaned gateway containers (B20-T13).
			for _, gw := range g.gateways {
				if gw.Status == ContainerStatusRunning || gw.Status == ContainerStatusStopped {
					cid := ContainerID(gw.ID)
					if err := driver.Remove(ctx, cid, true); err != nil {
						shortID := gw.ID
						if len(shortID) > 12 {
							shortID = shortID[:12]
						}
						return result, fmt.Errorf("reconcile: remove gateway %s (run %s): %w", shortID, runID, err)
					}
					result.RemovedContainers = append(result.RemovedContainers, cid)
				}
			}
			g.gateways = nil

			if len(g.agents) == 0 && len(g.gateways) == 0 && len(g.mcps) == 0 {
				emptyRuns[runID] = true
			}
		}
	}

	// Phase 2: Remove orphaned per-run networks (B20-T13).
	networks, netErr := driver.ListNetworks(ctx, LabelManagedBy+"="+ManagedByValue)
	if netErr != nil {
		return result, fmt.Errorf("reconcile: list owned networks: %w", netErr)
	}

	for _, net := range networks {
		runID := net.Labels[LabelRunID]
		if runID == "" {
			continue
		}
		_, inGroups := groups[runID]
		if !inGroups || emptyRuns[runID] {
			nid := NetworkID(net.ID)
			if err := driver.RemoveNetwork(ctx, nid); err != nil {
				continue
			}
			result.RemovedNetworks = append(result.RemovedNetworks, nid)
		}
	}

	return result, nil
}

// MCPContainerInfo describes an MCP sidecar discovered during reconciliation.
type MCPContainerInfo struct {
	ContainerID ContainerID
	ServerID    string
	RunID       string
}

// ReconcileMCPServers discovers MCP-labeled containers and returns their
// server IDs and run IDs for lifecycle reconciliation after daemon restart.
func ReconcileMCPServers(ctx context.Context, driver RuntimeDriver) ([]MCPContainerInfo, error) {
	if driver == nil {
		return nil, errors.New("reconcile MCP servers: runtime driver is nil")
	}

	containers, err := driver.ListContainers(
		ctx,
		LabelManagedBy+"="+ManagedByValue,
		LabelResourceType+"="+ResourceTypeMCP,
	)
	if err != nil {
		return nil, fmt.Errorf("reconcile MCP servers: list containers: %w", err)
	}

	infos := make([]MCPContainerInfo, 0, len(containers))
	for _, c := range containers {
		if c.ResourceType != ResourceTypeMCP && c.Labels[LabelResourceType] != ResourceTypeMCP {
			continue
		}
		serverID := c.Labels[LabelMCPServerID]
		runID := c.RunID
		if runID == "" {
			runID = c.Labels[LabelRunID]
		}
		if serverID == "" || runID == "" {
			continue
		}
		infos = append(infos, MCPContainerInfo{
			ContainerID: ContainerID(c.ID),
			ServerID:    serverID,
			RunID:       runID,
		})
	}
	return infos, nil
}

// IsUnrelatedContainer returns true if the given container is NOT owned by
// AgentPaaS. Used to verify reconciliation never touches unrelated resources.
func IsUnrelatedContainer(info ContainerInfo) bool {
	return !IsOwned(info.Labels)
}
