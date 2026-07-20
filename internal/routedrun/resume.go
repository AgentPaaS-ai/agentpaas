package routedrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Resume checkpoint delivery (B27-T05)
// ---------------------------------------------------------------------------

// ResumeCheckpointData is the trusted data delivered to a resumed attempt's
// harness, never injected into the user trigger payload.
type ResumeCheckpointData struct {
	CheckpointID       CheckpointID `json:"checkpoint_id"`
	SourceAttemptID    AttemptID    `json:"source_attempt_id"`
	RunID              RunID        `json:"run_id"`
	WorkflowID         WorkflowID   `json:"workflow_id"`
	NodeID             NodeID       `json:"node_id,omitempty"`

	// Semantic fields.
	Phase               string   `json:"phase"`
	CompletedWork       []string `json:"completed_work"`
	RemainingWork       []string `json:"remaining_work"`
	ArtifactRefs       []string `json:"artifact_references"`
	LastCommittedAction string   `json:"last_committed_action"`

	// Integrity digests.
	CheckpointDigest   string `json:"checkpoint_digest"`
	ArtifactMetaDigest string `json:"artifact_meta_digest"`

	// Artifact metadata (path, digest, size — NOT content).
	Artifacts []ArtifactMetadata `json:"artifacts,omitempty"`

	// Resume reason (trusted, set by daemon — never by trigger payload).
	ResumeReason ResumeReason `json:"resume_reason"`

	// Checkpoint creation timestamp.
	CreatedAt time.Time `json:"created_at"`
}

// ResumeReason is a trusted enum set by the daemon.
type ResumeReason string

const (
	ResumeReasonFailureContinuation ResumeReason = "failure_continuation"
	ResumeReasonOperatorPauseResume ResumeReason = "operator_pause_resume"
)

// ResumeCheckpointLoader loads and validates a checkpoint for resume.
type ResumeCheckpointLoader struct {
	store CheckpointStore
}

// NewResumeCheckpointLoader creates a loader.
func NewResumeCheckpointLoader(store CheckpointStore) *ResumeCheckpointLoader {
	return &ResumeCheckpointLoader{store: store}
}

// LoadResumeCheckpoint loads the latest safe checkpoint for an attempt and
// converts it to trusted resume data.
// Returns ErrNotFound if no checkpoint exists (initial attempt).
func (l *ResumeCheckpointLoader) LoadResumeCheckpoint(
	ctx context.Context,
	attemptID AttemptID,
	runID RunID,
	resumeReason ResumeReason,
) (*ResumeCheckpointData, error) {
	cp, err := l.store.GetLatestCheckpoint(ctx, attemptID)
	if err != nil {
		return nil, fmt.Errorf("load checkpoint: %w", err)
	}

	// Verify run relationship.
	if cp.RunID != runID {
		return nil, fmt.Errorf("%w: checkpoint run_id mismatch: expected %s got %s",
			ErrInvalidArgument, runID, cp.RunID)
	}

	// Verify checkpoint digest (integrity).
	// B27 harness always computes a digest for safe_to_resume=True checkpoints.
	// An empty digest on a safe checkpoint is invalid (missing integrity proof).
	// B26-era checkpoints without safe_to_resume may have empty digests (backward compat).
	recalcDigest := recomputeCheckpointDigest(cp)
	if cp.CheckpointDigest != "" && recalcDigest != cp.CheckpointDigest {
		return nil, fmt.Errorf("%w: checkpoint digest mismatch", ErrInvalidArgument)
	}
	if cp.SafeToResume && cp.CheckpointDigest == "" {
		return nil, fmt.Errorf("%w: safe checkpoint missing digest", ErrInvalidArgument)
	}

	// Build trusted resume data.
	data := &ResumeCheckpointData{
		CheckpointID:        cp.CheckpointID,
		SourceAttemptID:     cp.AttemptID,
		RunID:               cp.RunID,
		WorkflowID:          cp.WorkflowID,
		NodeID:              cp.NodeID,
		Phase:               cp.Phase,
		CompletedWork:       cp.CompletedWork,
		RemainingWork:       cp.RemainingWork,
		ArtifactRefs:       cp.ArtifactRefs,
		LastCommittedAction: cp.LastCommittedAction,
		CheckpointDigest:    cp.CheckpointDigest,
		ArtifactMetaDigest:  cp.ArtifactMetaDigest,
		ResumeReason:        resumeReason,
		CreatedAt:           cp.CreatedAt,
	}

	return data, nil
}

// LoadResumeCheckpointForRun loads the latest checkpoint across all attempts
// for a given run. This is used when a new attempt is created as a
// replacement for a failed prior attempt.
func (l *ResumeCheckpointLoader) LoadResumeCheckpointForRun(
	ctx context.Context,
	runID RunID,
	priorAttemptID AttemptID,
	resumeReason ResumeReason,
) (*ResumeCheckpointData, error) {
	return l.LoadResumeCheckpoint(ctx, priorAttemptID, runID, resumeReason)
}

// ValidateResumeCheckpoint verifies a checkpoint is compatible with the
// current attempt's policy/image/catalog. Returns nil if compatible.
func ValidateResumeCheckpoint(
	cp *ResumeCheckpointData,
	expectedRunID RunID,
	policyDigest string,
	imageDigest string,
) error {
	if cp == nil {
		return fmt.Errorf("%w: nil checkpoint", ErrInvalidArgument)
	}
	if cp.RunID != expectedRunID {
		return fmt.Errorf("%w: run_id mismatch", ErrInvalidArgument)
	}
	// Policy and image compatibility checks are stubs for B35.
	// B27 only verifies the checkpoint is structurally valid.
	if cp.CheckpointID == "" {
		return fmt.Errorf("%w: empty checkpoint ID", ErrInvalidArgument)
	}
	if !cp.ResumeReason.Valid() {
		return fmt.Errorf("%w: invalid resume reason", ErrInvalidArgument)
	}
	return nil
}

// Valid checks if a ResumeReason is a known value.
func (r ResumeReason) Valid() bool {
	switch r {
	case ResumeReasonFailureContinuation, ResumeReasonOperatorPauseResume:
		return true
	default:
		return false
	}
}

// recomputeCheckpointDigest recomputes the digest from a semantic checkpoint.
func recomputeCheckpointDigest(cp *SemanticCheckpoint) string {
	fields := map[string]any{
		"phase":                cp.Phase,
		"completed_work":       cp.CompletedWork,
		"remaining_work":       cp.RemainingWork,
		"last_committed_action": cp.LastCommittedAction,
		"safe_to_resume":       cp.SafeToResume,
		"artifact_references":  cp.ArtifactRefs,
	}
	b, _ := json.Marshal(fields) // best-effort marshal
	return hexSha256String(b)
}

// hexSha256String returns hex-encoded SHA-256 of bytes.
func hexSha256String(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// SerializeForHarness serializes resume checkpoint data as JSON for the
// harness startup file (never sent through trigger payload).
func (d *ResumeCheckpointData) SerializeForHarness() ([]byte, error) {
	return json.Marshal(d)
}

// DeserializeResumeCheckpoint deserializes harness-provided JSON.
func DeserializeResumeCheckpoint(data []byte) (*ResumeCheckpointData, error) {
	var d ResumeCheckpointData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("unmarshal resume checkpoint: %w", err)
	}
	return &d, nil
}
