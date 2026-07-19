package docker

import (
	"context"
	"fmt"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"sync"
	"time"
)

// DockerWorkloadRuntime adapts a RuntimeDriver to WorkloadRuntime.
type DockerWorkloadRuntime struct {
	driver     runtime.RuntimeDriver
	mu         sync.Mutex
	containers map[port.WorkloadID]runtime.ContainerID
	states     map[port.WorkloadID]port.WorkloadState
}

var _ port.WorkloadRuntime = (*DockerWorkloadRuntime)(nil)

func (w *DockerWorkloadRuntime) Prepare(ctx context.Context, r port.PrepareRequest) (port.WorkloadID, error) {
	if w.driver == nil {
		return "", fmt.Errorf("runtime unavailable")
	}
	id, err := w.driver.Create(ctx, runtime.ContainerSpec{Image: r.ImageDigest, Labels: map[string]string{"agentpaas.tenant": r.TenantID, "agentpaas.run": r.RunID}})
	if err != nil {
		return "", err
	}
	wid := port.WorkloadID(string(id))
	w.mu.Lock()
	w.containers[wid] = id
	if w.states == nil {
		w.states = make(map[port.WorkloadID]port.WorkloadState)
	}
	w.states[wid] = port.WorkloadPrepared
	w.mu.Unlock()
	return wid, nil
}
func (w *DockerWorkloadRuntime) container(id port.WorkloadID) (runtime.ContainerID, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.containers[id]
	if !ok {
		return "", port.ErrNotFound
	}
	return c, nil
}
func (w *DockerWorkloadRuntime) Start(ctx context.Context, id port.WorkloadID) error {
	c, e := w.container(id)
	if e != nil {
		return e
	}
	e = w.driver.Start(ctx, c)
	if e == nil {
		w.mu.Lock()
		w.states[id] = port.WorkloadRunning
		w.mu.Unlock()
	}
	return e
}
func (w *DockerWorkloadRuntime) Signal(ctx context.Context, id port.WorkloadID, s port.WorkloadSignal) error {
	c, e := w.container(id)
	if e != nil {
		return e
	}
	_, _, code, e := w.driver.Exec(ctx, c, []string{"kill", "-" + string(s), "1"})
	if e != nil {
		return e
	}
	if code != 0 {
		return fmt.Errorf("signal exit code %d", code)
	}
	return nil
}
func (w *DockerWorkloadRuntime) Fence(context.Context, port.WorkloadID) error { return nil }
func (w *DockerWorkloadRuntime) Stop(ctx context.Context, id port.WorkloadID, d *time.Duration) error {
	c, e := w.container(id)
	if e != nil {
		return e
	}
	e = w.driver.Stop(ctx, c, d)
	if e == nil {
		w.mu.Lock()
		w.states[id] = port.WorkloadStopped
		w.mu.Unlock()
	}
	return e
}
func (w *DockerWorkloadRuntime) Inspect(ctx context.Context, id port.WorkloadID) (port.WorkloadStatus, error) {
	c, e := w.container(id)
	if e != nil {
		return port.WorkloadStatus{}, e
	}
	s, e := w.driver.Status(ctx, c)
	if e != nil {
		return port.WorkloadStatus{}, e
	}
	state := port.WorkloadStopped
	if s == runtime.ContainerStatusRunning {
		state = port.WorkloadRunning
	}
	w.mu.Lock()
	if v := w.states[id]; v != "" {
		state = v
	}
	w.mu.Unlock()
	return port.WorkloadStatus{ID: id, State: state}, nil
}
func (w *DockerWorkloadRuntime) Cleanup(ctx context.Context, id port.WorkloadID) error {
	c, e := w.container(id)
	if e != nil {
		return e
	}
	e = w.driver.Remove(ctx, c, true)
	if e == nil {
		w.mu.Lock()
		delete(w.containers, id)
		w.states[id] = port.WorkloadCleaned
		w.mu.Unlock()
	}
	return e
}
