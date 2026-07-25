package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ValidateHandoffEnvelope validates a B34 handoff envelope against conformance rules.
// Returns stable failure codes.
func ValidateHandoffEnvelope(ho *HandoffEnvelope) []string {
	if ho == nil {
		return []string{CodeHandoffSchemaVersion}
	}

	var codes []string

	// Schema version check.
	if ho.SchemaVersion != SchemaVersionHandoffV1 {
		codes = append(codes, CodeHandoffSchemaVersion)
	}

	// Required producer identity fields.
	if ho.ProducerRunID == "" || ho.ProducerAttemptID == "" {
		codes = append(codes, CodeHandoffNonCanonical)
	}
	if ho.WorkflowID == "" || ho.HandoffID == "" {
		codes = append(codes, CodeHandoffNonCanonical)
	}
	if ho.FromNodeID == "" || ho.ToNodeID == "" {
		codes = append(codes, CodeHandoffNonCanonical)
	}

	// ProducerResultDigest must be sha256:hex.
	if !isValidDigest(ho.ProducerResultDigest) {
		codes = append(codes, CodeHandoffDigestMismatch)
	}

	// Classification validation.
	envRank := classificationRank(ho.Classification)
	if envRank == -1 {
		codes = append(codes, CodeHandoffDeclassification)
	}

	// Context validation.
	if code := validateContextValue(ho.Context.Value); code != "" {
		codes = append(codes, code)
	}

	// Context size check.
	if len(ho.Context.Value) > HandoffContextMaxBytes {
		codes = append(codes, CodeHandoffContextOversize)
	}

	// Check for reserved keys in context.
	if hasReservedKeys(ho.Context.Value) {
		codes = append(codes, CodeHandoffReservedKey)
	}

	// Artifact validation.
	if len(ho.Artifacts) > 0 {
		// Aggregate metadata size check.
		metaSize := estimateArtifactMetaSize(ho.Artifacts)
		if metaSize > HandoffArtifactMetaMaxBytes {
			codes = append(codes, CodeHandoffArtifactMetaOversize)
		}

		for i, art := range ho.Artifacts {
			// Artifact digest validation.
			if !isValidDigest(art.Digest) {
				if !ContainsCode(codes, CodeHandoffDigestMismatch) {
					codes = append(codes, CodeHandoffDigestMismatch)
				}
			}

			// Artifact classification must not be less restrictive than envelope.
			if envRank != -1 {
				artRank := classificationRank(art.Classification)
				if artRank != -1 && artRank < envRank {
					codes = append(codes, CodeHandoffDeclassification)
				}
			}

			// immutable_ref path safety.
			if !isSafeRef(art.ImmutableRef) {
				codes = append(codes, CodeHandoffReservedKey)
			}

			_ = i // used in diagnostic context
		}
	}

	return codes
}

// validateContextValue checks if the context value is valid canonical JSON.
func validateContextValue(value json.RawMessage) string {
	if len(value) == 0 {
		return "" // empty context is valid
	}

	// Try to unmarshal as JSON object/array/value.
	var v interface{}
	if err := json.Unmarshal(value, &v); err != nil {
		return CodeHandoffNonCanonical
	}
	return ""
}

