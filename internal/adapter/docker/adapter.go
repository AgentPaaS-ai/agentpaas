package docker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

// DockerAdapter groups the Docker-backed port implementations.
type DockerAdapter struct {
	Runtime   *DockerWorkloadRuntime
	State     *DockerStateStore
	Events    *DockerEventStore
	Artifacts *DockerArtifactStore
	Egress    *DockerEgressEnforcer
	Ingress   *DockerIngressEnforcer
	Secrets   *DockerSecretBroker
	Packages  *DockerPackageStore
	Metering  *DockerMeteringSink
	Clock     *DockerClock
	Leases    *DockerLeaseStore
}

// DockerAdapterDeps contains existing implementations used by the adapters.
type DockerAdapterDeps struct {
	RuntimeDriver   runtime.RuntimeDriver
	DeploymentStore routedrun.DeploymentStore
	RunStore        routedrun.RunStore
	WorkflowStore   routedrun.WorkflowStore
	CheckpointStore routedrun.CheckpointStore
	EventBus        *trigger.EventBus
}

// NewDockerAdapter constructs all port adapters without contacting Docker.
func NewDockerAdapter(deps DockerAdapterDeps) *DockerAdapter {
	return &DockerAdapter{
		Runtime:   &DockerWorkloadRuntime{driver: deps.RuntimeDriver, containers: make(map[port.WorkloadID]runtime.ContainerID)},
		State:     &DockerStateStore{deployments: deps.DeploymentStore, runs: deps.RunStore, workflows: deps.WorkflowStore},
		Events:    &DockerEventStore{bus: deps.EventBus, events: make(map[string][]port.Event)},
		Artifacts: &DockerArtifactStore{},
		Egress:    &DockerEgressEnforcer{&policyEnforcer{rules: map[string]port.CommSnapshot{}}},
		Ingress:   &DockerIngressEnforcer{&policyEnforcer{rules: map[string]port.CommSnapshot{}}},
		Secrets:   &DockerSecretBroker{}, Packages: &DockerPackageStore{}, Metering: &DockerMeteringSink{},
		Clock: &DockerClock{}, Leases: &DockerLeaseStore{},
	}
}

// DockerClock is the system clock used by the baseline adapter.
type DockerClock struct{ n atomic.Uint64 }

// DockerClock.Now now.
func (c *DockerClock) Now() time.Time    { return time.Now() }
// DockerClock.Monotonic monotonic.
func (c *DockerClock) Monotonic() uint64 { return c.n.Add(1) }

// DockerLeaseStore is an in-memory lease store with TTL-based expiry.
type DockerLeaseStore struct {
	mu     sync.Mutex
	leases map[port.LeaseID]port.LeaseStatus
}

var _ port.LeaseStore = (*DockerLeaseStore)(nil)

// DockerLeaseStore.Acquire acquires docker lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *DockerLeaseStore) Acquire(_ context.Context, r port.LeaseRequest) (port.LeaseID, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.leases == nil {
		l.leases = make(map[port.LeaseID]port.LeaseStatus)
	}
	id := port.LeaseID(fmt.Sprintf("lease-%s-%d", r.AttemptID, time.Now().UnixNano()))
	l.leases[id] = port.LeaseStatus{
		ID:      id,
		Valid:   true,
		Expiry:  time.Now().Add(r.TTL),
		Revoked: false,
	}
	return id, nil
}

// DockerLeaseStore.Renew renews docker lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *DockerLeaseStore) Renew(_ context.Context, id port.LeaseID, extendBy time.Duration) (time.Time, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.leases[id]
	if !ok || !s.Valid || s.Revoked {
		return time.Time{}, port.ErrNotFound
	}
	s.Expiry = s.Expiry.Add(extendBy)
	l.leases[id] = s
	return s.Expiry, nil
}

// DockerLeaseStore.Release releases docker lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *DockerLeaseStore) Release(_ context.Context, id port.LeaseID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.leases[id]
	if !ok {
		return port.ErrNotFound
	}
	s.Valid = false
	l.leases[id] = s
	return nil
}

// DockerLeaseStore.Verify verifies docker lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *DockerLeaseStore) Verify(_ context.Context, id port.LeaseID) (port.LeaseStatus, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.leases[id]
	if !ok {
		return port.LeaseStatus{}, port.ErrNotFound
	}
	if s.Revoked || time.Now().After(s.Expiry) {
		s.Valid = false
		l.leases[id] = s
	}
	return s, nil
}

// DockerLeaseStore.Revoke revokes docker lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *DockerLeaseStore) Revoke(_ context.Context, id port.LeaseID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.leases[id]
	if !ok {
		return port.ErrNotFound
	}
	s.Valid = false
	s.Revoked = true
	l.leases[id] = s
	return nil
}
