package k8s

import (
	"context"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"k8s.io/client-go/kubernetes"
)

// K8sAdapter groups Kubernetes-backed port implementations.
type K8sAdapter struct {
	Runtime   *K8sWorkloadRuntime
	State     *K8sStateStore
	Events    *K8sEventStore
	Artifacts *K8sArtifactStore
	Egress    *K8sEgressEnforcer
	Ingress   *K8sIngressEnforcer
	Secrets   *K8sSecretBroker
	Packages  *K8sPackageStore
	Metering  *K8sMeteringSink
	Clock     *K8sClock
	Leases    *K8sLeaseStore
}

// K8sAdapterDeps configures the Kubernetes API client and namespace.
type K8sAdapterDeps struct {
	Clientset kubernetes.Interface
	Namespace string
}

// NewK8sAdapter constructs the adapter without contacting the API server.
func NewK8sAdapter(deps K8sAdapterDeps) *K8sAdapter {
	return &K8sAdapter{
		Runtime: &K8sWorkloadRuntime{client: deps.Clientset, namespace: deps.Namespace, pods: make(map[port.WorkloadID]string)},
		State:   &K8sStateStore{}, Events: &K8sEventStore{events: make(map[string][]port.Event)}, Artifacts: &K8sArtifactStore{},
		Egress:  &K8sEgressEnforcer{policy: newK8sPolicyEnforcer(deps.Clientset, deps.Namespace, "egress")},
		Ingress: &K8sIngressEnforcer{policy: newK8sPolicyEnforcer(deps.Clientset, deps.Namespace, "ingress")},
		Secrets: &K8sSecretBroker{}, Packages: &K8sPackageStore{}, Metering: &K8sMeteringSink{}, Clock: &K8sClock{}, Leases: &K8sLeaseStore{},
	}
}

// K8sClock is the system clock used by the adapter.
type K8sClock struct{ n uint64 }

func (c *K8sClock) Now() time.Time    { return time.Now() }
func (c *K8sClock) Monotonic() uint64 { c.n++; return c.n }

// K8sLeaseStore is an in-memory lease placeholder.
type K8sLeaseStore struct{}

func (*K8sLeaseStore) Acquire(context.Context, port.LeaseRequest) (port.LeaseID, error) {
	return "", port.ErrNotFound
}
func (*K8sLeaseStore) Renew(context.Context, port.LeaseID, time.Duration) (time.Time, error) {
	return time.Time{}, port.ErrNotFound
}
func (*K8sLeaseStore) Release(context.Context, port.LeaseID) error { return port.ErrNotFound }
func (*K8sLeaseStore) Verify(context.Context, port.LeaseID) (port.LeaseStatus, error) {
	return port.LeaseStatus{}, port.ErrNotFound
}
func (*K8sLeaseStore) Revoke(context.Context, port.LeaseID) error { return port.ErrNotFound }
