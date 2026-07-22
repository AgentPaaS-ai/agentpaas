package delegation

import (
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Bounds
// ---------------------------------------------------------------------------

const (
	maxParts         = 32
	maxTextPartBytes = 64 * 1024      // 64 KiB
	maxTotalBytes    = 256 * 1024     // 256 KiB
	maxErrorMsgBytes = 4 * 1024       // 4 KiB
	maxArtifactSegments = 8
	maxArtifactPathLen  = 512
)

// ---------------------------------------------------------------------------
// Forbidden content patterns
// ---------------------------------------------------------------------------

var forbiddenFieldPatterns = []string{
	"endpoint", "host", "ip", "port", "capability_token", "raw_url",
}

var secretSentinels = []string{
	"sk-", "sk-or-", "Bearer ", "BEGIN PRIVATE KEY",
	"journal-key", "journal_key",
}

var forbiddenPartKindPrefixes = []string{
	"hidden_reasoning", "provider_continuation",
}

// artifactSegRe is the regex for valid artifact path segments.
// Matches B27 rules: starts with alphanumeric, followed by [A-Za-z0-9._-]{0,127}.
var artifactSegRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ---------------------------------------------------------------------------
// Validation errors
// ---------------------------------------------------------------------------

// ValidationError reports a schema or content validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error returns the error message.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("delegation: %s: %s", e.Field, e.Message)
}

func newValidationError(field, msg string) *ValidationError {
	return &ValidationError{Field: field, Message: msg}
}

// ---------------------------------------------------------------------------
// Validate helpers
// ---------------------------------------------------------------------------

// hasControlChars returns true if s contains any control characters
// (U+0000–U+001F or U+007F), same rule as B27.
func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7F {
			return true
		}
	}
	return false
}

// hasForbiddenField checks a map for keys containing endpoint-like patterns
// with values that look like network URLs.
func hasForbiddenField(text string) bool {
	lower := strings.ToLower(text)
	for _, pattern := range forbiddenFieldPatterns {
		if strings.Contains(lower, `"`+pattern+`"`) || strings.Contains(lower, `"`+pattern+`":`) {
			// If we see the pattern as a JSON key, check it's not just metadata.
			// Check for URL-like values nearby.
			if strings.Contains(lower, "://") || strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
				return true
			}
		}
	}
	return false
}

// hasSecretSentinels returns true if text contains any secret sentinel strings.
func hasSecretSentinels(text string) bool {
	for _, sentinel := range secretSentinels {
		if strings.Contains(text, sentinel) {
			return true
		}
	}
	return false
}

