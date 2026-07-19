package docker

import (
	"context"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"sync"
)

// DockerStateStore provides lightweight state backed by injected stores when available.
type DockerStateStore struct {
	deployments routedrun.DeploymentStore
	runs        routedrun.RunStore
	workflows   routedrun.WorkflowStore
	mu          sync.RWMutex
	ds          map[string]*port.DeploymentState
	rs          map[string]*port.RunState
	as          map[string]*port.AttemptState
	ws          map[string]*port.WorkflowState
}

var _ port.TransactionalStateStore = (*DockerStateStore)(nil)

func (s *DockerStateStore) init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ds == nil {
		s.ds = map[string]*port.DeploymentState{}
		s.rs = map[string]*port.RunState{}
		s.as = map[string]*port.AttemptState{}
		s.ws = map[string]*port.WorkflowState{}
	}
}
func (s *DockerStateStore) CasDeployment(_ context.Context, v port.DeploymentState, g int64) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	if x := s.ds[v.TenantID+"/"+v.DeploymentID]; x != nil && x.Generation != g {
		return port.ErrConflict
	}
	v.Generation = g + 1
	s.ds[v.TenantID+"/"+v.DeploymentID] = &v
	return nil
}
func (s *DockerStateStore) GetDeployment(_ context.Context, t, id string) (*port.DeploymentState, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.ds[t+"/"+id]
	if v == nil {
		return nil, port.ErrNotFound
	}
	x := *v
	return &x, nil
}
func (s *DockerStateStore) ListDeployments(_ context.Context, t string) ([]*port.DeploymentState, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*port.DeploymentState
	for _, v := range s.ds {
		if v.TenantID == t {
			x := *v
			out = append(out, &x)
		}
	}
	return out, nil
}
func (s *DockerStateStore) CasRun(_ context.Context, v port.RunState, g int64) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	k := v.TenantID + "/" + v.RunID
	if x := s.rs[k]; x != nil && x.Generation != g {
		return port.ErrConflict
	}
	v.Generation = g + 1
	s.rs[k] = &v
	return nil
}
func (s *DockerStateStore) GetRun(_ context.Context, t, id string) (*port.RunState, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.rs[t+"/"+id]
	if v == nil {
		return nil, port.ErrNotFound
	}
	x := *v
	return &x, nil
}
func (s *DockerStateStore) ListRuns(_ context.Context, t, w string) ([]*port.RunState, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*port.RunState
	for _, v := range s.rs {
		if v.TenantID == t && (w == "" || v.WorkflowID == w) {
			x := *v
			out = append(out, &x)
		}
	}
	return out, nil
}
func (s *DockerStateStore) CasAttempt(_ context.Context, v port.AttemptState, g int64) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	k := v.TenantID + "/" + v.AttemptID
	if x := s.as[k]; x != nil && x.Generation != g {
		return port.ErrConflict
	}
	v.Generation = g + 1
	s.as[k] = &v
	return nil
}
func (s *DockerStateStore) GetAttempt(_ context.Context, t, id string) (*port.AttemptState, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.as[t+"/"+id]
	if v == nil {
		return nil, port.ErrNotFound
	}
	x := *v
	return &x, nil
}
func (s *DockerStateStore) CasWorkflow(_ context.Context, v port.WorkflowState, g int64) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	k := v.TenantID + "/" + v.WorkflowID
	if x := s.ws[k]; x != nil && x.Generation != g {
		return port.ErrConflict
	}
	v.Generation = g + 1
	s.ws[k] = &v
	return nil
}
func (s *DockerStateStore) GetWorkflow(_ context.Context, t, id string) (*port.WorkflowState, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.ws[t+"/"+id]
	if v == nil {
		return nil, port.ErrNotFound
	}
	x := *v
	return &x, nil
}
