package pipeline

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// Stage context params
// ---------------------------------------------------------------------------

// StageContextParams is a plain struct containing pipeline stage context
// information, free of harness dependencies.
type StageContextParams struct {
	WorkflowKind        string          // "pipeline"
	NodeID              string          // current node ID
	StageOrder          int             // 0-based stage order
	IsFinalStage        bool            // true if last stage
	IncomingHandoffJSON json.RawMessage // nil/empty for stage 0
	LeaseExpiresAt      time.Time       // lease expiration
	LeaseGeneration     int64           // lease generation counter
	Classification      string          // data classification
	ToNodeID            string          // next node (empty if final)
}

// CollectStageContextParams loads node list + incoming handoff from the store
// for a claim, returning the stage context params.
func CollectStageContextParams(ctx context.Context, store PipelineStore, claim *Claim) (StageContextParams, error) {
	// Load the current node.
	node, err := store.GetNode(ctx, claim.NodeID)
	if err != nil {
		return StageContextParams{}, err
	}

	// Load all nodes to compute IsFinalStage and ToNodeID.
	nodes, err := store.ListNodes(ctx, claim.WorkflowID)
	if err != nil {
		return StageContextParams{}, err
	}
	// Sort by StageOrder.
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].StageOrder < nodes[j].StageOrder
	})

	isFinal := node.StageOrder == len(nodes)-1
	var toNodeID routedrun.NodeID
	if !isFinal {
		for _, n := range nodes {
			if n.StageOrder == node.StageOrder+1 {
				toNodeID = n.NodeID
				break
			}
		}
	}

	// Load incoming handoff if present.
	var incoming json.RawMessage
	if node.IncomingHandoffID != nil {
		handoff, err := store.GetHandoff(ctx, *node.IncomingHandoffID)
		if err == nil {
			incoming = json.RawMessage(handoff.ContextJSON)
		}
	}

	leaseExpires := time.Now().UTC().Add(10 * time.Minute)
	var leaseGen int64 = 1

	return StageContextParams{
		WorkflowKind:        "pipeline",
		NodeID:              string(node.NodeID),
		StageOrder:          node.StageOrder,
		IsFinalStage:        isFinal,
		IncomingHandoffJSON: incoming,
		LeaseExpiresAt:      leaseExpires,
		LeaseGeneration:     leaseGen,
		Classification:      "internal",
		ToNodeID:            string(toNodeID),
	}, nil
}
