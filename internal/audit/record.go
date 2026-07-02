package audit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// AuditRecord represents a single entry in the audit log. Each record is
// cryptographically linked to its predecessor via the hash chain.
//
// Fields:
//   - Seq: monotonically increasing sequence number (1-based).
//   - PrevHash: hex-encoded SHA-256 of the previous record's canonical JSON
//     (empty string for the genesis record at seq=1).
//   - RecordHash: hex-encoded SHA-256 of this record's canonical JSON (with
//     record_hash omitted from the hash input).
//   - Timestamp: RFC 3339 formatted timestamp of the event.
//   - EventType: a short string identifying the kind of event.
//   - DeploymentMode: either "local" or "hosted".
//   - Actor: the identity that triggered the event.
//   - Payload: arbitrary structured data associated with the event.
//   - HostedContext: optional deployment context; must be non-nil when
//     DeploymentMode is "hosted".
type AuditRecord struct {
	Seq            int64                  `json:"seq"`
	PrevHash       string                 `json:"prev_hash"`
	RecordHash     string                 `json:"record_hash"`
	Timestamp      string                 `json:"timestamp"`
	EventType      string                 `json:"event_type"`
	DeploymentMode string                 `json:"deployment_mode"`
	Actor          string                 `json:"actor"`
	Payload        map[string]interface{} `json:"payload"`
	HostedContext  *HostedContext         `json:"hosted_context,omitempty"`
}

// HostedContext captures optional deployment context for hosted deployments.
// It is serialized in the JSONL record when DeploymentMode is "hosted".
type HostedContext struct {
	TenantID        string `json:"tenant_id"`
	ProjectID       string `json:"project_id"`
	Region          string `json:"region"`
	RuntimeProvider string `json:"runtime_provider"`
}

// canonicalRecord is a helper struct used for deterministic JSON serialization.
// It mirrors AuditRecord but omits RecordHash (the hash field is excluded from
// the canonical input), and uses orderedKeyMap to ensure deterministic map
// key ordering for the Payload.
type canonicalRecord struct {
	Seq            int64           `json:"seq"`
	PrevHash       string          `json:"prev_hash"`
	Timestamp      string          `json:"timestamp"`
	EventType      string          `json:"event_type"`
	DeploymentMode string          `json:"deployment_mode"`
	Actor          string          `json:"actor"`
	Payload        orderedKeyMap   `json:"payload"`
	HostedContext  *HostedContext  `json:"hosted_context,omitempty"`
}

// orderedKeyMap is a named type for sorted-key map marshaling.
type orderedKeyMap map[string]interface{}

// MarshalJSON implements json.Marshaler for orderedKeyMap, producing
// a deterministic JSON object with keys sorted lexicographically.
func (m orderedKeyMap) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte(`{}`), nil
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf []byte
	buf = append(buf, '{')
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		// Key
		keyBytes, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("marshal key %q: %w", k, err)
		}
		buf = append(buf, keyBytes...)
		buf = append(buf, ':')

		// Value — recursively use ordered marshaling for nested maps
		valBytes, err := orderedMarshal(m[k])
		if err != nil {
			return nil, fmt.Errorf("marshal value for key %q: %w", k, err)
		}
		buf = append(buf, valBytes...)
	}
	buf = append(buf, '}')
	return buf, nil
}

// orderedMarshal recursively marshals a value, using sorted keys for maps.
func orderedMarshal(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		return orderedKeyMap(val).MarshalJSON()
	default:
		return json.Marshal(v)
	}
}

// CanonicalMarshal produces deterministic JSON for hashing. It serializes
// the record without the RecordHash field, with all map keys sorted
// lexicographically, and with no extra whitespace.
func (r *AuditRecord) CanonicalMarshal() ([]byte, error) {
	cr := canonicalRecord{
		Seq:            r.Seq,
		PrevHash:       r.PrevHash,
		Timestamp:      r.Timestamp,
		EventType:      r.EventType,
		DeploymentMode: r.DeploymentMode,
		Actor:          r.Actor,
		Payload:        orderedKeyMap(r.Payload),
		HostedContext:  r.HostedContext,
	}
	return json.Marshal(cr)
}

// ComputeRecordHash is an exported wrapper for computeRecordHash.
func (r *AuditRecord) ComputeRecordHash() (string, error) {
	return r.computeRecordHash()
}

// computeRecordHash computes the SHA-256 hex digest of the canonical JSON
// representation of the record (without the record_hash field).
func (r *AuditRecord) computeRecordHash() (string, error) {
	canonical, err := r.CanonicalMarshal()
	if err != nil {
		return "", fmt.Errorf("canonical marshal: %w", err)
	}
	hash := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", hash), nil
}