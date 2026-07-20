package trigger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

// PayloadMode determines how the handoff payload is constructed.
type PayloadMode string

const (
	PayloadModeEmpty       PayloadMode = "empty"
	PayloadModeSummaryRef  PayloadMode = "summary_ref"
	PayloadModeArtifactRef PayloadMode = "artifact_ref"
	PayloadModeFixedJSON   PayloadMode = "fixed_json"
)

// HandoffConcurrencyPolicy controls concurrent handoffs to the same target.
type HandoffConcurrencyPolicy string

const (
	HandoffConcurrencyAllow  HandoffConcurrencyPolicy = "allow"
	HandoffConcurrencyForbid HandoffConcurrencyPolicy = "forbid"
)

// HandoffConfig defines a static approved handoff rule.
type HandoffConfig struct {
	// SourceAgent is the agent that initiates the handoff.
	SourceAgent string
	// TargetAgent is the agent to invoke.
	TargetAgent string
	// TargetAgentVersion is the target agent version (optional).
	TargetAgentVersion string
	// TargetLockDigest is the expected digest of the target agent lock.
	TargetLockDigest string
	// PayloadMode controls how the handoff payload is built.
	PayloadMode PayloadMode
	// FixedJSON is the payload for PayloadModeFixedJSON.
	FixedJSON json.RawMessage
	// ContentType for the handoff invoke request.
	ContentType string
	// MaxDepth limits handoff chain depth (default 5).
	MaxDepth int
	// IdempotencyKeyPrefix for the handoff invoke.
	IdempotencyKeyPrefix string
	// ConcurrencyPolicy controls concurrent handoffs to the target.
	ConcurrencyPolicy HandoffConcurrencyPolicy
}

// A2AEnvelope is an Agent2Agent-compatible message envelope.
type A2AEnvelope struct {
	// SourceAgentCard is the source agent's card reference.
	SourceAgentCard string `json:"source_agent_card"`
	// TargetAgentCard is the target agent's card reference.
	TargetAgentCard string `json:"target_agent_card"`
	// TargetLockDigest is the expected digest of the target agent lock.
	TargetLockDigest string `json:"target_lock_digest,omitempty"`
	// ParentTaskID is the originating task.
	ParentTaskID string `json:"parent_task_id"`
	// ParentRunID is the originating run.
	ParentRunID string `json:"parent_run_id"`
	// ContextID groups related handoffs.
	ContextID string `json:"context_id"`
	// CorrelationID for tracing.
	CorrelationID string `json:"correlation_id"`
	// MessageRole: "user", "assistant", "system".
	MessageRole string `json:"message_role"`
	// Parts are the message parts (A2A format).
	Parts []A2APart `json:"parts"`
	// ArtifactRefs reference external artifacts without embedding them.
	ArtifactRefs []ArtifactRef `json:"artifact_refs"`
	// Metadata is an arbitrary key-value map.
	Metadata map[string]string `json:"metadata"`
}

// A2APart is a single message part in the A2A envelope.
type A2APart struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

// ArtifactRef references an artifact without embedding it.
type ArtifactRef struct {
	// URI is the artifact location (e.g. "agentpaas://artifact/<id>").
	URI string `json:"uri"`
	// Digest is the SHA-256 of the artifact.
	Digest string `json:"digest"`
	// Size is the artifact byte size.
	Size int64 `json:"size"`
}

// HandoffManager manages local handoff triggers.
type HandoffManager struct {
	mu      sync.Mutex
	configs map[string]*HandoffConfig
	audit   audit.AuditAppender

	activeChains  map[string]int
	activeTargets map[string]bool
	invoke        handoffInvokeFunc
}

type handoffInvokeFunc func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error)

// NewHandoffManager creates a new handoff manager.
func NewHandoffManager(cfgs []*HandoffConfig, auditAppender audit.AuditAppender) *HandoffManager {
	if auditAppender == nil {
		auditAppender = noOpCronAuditAppender{}
	}
	hm := &HandoffManager{
		configs:       make(map[string]*HandoffConfig),
		audit:         auditAppender,
		activeChains:  make(map[string]int),
		activeTargets: make(map[string]bool),
		invoke: func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
			return nil, fmt.Errorf("trigger service not configured")
		},
	}
	for _, cfg := range cfgs {
		if cfg == nil {
			continue
		}
		if cfg.MaxDepth == 0 {
			cfg.MaxDepth = 5
		}
		if cfg.PayloadMode == "" {
			cfg.PayloadMode = PayloadModeEmpty
		}
		if cfg.ConcurrencyPolicy == "" {
			cfg.ConcurrencyPolicy = HandoffConcurrencyAllow
		}
		hm.configs[cfg.SourceAgent] = cfg
	}
	return hm
}

