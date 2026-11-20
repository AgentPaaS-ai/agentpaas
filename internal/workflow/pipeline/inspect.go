// Package pipeline provides B34 pipeline and handoff conformance validation,
// plus the linear pipeline controller for durable stage advancement.
package pipeline

import (
	"context"
	"fmt"
	"sort"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// NodeInspect is a bounded view of a pipeline node without secret content.
type NodeInspect struct {
	NodeID           routedrun.NodeID  `json:"node_id"`
	Status           string            `json:"status"`
	RunID            routedrun.RunID   `json:"run_id,omitempty"`
	StageOrder       int               `json:"stage_order"`
	AttemptID        string            `json:"attempt_id,omitempty"`
	Outcome          string            `json:"outcome,omitempty"`
	IncomingHandoffID string           `json:"incoming_handoff_id,omitempty"`
	PackageName      string            `json:"package_name"`
}

// PipelineInspectSummary is a bounded, inspectable view of a pipeline workflow
// including ordered nodes, outcomes, and handoff digests without secret content.
type PipelineInspectSummary struct {
	WorkflowID     routedrun.WorkflowID `json:"workflow_id"`
	WorkflowKind   string               `json:"workflow_kind"`
	Status         string               `json:"status"`
	SnapshotDigest string               `json:"snapshot_digest,omitempty"`
	Nodes          []NodeInspect        `json:"nodes"`
	HandoffIDs     []string             `json:"handoff_ids,omitempty"`
	TerminalReason string               `json:"terminal_reason,omitempty"`
	ActiveNodeID   string               `json:"active_node_id,omitempty"`
}

// BuildPipelineInspect constructs a bounded PipelineInspectSummary from the
// store for a given workflow ID. It returns ordered nodes, handoff IDs, and
// active node information without exposing secret content.
func BuildPipelineInspect(ctx context.Context, store PipelineStore, wfID routedrun.WorkflowID) (*PipelineInspectSummary, error) {
	wf, err := store.GetWorkflow(ctx, wfID)
	if err != nil {
		return nil, fmt.Errorf("build pipeline inspect: %w", err)
	}

	nodes, err := store.ListNodes(ctx, wfID)
	if err != nil {
		return nil, fmt.Errorf("build pipeline inspect: %w", err)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].StageOrder < nodes[j].StageOrder
	})

	summary := &PipelineInspectSummary{
		WorkflowID:     wf.WorkflowID,
		WorkflowKind:   wf.WorkflowKind,
		Status:         wf.Status.String(),
		SnapshotDigest: "",
		Nodes:          make([]NodeInspect, 0, len(nodes)),
	}

	// Determine active node.
	for _, n := range nodes {
		if n.Status == routedrun.NodeStatusReady ||
			n.Status == routedrun.NodeStatusLaunching ||
			n.Status == routedrun.NodeStatusRunning {
			summary.ActiveNodeID = string(n.NodeID)
			break
		}
	}

	// Build node summaries.
	for _, n := range nodes {
		ni := NodeInspect{
			NodeID:     n.NodeID,
			Status:     n.Status.String(),
			RunID:      n.RunID,
			StageOrder: n.StageOrder,
			PackageName: n.PackageName,
		}
		if n.IncomingHandoffID != nil {
			ni.IncomingHandoffID = string(*n.IncomingHandoffID)
		}
		// Resolve the run outcome if the node has a run.
		if n.RunID != "" {
			run, err := store.GetRun(ctx, n.RunID)
			if err == nil && run != nil {
				ni.Outcome = run.Status.String()
			}
		}
		summary.Nodes = append(summary.Nodes, ni)
	}

	// Collect handoff IDs.
	handoffs, err := store.ListHandoffs(ctx, wfID)
	if err == nil {
		for _, h := range handoffs {
			summary.HandoffIDs = append(summary.HandoffIDs, string(h.HandoffID))
		}
	}

	// Terminal reason.
	if wf.TerminalReason != nil {
		summary.TerminalReason = wf.TerminalReason.String()
	}

	return summary, nil
}
