package mcpmanager

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// ---------------------------------------------------------------------------
// HealthSummary
// ---------------------------------------------------------------------------

// MaxRecentFailures caps the number of most-recent failure events in
// HealthSummary.
const MaxRecentFailures = 16

// ServiceHealthSummary exposes readiness, recent bounded failures,
// generation, and lease deadline for a service binding.
type ServiceHealthSummary struct {
	WorkflowID       string              `json:"workflow_id"`
	ServiceBindingID string              `json:"service_binding_id"`
	State            ServiceState        `json:"state"`
	Readiness        string              `json:"readiness"`
	Generation       int64               `json:"generation"`
	LeaseDeadline    time.Time           `json:"lease_deadline,omitempty"`
	RecentFailures   []HealthFailureItem `json:"recent_failures,omitempty"`
	ActiveCalls      int                 `json:"active_calls"`
}

// HealthFailureItem is a single bounded failure entry (no raw bodies).
type HealthFailureItem struct {
	Timestamp  time.Time `json:"timestamp"`
	StatusCode string    `json:"status_code"` // e.g. "mcp_timeout", "mcp_overloaded"
	Reason     string    `json:"reason"`      // redacted error message
	Tool       string    `json:"tool,omitempty"`
}

// WorkflowHealthSummary aggregates health across all services in a workflow.
type WorkflowHealthSummary struct {
	WorkflowID string                 `json:"workflow_id"`
	Services   []ServiceHealthSummary `json:"services"`
	Generated  time.Time              `json:"generated_at"`
}

// healthState tracks recent failures with a bounded ring buffer per service.
type healthState struct {
	mu           sync.Mutex
	failures     []HealthFailureItem // ring buffer, max MaxRecentFailures
	failureNext  int                 // write position in ring buffer
}

func newHealthState() *healthState {
	return &healthState{
		failures: make([]HealthFailureItem, 0, MaxRecentFailures),
	}
}

// recordFailure adds a failure entry, capping at MaxRecentFailures.
func (h *healthState) recordFailure(item HealthFailureItem) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.failures) < MaxRecentFailures {
		h.failures = append(h.failures, item)
	} else {
		// Ring buffer overwrite: keep most recent.
		h.failures[h.failureNext] = item
		h.failureNext = (h.failureNext + 1) % MaxRecentFailures
	}
}

// getFailures returns recent failures in chronological order.
func (h *healthState) getFailures() []HealthFailureItem {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.failures) < MaxRecentFailures {
		result := make([]HealthFailureItem, len(h.failures))
		copy(result, h.failures)
		return result
	}
	// Ring buffer: return entries in order from oldest to newest.
	result := make([]HealthFailureItem, MaxRecentFailures)
	for i := 0; i < MaxRecentFailures; i++ {
		result[i] = h.failures[(h.failureNext+i)%MaxRecentFailures]
	}
	return result
}

// HealthSummary returns a health summary for all services in a workflow.
// When serviceBindingID is non-empty, returns only that service.
func (r *ServiceRegistry) HealthSummary(workflowID, serviceBindingID string) *WorkflowHealthSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summary := &WorkflowHealthSummary{
		WorkflowID: workflowID,
		Generated:  time.Now().UTC(),
	}

	for key, inst := range r.instances {
		inst.mu.RLock()

		if inst.WorkflowID != workflowID {
			inst.mu.RUnlock()
			continue
		}
		if serviceBindingID != "" && inst.ServiceBindingID != serviceBindingID {
			inst.mu.RUnlock()
			continue
		}

		var readiness string
		switch inst.State {
		case StateReady:
			readiness = ReadinessReady
		case StateStarting:
			readiness = ReadinessStarting
		case StateFenced:
			readiness = "fenced"
		case StateUnhealthy, StateFailed:
			readiness = ReadinessUnhealthy
		case StateStopped:
			readiness = ReadinessStopped
		default:
			readiness = "unknown"
		}

		var failures []HealthFailureItem
		if hs, ok := r.healthStates[key]; ok {
			failures = hs.getFailures()
		}
		if failures == nil {
			failures = []HealthFailureItem{}
		}

		activeCalls := 0
		if inst.cancelTracker != nil {
			activeCalls = inst.cancelTracker.Active()
		}

		sh := ServiceHealthSummary{
			WorkflowID:       inst.WorkflowID,
			ServiceBindingID: inst.ServiceBindingID,
			State:            inst.State,
			Readiness:        readiness,
			Generation:       inst.Generation,
			LeaseDeadline:    inst.LeaseDeadline,
			RecentFailures:   failures,
			ActiveCalls:      activeCalls,
		}

		inst.mu.RUnlock()
		summary.Services = append(summary.Services, sh)
	}

	sort.Slice(summary.Services, func(i, j int) bool {
		return summary.Services[i].ServiceBindingID < summary.Services[j].ServiceBindingID
	})

	return summary
}

