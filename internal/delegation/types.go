package delegation

import "time"

// CurrentSchemaVersion is the current schema version for delegation records.
const CurrentSchemaVersion = "0.3.0"

// ---------------------------------------------------------------------------
// Classification (reuses pack handoff strings)
// ---------------------------------------------------------------------------

// Classification represents the data sensitivity level.
type Classification string

const (
	ClassificationPublic       Classification = "public"
	ClassificationInternal     Classification = "internal"
	ClassificationConfidential Classification = "confidential"
	ClassificationRestricted   Classification = "restricted"
)

// Valid returns true if c is a known Classification.
func (c Classification) Valid() bool {
	switch c {
	case ClassificationPublic, ClassificationInternal,
		ClassificationConfidential, ClassificationRestricted:
		return true
	}
	return false
}

// AllClassifications returns all valid Classification values in order.
func AllClassifications() []Classification {
	return []Classification{
		ClassificationPublic, ClassificationInternal,
		ClassificationConfidential, ClassificationRestricted,
	}
}

// ---------------------------------------------------------------------------
// CallerRef
// ---------------------------------------------------------------------------

// CallerRef identifies the caller of a task.
type CallerRef struct {
	DeploymentID  string `json:"deployment_id"`
	RunID         string `json:"run_id"`
	AttemptID     string `json:"attempt_id"`
	PackageName   string `json:"package_name"`
	PackageDigest string `json:"package_digest"`
}

// ---------------------------------------------------------------------------
// CalleeRef
// ---------------------------------------------------------------------------

// CalleeRef identifies the callee of a task.
type CalleeRef struct {
	DeploymentID   string `json:"deployment_id,omitempty"` // resolved at admission; empty for unresolved logical pin
	PackageName    string `json:"package_name"`
	PackageVersion string `json:"package_version"`
	PackageDigest  string `json:"package_digest"`
}

// ---------------------------------------------------------------------------
// Task
// ---------------------------------------------------------------------------

