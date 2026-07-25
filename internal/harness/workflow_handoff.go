package harness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline"
)

// PipelineStageContext holds the pipeline stage identity and state for
// workflow_input and commit_handoff RPC operations.
// Empty WorkflowKind means this is a non-pipeline (standalone) invoke.
type PipelineStageContext struct {
	WorkflowKind         string          // "pipeline" or empty for standalone
	NodeID               string          // current node ID
	StageOrder           int             // 0-based stage order
	IsFinalStage         bool            // true if this is the last stage
	IncomingHandoffJSON  json.RawMessage // validated envelope for this stage (empty for stage 0)
	LeaseExpiresAt       time.Time       // when the attempt lease expires
	LeaseGeneration      int64           // lease generation counter
	Terminal             bool            // true if invoke is terminal
	Classification       string          // data classification level
	ToNodeID             string          // next node (empty if unknown or final)

	// Candidate staging fields.
	HandoffCandidateJSON   json.RawMessage // staged candidate envelope
	HandoffCandidateDigest string          // sha256 digest of candidate
}

// SetPipelineContext sets the pipeline stage context on the current invoke state.
// Called by the daemon after SetInvoke but before the Python worker runs.
func (s *harnessRPCServer) SetPipelineContext(ctx PipelineStageContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invoke != nil {
		s.invoke.pipelineCtx = &ctx
	}
}

// ClearPipelineCandidate clears the staged handoff candidate (for retry/reset).
func (s *harnessRPCServer) ClearPipelineCandidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invoke != nil && s.invoke.pipelineCtx != nil {
		s.invoke.pipelineCtx.HandoffCandidateJSON = nil
		s.invoke.pipelineCtx.HandoffCandidateDigest = ""
	}
}

// leaseExpired checks whether the pipeline lease has expired or the invoke
// is terminal. Returns the error code string if invalid, empty string if ok.
func (state *rpcInvokeState) leaseExpiredOrTerminal() string {
	if state.pipelineCtx == nil {
		return ""
	}
	if state.pipelineCtx.Terminal {
		return "STALE_LEASE"
	}
	if !state.pipelineCtx.LeaseExpiresAt.IsZero() && time.Now().After(state.pipelineCtx.LeaseExpiresAt) {
		return "STALE_LEASE"
	}
	return ""
}

// hasPipelineContext returns true if the invoke has a pipeline context with a
// non-empty WorkflowKind.
func (state *rpcInvokeState) hasPipelineContext() bool {
	return state.pipelineCtx != nil && state.pipelineCtx.WorkflowKind == "pipeline"
}

// handleWorkflowInput handles the workflow_input RPC method.
func (s *harnessRPCServer) handleWorkflowInput(req rpcRequest, state *rpcInvokeState) rpcResponse {
	// Check lease/teminal first.
	if code := state.leaseExpiredOrTerminal(); code != "" {
		return rpcError(req.ID, "invoke is terminal or lease expired", code)
	}

	if !state.hasPipelineContext() {
		return rpcError(req.ID, "workflow context unavailable: not a pipeline stage", "WORKFLOW_CONTEXT_UNAVAILABLE")
	}

	ctx := state.pipelineCtx

	// Stage 0: no incoming handoff, available=false.
	if ctx.StageOrder == 0 || ctx.IncomingHandoffJSON == nil {
		return rpcResponse{
			ID: req.ID,
			OK: true,
			Result: map[string]any{
				"available": false,
			},
		}
	}

	// Mid-stage: return the incoming handoff envelope.
	var handoff map[string]any
	if err := json.Unmarshal(ctx.IncomingHandoffJSON, &handoff); err != nil {
		return rpcError(req.ID, "failed to marshal handoff envelope", "WORKFLOW_CONTEXT_UNAVAILABLE")
	}

	return rpcResponse{
		ID: req.ID,
		OK: true,
		Result: map[string]any{
			"available":     true,
			"handoff":       handoff,
			"artifact_refs": []any{},
		},
	}
}

