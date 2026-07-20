package k8s

import (
	"context"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// K8sStateStore provides lightweight state backed by in-memory maps.
// In a production K8s adapter, this would be backed by etcd/CRDs.
type K8sStateStore struct {
	mu sync.RWMutex
	ds map[string]*port.DeploymentState
	rs map[string]*port.RunState
	as map[string]*port.AttemptState
	ws map[string]*port.WorkflowState
}

var _ port.TransactionalStateStore = (*K8sStateStore)(nil)

func (s *K8sStateStore) init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ds == nil {
		s.ds = map[string]*port.DeploymentState{}
		s.rs = map[string]*port.RunState{}
		s.as = map[string]*port.AttemptState{}
		s.ws = map[string]*port.WorkflowState{}
	}
}
// K8sStateStore.CasDeployment compare-and-swaps deployment.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) CasDeployment(_ context.Context, v port.DeploymentState, g int64) error {
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
// K8sStateStore.GetDeployment returns the deployment.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) GetDeployment(_ context.Context, t, id string) (*port.DeploymentState, error) {
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
// K8sStateStore.ListDeployments lists the deployments.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) ListDeployments(_ context.Context, t string) ([]*port.DeploymentState, error) {
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
// K8sStateStore.CasRun compare-and-swaps run.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) CasRun(_ context.Context, v port.RunState, g int64) error {
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
// K8sStateStore.GetRun returns the run.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) GetRun(_ context.Context, t, id string) (*port.RunState, error) {
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
// K8sStateStore.ListRuns lists the runs.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) ListRuns(_ context.Context, t, w string) ([]*port.RunState, error) {
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
// K8sStateStore.CasAttempt compare-and-swaps attempt.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) CasAttempt(_ context.Context, v port.AttemptState, g int64) error {
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
// K8sStateStore.GetAttempt returns the attempt.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) GetAttempt(_ context.Context, t, id string) (*port.AttemptState, error) {
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
// K8sStateStore.CasWorkflow compare-and-swaps workflow.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) CasWorkflow(_ context.Context, v port.WorkflowState, g int64) error {
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
// K8sStateStore.GetWorkflow returns the workflow.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *K8sStateStore) GetWorkflow(_ context.Context, t, id string) (*port.WorkflowState, error) {
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
