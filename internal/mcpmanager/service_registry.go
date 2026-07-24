package mcpmanager

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// PromotionChecker validates that a package is promoted in the registry.
// Injected for testability; the real implementation uses the registry package.
type PromotionChecker interface {
	IsPromoted(ctx context.Context, packageName, packageVersion, bundleDigest string) (bool, error)
}

// ReadinessProbe checks whether a service instance is ready.
// Injected for testability; the real implementation (T05) does protocol initialize.
type ReadinessProbe interface {
	Check(ctx context.Context, instance *ServiceInstance) (ready bool, err error)
}

// ErrServiceNotFound is returned when a service instance is not found.
var ErrServiceNotFound = fmt.Errorf("service not found")

// ErrServiceNotPromoted is returned when the service package is not promoted.
var ErrServiceNotPromoted = fmt.Errorf("service package not promoted in registry")

// ErrConcurrentStartRace is returned when a concurrent start loses the CAS race.
var ErrConcurrentStartRace = fmt.Errorf("concurrent start race: generation already claimed")

// ServiceRegistry manages MCP service instances with durable state.
//
// Locking discipline:
//   - r.mu protects the instances map (lookup, insert, delete).
//   - inst.mu protects fields on an individual ServiceInstance.
//   - Lock order is always r.mu → inst.mu. Never reverse.
//   - After reading inst from the map under r.mu, lock inst.mu before
//     reading or writing its fields. Release r.mu first for long-running
//     operations (container create, readiness probes, etc.).
type ServiceRegistry struct {
	mu        sync.RWMutex
	instances map[string]*ServiceInstance // key: workflowID/serviceBindingID

	// serviceNetworks tracks workflow-scoped MCP service networks
	// keyed by workflowID.
	serviceNetworks map[string]*serviceNetworkState

	// healthStates tracks bounded failure history per service binding.
	// keyed by workflowID/serviceBindingID.
	healthStates map[string]*healthState

	driver           runtime.RuntimeDriver
	promotionChecker PromotionChecker
	readinessProbe   ReadinessProbe

	nextRunID int64 // monotonic counter for generating opaque RunIDs
}

// NewServiceRegistry creates a new ServiceRegistry.
func NewServiceRegistry(driver runtime.RuntimeDriver, promotionChecker PromotionChecker, readinessProbe ReadinessProbe) *ServiceRegistry {
	return &ServiceRegistry{
		instances:       make(map[string]*ServiceInstance),
		serviceNetworks: make(map[string]*serviceNetworkState),
		healthStates:    make(map[string]*healthState),
		driver:           driver,
		promotionChecker: promotionChecker,
		readinessProbe:   readinessProbe,
	}
}

// Declare registers a new service instance in DECLARED state.
// Validates that the package is promoted in the registry (when checker is available).
// Returns an error if the service is already declared.
func (r *ServiceRegistry) Declare(workflowID string, binding pack.ServiceBinding, packageDigest string, declaredTools []string) (*ServiceInstance, error) {
	key := workflowID + "/" + binding.ServiceID

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.instances[key]; exists {
		return nil, fmt.Errorf("service %q already declared for workflow %q", binding.ServiceID, workflowID)
	}

	// Validate promotion when checker is available.
	if r.promotionChecker != nil {
		promoted, err := r.promotionChecker.IsPromoted(context.Background(),
			binding.PackageName, binding.PackageVersion, packageDigest)
		if err != nil {
			return nil, fmt.Errorf("declare service %q: promotion check: %w", binding.ServiceID, err)
		}
		if !promoted {
			return nil, fmt.Errorf("declare service %q: %w", binding.ServiceID, ErrServiceNotPromoted)
		}
	}

	inst := NewServiceInstance(workflowID, binding.ServiceID,
		binding.PackageName, binding.PackageVersion, packageDigest, declaredTools)
	r.instances[key] = inst

	return inst, nil
}