// HandoffRequest is the input to trigger a handoff.
type HandoffRequest struct {
	// SourceAgent is the agent requesting the handoff.
	SourceAgent string
	// ParentRunID is the run that triggered this handoff.
	ParentRunID string
	// ContextID groups related handoffs.
	ContextID string
	// CorrelationID for tracing (generated if empty).
	CorrelationID string
	// SummaryRef references a summary of the parent run.
	SummaryRef string
	// ArtifactRefs are artifacts from the parent run.
	ArtifactRefs []ArtifactRef
	// Metadata is passed through to the envelope.
	Metadata map[string]string
}

// HandoffResult is the outcome of a handoff attempt.
type HandoffResult struct {
	// Invoked is true if the handoff was triggered.
	Invoked bool
	// RunID is the new run ID (if invoked).
	RunID string
	// Envelope is the A2A envelope that was sent.
	Envelope *A2AEnvelope
	// Reason explains why the handoff was skipped or denied.
	Reason string
}

// Trigger attempts a handoff for the given source agent.
func (hm *HandoffManager) Trigger(ctx context.Context, req *HandoffRequest) (*HandoffResult, error) {
	if req == nil {
		return nil, fmt.Errorf("handoff request is required")
	}

	hm.mu.Lock()
	cfg, ok := hm.configs[req.SourceAgent]
	hm.mu.Unlock()
	if !ok {
		hm.auditHandoffDenied(req, "no_approved_config")
		return &HandoffResult{Invoked: false, Reason: "no_approved_config"}, nil
	}

	if req.CorrelationID == "" {
		req.CorrelationID = generateCorrelationID()
	}

	hm.mu.Lock()
	depth := hm.activeChains[req.CorrelationID]
	if depth >= cfg.MaxDepth {
		hm.mu.Unlock()
		hm.auditHandoffDenied(req, "max_depth_exceeded")
		return &HandoffResult{Invoked: false, Reason: "max_depth_exceeded"}, nil
	}
	if cfg.ConcurrencyPolicy == HandoffConcurrencyForbid && hm.activeTargets[cfg.TargetAgent] {
		hm.mu.Unlock()
		hm.auditHandoffSkipped(req, "concurrency_forbid")
		return &HandoffResult{Invoked: false, Reason: "concurrency_forbid"}, nil
	}
	hm.activeChains[req.CorrelationID] = depth + 1
	hm.activeTargets[cfg.TargetAgent] = true
	hm.mu.Unlock()

	defer hm.releaseActive(cfg.TargetAgent, req.CorrelationID)

	envelope := hm.buildEnvelope(cfg, req)
	payload, contentType, err := hm.buildPayload(cfg, req)
	if err != nil {
		hm.auditHandoffSkipped(req, "payload_build_error")
		return &HandoffResult{Invoked: false, Reason: "payload_build_error", Envelope: envelope}, fmt.Errorf("handoff manager trigger: %w", err)
	}

	if contentType == "" {
		contentType = cfg.ContentType
	}

	invokeReq := &triggerv1.InvokeRequest{
		AgentName:      cfg.TargetAgent,
		AgentVersion:   cfg.TargetAgentVersion,
		Payload:        payload,
		ContentType:    contentType,
		IdempotencyKey: hm.idempotencyKey(cfg, req),
	}

	resp, err := hm.invoke(context.WithValue(ctx, callerKey{}, CallerID("system:handoff:"+req.SourceAgent)), invokeReq)
	if err != nil {
		hm.auditHandoffSkipped(req, "invoke_error")
		return &HandoffResult{Invoked: false, Reason: "invoke_error", Envelope: envelope}, fmt.Errorf("handoff manager trigger: %w", err)
	}

	runID := ""
	if resp != nil && resp.Run != nil {
		runID = resp.Run.RunId
	}

	hm.auditHandoffInvoked(req, cfg, envelope, runID)
	return &HandoffResult{
		Invoked:  true,
		RunID:    runID,
		Envelope: envelope,
	}, nil
}

func (hm *HandoffManager) releaseActive(targetAgent, correlationID string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.activeChains[correlationID]--
	if hm.activeChains[correlationID] <= 0 {
		delete(hm.activeChains, correlationID)
	}
	delete(hm.activeTargets, targetAgent)
}

func (hm *HandoffManager) buildEnvelope(cfg *HandoffConfig, req *HandoffRequest) *A2AEnvelope {
	metadata := make(map[string]string, len(req.Metadata)+1)
	for key, value := range req.Metadata {
		metadata[key] = value
	}
	if cfg.TargetLockDigest != "" {
		metadata["target_lock_digest"] = cfg.TargetLockDigest
	}

	return &A2AEnvelope{
		SourceAgentCard:  fmt.Sprintf("agentpaas://agent/%s", cfg.SourceAgent),
		TargetAgentCard:  fmt.Sprintf("agentpaas://agent/%s", cfg.TargetAgent),
		TargetLockDigest: cfg.TargetLockDigest,
		ParentTaskID:     req.ParentRunID,
		ParentRunID:      req.ParentRunID,
		ContextID:        req.ContextID,
		CorrelationID:    req.CorrelationID,
		MessageRole:      "assistant",
		Parts: []A2APart{
			{Type: "text", Data: fmt.Sprintf("handoff from %s to %s", cfg.SourceAgent, cfg.TargetAgent)},
		},
		ArtifactRefs: req.ArtifactRefs,
		Metadata:     metadata,
	}
}

