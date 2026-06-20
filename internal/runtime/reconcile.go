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
// removes any agent container whose gateway is absent (stopped or removed).
//
// The reconciliation respects the following rules:
//   - Only resources with agentpaas.managed-by=agentpaas are considered
//   - Only agent containers (agentpaas.resource-type=agent) are removed
//   - An agent container is removed only if NO running gateway container
//     (agentpaas.resource-type=gateway) exists for the same run ID
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
		}
	}

	// For each run group: if no running gateway exists, remove all agents.
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
	}

	return removed, nil
}

// IsUnrelatedContainer returns true if the given container is NOT owned by
// AgentPaaS. Used to verify reconciliation never touches unrelated resources.
func IsUnrelatedContainer(info ContainerInfo) bool {
	return !IsOwned(info.Labels)
}