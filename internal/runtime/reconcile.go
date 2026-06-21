package runtime

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotOwned is returned when an operation targets a resource that is not
// owned by AgentPaaS.
var ErrNotOwned = errors.New("resource is not owned by AgentPaaS")

// ReconcileAfterCrash performs startup reconciliation after a daemon crash.
// It discovers all AgentPaaS-owned containers via the managed-by label and
// removes any agent container whose gateway is absent (stopped or removed)
// and any MCP sidecar whose owning agent is absent.
//
// The reconciliation respects the following rules:
//   - Only resources with agentpaas.managed-by=agentpaas are considered
//   - Only agent and MCP containers are removed
//   - An agent container is removed only if NO running gateway container
//     (agentpaas.resource-type=gateway) exists for the same run ID
//   - An MCP container is removed only if NO running agent container
//     (agentpaas.resource-type=agent) exists for the same run ID
//   - Unrelated Docker resources are never touched
//
// ReconcileAfterCrash returns a summary of actions taken (containers removed).
func ReconcileAfterCrash(ctx context.Context, driver RuntimeDriver) ([]ContainerID, error) {
	if driver == nil {
		return nil, errors.New("reconcile: runtime driver is nil")
	}

	// List all AgentPaaS-owned containers.
	containers, err := driver.ListContainers(ctx, LabelManagedBy+"="+ManagedByValue)
	if err != nil {
		return nil, fmt.Errorf("reconcile: list owned containers: %w", err)
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
			continue // skip containers without a run ID label
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

	// For each run group: if no running gateway exists, remove all agents;
	// if no running agent exists, remove all MCP sidecars.
	var removed []ContainerID
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
						return removed, fmt.Errorf("reconcile: remove agent %s (run %s): %w", shortID, runID, err)
					}
					removed = append(removed, cid)
				}
			}
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
						return removed, fmt.Errorf("reconcile: remove MCP sidecar %s (run %s): %w", shortID, runID, err)
					}
					removed = append(removed, cid)
				}
			}
		}
	}

	return removed, nil
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