// handleCommitHandoff handles the commit_handoff RPC method.
func (s *harnessRPCServer) handleCommitHandoff(req rpcRequest, state *rpcInvokeState) rpcResponse {
	// Check lease/terminal first.
	if code := state.leaseExpiredOrTerminal(); code != "" {
		return rpcError(req.ID, "invoke is terminal or lease expired", code)
	}

	if !state.hasPipelineContext() {
		return rpcError(req.ID, "commit_handoff is not allowed in non-pipeline context", "HANDOFF_NOT_ALLOWED")
	}

	ctx := state.pipelineCtx

	// Final stage cannot handoff.
	if ctx.IsFinalStage {
		return rpcError(req.ID, "commit_handoff is not allowed on final stage", "HANDOFF_NOT_ALLOWED")
	}

	// Extract worker-controlled params.
	schema := stringParam(req.Params, "schema")
	if schema == "" {
		return rpcError(req.ID, "schema is required", "HANDOFF_INVALID")
	}

	// Context is a JSON value from the RPC — marshal it to canonical form.
	contextRaw := req.Params["context"]
	if contextRaw == nil {
		return rpcError(req.ID, "context is required", "HANDOFF_INVALID")
	}
	// Handle different forms of context: if it's already json.RawMessage bytes, use directly.
	// Otherwise marshal the value.
	var contextBytes []byte
	switch v := contextRaw.(type) {
	case json.RawMessage:
		contextBytes = v
	case []byte:
		contextBytes = v
	case string:
		contextBytes = []byte(v)
	default:
		var err error
		contextBytes, err = json.Marshal(v)
		if err != nil {
			return rpcError(req.ID, "invalid context JSON", "HANDOFF_INVALID")
		}
	}

	// Build the full envelope using harness identity.
	classification := ctx.Classification
	if classification == "" {
		classification = "internal"
	}
	toNodeID := ctx.ToNodeID
	if toNodeID == "" {
		toNodeID = ctx.NodeID // fallback: same node if unknown
	}

	// Compute a provisional producer result digest from context.
	ctxDigest := sha256.Sum256(contextBytes)
	producerDigest := fmt.Sprintf("sha256:%x", ctxDigest[:])

	env := pipeline.HandoffEnvelope{
		SchemaVersion: pipeline.SchemaVersionHandoffV1,
		WorkflowID:    state.progressIdentity.WorkflowID,
		FromNodeID:    ctx.NodeID,
		ToNodeID:      toNodeID,
		HandoffID: fmt.Sprintf("%s:%s:%d", state.progressIdentity.RunID, ctx.NodeID, ctx.StageOrder+1),
		ProducerRunID:         state.progressIdentity.RunID,
		ProducerAttemptID:     state.progressIdentity.AttemptID,
		ProducerResultDigest:  producerDigest,
		Sequence:      ctx.StageOrder + 1,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Classification: classification,
		Context: pipeline.HandoffContext{
			Schema: schema,
			Value:  contextBytes,
		},
	}

	// Add artifacts if provided.
	if rawArtifacts, ok := req.Params["artifacts"].([]any); ok {
		for _, raw := range rawArtifacts {
			artMap, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			art := pipeline.HandoffArtifact{
				ArtifactID:   stringParam(artMap, "artifact_id"),
				OwnerNodeID:  ctx.NodeID,
				OwnerRunID:   state.progressIdentity.RunID,
				ImmutableRef: stringParam(artMap, "immutable_ref"),
				Digest:       stringParam(artMap, "digest"),
				MediaType:    stringParam(artMap, "media_type"),
				Classification: stringParam(artMap, "classification"),
			}
			if val, ok := artMap["size_bytes"].(float64); ok {
				art.SizeBytes = int64(val)
			}
			env.Artifacts = append(env.Artifacts, art)
		}
	}

	// Validate the envelope using pipeline validation.
	codes := pipeline.ValidateHandoffEnvelope(&env)
	if len(codes) > 0 {
		return rpcError(req.ID, fmt.Sprintf("handoff validation failed: %v", codes), codes[0])
	}

	// Canonicalize and compute digest from content fields (excluding timestamp).
	// Marshal the content-relevant fields for deterministic digest.
	contentKey := jsonCanonicalBytes(env)
	h := sha256.Sum256(contentKey)
	digest := fmt.Sprintf("sha256:%x", h[:])

	// Store the full canonical envelope for retrieval.
	canonical, err := json.Marshal(env)
	if err != nil {
		return rpcError(req.ID, "failed to canonicalize handoff envelope", "HANDOFF_INVALID")
	}

	// Idempotency: same digest → success, different bytes same handoff_id → conflict.
	if ctx.HandoffCandidateJSON != nil {
		if ctx.HandoffCandidateDigest == digest {
			// Idempotent success.
			return rpcResponse{
				ID: req.ID,
				OK: true,
				Result: map[string]any{
					"accepted":       true,
					"handoff_digest": digest,
					"staged":         true,
				},
			}
		}
		// Existing candidate with different content → conflict.
		return rpcError(req.ID, "handoff already committed with different content", "HANDOFF_CONFLICT")
	}

	// Stage the candidate.
	ctx.HandoffCandidateJSON = canonical
	ctx.HandoffCandidateDigest = digest

	return rpcResponse{
		ID: req.ID,
		OK: true,
		Result: map[string]any{
			"accepted":       true,
			"handoff_digest": digest,
			"staged":         true,
		},
	}
}

// jsonCanonicalBytes produces a deterministic JSON byte slice for content-
// relevant handoff fields, excluding timestamps and schema_version.
func jsonCanonicalBytes(ho pipeline.HandoffEnvelope) []byte {
	data, _ := json.Marshal(struct {
		WorkflowID          string                  `json:"workflow_id"`
		HandoffID           string                  `json:"handoff_id"`
		FromNodeID          string                  `json:"from_node_id"`
		ToNodeID            string                  `json:"to_node_id"`
		ProducerRunID       string                  `json:"producer_run_id"`
		ProducerAttemptID   string                  `json:"producer_attempt_id"`
		ProducerResultDigest string                 `json:"producer_result_digest"`
		Sequence            int                     `json:"sequence"`
		Classification      string                  `json:"classification"`
		Context             pipeline.HandoffContext  `json:"context"`
		Artifacts           []pipeline.HandoffArtifact `json:"artifacts"`
	}{
		WorkflowID:           ho.WorkflowID,
		HandoffID:            ho.HandoffID,
		FromNodeID:           ho.FromNodeID,
		ToNodeID:             ho.ToNodeID,
		ProducerRunID:        ho.ProducerRunID,
		ProducerAttemptID:    ho.ProducerAttemptID,
		ProducerResultDigest: ho.ProducerResultDigest,
		Sequence:             ho.Sequence,
		Classification:       ho.Classification,
		Context:              ho.Context,
		Artifacts:            ho.Artifacts,
	})
	return data
}