// Start transitions a service from DECLARED/STOPPED/FAILED/UNHEALTHY to STARTING,
// creates a container, and optionally runs readiness checks to reach READY.
// If the service is already READY with the same generation, it is idempotent (no-op).
// Concurrent Start calls race on CAS generation; only one wins.
func (r *ServiceRegistry) Start(ctx context.Context, workflowID, serviceBindingID string) (*ServiceInstance, error) {
	key := workflowID + "/" + serviceBindingID

	// Phase 1: lock registry, then instance, to check state and CAS bump generation.
	r.mu.Lock()
	inst, ok := r.instances[key]
	if !ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("start service %q: %w", serviceBindingID, ErrServiceNotFound)
	}

	// Acquire instance lock before reading/writing fields.
	inst.mu.Lock()
	state := inst.State

	// Idempotent: already READY with same generation.
	if state == StateReady {
		inst.mu.Unlock()
		r.mu.Unlock()
		// Return a stable copy.
		return r.getCopy(key), nil
	}

	// Validate transition.
	if state != StateDeclared && state != StateStopped && state != StateFailed && state != StateUnhealthy {
		from := state
		inst.mu.Unlock()
		r.mu.Unlock()
		return nil, &ErrIllegalStateTransition{From: from, To: StateStarting}
	}

	// CAS: bump generation to prevent concurrent start races.
	inst.Generation++
	gen := inst.Generation
	inst.State = StateStarting
	inst.UpdatedAt = time.Now().UTC()
	inst.RunID = fmt.Sprintf("svc-run-%d", r.nextRunID)
	r.nextRunID++
	inst.AttemptID = fmt.Sprintf("svc-att-%d", r.nextRunID)
	r.nextRunID++
	inst.LeaseID = fmt.Sprintf("svc-lease-%d", r.nextRunID)
	r.nextRunID++
	inst.LastError = ""

	// Release instance lock, keep registry lock to prevent map mutation.
	inst.mu.Unlock()

	r.mu.Unlock()

	// Phase 2: create and start container (no locks held).
	containerID, err := r.createServiceContainer(ctx, inst, gen)
	if err != nil {
		r.failInstance(key, gen, "container create: "+err.Error())
		return nil, fmt.Errorf("start service %q: create container: %w", serviceBindingID, err)
	}

	if err := r.driver.Start(ctx, containerID); err != nil {
		r.failInstance(key, gen, "container start: "+err.Error())
		return nil, fmt.Errorf("start service %q: start container: %w", serviceBindingID, err)
	}

	// Store container ID under instance lock.
	inst.mu.Lock()
	inst.ContainerID = containerID
	inst.mu.Unlock()

	// Phase 2.5: ensure service network, generate capability, attach container.
	networkState, err := r.getOrCreateNetworkState(workflowID)
	if err != nil {
		r.failInstance(key, gen, "network create: "+err.Error())
		_ = r.driver.Stop(context.Background(), containerID, nil)
		_ = r.driver.Remove(context.Background(), containerID, true)
		return nil, fmt.Errorf("start service %q: network: %w", serviceBindingID, err)
	}

	// Ensure the service network exists (idempotent).
	_, alias, err := EnsureServiceNetwork(ctx, r.driver, workflowID, networkState)
	if err != nil {
		r.failInstance(key, gen, "ensure network: "+err.Error())
		_ = r.driver.Stop(context.Background(), containerID, nil)
		_ = r.driver.Remove(context.Background(), containerID, true)
		return nil, fmt.Errorf("start service %q: ensure network: %w", serviceBindingID, err)
	}

	// Attach the service container to the service network.
	if err := AttachToServiceNetwork(ctx, r.driver, containerID, networkState); err != nil {
		r.failInstance(key, gen, "attach network: "+err.Error())
		_ = r.driver.Stop(context.Background(), containerID, nil)
		_ = r.driver.Remove(context.Background(), containerID, true)
		return nil, fmt.Errorf("start service %q: attach network: %w", serviceBindingID, err)
	}

	// Generate per-binding capability token (crypto/rand).
	capability, err := GenerateCapability()
	if err != nil {
		r.failInstance(key, gen, "generate capability: "+err.Error())
		detachAndCleanupContainer(ctx, r.driver, containerID, networkState)
		_ = r.driver.Stop(context.Background(), containerID, nil)
		_ = r.driver.Remove(context.Background(), containerID, true)
		return nil, fmt.Errorf("start service %q: generate capability: %w", serviceBindingID, err)
	}

	// Store network alias and capability on the instance (trusted fields).
	inst.mu.Lock()
	inst.NetworkAlias = alias
	inst.Capability = capability
	inst.mu.Unlock()

	// Phase 3: readiness checks.
	if r.readinessProbe != nil {
		ready, probeErr := r.readinessProbe.Check(ctx, inst)
		if !ready || probeErr != nil {
			errMsg := "readiness failed"
			if probeErr != nil {
				errMsg = probeErr.Error()
			}
			r.failInstance(key, gen, errMsg)
			return nil, fmt.Errorf("start service %q: readiness: %s", serviceBindingID, errMsg)
		}
	}

	// Phase 4: transition to READY.
	r.mu.Lock()
	inst2, ok2 := r.instances[key]
	if !ok2 || inst2.Generation != gen {
		// Our generation was superseded; clean up container.
		r.mu.Unlock()
		_ = r.driver.Stop(context.Background(), containerID, nil)
		_ = r.driver.Remove(context.Background(), containerID, true)
		return nil, ErrConcurrentStartRace
	}
	inst2.mu.Lock()
	inst2.State = StateReady
	inst2.Endpoint = "internal://" + string(containerID) // trusted-only; T04 replaces with real endpoint
	inst2.UpdatedAt = time.Now().UTC()
	inst2.mu.Unlock()
	r.mu.Unlock()

	return r.getCopy(key), nil
}