func (hm *HandoffManager) buildPayload(cfg *HandoffConfig, req *HandoffRequest) ([]byte, string, error) {
	switch cfg.PayloadMode {
	case PayloadModeEmpty:
		return nil, "", nil
	case PayloadModeSummaryRef:
		payload := map[string]interface{}{
			"summary_ref": req.SummaryRef,
			"correlation": req.CorrelationID,
			"parent_run":  req.ParentRunID,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, "", fmt.Errorf("handoff manager build payload: %w", err)
		}
		return b, "application/json", nil
	case PayloadModeArtifactRef:
		refs := make([]map[string]interface{}, len(req.ArtifactRefs))
		for i, ref := range req.ArtifactRefs {
			refs[i] = map[string]interface{}{
				"uri":    ref.URI,
				"digest": ref.Digest,
				"size":   ref.Size,
			}
		}
		payload := map[string]interface{}{
			"artifact_refs": refs,
			"correlation":   req.CorrelationID,
			"parent_run":    req.ParentRunID,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, "", fmt.Errorf("handoff manager build payload: %w", err)
		}
		return b, "application/json", nil
	case PayloadModeFixedJSON:
		if len(cfg.FixedJSON) == 0 {
			return nil, "", fmt.Errorf("fixed_json mode requires FixedJSON")
		}
		return cfg.FixedJSON, "application/json", nil
	default:
		return nil, "", fmt.Errorf("unknown payload mode: %s", cfg.PayloadMode)
	}
}

func (hm *HandoffManager) idempotencyKey(cfg *HandoffConfig, req *HandoffRequest) string {
	if cfg.IdempotencyKeyPrefix == "" {
		return ""
	}
	return cfg.IdempotencyKeyPrefix + ":" + req.CorrelationID
}

const (
	eventHandoffInvoked = "handoff_invoked"
	eventHandoffSkipped = "handoff_skipped"
	eventHandoffDenied  = "handoff_denied"
)

func (hm *HandoffManager) auditHandoffInvoked(req *HandoffRequest, cfg *HandoffConfig, env *A2AEnvelope, runID string) {
	if err := hm.audit.Append(audit.AuditRecord{
		EventType:      eventHandoffInvoked,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		DeploymentMode: "local",
		Actor:          "system:handoff:" + req.SourceAgent,
		Payload: map[string]interface{}{
			"source_agent":   req.SourceAgent,
			"target_agent":   cfg.TargetAgent,
			"parent_run_id":  req.ParentRunID,
			"correlation_id": req.CorrelationID,
			"run_id":         runID,
			"envelope":       env,
		},
	}); err != nil {
		log.Printf("trigger: audit append (%s): %v", eventHandoffInvoked, err)
	}
}

func (hm *HandoffManager) auditHandoffSkipped(req *HandoffRequest, reason string) {
	if err := hm.audit.Append(audit.AuditRecord{
		EventType:      eventHandoffSkipped,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		DeploymentMode: "local",
		Actor:          "system:handoff:" + req.SourceAgent,
		Payload: map[string]interface{}{
			"source_agent":   req.SourceAgent,
			"parent_run_id":  req.ParentRunID,
			"correlation_id": req.CorrelationID,
			"reason":         reason,
		},
	}); err != nil {
		log.Printf("trigger: audit append (%s): %v", eventHandoffSkipped, err)
	}
}

func (hm *HandoffManager) auditHandoffDenied(req *HandoffRequest, reason string) {
	if err := hm.audit.Append(audit.AuditRecord{
		EventType:      eventHandoffDenied,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		DeploymentMode: "local",
		Actor:          "system:handoff:" + req.SourceAgent,
		Payload: map[string]interface{}{
			"source_agent":   req.SourceAgent,
			"parent_run_id":  req.ParentRunID,
			"correlation_id": req.CorrelationID,
			"reason":         reason,
		},
	}); err != nil {
		log.Printf("trigger: audit append (%s): %v", eventHandoffDenied, err)
	}
}

func generateCorrelationID() string {
	b := make([]byte, 16)
	if _, err := readRand(b); err != nil {
		return fmt.Sprintf("corr-%d", time.Now().UnixNano())
	}
	return "corr-" + hex.EncodeToString(b)
}

func readRand(b []byte) (int, error) {
	return cryptoRandRead(b)
}

var cryptoRandRead = func(b []byte) (int, error) {
	return rand.Read(b)
}
