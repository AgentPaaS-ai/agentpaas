package k8s

import (
	"context"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// K8sSecretBroker manages credential application for K8s workloads.
// In the K8s adapter, credentials are applied via bind-mount files.
// The broker records credential IDs (never values) and their mount paths.
type K8sSecretBroker struct {
	mu      sync.Mutex
	applied map[string][]string // workloadID -> []credentialID
}

var _ port.SecretBroker = (*K8sSecretBroker)(nil)

func (s *K8sSecretBroker) Apply(_ context.Context, r port.ApplyCredentialRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied == nil {
		s.applied = make(map[string][]string)
	}
	s.applied[r.WorkloadID] = append(s.applied[r.WorkloadID], r.CredentialID)
	return nil
}

func (s *K8sSecretBroker) Revoke(_ context.Context, workloadID, credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	creds := s.applied[workloadID]
	for i, c := range creds {
		if c == credentialID {
			s.applied[workloadID] = append(creds[:i], creds[i+1:]...)
			return nil
		}
	}
	return port.ErrNotFound
}

func (s *K8sSecretBroker) List(_ context.Context, workloadID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.applied[workloadID]...), nil
}