// isValidDigest checks if a digest string is a valid sha256:hex format.
func isValidDigest(d string) bool {
	if d == "" {
		return false
	}
	prefix := "sha256:"
	if !strings.HasPrefix(d, prefix) {
		return false
	}
	hexPart := d[len(prefix):]
	if len(hexPart) == 0 {
		return false
	}
	// Must be lowercase hex characters.
	for _, c := range hexPart {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// isSafeRef checks that an immutable_ref does not contain path traversal or absolute paths.
func isSafeRef(ref string) bool {
	if ref == "" {
		return true
	}
	if strings.HasPrefix(ref, "/") {
		return false
	}
	if strings.Contains(ref, "..") {
		return false
	}
	for _, prefix := range forbiddenPathPrefixes {
		if strings.HasPrefix(ref, prefix) {
			return false
		}
	}
	return true
}

// hasReservedKeys checks if the context value contains any reserved keys.
func hasReservedKeys(value json.RawMessage) bool {
	if len(value) == 0 {
		return false
	}
	var m map[string]interface{}
	if err := json.Unmarshal(value, &m); err != nil {
		// If it's not a map, check for reserved key strings in the raw value.
		for k := range reservedContextKeys {
			if strings.Contains(string(value), fmt.Sprintf(`"%s"`, k)) {
				return true
			}
		}
		return false
	}
	for k := range m {
		if reservedContextKeys[k] {
			return true
		}
		// Also check nested values for reserved patterns (path-like).
		if s, ok := m[k].(string); ok {
			if strings.HasPrefix(s, "/var/") || strings.HasPrefix(s, "/app/") ||
				strings.HasPrefix(s, "/etc/") || strings.HasPrefix(s, "/root/") {
				return true
			}
			if strings.Contains(s, "../") {
				return true
			}
		}
	}
	return false
}

// estimateArtifactMetaSize estimates the aggregate size of artifact reference metadata.
func estimateArtifactMetaSize(artifacts []HandoffArtifact) int {
	total := 0
	for _, a := range artifacts {
		total += len(a.ArtifactID)
		total += len(a.OwnerNodeID)
		total += len(a.OwnerRunID)
		total += len(a.ImmutableRef)
		total += len(a.Digest)
		total += len(a.MediaType)
		total += 8 // SizeBytes (int64)
		total += len(a.Classification)
	}
	return total
}

// ---------------------------------------------------------------------------
// Handoff ID store for duplicate detection (pure function, no persistent store)
// ---------------------------------------------------------------------------

// HandoffIDStore tracks handoff IDs for duplicate detection.
type HandoffIDStore struct {
	mu   sync.Mutex
	seen map[string]string // handoff_id -> canonical content hash
}

// NewHandoffIDStore creates a new in-memory handoff ID store.
func NewHandoffIDStore() *HandoffIDStore {
	return &HandoffIDStore{
		seen: make(map[string]string),
	}
}

// newHandoffIDStore is an internal alias for testing.
func newHandoffIDStore() *HandoffIDStore {
	return NewHandoffIDStore()
}

// Record stores a handoff ID and returns an error if the same ID was already
// recorded with different content.
func (s *HandoffIDStore) Record(ho *HandoffEnvelope) error {
	if ho == nil || ho.HandoffID == "" {
		return fmt.Errorf("%s: handoff_id is required", CodeHandoffNonCanonical)
	}

	// Compute a content hash (simple JSON marshal of key fields).
	contentKey := handoffContentKey(ho)

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.seen[ho.HandoffID]; ok {
		if existing != contentKey {
			return fmt.Errorf("%s: handoff_id %q already recorded with different content", CodeHandoffDuplicateID, ho.HandoffID)
		}
		return nil // same content, idempotent
	}

	s.seen[ho.HandoffID] = contentKey
	return nil
}

// handoffContentKey produces a deterministic key for handoff content comparison.
func handoffContentKey(ho *HandoffEnvelope) string {
	// Use only the fields that define content identity (not timestamps).
	data, _ := json.Marshal(struct {
		WorkflowID          string          `json:"workflow_id"`
		FromNodeID          string          `json:"from_node_id"`
		ToNodeID            string          `json:"to_node_id"`
		ProducerRunID       string          `json:"producer_run_id"`
		ProducerAttemptID   string          `json:"producer_attempt_id"`
		ProducerResultDigest string         `json:"producer_result_digest"`
		Sequence            int             `json:"sequence"`
		Classification      string          `json:"classification"`
		Context             HandoffContext  `json:"context"`
		Artifacts           []HandoffArtifact `json:"artifacts"`
	}{
		WorkflowID:          ho.WorkflowID,
		FromNodeID:          ho.FromNodeID,
		ToNodeID:            ho.ToNodeID,
		ProducerRunID:       ho.ProducerRunID,
		ProducerAttemptID:   ho.ProducerAttemptID,
		ProducerResultDigest: ho.ProducerResultDigest,
		Sequence:            ho.Sequence,
		Classification:      ho.Classification,
		Context:             ho.Context,
		Artifacts:           ho.Artifacts,
	})
	return string(data)
}