// failInstance marks an instance as FAILED if the generation matches.
func (r *ServiceRegistry) failInstance(key string, gen int64, errMsg string) {
	r.mu.Lock()
	inst, ok := r.instances[key]
	if !ok || inst.Generation != gen {
		r.mu.Unlock()
		return
	}
	inst.mu.Lock()
	inst.State = StateFailed
	inst.LastError = sanitizeLastError(errMsg)
	inst.UpdatedAt = time.Now().UTC()
	inst.mu.Unlock()
	r.mu.Unlock()
}

// Stop transitions a service to STOPPED, releasing all resources.
// Idempotent: stopping an already-stopped service is a no-op.
func (r *ServiceRegistry) Stop(ctx context.Context, workflowID, serviceBindingID string) error {
	key := workflowID + "/" + serviceBindingID

	r.mu.Lock()
	inst, ok := r.instances[key]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("stop service %q: %w", serviceBindingID, ErrServiceNotFound)
	}

	inst.mu.Lock()
	state := inst.State
	gen := inst.Generation
	containerID := inst.ContainerID
	inst.mu.Unlock()

	// Idempotent: already stopped.
	if state == StateStopped {
		r.mu.Unlock()
		return nil
	}

	// Validate transition.
	if state != StateStopping {
		if !isLegalTransition(state, StateStopping) {
			r.mu.Unlock()
			return &ErrIllegalStateTransition{From: state, To: StateStopping}
		}

		// Mark STOPPING.
		inst.mu.Lock()
		inst.State = StateStopping
		inst.UpdatedAt = time.Now().UTC()
		inst.mu.Unlock()
		r.mu.Unlock()

		// Stop and remove container.
		if containerID != "" && r.driver != nil {
			_ = r.driver.Stop(ctx, containerID, nil)
			// Detach from service network before removing container.
			if netState, ok := r.serviceNetworks[workflowID]; ok {
				_ = r.driver.DetachNetwork(ctx, containerID, netState.NetworkID)
				netState.mu.Lock()
				delete(netState.attachedContainers, containerID)
				netState.mu.Unlock()
			}
			_ = r.driver.Remove(ctx, containerID, true)
		}

		// Mark STOPPED.
		r.mu.Lock()
		inst2, ok2 := r.instances[key]
		if !ok2 || inst2.Generation != gen {
			r.mu.Unlock()
			return nil // Instance was replaced, nothing to do.
		}
		inst2.mu.Lock()
		inst2.State = StateStopped
		inst2.ContainerID = ""
		inst2.Endpoint = ""
		inst2.NetworkAlias = ""
		inst2.Capability = ""
		inst2.UpdatedAt = time.Now().UTC()
		inst2.mu.Unlock()

		// Check if this was the last attachment for the workflow network.
		r.cleanupNetworkIfEmpty(workflowID)
		r.mu.Unlock()
		return nil
	}

	// Already STOPPING.
	r.mu.Unlock()
	return nil
}