// RecordHealthFailure records a bounded failure for a service binding.
// This is called from the Router when a call fails with a typed error.
func (r *ServiceRegistry) RecordHealthFailure(workflowID, serviceBindingID, statusCode, reason, tool string) {
	key := workflowID + "/" + serviceBindingID

	r.mu.RLock()
	_, ok := r.instances[key]
	if !ok {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()

	r.mu.Lock()
	hs, ok := r.healthStates[key]
	if !ok {
		hs = newHealthState()
		r.healthStates[key] = hs
	}
	r.mu.Unlock()

	hs.recordFailure(HealthFailureItem{
		Timestamp:  time.Now().UTC(),
		StatusCode: statusCode,
		Reason:     sanitizeLastError(reason),
		Tool:       tool,
	})
}

// ---------------------------------------------------------------------------
// CleanupServiceResources
// ---------------------------------------------------------------------------

// CleanupServiceResources removes a service's container, network attachments,
// network (if empty), and clears capability/endpoint. Safe to call twice
// (idempotent). Returns true if any resources were cleaned up.
//
// Locking: acquires r.mu → inst.mu, then releases locks for slow I/O.
func (r *ServiceRegistry) CleanupServiceResources(ctx context.Context, workflowID, serviceBindingID string) (bool, error) {
	key := workflowID + "/" + serviceBindingID

	r.mu.Lock()
	inst, ok := r.instances[key]
	if !ok {
		r.mu.Unlock()
		// Instance not in registry — check for orphan containers/networks.
		return r.reconcileOrphanCleanup(ctx, workflowID, serviceBindingID)
	}

	inst.mu.Lock()
	containerID := inst.ContainerID
	generation := inst.Generation
	state := inst.State
	inst.mu.Unlock()

	// Already cleaned up.
	if state == StateStopped && containerID == "" {
		r.mu.Unlock()
		return false, nil
	}

	// Validate transition: allow cleanup from any state but STOPPING/STOPPED
	// is idempotent. Force transition to STOPPING for safety.
	if state != StateStopping && state != StateStopped {
		inst.mu.Lock()
		inst.State = StateStopping
		inst.UpdatedAt = time.Now().UTC()
		inst.mu.Unlock()
	}

	r.mu.Unlock()

	cleaned := false

	// 1. Stop and remove container.
	if containerID != "" && r.driver != nil {
		_ = r.driver.Stop(ctx, containerID, nil)
		// Detach from service network.
		netState := r.getNetworkState(workflowID)
		if netState != nil {
			_ = r.driver.DetachNetwork(ctx, containerID, netState.NetworkID)
			netState.mu.Lock()
			delete(netState.attachedContainers, containerID)
			netState.mu.Unlock()
		}
		_ = r.driver.Remove(ctx, containerID, true)
		cleaned = true
	}

	// 2. Clean up service network if empty.
	r.mu.Lock()
	r.cleanupNetworkIfEmptyLocked(workflowID)
	r.mu.Unlock()

	// 3. Update instance state to STOPPED.
	r.mu.Lock()
	inst2, ok2 := r.instances[key]
	if ok2 && inst2.Generation == generation {
		inst2.mu.Lock()
		inst2.State = StateStopped
		inst2.ContainerID = ""
		inst2.Endpoint = ""
		inst2.NetworkAlias = ""
		inst2.Capability = ""
		inst2.UpdatedAt = time.Now().UTC()
		inst2.mu.Unlock()
	}
	r.mu.Unlock()

	return cleaned, nil
}

// getNetworkState returns the service network state for a workflow
// without holding r.mu. Called during cleanup I/O phase.
func (r *ServiceRegistry) getNetworkState(workflowID string) *serviceNetworkState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.serviceNetworks[workflowID]
}

