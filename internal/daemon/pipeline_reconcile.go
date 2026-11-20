package daemon

import "context"

// PipelineReconcileHook is a thin wrapper that accepts a reconcile function
// for single-tick unit testing and future daemon loop integration.
// The daemon does NOT yet enable pipeline admission; this is purely a test
// seam for chunk 3.
type PipelineReconcileHook struct {
	Reconcile func(ctx context.Context, workflowID string) error
}

// Tick calls the injected reconcile function once for the given workflow.
func (h *PipelineReconcileHook) Tick(ctx context.Context, workflowID string) error {
	return h.Reconcile(ctx, workflowID)
}