// Fence blocks the service from accepting new calls and cancels all
// in-flight MCP calls for that service binding.
// Only valid when READY.
func (r *ServiceRegistry) Fence(ctx context.Context, workflowID, serviceBindingID, reason string) error {
	key := workflowID + "/" + serviceBindingID

	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.instances[key]
	if !ok {
		return fmt.Errorf("fence service %q: %w", serviceBindingID, ErrServiceNotFound)
	}

	inst.mu.Lock()

	if inst.State != StateReady {
		inst.mu.Unlock()
		return &ErrIllegalStateTransition{From: inst.State, To: StateFenced}
	}

	inst.State = StateFenced
	inst.UpdatedAt = time.Now().UTC()
	if reason != "" {
		inst.LastError = sanitizeLastError(reason)
	}

	// Cancel all in-flight MCP calls for this service binding (B33-T06).
	tracker := inst.cancelTracker
	inst.cancelTracker = nil // release reference
	inst.mu.Unlock()

	// Cancel outside the lock to avoid deadlocks with in-flight call cleanup.
	if tracker != nil {
		tracker.CancelAll()
	}

	return nil
}

// RegisterCall registers an in-flight MCP call's cancel function with the
// service's CancelTracker (B33-T06). Returns a call ID for UnregisterCall.
// The cancel function is invoked when Fence is called on the service.
func (r *ServiceRegistry) RegisterCall(workflowID, serviceBindingID string, cancel context.CancelFunc) (string, error) {
	key := workflowID + "/" + serviceBindingID
	r.mu.RLock()
	inst, ok := r.instances[key]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("register call: %w", ErrServiceNotFound)
	}

	inst.mu.Lock()
	tracker := inst.getCancelTracker()
	callID := tracker.Register(cancel)
	inst.mu.Unlock()
	return callID, nil
}

// UnregisterCall removes an in-flight call from the service's CancelTracker.
// Safe to call after the call has completed or been cancelled.
func (r *ServiceRegistry) UnregisterCall(workflowID, serviceBindingID, callID string) {
	key := workflowID + "/" + serviceBindingID
	r.mu.RLock()
	inst, ok := r.instances[key]
	r.mu.RUnlock()
	if !ok {
		return
	}
	inst.mu.RLock()
	tracker := inst.cancelTracker
	inst.mu.RUnlock()
	if tracker != nil {
		tracker.Unregister(callID)
	}
}

