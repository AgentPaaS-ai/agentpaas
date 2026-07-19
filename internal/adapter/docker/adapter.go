package docker

import (
	"context"
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
type DockerClock struct{ n uint64 }

func (c *DockerClock) Now() time.Time    { return time.Now() }
func (c *DockerClock) Monotonic() uint64 { c.n++; return c.n }

// DockerLeaseStore is a baseline lease store. Production lease wiring is injected by a later adapter.
type DockerLeaseStore struct{}

func (*DockerLeaseStore) Acquire(context.Context, port.LeaseRequest) (port.LeaseID, error) {
	return "", port.ErrNotFound
}
func (*DockerLeaseStore) Renew(context.Context, port.LeaseID, time.Duration) (time.Time, error) {
	return time.Time{}, port.ErrNotFound
}
func (*DockerLeaseStore) Release(context.Context, port.LeaseID) error { return port.ErrNotFound }
func (*DockerLeaseStore) Verify(context.Context, port.LeaseID) (port.LeaseStatus, error) {
	return port.LeaseStatus{}, port.ErrNotFound
}
func (*DockerLeaseStore) Revoke(context.Context, port.LeaseID) error { return port.ErrNotFound }
