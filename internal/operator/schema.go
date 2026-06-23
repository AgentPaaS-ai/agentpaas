package operator

import "time"

// EvidenceRef points to an auditable artifact that supports a diagnosis.
// Evidence refs never contain secret values; they are opaque identifiers a
// tool can resolve to retrieve the underlying data from the audit log, run
// store, or policy compiler.
type EvidenceRef struct {
	// Type identifies the kind of evidence.
	//   "audit_seq"     — audit log sequence number(s)
	//   "run_id"        — agent run identifier
	//   "policy_rule"   — policy rule id
	//   "span"          — OTel span id
	//   "log"           — log line id / file offset
	//   "redacted_excerpt" — sanitized excerpt of source/log/trace
	//   "verification"  — command to reproduce the finding
	Type string `json:"type"`

	// Ref is the opaque identifier for the evidence (e.g. "42" for an audit
	// seq, "run_abc123" for a run_id, "egress[2]" for a policy rule).
	Ref string `json:"ref"`

	// Detail is an optional human-readable description of what this evidence
	// proves. It is always redacted: secret values, tokens, and private keys
	// are stripped before inclusion.
	Detail string `json:"detail,omitempty"`
}

// RedactedExcerpt is a sanitized snippet of source code, log output, or trace
// data. Secret patterns (bearer tokens, API keys, private keys) are redacted
// before the excerpt is included in any operator response.
type RedactedExcerpt struct {
	// Source identifies where the excerpt came from: a file path, log file
	// name, or span id.
	Source string `json:"source"`

	// StartLine is the 1-based line number where the excerpt starts (for
	// file-based sources). 0 for non-file sources.
	StartLine int `json:"start_line,omitempty"`

	// EndLine is the 1-based line number where the excerpt ends.
	EndLine int `json:"end_line,omitempty"`

	// Content is the redacted text. Secrets are replaced with "[REDACTED]".
	Content string `json:"content"`
}

// ConfirmationRequirement encodes the trust-boundary confirmation protocol.
// When an operator method proposes a trust-boundary change (policy patch,
// credential binding, direct lease, handoff trigger, exposed listener,
// retention purge, destructive operation), the response includes this struct
// with RequiresConfirmation=true. The agentic tool cannot apply the change;
// only the daemon/UI/CLI confirmation path can.
type ConfirmationRequirement struct {
	// RequiresConfirmation is true when the proposed action crosses a trust
	// boundary and needs explicit user/daemon confirmation.
	RequiresConfirmation bool `json:"requires_confirmation"`

	// ConfirmationID is an opaque id the daemon uses to track the pending
	// confirmation. Empty when RequiresConfirmation is false.
	ConfirmationID string `json:"confirmation_id,omitempty"`

	// RiskLevel classifies the potential impact: "low", "medium", or "high".
	RiskLevel RiskLevel `json:"risk_level,omitempty"`

	// Rationale explains why the change is needed and what it does.
	Rationale string `json:"rationale,omitempty"`

	// AffectedDestinations lists network destinations affected by the change
	// (for egress/policy patches). Empty for non-network changes.
	AffectedDestinations []string `json:"affected_destinations,omitempty"`

	// CredentialIDs lists credential ids affected by the change (for
	// credential bindings and direct leases). Empty for non-credential
	// changes.
	CredentialIDs []string `json:"credential_ids,omitempty"`

	// EvidenceRefs points to audit/policy evidence supporting the proposal.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`
}

// ValidateAgentProjectResponse is the operator response for project
// readiness validation. It reports whether the project is ready to pack/run,
// and if not, lists the specific issues with evidence refs and next actions.
type ValidateAgentProjectResponse struct {
	// SchemaVersion is the operator contract schema version.
	SchemaVersion string `json:"schema_version"`

	// Ready is true when the project can be packed and run without blocking
	// issues.
	Ready bool `json:"ready"`

	// ProjectDir is the absolute path of the validated project.
	ProjectDir string `json:"project_dir"`

	// Runtime is the detected agent runtime ("python", "langgraph", etc.).
	Runtime string `json:"runtime,omitempty"`

	// Issues lists readiness problems. Empty when Ready is true.
	Issues []ValidationIssue `json:"issues,omitempty"`
}

// ValidationIssue is a single readiness problem found during project
// validation.
type ValidationIssue struct {
	// Category is the stable error category for this issue.
	Category ErrorCategory `json:"category"`

	// Message is a human-readable description of the issue.
	Message string `json:"message"`

	// EvidenceRefs point to the files/config that caused the issue.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`

	// NextAction is the recommended operator action to resolve this issue.
	NextAction NextAction `json:"next_action"`
}

// SummarizeRunResponse is the operator response for a run summary. It
// provides a structured final result for a completed (or failed) run.
type SummarizeRunResponse struct {
	SchemaVersion string    `json:"schema_version"`
	RunID         string    `json:"run_id"`
	Status        string    `json:"status"` // "completed", "failed", "running", "stopped"
	ExitCode      int       `json:"exit_code,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at,omitempty"`

	// DurationMS is the wall-clock run duration in milliseconds.
	DurationMS int64 `json:"duration_ms,omitempty"`

	// Summary is a concise natural-language description of the run outcome.
	Summary string `json:"summary"`

	// Invocations is the number of harness invocations during the run.
	Invocations int `json:"invocations,omitempty"`

	// PolicyDenials is the count of policy-denied events during the run.
	PolicyDenials int `json:"policy_denials,omitempty"`

	// ErrorCategory is set when Status is "failed".
	ErrorCategory ErrorCategory `json:"error_category,omitempty"`

	// EvidenceRefs point to the audit/timeline evidence for this summary.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`
}