// Reconcile discovers labelled resources and validates their state.
// After daemon restart, this revokes ambiguous leases and never trusts stale endpoints.
//
// Reconciliation rules:
// - Two active containers for the same (service, generation) → mark UNHEALTHY.
// - Ready service with no running container → UNHEALTHY.
// - Unknown/orphan containers → ignored.
func (r *ServiceRegistry) Reconcile(ctx context.Context, workflowID string) error {
	if r.driver == nil {
		return nil
	}

	containers, err := r.driver.ListContainers(ctx,
		runtime.LabelManagedBy+"="+runtime.ManagedByValue,
		runtime.LabelWorkflowID+"="+workflowID,
		runtime.LabelResourceType+"="+runtime.ResourceTypeMCP,
	)
	if err != nil {
		return fmt.Errorf("reconcile: list containers: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Track generation→container mappings for duplicate detection.
	generationContainers := make(map[string]map[string]bool)

	// Build discovery set.
	discoveredIDs := make(map[string]bool)
	for _, c := range containers {
		discoveredIDs[c.ID] = true

		serviceID := c.Labels[runtime.LabelServiceID]
		genStr := c.Labels[runtime.LabelServiceGeneration]

		if serviceID == "" {
			continue
		}

		instKey := workflowID + "/" + serviceID
		gen, _ := strconv.ParseInt(genStr, 10, 64)

		genKey := instKey + "/" + genStr
		if generationContainers[genKey] == nil {
			generationContainers[genKey] = make(map[string]bool)
		}
		generationContainers[genKey][c.ID] = true

		// Check if this container belongs to a known instance.
		if inst, ok := r.instances[instKey]; ok {
			inst.mu.Lock()
			if inst.Generation == gen {
				if string(inst.ContainerID) != c.ID {
					// Two containers claiming same generation → conflict.
					if inst.State == StateReady {
						inst.State = StateUnhealthy
						inst.LastError = sanitizeLastError("reconcile: duplicate container for generation " + genStr)
						inst.UpdatedAt = time.Now().UTC()
					}
				} else {
					// Our container — verify it's running.
					status, statusErr := r.driver.Status(ctx, runtime.ContainerID(c.ID))
					if statusErr != nil || status != runtime.ContainerStatusRunning {
						if inst.State == StateReady {
							inst.State = StateUnhealthy
							inst.LastError = sanitizeLastError("reconcile: container not running")
							inst.UpdatedAt = time.Now().UTC()
						}
					}
				}
			}
			inst.mu.Unlock()
		}
	}

	// Check instances that think they have a container but none was discovered.
	for _, inst := range r.instances {
		if inst.WorkflowID != workflowID {
			continue
		}
		inst.mu.Lock()
		if inst.ContainerID != "" && !discoveredIDs[string(inst.ContainerID)] {
			if inst.State == StateReady || inst.State == StateFenced {
				inst.State = StateUnhealthy
				inst.LastError = sanitizeLastError("reconcile: container not found")
				inst.UpdatedAt = time.Now().UTC()
			}
		}
		inst.mu.Unlock()
	}

	// Detect duplicate generations across discovered containers.
	for genKey, containerIDs := range generationContainers {
		if len(containerIDs) > 1 {
			for _, inst := range r.instances {
				inst.mu.Lock()
				instGenKey := inst.serviceKey() + "/" + strconv.FormatInt(inst.Generation, 10)
				if instGenKey == genKey && (inst.State == StateReady || inst.State == StateFenced) {
					inst.State = StateUnhealthy
					inst.LastError = sanitizeLastError("reconcile: ambiguous lease — multiple containers for generation")
					inst.UpdatedAt = time.Now().UTC()
				}
				inst.mu.Unlock()
			}
		}
	}

	// Reconcile orphan service networks: remove networks with no
	// attached containers.
	r.mu.Unlock()
	reconciledOrphans, orphanErr := ReconcileOrphanServiceNetworks(ctx, r.driver, workflowID)
	r.mu.Lock()
	if orphanErr != nil {
		// Non-fatal: log and continue.
		_ = orphanErr
	}
	if reconciledOrphans > 0 {
		// Clear orphaned states.
		for wfID, netState := range r.serviceNetworks {
			if wfID == workflowID && netState.RemainingAttachments() == 0 {
				delete(r.serviceNetworks, wfID)
			}
		}
	}

	return nil
}

// BindClient increments the client refcount for a service.
func (r *ServiceRegistry) BindClient(workflowID, serviceBindingID, clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := workflowID + "/" + serviceBindingID
	inst, ok := r.instances[key]
	if !ok {
		return fmt.Errorf("bind client %q: %w", serviceBindingID, ErrServiceNotFound)
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.State != StateReady {
		return fmt.Errorf("bind client %q: service is %s, must be %s", serviceBindingID, inst.State, StateReady)
	}

	inst.BoundClients[clientID] = true
	inst.UpdatedAt = time.Now().UTC()
	return nil
}

// UnbindClient decrements the client refcount.
// When the last client unbinds a workflow-scoped service, the service is stopped.
func (r *ServiceRegistry) UnbindClient(ctx context.Context, workflowID, serviceBindingID, clientID string) error {
	r.mu.Lock()
	key := workflowID + "/" + serviceBindingID
	inst, ok := r.instances[key]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("unbind client %q: %w", serviceBindingID, ErrServiceNotFound)
	}

	inst.mu.Lock()
	delete(inst.BoundClients, clientID)
	remaining := len(inst.BoundClients)
	inst.mu.Unlock()
	r.mu.Unlock()

	if remaining == 0 {
		return r.Stop(ctx, workflowID, serviceBindingID)
	}
	return nil
}

// WorkflowTerminal stops all services for a workflow.
func (r *ServiceRegistry) WorkflowTerminal(ctx context.Context, workflowID string) error {
	r.mu.RLock()
	var toStop []string
	for key, inst := range r.instances {
		inst.mu.RLock()
		stopped := inst.State == StateStopped
		matches := inst.WorkflowID == workflowID
		inst.mu.RUnlock()
		if matches && !stopped {
			toStop = append(toStop, key)
		}
	}
	r.mu.RUnlock()

	var errs []error
	for _, key := range toStop {
		// Parse serviceBindingID from key (key = workflowID/serviceBindingID).
		serviceBindingID := key[len(workflowID)+1:]
		if err := r.Stop(ctx, workflowID, serviceBindingID); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("workflow terminal: %d services failed to stop: %v", len(errs), errs)
	}
	return nil
}

// Get returns a copy of the service instance for safe read access.
func (r *ServiceRegistry) Get(workflowID, serviceBindingID string) (*ServiceInstance, error) {
	r.mu.RLock()
	key := workflowID + "/" + serviceBindingID
	inst, ok := r.instances[key]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("get service %q: %w", serviceBindingID, ErrServiceNotFound)
	}

	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return copyInstance(inst), nil
}

// getCopy returns a copy of the instance without acquiring r.mu.
// Caller must hold at least r.mu.RLock.
func (r *ServiceRegistry) getCopy(key string) *ServiceInstance {
	inst, ok := r.instances[key]
	if !ok {
		return nil
	}
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return copyInstance(inst)
}

// List returns all service instances, optionally filtered by workflow.
func (r *ServiceRegistry) List(workflowID string) []*ServiceInstance {
	r.mu.RLock()
	// Collect instances while holding r.mu.RLock.
	var insts []*ServiceInstance
	for _, inst := range r.instances {
		if workflowID == "" || inst.WorkflowID == workflowID {
			insts = append(insts, inst)
		}
	}
	r.mu.RUnlock()

	// Now lock each instance to copy.
	result := make([]*ServiceInstance, 0, len(insts))
	for _, inst := range insts {
		inst.mu.RLock()
		result = append(result, copyInstance(inst))
		inst.mu.RUnlock()
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].WorkflowID != result[j].WorkflowID {
			return result[i].WorkflowID < result[j].WorkflowID
		}
		return result[i].ServiceBindingID < result[j].ServiceBindingID
	})
	return result
}

