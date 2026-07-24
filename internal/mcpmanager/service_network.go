package mcpmanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

const (
	// networkAliasLength is the byte length for random network aliases.
	networkAliasLength = 16
)

// serviceNetworkState tracks the network resources for a workflow's
// MCP service network. Protected by ServiceRegistry-level locking.
type serviceNetworkState struct {
	mu sync.RWMutex

	// NetworkID is the Docker network ID for the workflow-scoped
	// internal service network.
	NetworkID runtime.NetworkID

	// NetworkAlias is the random DNS alias for internal routing.
	NetworkAlias string

	// attachedContainers tracks which containers are attached to this
	// network (container ID -> true).
	attachedContainers map[runtime.ContainerID]bool
}

// newServiceNetworkState creates an empty network state.
func newServiceNetworkState() *serviceNetworkState {
	return &serviceNetworkState{
		attachedContainers: make(map[runtime.ContainerID]bool),
	}
}

// generateNetworkAlias produces a cryptographically random unguessable
// network alias. This is never exposed to agent/Python code.
func generateNetworkAlias() (string, error) {
	var b [networkAliasLength]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("mcpmanager: generate network alias: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// EnsureServiceNetwork creates the workflow-scoped internal MCP service
// network if it does not already exist. Returns the network ID and alias.
// Calling this multiple times for the same workflow is idempotent.
func EnsureServiceNetwork(ctx context.Context, driver runtime.RuntimeDriver, workflowID string, state *serviceNetworkState) (runtime.NetworkID, string, error) {
	state.mu.Lock()
	defer state.mu.Unlock()

	// If we already have a network, return it (idempotent).
	if state.NetworkID != "" {
		return state.NetworkID, state.NetworkAlias, nil
	}

	if driver == nil {
		return "", "", fmt.Errorf("mcpmanager: ensure service network: no runtime driver configured")
	}

	// Generate a random alias — never accepted from user input.
	alias, err := generateNetworkAlias()
	if err != nil {
		return "", "", err
	}

	spec := runtime.NetworkSpec{
		Name: fmt.Sprintf("agentpaas-mcp-svc-%s", workflowID),
		// Internal: true — no external route, no internet access.
		Internal: true,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeMCPServiceNet,
			runtime.LabelWorkflowID:   workflowID,
		},
	}

	netID, err := driver.CreateNetwork(ctx, spec)
	if err != nil {
		return "", "", fmt.Errorf("mcpmanager: create service network: %w", err)
	}

	state.NetworkID = netID
	state.NetworkAlias = alias

	return netID, alias, nil
}

// AttachToServiceNetwork attaches a container to the service network.
// This is idempotent — attaching a container that is already on the
// network succeeds without error.
func AttachToServiceNetwork(ctx context.Context, driver runtime.RuntimeDriver, containerID runtime.ContainerID, state *serviceNetworkState) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.NetworkID == "" {
		return fmt.Errorf("mcpmanager: attach to service network: network not created")
	}
	if driver == nil {
		return fmt.Errorf("mcpmanager: attach to service network: no runtime driver configured")
	}

	// Idempotent: skip if already attached.
	if state.attachedContainers[containerID] {
		return nil
	}

	if err := driver.AttachNetwork(ctx, containerID, state.NetworkID); err != nil {
		return fmt.Errorf("mcpmanager: attach container %q to network %q: %w",
			containerID, state.NetworkID, err)
	}

	state.attachedContainers[containerID] = true
	return nil
}

// DetachFromServiceNetwork disconnects a container from the service
// network. Returns true if the container was attached (and is now
// detached). Idempotent: detaching a container that is not attached
// succeeds without error.
func DetachFromServiceNetwork(ctx context.Context, driver runtime.RuntimeDriver, containerID runtime.ContainerID, state *serviceNetworkState) bool {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.NetworkID == "" {
		return false
	}
	if !state.attachedContainers[containerID] {
		return false
	}

	// Best-effort detach via driver.
	if driver != nil {
		_ = driver.DetachNetwork(ctx, containerID, state.NetworkID)
	}

	delete(state.attachedContainers, containerID)
	return true
}

// RemainingAttachments returns the count of containers still attached
// to the service network.
func (s *serviceNetworkState) RemainingAttachments() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.attachedContainers)
}

// RemoveServiceNetwork removes the service network via the driver and
// clears all state. Only call this when no containers remain attached.
func RemoveServiceNetwork(ctx context.Context, driver runtime.RuntimeDriver, state *serviceNetworkState) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.NetworkID == "" {
		return nil
	}

	// Safety check: refuse to remove if containers are still attached.
	if len(state.attachedContainers) > 0 {
		return fmt.Errorf("mcpmanager: remove service network: %d containers still attached", len(state.attachedContainers))
	}

	if driver != nil {
		if err := driver.RemoveNetwork(ctx, state.NetworkID); err != nil {
			return fmt.Errorf("mcpmanager: remove service network %q: %w", state.NetworkID, err)
		}
	}

	state.NetworkID = ""
	state.NetworkAlias = ""
	state.attachedContainers = make(map[runtime.ContainerID]bool)
	return nil
}

// ReconcileOrphanServiceNetworks discovers service networks by label,
// verifies they have attached containers, and removes orphan networks
// that have zero containers. Returns the count of removed networks.
func ReconcileOrphanServiceNetworks(ctx context.Context, driver runtime.RuntimeDriver, workflowID string) (removed int, err error) {
	if driver == nil {
		return 0, nil
	}

	networks, err := driver.ListNetworks(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCPServiceNet,
		runtime.LabelWorkflowID+"="+workflowID,
	)
	if err != nil {
		return 0, fmt.Errorf("mcpmanager: reconcile orphan networks: list: %w", err)
	}

	for _, n := range networks {
		// Check if any containers are attached to this network.
		containers, listErr := driver.ListContainers(ctx)
		if listErr != nil {
			continue // best effort
		}

		hasAttachment := false
		for _, c := range containers {
			networks, inspectErr := driver.InspectContainerNetworks(ctx, runtime.ContainerID(c.ID))
			if inspectErr != nil {
				continue
			}
			for _, cn := range networks {
				if cn.ID == n.ID {
					hasAttachment = true
					break
				}
			}
			if hasAttachment {
				break
			}
		}

		if !hasAttachment {
			// Orphan: remove it.
			if removeErr := driver.RemoveNetwork(ctx, runtime.NetworkID(n.ID)); removeErr != nil {
				continue // best effort
			}
			removed++
		}
	}

	return removed, nil
}
