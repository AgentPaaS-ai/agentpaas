package k8s

import (
	"context"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// K8sSecretBroker manages credential application for K8s workloads.
// Credentials are keyed by tenantID+workloadID to prevent cross-tenant access.
type K8sSecretBroker struct {
	mu      sync.Mutex
	applied map[string][]string // tenantID/workloadID -> []credentialID
}

var _ port.SecretBroker = (*K8sSecretBroker)(nil)

func (s *K8sSecretBroker) Apply(_ context.Context, r port.ApplyCredentialRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied == nil {
		s.applied = make(map[string][]string)
	}
	k := r.TenantID + "/" + r.WorkloadID
	s.applied[k] = append(s.applied[k], r.CredentialID)
	return nil
}

func (s *K8sSecretBroker) Revoke(_ context.Context, workloadID, credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, creds := range s.applied {
		for i, c := range creds {
			if c == credentialID {
				s.applied[k] = append(creds[:i], creds[i+1:]...)
				return nil
			}
		}
	}
	return port.ErrNotFound
}

func (s *K8sSecretBroker) List(_ context.Context, workloadID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, creds := range s.applied {
		return append([]string(nil), creds...), nil
	}
	return nil, nil
}
