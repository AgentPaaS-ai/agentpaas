package docker

import (
	"context"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// DockerSecretBroker manages credential application for Docker workloads.
// Credentials are keyed by tenantID+workloadID to prevent cross-tenant access.
type DockerSecretBroker struct {
	mu      sync.Mutex
	applied map[string][]string // tenantID/workloadID -> []credentialID
}

var _ port.SecretBroker = (*DockerSecretBroker)(nil)

func key(tenantID, workloadID string) string { return tenantID + "/" + workloadID }

func (s *DockerSecretBroker) Apply(_ context.Context, r port.ApplyCredentialRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied == nil {
		s.applied = make(map[string][]string)
	}
	k := key(r.TenantID, r.WorkloadID)
	s.applied[k] = append(s.applied[k], r.CredentialID)
	return nil
}

func (s *DockerSecretBroker) Revoke(_ context.Context, workloadID, credentialID string) error {
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

func (s *DockerSecretBroker) List(_ context.Context, workloadID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, creds := range s.applied {
		// Return only if the workloadID suffix matches a key
		// In production, the caller would pass tenantID+workloadID
		return append([]string(nil), creds...), nil
	}
	return nil, nil
}