// validateArtifactRefPath validates a logical artifact reference path
// using B27 rules: relative, no .., segment regex.
func validateArtifactRefPath(path string) error {
	if len(path) == 0 {
		return newValidationError("artifact_ref", "empty path")
	}
	if len(path) > maxArtifactPathLen {
		return newValidationError("artifact_ref", fmt.Sprintf("path exceeds %d chars", maxArtifactPathLen))
	}
	if strings.Contains(path, "\\") {
		return newValidationError("artifact_ref", "path contains backslashes")
	}
	if strings.HasPrefix(path, "/") {
		return newValidationError("artifact_ref", "path cannot be absolute")
	}
	segments := strings.Split(path, "/")
	if len(segments) > maxArtifactSegments {
		return newValidationError("artifact_ref", fmt.Sprintf("path exceeds %d segments", maxArtifactSegments))
	}
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return newValidationError("artifact_ref", "path has empty or dot segment")
		}
		if !artifactSegRe.MatchString(seg) {
			return newValidationError("artifact_ref", fmt.Sprintf("segment %q is invalid", seg))
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// ValidateTask
// ---------------------------------------------------------------------------

// ValidateTask validates a Task record.
func ValidateTask(t *Task) error {
	if t == nil {
		return newValidationError("task", "nil")
	}
	if t.SchemaVersion != CurrentSchemaVersion {
		return newValidationError("schema_version", fmt.Sprintf("expected %q, got %q", CurrentSchemaVersion, t.SchemaVersion))
	}
	if !t.TaskID.Validate() {
		return newValidationError("task_id", "empty or invalid prefix")
	}
	if t.WorkflowID == "" {
		return newValidationError("workflow_id", "empty")
	}
	if t.TenantID == "" {
		return newValidationError("tenant_id", "empty")
	}
	if t.IdempotencyKey == "" {
		return newValidationError("idempotency_key", "empty")
	}
	if t.CallerIdentity == "" {
		return newValidationError("caller_identity", "empty")
	}
	if t.BindingID == "" {
		return newValidationError("binding_id", "empty")
	}
	if !t.Status.Valid() {
		return newValidationError("status", "invalid")
	}
	if t.Caller.DeploymentID == "" {
		return newValidationError("caller.deployment_id", "empty")
	}
	if t.Caller.RunID == "" {
		return newValidationError("caller.run_id", "empty")
	}
	if t.Callee.PackageName == "" {
		return newValidationError("callee.package_name", "empty")
	}

	// Scan for endpoint-like fields.
	// No Task field may contain network endpoints.
	taskJSON := fmt.Sprintf("%+v", t)
	if hasForbiddenField(taskJSON) {
		return newValidationError("task", "contains endpoint-like field with URL value")
	}

	return nil
}

// ---------------------------------------------------------------------------
// ValidateMessage
// ---------------------------------------------------------------------------

// ValidateMessage validates a Message envelope. Rejects control chars,
// secret sentinels, oversized parts, forbidden content, and endpoint-like fields.
func ValidateMessage(m *Message) error {
	if m == nil {
		return newValidationError("message", "nil")
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		return newValidationError("schema_version", fmt.Sprintf("expected %q, got %q", CurrentSchemaVersion, m.SchemaVersion))
	}
	if !m.MessageID.Validate() {
		return newValidationError("message_id", "empty or invalid prefix")
	}
	if !m.TaskID.Validate() {
		return newValidationError("task_id", "empty or invalid prefix")
	}
	if m.WorkflowID == "" {
		return newValidationError("workflow_id", "empty")
	}
	if m.TenantID == "" {
		return newValidationError("tenant_id", "empty")
	}
	if m.Sequence < 1 {
		return newValidationError("sequence", "must be >= 1")
	}
	if !m.Role.Valid() {
		return newValidationError("role", "invalid")
	}
	if !m.Classification.Valid() {
		return newValidationError("classification", "invalid")
	}

	if len(m.Parts) == 0 {
		return newValidationError("parts", "empty")
	}
	if len(m.Parts) > maxParts {
		return newValidationError("parts", fmt.Sprintf("exceeds max %d", maxParts))
	}

	var totalSize int64
	for i, p := range m.Parts {
		if err := validateMessagePart(&p, i); err != nil {
			return err
		}
		totalSize += partByteSize(&p)
	}

	m.ByteSize = totalSize
	if totalSize > maxTotalBytes {
		return newValidationError("byte_size", fmt.Sprintf("total message size %d exceeds max %d", totalSize, maxTotalBytes))
	}

	// Check for forbidden content in the whole message.
	// ContentDigest is computed by the caller or CanonicalMessagePartsDigest.

	// Scan message text for secret sentinels.
	if err := scanMessageForSecrets(m); err != nil {
		return err
	}

	// No endpoint-like fields anywhere.
	msgJSON := fmt.Sprintf("%+v", m)
	if hasForbiddenField(msgJSON) {
		return newValidationError("message", "contains endpoint-like field")
	}

	return nil
}

func validateMessagePart(p *MessagePart, idx int) error {
	prefix := fmt.Sprintf("parts[%d]", idx)

	if !p.Kind.Valid() {
		return newValidationError(prefix+".kind", "invalid")
	}

	// Reject forbidden part kinds.
	for _, fk := range forbiddenPartKindPrefixes {
		if strings.EqualFold(string(p.Kind), fk) {
			return newValidationError(prefix+".kind", fmt.Sprintf("forbidden kind: %s", p.Kind))
		}
	}

	switch p.Kind {
	case PartKindText, PartKindError:
		if hasControlChars(p.Text) {
			return newValidationError(prefix+".text", "contains control characters")
		}
		if len(p.Text) > maxTextPartBytes {
			return newValidationError(prefix+".text", fmt.Sprintf("exceeds max %d UTF-8 bytes", maxTextPartBytes))
		}
		// Text must not contain secret sentinels.
		if hasSecretSentinels(p.Text) {
			return newValidationError(prefix+".text", "contains secret sentinel")
		}

	case PartKindJSON:
		if len(p.JSON) > maxTextPartBytes {
			return newValidationError(prefix+".json", fmt.Sprintf("exceeds max %d bytes", maxTextPartBytes))
		}
		if hasSecretSentinels(p.JSON) {
			return newValidationError(prefix+".json", "contains secret sentinel")
		}
		if hasForbiddenField(p.JSON) {
			return newValidationError(prefix+".json", "contains endpoint-like field")
		}

	case PartKindArtifactRef:
		if err := validateArtifactRefPath(p.ArtifactRef); err != nil {
			return err
		}
	}

	// MediaType must not have control characters.
	if hasControlChars(p.MediaType) {
		return newValidationError(prefix+".media_type", "contains control characters")
	}

	return nil
}

// partByteSize returns the approximate byte size of a message part.
func partByteSize(p *MessagePart) int64 {
	var size int64
	switch p.Kind {
	case PartKindText, PartKindError:
		size = int64(len(p.Text))
	case PartKindJSON:
		size = int64(len(p.JSON))
	case PartKindArtifactRef:
		size = int64(len(p.ArtifactRef))
	}
	size += int64(len(p.MediaType))
	return size
}

// scanMessageForSecrets scans all message parts for secret sentinels.
func scanMessageForSecrets(m *Message) error {
	for i, p := range m.Parts {
		if hasSecretSentinels(p.Text) {
			return newValidationError(fmt.Sprintf("parts[%d]", i), "contains secret sentinel")
		}
		if hasSecretSentinels(p.JSON) {
			return newValidationError(fmt.Sprintf("parts[%d]", i), "contains secret sentinel in json")
		}
		if hasSecretSentinels(p.ArtifactRef) {
			return newValidationError(fmt.Sprintf("parts[%d]", i), "artifact_ref contains secret sentinel")
		}
	}
	// Also check top-level fields for sentinels.
	if hasSecretSentinels(string(m.Role)) {
		return newValidationError("role", "contains secret sentinel")
	}
	return nil
}

// ValidateTaskTransitionString validates a task transition by string names.
// This is a convenience for tests and API layers.
func ValidateTaskTransitionString(from, to string) error {
	fromStatus, ok := taskStatusValues[from]
	if !ok {
		return newValidationError("from", fmt.Sprintf("unknown status: %q", from))
	}
	toStatus, ok := taskStatusValues[to]
	if !ok {
		return newValidationError("to", fmt.Sprintf("unknown status: %q", to))
	}
	return ValidateTaskTransition(fromStatus, toStatus)
}

// ---------------------------------------------------------------------------
// ValidateResult
// ---------------------------------------------------------------------------

// ValidateResult validates a task Result.
func ValidateResult(r *Result) error {
	if r == nil {
		return newValidationError("result", "nil")
	}
	if r.SchemaVersion != CurrentSchemaVersion {
		return newValidationError("schema_version", fmt.Sprintf("expected %q, got %q", CurrentSchemaVersion, r.SchemaVersion))
	}
	if !r.ResultID.Validate() {
		return newValidationError("result_id", "empty or invalid prefix")
	}
	if !r.TaskID.Validate() {
		return newValidationError("task_id", "empty or invalid prefix")
	}
	if r.WorkflowID == "" {
		return newValidationError("workflow_id", "empty")
	}
	if !r.Status.Valid() {
		return newValidationError("status", "invalid")
	}
	if !r.Status.IsTerminal() {
		return newValidationError("status", "result status must be terminal")
	}

	// Error message must not contain secrets.
	if len(r.ErrorMessage) > maxErrorMsgBytes {
		return newValidationError("error_message", fmt.Sprintf("exceeds max %d bytes", maxErrorMsgBytes))
	}
	if hasControlChars(r.ErrorMessage) {
		return newValidationError("error_message", "contains control characters")
	}
	if hasSecretSentinels(r.ErrorMessage) {
		return newValidationError("error_message", "contains secret sentinel")
	}

	for i, ref := range r.ArtifactRefs {
		if err := ValidateTransferableArtifactRef(&ref); err != nil {
			return newValidationError(fmt.Sprintf("artifact_refs[%d]", i), err.Error())
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// ValidateTransferableArtifactRef
// ---------------------------------------------------------------------------

// ValidateTransferableArtifactRef validates a TransferableArtifactRef.
func ValidateTransferableArtifactRef(ref *TransferableArtifactRef) error {
	if ref == nil {
		return newValidationError("artifact_ref", "nil")
	}
	if ref.ArtifactID == "" {
		return newValidationError("artifact_id", "empty")
	}
	if ref.Digest == "" {
		return newValidationError("digest", "empty")
	}
	if ref.WorkflowID == "" {
		return newValidationError("workflow_id", "empty")
	}
	if !ref.Classification.Valid() {
		return newValidationError("classification", "invalid")
	}
	if err := validateArtifactRefPath(ref.LogicalRef); err != nil {
		return err
	}
	// No storage URLs, host paths, shared mounts.
	if strings.Contains(ref.ArtifactID, "://") || strings.Contains(ref.ArtifactID, "/") {
		return newValidationError("artifact_id", "must not contain URL or path")
	}
	// Audience must not be empty.
	if len(ref.Audience) == 0 {
		return newValidationError("audience", "empty")
	}
	return nil
}

// ---------------------------------------------------------------------------
// ValidateTaskEvent
// ---------------------------------------------------------------------------

// ValidateTaskEvent performs lightweight validation on a TaskEvent.
func ValidateTaskEvent(ev *TaskEvent) error {
	if ev == nil {
		return newValidationError("event", "nil")
	}
	if !ev.EventID.Validate() {
		return newValidationError("event_id", "empty or invalid prefix")
	}
	if !ev.TaskID.Validate() {
		return newValidationError("task_id", "empty or invalid prefix")
	}
	if ev.WorkflowID == "" {
		return newValidationError("workflow_id", "empty")
	}
	if ev.TenantID == "" {
		return newValidationError("tenant_id", "empty")
	}
	if ev.Sequence < 1 {
		return newValidationError("sequence", "must be >= 1")
	}
	if !ev.Type.Valid() {
		return newValidationError("type", "invalid")
	}
	return nil
}

// ---------------------------------------------------------------------------
// ValidateSystemRole – gates system-role messages to WriterRuntime only.
// ---------------------------------------------------------------------------

// ValidateSystemRole returns an error if the role is system and the writer
// is not a trusted runtime.
func ValidateSystemRole(role MessageRole, writer WriterKind) error {
	if role == RoleSystem && writer != WriterRuntime {
		return newValidationError("role", "system role only allowed for trusted runtime writers")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Error codes (stable strings)
// ---------------------------------------------------------------------------

const (
	DenyCallerBinding     = "DENY_CALLER_BINDING"
	DenyCalleePolicy      = "DENY_CALLEE_POLICY"
	DenyUnpromoted        = "DENY_UNPROMOTED"
	DenySnapshotMismatch  = "DENY_SNAPSHOT_MISMATCH"
	DenyExpired           = "DENY_EXPIRED"
	DenyBudget            = "DENY_BUDGET"
	ErrIdempotencyConflict = "ERR_IDEMPOTENCY_CONFLICT"
	ErrSequenceGap         = "ERR_SEQUENCE_GAP"
	ErrInvalidMessage      = "ERR_INVALID_MESSAGE"
	ErrForbiddenContent   = "ERR_FORBIDDEN_CONTENT"
)

// ValidErrorCode returns true if code is a known stable error/denial code.
func ValidErrorCode(code string) bool {
	switch code {
	case DenyCallerBinding, DenyCalleePolicy, DenyUnpromoted,
		DenySnapshotMismatch, DenyExpired, DenyBudget,
		ErrIdempotencyConflict, ErrSequenceGap, ErrInvalidMessage,
		ErrForbiddenContent:
		return true
	}
	return false
}

// AllErrorCodes returns all known stable error/denial codes.
func AllErrorCodes() []string {
	return []string{
		DenyCallerBinding, DenyCalleePolicy, DenyUnpromoted,
		DenySnapshotMismatch, DenyExpired, DenyBudget,
		ErrIdempotencyConflict, ErrSequenceGap, ErrInvalidMessage,
		ErrForbiddenContent,
	}
}