// ExplainFailureResponse is the operator response for root-cause diagnosis of
// a failed run.
type ExplainFailureResponse struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`

	// ErrorCategory classifies the failure.
	ErrorCategory ErrorCategory `json:"error_category"`

	// RootCause is a concise natural-language description of the root cause.
	RootCause string `json:"root_cause"`

	// RedactedExcerpts are sanitized snippets of source/log/trace that
	// illustrate the failure. Secrets are always stripped.
	RedactedExcerpts []RedactedExcerpt `json:"redacted_excerpts,omitempty"`

	// EvidenceRefs point to the audit spans, log ids, and run state that
	// support this diagnosis.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`

	// NextAction is the recommended operator action to resolve the failure.
	NextAction NextAction `json:"next_action"`
}

// ExplainPolicyDenialResponse is the operator response for identifying the
// blocking policy rule for a denied action.
type ExplainPolicyDenialResponse struct {
	SchemaVersion string `json:"schema_version"`

	// RunID is the run that was denied (empty for pre-run policy checks).
	RunID string `json:"run_id,omitempty"`

	// DeniedAction describes what the agent tried to do.
	DeniedAction string `json:"denied_action"`

	// BlockingRuleID identifies the policy rule that denied the action
	// (e.g. "egress[2]", "default_deny").
	BlockingRuleID string `json:"blocking_rule_id"`

	// PolicyDigest is the SHA-256 digest of the compiled policy at the time
	// of denial, for audit correlation.
	PolicyDigest string `json:"policy_digest,omitempty"`

	// Rationale explains why the rule blocked the action.
	Rationale string `json:"rationale"`

	// EvidenceRefs point to the audit seq and policy decision that recorded
	// the denial.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`

	// NextAction is always "review_policy_patch" for policy denials.
	NextAction NextAction `json:"next_action"`
}

// RecommendPolicyPatchResponse is the operator response for a proposed policy
// patch. It recommends a change to the policy.yaml but does NOT apply it —
// the ConfirmationRequirement encodes the trust-boundary gate.
type RecommendPolicyPatchResponse struct {
	SchemaVersion string `json:"schema_version"`

	// ProposedPatch is a unified-diff or YAML snippet showing the policy
	// change. It is a proposal, not an applied change.
	ProposedPatch string `json:"proposed_patch"`

	// RiskLevel classifies the impact of the proposed change.
	RiskLevel RiskLevel `json:"risk_level"`

	// Rationale explains why the patch is needed and what it permits.
	Rationale string `json:"rationale"`

	// AffectedDestinations lists network destinations the patch would allow.
	AffectedDestinations []string `json:"affected_destinations,omitempty"`

	// CredentialIDs lists credential ids the patch would bind.
	CredentialIDs []string `json:"credential_ids,omitempty"`

	// EvidenceRefs point to the denial event and audit evidence motivating
	// the patch.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`

	// Confirmation encodes the trust-boundary gate. RequiresConfirmation is
	// always true for policy patches.
	Confirmation ConfirmationRequirement `json:"confirmation"`

	// NextAction is "review_policy_patch" — the human must confirm before
	// the patch is applied.
	NextAction NextAction `json:"next_action"`
}

// GetRunTimelineResponse is the operator response for a chronological event
// list for a run.
type GetRunTimelineResponse struct {
	SchemaVersion string         `json:"schema_version"`
	RunID         string         `json:"run_id"`
	Events        []TimelineEvent `json:"events"`
}

// TimelineEvent is a single event in a run timeline.
type TimelineEvent struct {
	// Timestamp is when the event occurred (RFC 3339).
	Timestamp time.Time `json:"timestamp"`

	// EventType is a short string: "run_start", "invoke", "policy_denied",
	// "invoke_complete", "run_complete", "run_failed", etc.
	EventType string `json:"event_type"`

	// AuditSeq is the audit log sequence number for this event (0 if not
	// audited).
	AuditSeq int64 `json:"audit_seq,omitempty"`

	// Detail is a concise natural-language description of the event.
	Detail string `json:"detail"`

	// EvidenceRefs point to the audit record and/or span for this event.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`
}

// NextActionResponse is the operator response for the next-action
// recommendation. It returns exactly one NextAction from the fixed enum,
// with evidence supporting the recommendation.
type NextActionResponse struct {
	SchemaVersion string `json:"schema_version"`

	// RunID is the run context (empty for project-level next actions).
	RunID string `json:"run_id,omitempty"`

	// NextAction is the recommended action from the fixed enum.
	NextAction NextAction `json:"next_action"`

	// Rationale explains why this action is recommended.
	Rationale string `json:"rationale"`

	// EvidenceRefs point to the run/audit/policy state that motivates this
	// recommendation.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`

	// Confirmation is set when NextAction requires confirmation (e.g.
	// "review_policy_patch"). Empty for non-trust-boundary actions.
	Confirmation *ConfirmationRequirement `json:"confirmation,omitempty"`
}
