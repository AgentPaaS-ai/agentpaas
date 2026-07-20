package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

// v0.3 safety bounds (mirror SDK bounds in Go).
const (
	progressPhaseMax       = 128
	progressStrItemMax     = 1024
	progressListMax        = 50
	progressArtifactMax    = 32
	progressArtifactPathMax = 512
	progressArtifactSegMax  = 8
)

// handleProgress processes an SDK agent.progress() call.
// It validates fields in Go (never trusts Python validation alone),
// appends an authenticated record to the journal, and returns metadata.
func (s *harnessRPCServer) handleProgress(req rpcRequest, state *rpcInvokeState) rpcResponse {
	p := req.Params
	if p == nil {
		p = map[string]any{}
	}

	// --- Reject if invoke has ended or lease expired ---
	if state.leaseExpired.Load() {
		return rpcError(req.ID, "attempt lease has expired", "LEASE_EXPIRED")
	}
	if state.progressJournal == nil {
		return rpcError(req.ID, "progress journal not initialized", "INVALID_PROGRESS")
	}

	// --- Validate event_id ---
	eventID, _ := p["event_id"].(string) // optional value; zero on miss
	if eventID == "" {
		return rpcError(req.ID, "event_id is required", "INVALID_PROGRESS")
	}

	// --- Validate phase ---
	phase, _ := p["phase"].(string) // optional value; zero on miss
	if phase == "" {
		return rpcError(req.ID, "phase must not be empty", "INVALID_PROGRESS")
	}
	if len([]byte(phase)) > progressPhaseMax {
		return rpcError(req.ID, "phase exceeds max length", "INVALID_PROGRESS")
	}
	if hasControlChars(phase) {
		return rpcError(req.ID, "phase contains control characters", "INVALID_PROGRESS")
	}

	// --- Validate completed_work ---
	cw, err := validateStringList(p["completed_work"], "completed_work", progressListMax, progressStrItemMax)
	if err != nil {
		return rpcError(req.ID, err.Error(), "INVALID_PROGRESS")
	}

	// --- Validate remaining_work ---
	rw, err := validateStringList(p["remaining_work"], "remaining_work", progressListMax, progressStrItemMax)
	if err != nil {
		return rpcError(req.ID, err.Error(), "INVALID_PROGRESS")
	}

	// --- Validate artifact_references ---
	artRefs, err := validateArtifactRefs(p["artifact_references"])
	if err != nil {
		return rpcError(req.ID, err.Error(), "ARTIFACT_REJECTED")
	}

	// --- Validate last_committed_action ---
	lastCommitted, _ := p["last_committed_action"].(string) // optional value; zero on miss
	if lastCommitted != "" && len([]byte(lastCommitted)) > progressStrItemMax {
		return rpcError(req.ID, "last_committed_action exceeds max length", "INVALID_PROGRESS")
	}
	if lastCommitted != "" && hasControlChars(lastCommitted) {
		return rpcError(req.ID, "last_committed_action contains control characters", "INVALID_PROGRESS")
	}

	// --- Validate safe_to_resume ---
	safeToResume, _ := p["safe_to_resume"].(bool) // optional value; zero on miss
	if safeToResume {
		if lastCommitted == "" {
			return rpcError(req.ID, "safe_to_resume=true requires last_committed_action", "INVALID_PROGRESS")
		}
		// Spec: "at least one non-empty completed_work entry"
		hasNonEmpty := false
		for _, entry := range cw {
			if entry != "" {
				hasNonEmpty = true
				break
			}
		}
		if !hasNonEmpty {
			return rpcError(req.ID, "safe_to_resume=true requires at least one non-empty completed_work entry", "INVALID_PROGRESS")
		}
	}

	// --- Secret sentinel rejection (verifier-17) ---
	// T02 spec item 5: "Redact or reject registered secret fingerprints and
	// configured test sentinels before persistence; do not claim semantic
	// secret detection." This is a lexical check only.
	if containsSecretSentinel(phase) ||
		containsSecretSentinel(lastCommitted) ||
		containsSecretSentinel(strings.Join(cw, "\n")) ||
		containsSecretSentinel(strings.Join(rw, "\n")) {
		return rpcError(req.ID, "checkpoint content contains secret sentinel", "CHECKPOINT_REJECTED")
	}

	// --- Build checkpoint digest if safe ---
	var checkpointDigest, artifactMetaDigest string
	if safeToResume {
		// Build a provisional record for digest computation.
		provRec := &progressJournalRecord{
			Phase:               phase,
			CompletedWork:       cw,
			RemainingWork:       rw,
			LastCommittedAction: lastCommitted,
			SafeToResume:        safeToResume,
		}
		checkpointDigest = computeCheckpointDigest(provRec)
		// Artifact metadata digest: hash of sorted artifact paths + sizes (T04 fills sizes).
		// For now, hash the artifact references list.
		artifactMetaDigest = computeArtifactMetaDigest(artRefs)
	}

	// --- Append to journal ---
	checkpointID, err := state.progressJournal.append(
		eventID, phase, cw, rw, artRefs, lastCommitted, safeToResume,
		checkpointDigest, artifactMetaDigest,
	)
	if err != nil {
		return rpcError(req.ID, "journal write failed", "CHECKPOINT_REJECTED")
	}

	// --- Build response ---
	resp := progressResponse{
		Recorded:       true,
		WorkflowID:     state.progressIdentity.WorkflowID,
		NodeID:         state.progressIdentity.NodeID,
		RunID:          state.progressIdentity.RunID,
		AttemptID:      state.progressIdentity.AttemptID,
		LeaseExpiresAt: state.progressIdentity.LeaseExpiry.UTC().Format(time.RFC3339Nano),
	}
	if safeToResume && checkpointID != "" {
		cp := checkpointID
		resp.CheckpointID = &cp
	}
	// T02 spec item 9: "Return current attempt metadata and the B35-provided
	// resume checkpoint." On a resumed attempt, resumeCheckpoint is populated
	// by SetProgressMetadata from the daemon's trusted store.
	if state.resumeCheckpoint != nil {
		resp.ResumeCheckpoint = state.resumeCheckpoint
		resp.ResumeReason = state.resumeReason
	}

	// --- Emit audit event (non-secret summary) ---
	if s.audit != nil {
		if err := s.audit.Append(audit.AuditRecord{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			EventType: "progress",
			Actor:     "harness",
			Payload: map[string]interface{}{
				"run_id":       state.progressIdentity.RunID,
				"attempt_id":   state.progressIdentity.AttemptID,
				"phase":        phase,
				"safe_to_resume": safeToResume,
				"checkpoint_id": checkpointID,
			},
		}); err != nil {
			log.Printf("harness: audit append (%s): %v", "progress", err)
		}
	}

	return rpcResponse{ID: req.ID, OK: true, Result: resp}
}

