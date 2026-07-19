package docker

import (
	"context"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// DockerSecretBroker manages credential application for Docker workloads.
// In the Docker adapter, credentials are applied via bind-mount files.
// The broker records credential IDs (never values) and their mount paths.
type DockerSecretBroker struct {
	mu      sync.Mutex
	applied map[string][]string // workloadID -> []credentialID
}

var _ port.SecretBroker = (*DockerSecretBroker)(nil)

func (s *DockerSecretBroker) Apply(_ context.Context, r port.ApplyCredentialRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied == nil {
		s.applied = make(map[string][]string)
	}
	s.applied[r.WorkloadID] = append(s.applied[r.WorkloadID], r.CredentialID)
	return nil
}

func (s *DockerSecretBroker) Revoke(_ context.Context, workloadID, credentialID string) error {
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

func (s *DockerSecretBroker) List(_ context.Context, workloadID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.applied[workloadID]...), nil
}
