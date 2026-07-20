package k8s

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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
		Runtime:   &K8sWorkloadRuntime{client: deps.Clientset, namespace: deps.Namespace, pods: make(map[port.WorkloadID]string)},
		State:     &K8sStateStore{},
		Events:    &K8sEventStore{events: make(map[string][]port.Event)},
		Artifacts: &K8sArtifactStore{},
		Egress:    &K8sEgressEnforcer{policy: newK8sPolicyEnforcer(deps.Clientset, deps.Namespace, "egress")},
		Ingress:   &K8sIngressEnforcer{policy: newK8sPolicyEnforcer(deps.Clientset, deps.Namespace, "ingress")},
		Secrets:   &K8sSecretBroker{},
		Packages:  &K8sPackageStore{},
		Metering:  &K8sMeteringSink{},
		Clock:     &K8sClock{},
		Leases:    &K8sLeaseStore{},
	}
}

// K8sClock is the system clock used by the adapter.
type K8sClock struct{ n atomic.Uint64 }

// K8sClock.Now now.
func (c *K8sClock) Now() time.Time    { return time.Now() }
// K8sClock.Monotonic monotonic.
func (c *K8sClock) Monotonic() uint64 { return c.n.Add(1) }

// K8sLeaseStore is an in-memory lease store with TTL-based expiry.
type K8sLeaseStore struct {
	mu     sync.Mutex
	leases map[port.LeaseID]port.LeaseStatus
}

var _ port.LeaseStore = (*K8sLeaseStore)(nil)

// K8sLeaseStore.Acquire acquires k8s lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *K8sLeaseStore) Acquire(_ context.Context, r port.LeaseRequest) (port.LeaseID, error) {
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

// K8sLeaseStore.Renew renews k8s lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *K8sLeaseStore) Renew(_ context.Context, id port.LeaseID, extendBy time.Duration) (time.Time, error) {
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

// K8sLeaseStore.Release releases k8s lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *K8sLeaseStore) Release(_ context.Context, id port.LeaseID) error {
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

// K8sLeaseStore.Verify verifies k8s lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *K8sLeaseStore) Verify(_ context.Context, id port.LeaseID) (port.LeaseStatus, error) {
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

// K8sLeaseStore.Revoke revokes k8s lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *K8sLeaseStore) Revoke(_ context.Context, id port.LeaseID) error {
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
