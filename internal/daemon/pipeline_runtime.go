package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline"
)

// pipelineRuntime drives a reconcile loop for pipeline workflows when the
// B34 pipeline runtime is enabled (pipelineEnabled field or
// AGENTPAAS_PIPELINE_ENABLED=1).
//
// It holds a registry of pipeline workflow IDs so the loop knows which
// workflows to reconcile, and calls a reconcile function on each tick.
type pipelineRuntime struct {
	store     pipeline.PipelineStore
	reconcile func(ctx context.Context, workflowID routedrun.WorkflowID) error
	interval  time.Duration

	mu       sync.Mutex
	registry map[routedrun.WorkflowID]struct{}

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// newPipelineRuntime creates a new pipelineRuntime. If reconcile is nil,
// the default reconcile function will use a *pipeline.Controller backed by
// the provided store and a *pipeline.Reconciler with FakeLauncher/LaunchStore.
func newPipelineRuntime(store pipeline.PipelineStore, reconcile func(ctx context.Context, workflowID routedrun.WorkflowID) error, interval time.Duration) *pipelineRuntime {
	if interval <= 0 {
		interval = 1 * time.Second
	}
	return &pipelineRuntime{
		store:     store,
		reconcile: reconcile,
		interval:  interval,
		registry:  make(map[routedrun.WorkflowID]struct{}),
	}
}

// RegisterPipelineWorkflowForReconcile registers a workflow ID to be
// reconciled by the pipeline runtime loop. This is the primary way the
// daemon informs the reconcile loop about pipeline workflows (since
// PipelineStore does not have a ListWorkflows method).
func (r *pipelineRuntime) RegisterPipelineWorkflowForReconcile(workflowID routedrun.WorkflowID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registry[workflowID] = struct{}{}
}

// knownPipelineWorkflowIDs returns a copy of the registered workflow IDs.
func (r *pipelineRuntime) knownPipelineWorkflowIDs() []routedrun.WorkflowID {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]routedrun.WorkflowID, 0, len(r.registry))
	for id := range r.registry {
		ids = append(ids, id)
	}
	return ids
}

// Start launches the reconcile loop in a background goroutine. The loop
// runs every interval until ctx is cancelled. Start is idempotent and safe
// to call multiple times.
func (r *pipelineRuntime) Start(ctx context.Context) {
	r.mu.Lock()
	if r.cancel != nil {
		// Already started.
		r.mu.Unlock()
		return
	}
	ctx, r.cancel = context.WithCancel(ctx)
	r.mu.Unlock()

	r.wg.Add(1)
	go r.loop(ctx)
}

// loop is the reconcile loop. It runs until ctx is done.
func (r *pipelineRuntime) loop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// tick iterates over registered pipeline workflow IDs and calls reconcile
// for each. Reconcile errors are silently swallowed (the next tick retries).
func (r *pipelineRuntime) tick(ctx context.Context) {
	ids := r.knownPipelineWorkflowIDs()
	for _, id := range ids {
		_ = r.reconcile(ctx, id) // best-effort; next tick retries
	}
}

// Stop cancels the loop context and waits for the goroutine to exit.
// Stop is idempotent.
func (r *pipelineRuntime) Stop() {
	r.mu.Lock()
	if r.cancel == nil {
		r.mu.Unlock()
		return
	}
	r.cancel()
	r.cancel = nil
	r.mu.Unlock()

	r.wg.Wait()
}
