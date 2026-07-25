package pipeline

import (
	"context"
	"fmt"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// AdvanceWithFence
// ---------------------------------------------------------------------------

// AdvanceWithFence is a test/utility helper that fences the previous node's
// containers (if any) and then calls ReconcileOnce for the given workflow.
//
// This ensures clean isolation between pipeline stages: before advancing to
// the next stage, all containers from the previous stage are stopped and
// removed via the RuntimeStageLauncher's FenceStage method.
//
// Steps:
//  1. If prevNodeID is non-empty, call launcher.FenceStage to stop/remove
//     containers for that node.
//  2. Call rec.ReconcileOnce to advance the workflow (claim+launch+ack).
//  3. Return the claim.
func AdvanceWithFence(
	ctx context.Context,
	rec *Reconciler,
	launcher *RuntimeStageLauncher,
	wfID string,
	prevNodeID string,
) (*Claim, error) {
	// Fence previous node (if any).
	if prevNodeID != "" {
		if err := launcher.FenceStage(ctx, wfID, prevNodeID); err != nil {
			return nil, fmt.Errorf("AdvanceWithFence: fence previous node %s: %w", prevNodeID, err)
		}
	}

	// Advance the pipeline.
	claim, err := rec.ReconcileOnce(ctx, routedrun.WorkflowID(wfID))
	if err != nil {
		return nil, fmt.Errorf("AdvanceWithFence: reconcile: %w", err)
	}

	return claim, nil
}
