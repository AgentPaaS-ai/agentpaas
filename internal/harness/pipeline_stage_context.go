package harness

import (
	"encoding/json"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline"
)

// PipelineStageContextFromParams converts pipeline stage context params into
// the harness PipelineStageContext struct for use with SetPipelineContext.
func PipelineStageContextFromParams(p pipeline.StageContextParams) PipelineStageContext {
	leaseExpires := p.LeaseExpiresAt
	if leaseExpires.IsZero() {
		leaseExpires = time.Now().UTC().Add(10 * time.Minute)
	}

	var incomingJSON json.RawMessage
	if p.IncomingHandoffJSON != nil {
		incomingJSON = p.IncomingHandoffJSON
	}

	return PipelineStageContext{
		WorkflowKind:        p.WorkflowKind,
		NodeID:              p.NodeID,
		StageOrder:          p.StageOrder,
		IsFinalStage:        p.IsFinalStage,
		IncomingHandoffJSON: incomingJSON,
		LeaseExpiresAt:      leaseExpires,
		LeaseGeneration:     p.LeaseGeneration,
		Classification:      p.Classification,
		ToNodeID:            p.ToNodeID,
	}
}
