package daemon

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestPipelineReconcileHookTick verifies that Tick calls the injected
// reconcile function exactly once with the correct workflow ID.
func TestPipelineReconcileHookTick(t *testing.T) {
	var callCount int32
	var lastWorkflowID string

	hook := &PipelineReconcileHook{
		Reconcile: func(ctx context.Context, workflowID string) error {
			atomic.AddInt32(&callCount, 1)
			lastWorkflowID = workflowID
			return nil
		},
	}

	ctx := context.Background()
	err := hook.Tick(ctx, "wf-test-123")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("Tick: want 1 call, got %d", callCount)
	}
	if lastWorkflowID != "wf-test-123" {
		t.Fatalf("Tick: want workflowID='wf-test-123', got %s", lastWorkflowID)
	}
}