// cleanupNetworkIfEmptyLocked removes the service network for a workflow
// if no containers remain attached. Caller must hold r.mu.
func (r *ServiceRegistry) cleanupNetworkIfEmptyLocked(workflowID string) {
	netState, ok := r.serviceNetworks[workflowID]
	if !ok {
		return
	}
	if netState.RemainingAttachments() > 0 {
		return
	}
	_ = RemoveServiceNetwork(context.Background(), r.driver, netState)
	delete(r.serviceNetworks, workflowID)
}

// reconcileOrphanCleanup handles cleanup when the instance is not in the
// registry but orphan containers or networks may exist. Returns true if
// any orphan resources were cleaned.
func (r *ServiceRegistry) reconcileOrphanCleanup(ctx context.Context, workflowID, serviceBindingID string) (bool, error) {
	if r.driver == nil {
		return false, nil
	}

	cleaned := false

	// List containers for this workflow and service binding.
	containers, err := r.driver.ListContainers(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelWorkflowID+"="+workflowID,
		runtime.LabelServiceID+"="+serviceBindingID,
	)
	if err != nil {
		return false, fmt.Errorf("reconcile orphan: list containers: %w", err)
	}

	for _, c := range containers {
		id := runtime.ContainerID(c.ID)
		_ = r.driver.Stop(ctx, id, nil)
		// Detach from any service network.
		if netState := r.getNetworkState(workflowID); netState != nil {
			_ = r.driver.DetachNetwork(ctx, id, netState.NetworkID)
			netState.mu.Lock()
			delete(netState.attachedContainers, id)
			netState.mu.Unlock()
		}
		_ = r.driver.Remove(ctx, id, true)
		cleaned = true
	}

	// Clean up orphan network if empty.
	r.mu.Lock()
	r.cleanupNetworkIfEmptyLocked(workflowID)
	r.mu.Unlock()

	return cleaned, nil
}

// DiscoverOrphans returns container IDs that exist but are not tracked by
// any active service instance. This extends T03/T04 reconcile for cleanup.
func (r *ServiceRegistry) DiscoverOrphans(ctx context.Context, workflowID string) ([]runtime.ContainerID, error) {
	if r.driver == nil {
		return nil, nil
	}

	containers, err := r.driver.ListContainers(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelWorkflowID+"="+workflowID,
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCP,
	)
	if err != nil {
		return nil, fmt.Errorf("discover orphans: list containers: %w", err)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Build set of tracked container IDs.
	tracked := make(map[string]bool)
	for _, inst := range r.instances {
		inst.mu.RLock()
		if inst.WorkflowID == workflowID && inst.ContainerID != "" {
			if inst.State == StateReady || inst.State == StateFenced || inst.State == StateUnhealthy || inst.State == StateStarting {
				tracked[string(inst.ContainerID)] = true
			}
		}
		inst.mu.RUnlock()
	}

	var orphans []runtime.ContainerID
	for _, c := range containers {
		if !tracked[c.ID] {
			orphans = append(orphans, runtime.ContainerID(c.ID))
		}
	}
	return orphans, nil
}