// Task is the authoritative record for a delegated task.
// NO endpoints, IPs, DNS, ports, capability tokens, or container addresses
// in any field.
type Task struct {
	SchemaVersion string `json:"schema_version"`

	TaskID     TaskID `json:"task_id"`
	WorkflowID string `json:"workflow_id"`
	TenantID   string `json:"tenant_id"`

	Caller CallerRef `json:"caller"`
	Callee CalleeRef `json:"callee"`

	// BindingID is the logical binding ID from the signed workflow.
	BindingID string `json:"binding_id"`
	// Capability is the logical name from the signed workflow, e.g. "report.verify".
	Capability string `json:"capability"`
	// Operation is optional; default "" when capability alone is enough.
	Operation string `json:"operation,omitempty"`

	Status TaskStatus `json:"status"`

	// Generation is a monotonically increasing CAS field.
	Generation int64 `json:"generation"`

	// IdempotencyKey + CallerIdentity are required for admission.
	IdempotencyKey string `json:"idempotency_key"`
	CallerIdentity string `json:"caller_identity"`

	// CommunicationSnapshotGeneration pins which workflow snapshot
	// authorized this task.
	CommunicationSnapshotGeneration int64 `json:"communication_snapshot_generation"`

	// InputMessageID is optional until the first message is appended.
	InputMessageID *MessageID `json:"input_message_id,omitempty"`

	// DeadlineAt is an optional time after which the task may be expired.
	DeadlineAt *time.Time `json:"deadline_at,omitempty"`

	// Budget ceilings.
	MaxActiveDurationMs int64  `json:"max_active_duration_ms"`
	MaxCostUsdDecimal   string `json:"max_cost_usd_decimal"`

	// ResultID is set when the task reaches a terminal SUCCEEDED state.
	ResultID *ResultID `json:"result_id,omitempty"`

	// DenialReason is set when status is DENIED.
	DenialReason string `json:"denial_reason,omitempty"`
	// FailureReason is set when status is FAILED.
	FailureReason string `json:"failure_reason,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Message part
// ---------------------------------------------------------------------------

// MessagePart is a single typed part within a message.
type MessagePart struct {
	Kind PartKind `json:"kind"`

	// Text is bounded (max 64 KiB UTF-8). Set for kind=text, kind=error.
	Text string `json:"text,omitempty"`

	// JSON is a raw JSON message (size-bounded). Set for kind=json.
	JSON string `json:"json,omitempty"`

	// ArtifactRef is a logical artifact reference. Set for kind=artifact_ref.
	ArtifactRef string `json:"artifact_ref,omitempty"`

	// MediaType is the MIME type for this part.
	MediaType string `json:"media_type,omitempty"`
}

// ---------------------------------------------------------------------------
// Message envelope
// ---------------------------------------------------------------------------

// Message is a durable message envelope within a task.
// Forbidden: endpoints, IPs, DNS, ports, capability tokens, raw URLs,
// secret sentinels, control characters, hidden_reasoning, provider
// continuation identifiers.
type Message struct {
	SchemaVersion string    `json:"schema_version"`
	MessageID     MessageID `json:"message_id"`
	TaskID        TaskID    `json:"task_id"`
	WorkflowID    string    `json:"workflow_id"`
	TenantID      string    `json:"tenant_id"`

	// Sequence is monotonic per task, starting at 1.
	Sequence int64 `json:"sequence"`

	Role              MessageRole `json:"role"`
	SenderLogicalID   string      `json:"sender_logical_id"`
	RecipientLogicalID string     `json:"recipient_logical_id"`

	Parts []MessagePart `json:"parts"`

	// ContentDigest is the SHA-256 hex of the canonical parts JSON.
	ContentDigest string `json:"content_digest"`

	// ByteSize is the total message byte size (≤ 256 KiB).
	ByteSize int64 `json:"byte_size"`

	Classification Classification `json:"classification"`

	CreatedAt time.Time `json:"created_at"`

	// IdempotencyKey is optional.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// ---------------------------------------------------------------------------
// Task result
// ---------------------------------------------------------------------------

// Result is the terminal result of a task.
type Result struct {
	SchemaVersion string   `json:"schema_version"`
	ResultID      ResultID `json:"result_id"`
	TaskID        TaskID   `json:"task_id"`
	WorkflowID    string   `json:"workflow_id"`

	// Status must match the task's terminal status.
	Status TaskStatus `json:"status"`

	// OutputMessageID is optional.
	OutputMessageID *MessageID `json:"output_message_id,omitempty"`

	// ArtifactRefs are schema-only transferable artifact references.
	ArtifactRefs []TransferableArtifactRef `json:"artifact_refs,omitempty"`

	// ErrorCode is a stable error code (e.g. ERR_SEQUENCE_GAP).
	ErrorCode string `json:"error_code,omitempty"`
	// ErrorMessage is bounded and must not contain secrets.
	ErrorMessage string `json:"error_message,omitempty"`

	// UsageSummary is optional (tokens/cost strings).
	UsageSummary *UsageSummary `json:"usage_summary,omitempty"`

	ContentDigest string    `json:"content_digest"`
	CreatedAt     time.Time `json:"created_at"`
}

// UsageSummary captures optional token and cost usage.
type UsageSummary struct {
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	TotalCostUsdStr string `json:"total_cost_usd_str,omitempty"`
}

// ---------------------------------------------------------------------------
// Transferable artifact reference (schema only — T04 implements broker)
// ---------------------------------------------------------------------------

// TransferableArtifactRef is a reduced grant model from B32 simplification.
// NEVER: storage URL, raw credential, host path, shared mount path.
type TransferableArtifactRef struct {
	ArtifactID       string `json:"artifact_id"`
	Digest           string `json:"digest"` // SHA-256 hex
	WorkflowID       string `json:"workflow_id"`
	ProducerRunID    string `json:"producer_run_id"`
	ProducerAttemptID string `json:"producer_attempt_id"`
	ProducerTaskID   string `json:"producer_task_id"`
	MediaType        string `json:"media_type"`
	ByteSize         int64  `json:"byte_size"`
	Classification   Classification `json:"classification"`
	// Audience is the list of logical package/deployment IDs allowed to read.
	Audience  []string  `json:"audience"`
	ExpiresAt time.Time `json:"expires_at"`
	// LogicalRef is a relative path (e.g. "output.json").
	LogicalRef string `json:"logical_ref"`
}

// ---------------------------------------------------------------------------
// Task event
// ---------------------------------------------------------------------------

// TaskEvent is an ordered observation event for a task.
// Delivery is at-least-once; consumers dedupe by task_id+sequence.
type TaskEvent struct {
	EventID    EventID   `json:"event_id"`
	TaskID     TaskID    `json:"task_id"`
	WorkflowID string    `json:"workflow_id"`
	TenantID   string    `json:"tenant_id"`
	Sequence   int64     `json:"sequence"`
	Type       EventType `json:"type"`
	// PayloadDigest is optional.
	PayloadDigest string    `json:"payload_digest,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}