// getOrCreateNetworkState returns the serviceNetworkState for a workflow,
// creating one if it does not exist. Caller must hold at least r.mu.RLock.
func (r *ServiceRegistry) getOrCreateNetworkState(workflowID string) (*serviceNetworkState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.serviceNetworks[workflowID]
	if !ok {
		state = newServiceNetworkState()
		r.serviceNetworks[workflowID] = state
	}
	return state, nil
}

// detachAndCleanupContainer detaches a container from its service network
// and is used during rollback on failure.
func detachAndCleanupContainer(ctx context.Context, driver runtime.RuntimeDriver, containerID runtime.ContainerID, state *serviceNetworkState) {
	if driver == nil || state == nil {
		return
	}
	_ = driver.DetachNetwork(ctx, containerID, state.NetworkID)
}

// cleanupNetworkIfEmpty removes the service network for a workflow if no
// containers remain attached. Caller must hold r.mu (write lock).
func (r *ServiceRegistry) cleanupNetworkIfEmpty(workflowID string) {
	netState, ok := r.serviceNetworks[workflowID]
	if !ok {
		return
	}
	if netState.RemainingAttachments() > 0 {
		return
	}
	// Remove the network; ignore errors (best-effort cleanup).
	_ = RemoveServiceNetwork(context.Background(), r.driver, netState)
	delete(r.serviceNetworks, workflowID)
}

