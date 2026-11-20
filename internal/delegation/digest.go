package delegation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// CanonicalMessagePartsDigest returns the SHA-256 hex digest of the
// canonical JSON representation of the message parts.  Parts are sorted
// by kind, then text, for deterministic output.
func CanonicalMessagePartsDigest(parts []MessagePart) (string, error) {
	// Normalize: sort parts deterministically.
	normalized := make([]MessagePart, len(parts))
	copy(normalized, parts)
	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].Kind != normalized[j].Kind {
			return string(normalized[i].Kind) < string(normalized[j].Kind)
		}
		return normalized[i].Text < normalized[j].Text
	})

	b, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("canonical parts digest: marshal: %w", err)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h), nil
}

// CanonicalResultDigest returns the SHA-256 hex digest of the canonical
// JSON representation of a Result struct (excluding the digest field itself).
func CanonicalResultDigest(r *Result) (string, error) {
	// Marshal without ContentDigest to avoid circularity.
	type resultForDigest struct {
		Status       TaskStatus                 `json:"status"`
		ErrorCode    string                     `json:"error_code,omitempty"`
		ErrorMessage string                     `json:"error_message,omitempty"`
		ArtifactRefs []TransferableArtifactRef  `json:"artifact_refs,omitempty"`
		UsageSummary *UsageSummary              `json:"usage_summary,omitempty"`
	}
	rd := resultForDigest{
		Status:       r.Status,
		ErrorCode:    r.ErrorCode,
		ErrorMessage: r.ErrorMessage,
		ArtifactRefs: r.ArtifactRefs,
		UsageSummary: r.UsageSummary,
	}
	b, err := json.Marshal(rd)
	if err != nil {
		return "", fmt.Errorf("canonical result digest: marshal: %w", err)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h), nil
}

// CanonicalJSON returns the canonical JSON bytes for any value
// (sorted keys, no trailing whitespace). Used for deterministic digests.
func CanonicalJSON(v interface{}) ([]byte, error) {
	// json.Marshal with struct tags already produces deterministic output
	// for structs. For maps, we rely on the caller to use structs.
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonical json: marshal: %w", err)
	}
	return b, nil
}

// Sha256Hex returns the SHA-256 hex digest of arbitrary bytes.
func Sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}