// validateStringList extracts and validates a []string from an any value.
func validateStringList(v any, name string, maxItems, maxItemLen int) ([]string, error) {
	if v == nil {
		return []string{}, nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list", name)
	}
	if len(raw) > maxItems {
		return nil, fmt.Errorf("%s exceeds max %d entries", name, maxItems)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s entries must be strings", name)
		}
		if len([]byte(s)) > maxItemLen {
			return nil, fmt.Errorf("%s entry exceeds %d bytes", name, maxItemLen)
		}
		if hasControlChars(s) {
			return nil, fmt.Errorf("%s entry contains control characters", name)
		}
		out = append(out, s)
	}
	return out, nil
}

// validateArtifactRefs validates artifact reference paths.
func validateArtifactRefs(v any) ([]string, error) {
	if v == nil {
		return []string{}, nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("artifact_references must be a list")
	}
	if len(raw) > progressArtifactMax {
		return nil, fmt.Errorf("artifact_references exceeds max %d entries", progressArtifactMax)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("artifact_reference entries must be strings")
		}
		if err := validateArtifactPath(s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// validateArtifactPath lexically validates a single artifact reference path.
func validateArtifactPath(path string) error {
	if path == "" {
		return fmt.Errorf("artifact_reference cannot be empty")
	}
	if len(path) > progressArtifactPathMax {
		return fmt.Errorf("artifact_reference exceeds %d chars", progressArtifactPathMax)
	}
	if strings.Contains(path, "\\") {
		return fmt.Errorf("artifact_reference cannot contain backslashes")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("artifact_reference cannot be absolute")
	}
	segments := strings.Split(path, "/")
	if len(segments) > progressArtifactSegMax {
		return fmt.Errorf("artifact_reference exceeds %d segments", progressArtifactSegMax)
	}
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("artifact_reference has empty or dot segment")
		}
		if !isValidArtifactSegment(seg) {
			return fmt.Errorf("artifact_reference segment %q is invalid", seg)
		}
	}
	return nil
}

// isValidArtifactSegment checks if a path segment matches ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$.
func isValidArtifactSegment(seg string) bool {
	if len(seg) == 0 || len(seg) > 128 {
		return false
	}
	for i, r := range seg {
		if i == 0 {
			if !isAlnum(r) {
				return false
			}
			continue
		}
		if !isAlnum(r) && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// hasControlChars returns true if the string contains ASCII control characters
// (U+0000–U+001F or U+007F DEL). The spec requires these to fail with a typed
// SDK/RPC error.
func hasControlChars(s string) bool {
	for _, b := range []byte(s) {
		if b < 0x20 || b == 0x7f {
			return true
		}
	}
	return false
}

// containsSecretSentinel returns true if the value matches a registered secret
// fingerprint or configured test sentinel. B27 does not claim semantic secret
// detection — this is a lexical check against known sentinels only.
var secretSentinels = [][]byte{
	[]byte("sk-"),
	[]byte("sk_or_"),
	[]byte("sk-or-"),
	[]byte("Bearer "),
	[]byte("BEGIN PRIVATE KEY"),
	[]byte("BEGIN RSA PRIVATE KEY"),
	[]byte("BEGIN EC PRIVATE KEY"),
	[]byte("journal-key"),
	[]byte("journal_key"),
}

func containsSecretSentinel(s string) bool {
	for _, sentinel := range secretSentinels {
		if bytes.Contains([]byte(s), sentinel) {
			return true
		}
	}
	return false
}

// computeArtifactMetaDigest computes a simple SHA-256 hex of the artifact
// reference list for integrity checking.
func computeArtifactMetaDigest(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	b := []byte(strings.Join(refs, "\x00"))
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