// createServiceContainer builds and creates a container for a service instance.
func (r *ServiceRegistry) createServiceContainer(ctx context.Context, inst *ServiceInstance, gen int64) (runtime.ContainerID, error) {
	if r.driver == nil {
		return "", fmt.Errorf("no runtime driver configured")
	}

	inst.mu.RLock()
	runID := inst.RunID
	workflowID := inst.WorkflowID
	serviceID := inst.ServiceBindingID
	tools := stringsJoin(inst.DeclaredTools)
	inst.mu.RUnlock()

	labels := runtime.Labels(runtime.ResourceTypeMCP, runID)
	labels[runtime.LabelManagedBy] = runtime.ManagedByValue
	labels[runtime.LabelWorkflowID] = workflowID
	labels[runtime.LabelServiceID] = serviceID
	labels[runtime.LabelServiceGeneration] = strconv.FormatInt(gen, 10)
	labels[runtime.LabelServiceRunID] = runID

	spec := runtime.ContainerSpec{
		Image:   "agentpaas-mcp-service:latest", // TODO(T05): resolve from package digest
		Command: []string{"sleep", "infinity"},
		Env: []string{
			"AGENTPAAS_AGENT_KIND=mcp_service",
			"AGENTPAAS_MCP_DECLARED_TOOLS=" + tools,
		},
		Labels: labels,
	}

	return r.driver.Create(ctx, spec)
}

// copyInstance returns a field-by-field copy of the instance for safe read access.
// Caller must hold inst.mu (at least RLock).
func copyInstance(inst *ServiceInstance) *ServiceInstance {
	if inst == nil {
		return nil
	}
	cp := &ServiceInstance{
		WorkflowID:       inst.WorkflowID,
		ServiceBindingID: inst.ServiceBindingID,
		PackageName:      inst.PackageName,
		PackageVersion:   inst.PackageVersion,
		BundleDigest:     inst.BundleDigest,
		Generation:       inst.Generation,
		State:            inst.State,
		RunID:            inst.RunID,
		AttemptID:        inst.AttemptID,
		LeaseID:          inst.LeaseID,
		LeaseDeadline:    inst.LeaseDeadline,
		MaxConcurrency:   inst.MaxConcurrency,
		ContainerID:      inst.ContainerID,
		Endpoint:         inst.Endpoint,
		NetworkAlias:     inst.NetworkAlias,
		Capability:       inst.Capability,
		BoundClients:     make(map[string]bool, len(inst.BoundClients)),
		CreatedAt:        inst.CreatedAt,
		UpdatedAt:        inst.UpdatedAt,
		LastError:        inst.LastError,
	}
	cp.DeclaredTools = make([]string, len(inst.DeclaredTools))
	copy(cp.DeclaredTools, inst.DeclaredTools)
	for k, v := range inst.BoundClients {
		cp.BoundClients[k] = v
	}
	return cp
}

func stringsJoin(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ","
		}
		result += s
	}
	return result
}
