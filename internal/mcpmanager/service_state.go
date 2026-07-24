package mcpmanager

import (
	"fmt"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// ServiceState represents the lifecycle state of a managed MCP service instance.
type ServiceState string

const (
	// StateDeclared: service has been registered but not yet started.
	StateDeclared ServiceState = "DECLARED"
	// StateStarting: container is being created and readiness checks are in progress.
	StateStarting ServiceState = "STARTING"
	// StateReady: service is running, passed all readiness gates, and can accept calls.
	StateReady ServiceState = "READY"
	// StateUnhealthy: service was ready but is now unreachable (crash detected by reconcile).
	StateUnhealthy ServiceState = "UNHEALTHY"
	// StateFenced: service is blocked from accepting new calls but still running.
	StateFenced ServiceState = "FENCED"
	// StateStopping: service is being gracefully shut down.
	StateStopping ServiceState = "STOPPING"
	// StateStopped: service has been stopped and all resources released.
	StateStopped ServiceState = "STOPPED"
	// StateFailed: service failed before reaching READY (crash during startup or readiness failure).
	StateFailed ServiceState = "FAILED"
)

// ErrIllegalStateTransition is returned when a state transition is not allowed.
type ErrIllegalStateTransition struct {
	From ServiceState
	To   ServiceState
}

func (e *ErrIllegalStateTransition) Error() string {
	return fmt.Sprintf("illegal service state transition from %s to %s", e.From, e.To)
}

// isLegalTransition checks whether a state transition is permitted.
func isLegalTransition(from, to ServiceState) bool {
	switch from {
	case StateDeclared:
		return to == StateStarting || to == StateStopped
	case StateStarting:
		return to == StateReady || to == StateFailed || to == StateStopping
	case StateReady:
		return to == StateUnhealthy || to == StateFenced || to == StateStopping
	case StateUnhealthy:
		return to == StateStarting || to == StateStopping
	case StateFenced:
		return to == StateStopping
	case StateStopping:
		return to == StateStopped || to == StateFailed
	case StateStopped:
		return to == StateStarting
	case StateFailed:
		return to == StateStarting
	default:
		return false
	}
}

// ServiceInstance represents a managed MCP service with its own run, lease, container, and generation.
type ServiceInstance struct {
	mu sync.RWMutex

	// Identity (immutable after Declare).
	WorkflowID       string `json:"workflow_id"`
	ServiceBindingID string `json:"service_binding_id"`
	PackageName      string `json:"package_name"`
	PackageVersion   string `json:"package_version"`
	BundleDigest     string `json:"bundle_digest"`

	// Generation is a monotonic CAS counter updated on each Start attempt.
	Generation int64 `json:"generation"`

	// Current state.
	State ServiceState `json:"state"`

	// Run/attempt/lease tracking.
	RunID     string `json:"run_id"`
	AttemptID string `json:"attempt_id"`
	LeaseID   string `json:"lease_id"`

	// Container tracking.
	ContainerID runtime.ContainerID `json:"container_id"`

	// Endpoint is the trusted-only service address; never exposed to untrusted code.
	Endpoint string `json:"endpoint"`

	// Declared tools from the service package manifest.
	DeclaredTools []string `json:"declared_tools"`

	// BoundClients tracks active client IDs for refcount.
	BoundClients map[string]bool `json:"bound_clients"`

	// Timestamps.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// LastError holds the redacted last error message.
	LastError string `json:"last_error,omitempty"`
}

// NewServiceInstance creates a ServiceInstance in DECLARED state.
func NewServiceInstance(workflowID, serviceBindingID, packageName, packageVersion, bundleDigest string, declaredTools []string) *ServiceInstance {
	tools := make([]string, len(declaredTools))
	copy(tools, declaredTools)
	now := time.Now().UTC()
	return &ServiceInstance{
		WorkflowID:       workflowID,
		ServiceBindingID: serviceBindingID,
		PackageName:      packageName,
		PackageVersion:   packageVersion,
		BundleDigest:     bundleDigest,
		State:            StateDeclared,
		DeclaredTools:    tools,
		BoundClients:     make(map[string]bool),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// transition attempts to change the service state. Returns an error for illegal transitions.
// Must be called while holding s.mu (write lock).
func (s *ServiceInstance) transition(to ServiceState) error {
	if !isLegalTransition(s.State, to) {
		return &ErrIllegalStateTransition{From: s.State, To: to}
	}
	s.State = to
	s.UpdatedAt = time.Now().UTC()
	return nil
}

// setError sets the last error (redacted) on the instance.
// Must be called while holding s.mu.
func (s *ServiceInstance) setError(errMsg string) {
	s.LastError = sanitizeToolOutputString(errMsg)
	s.UpdatedAt = time.Now().UTC()
}

// stateLocked returns the current state. Caller must hold s.mu.RLock.
func (s *ServiceInstance) stateLocked() ServiceState {
	return s.State
}

// serviceKey returns a unique key for the service instance.
func (s *ServiceInstance) serviceKey() string {
	return s.WorkflowID + "/" + s.ServiceBindingID
}
