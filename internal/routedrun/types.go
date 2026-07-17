package routedrun

import (
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Version constant
// ---------------------------------------------------------------------------

// CurrentSchemaVersion is the current schema version for routed run state.
const CurrentSchemaVersion = "0.3.0"

// ---------------------------------------------------------------------------
// Deployment types
// ---------------------------------------------------------------------------

// DeploymentRecord is the immutable record of a deployment version.
type DeploymentRecord struct {
	SchemaVersion string `json:"schema_version"`

	DeploymentID     DeploymentID `json:"deployment_id"`
	PackageName      string       `json:"package_name"`
	PackageVersion   string       `json:"package_version"`
	Generation       int64        `json:"generation"`
	Status           DeploymentStatus `json:"status"`
	MaxConcurrentRuns int         `json:"max_concurrent_runs"`

	// Immutable digests
	BundleDigest       string `json:"bundle_digest"`
	PolicyDigest       string `json:"policy_digest"`
	ImageLockDigest    string `json:"image_lock_digest"`
	ProvenanceDigest   string `json:"provenance_digest"`

	// For workflow deployments: exact version/digest of every statically
	// declared stage, MCP service, and child-allowlist package.
	NestedPackageDigests map[string]string `json:"nested_package_digests,omitempty"`

	// Audit references
	CreatedAt    time.Time `json:"created_at"`
	ActivatedAt  *time.Time `json:"activated_at,omitempty"`
	DeactivatedAt *time.Time `json:"deactivated_at,omitempty"`
	CreatedBy    string    `json:"created_by"`
}

// AliasRecord is a mutable, generation-checked pointer to a deployment version.
type AliasRecord struct {
	SchemaVersion  string `json:"schema_version"`
	Alias          string `json:"alias"`
	TargetDeploymentID DeploymentID `json:"target_deployment_id"`
	TargetVersion  string `json:"target_version"`
	Generation     int64  `json:"generation"`
	UpdatedAt      time.Time `json:"updated_at"`
	UpdatedBy      string `json:"updated_by"`
}

// ---------------------------------------------------------------------------
// Invocation types
// ---------------------------------------------------------------------------

// InvocationRequest represents a durable invocation request.
type InvocationRequest struct {
	SchemaVersion string `json:"schema_version"`

	// Requested deployment reference (alias or exact version).
	RequestedDeploymentRef string `json:"requested_deployment_ref"`

	// Bounded input JSON.
	InputJSON string `json:"input_json"`
	InputDigest string `json:"input_digest"`

	// Initial ceilings.
	InitialMaxActiveDurationMs int64 `json:"initial_max_active_duration_ms"`
	InitialAttemptLeaseMs      int64 `json:"initial_attempt_lease_ms"`
	InitialMaxCostUsdDecimal   string `json:"initial_max_cost_usd_decimal"`

	// Creation options digest captures all options that can change
	// execution or authority.
	CreationOptionsDigest string `json:"creation_options_digest"`

	// Idempotency key (required by API).
	IdempotencyKey string `json:"idempotency_key"`

	// Caller identity for scoping idempotency lookup.
	CallerIdentity string `json:"caller_identity"`
}

// InvocationReceipt is the durable record returned on admission.
type InvocationReceipt struct {
	SchemaVersion string `json:"schema_version"`

	InvocationID InvocationID `json:"invocation_id"`
	WorkflowID   WorkflowID   `json:"workflow_id"`
	RunID        RunID        `json:"run_id"`

	// Resolved exact deployment identity at admission time.
	ResolvedDeploymentID      DeploymentID `json:"resolved_deployment_id"`
	ResolvedDeploymentVersion string        `json:"resolved_deployment_version"`
	ResolvedDeploymentDigest  string        `json:"resolved_deployment_digest"`

	// Nested package identities captured at admission.
	NestedPackageDigests map[string]string `json:"nested_package_digests,omitempty"`

	// Requested reference as supplied by the caller.
	RequestedDeploymentRef string `json:"requested_deployment_ref"`

	// Canonical invocation-intent digest over the deployment reference,
	// input, initial ceilings, and creation options.
	InvocationIntentDigest string `json:"invocation_intent_digest"`

	// Caller identity.
	CallerIdentity string `json:"caller_identity"`

	// Initial ceilings.
	InitialMaxActiveDurationMs int64 `json:"initial_max_active_duration_ms"`
	InitialAttemptLeaseMs      int64 `json:"initial_attempt_lease_ms"`
	InitialMaxCostUsdDecimal   string `json:"initial_max_cost_usd_decimal"`

	// Timestamp.
	AdmittedAt time.Time `json:"admitted_at"`
}

// DurableIdempotencyRecord stores the data needed to detect idempotent replays.
type DurableIdempotencyRecord struct {
	SchemaVersion string `json:"schema_version"`

	InvocationID  InvocationID `json:"invocation_id"`
	CallerIdentity string      `json:"caller_identity"`
	IdempotencyKey string      `json:"idempotency_key"`

	// Canonical intent digest for comparison.
	InvocationIntentDigest string `json:"invocation_intent_digest"`

	// Outcome of the original admission.
	Outcome AdmissionOutcome `json:"outcome"`

	CreatedAt time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Control and request types
// ---------------------------------------------------------------------------

// ControlRequest represents an operator lifecycle command.
type ControlRequest struct {
	SchemaVersion string `json:"schema_version"`

	ControlRequestID ControlRequestID `json:"control_request_id"`
	WorkflowID       WorkflowID       `json:"workflow_id"`
	Command          ControlCommand   `json:"command"`

	// Generation for compare-and-swap.
	ExpectedGeneration int64 `json:"expected_generation"`

	// For restart: the source exact deployment ref (default) or current alias.
	TargetDeploymentRef string `json:"target_deployment_ref,omitempty"`

	// For continue: recovery action.
	RecoveryAction string `json:"recovery_action,omitempty"`

	// Actor identity.
	ActorIdentity string `json:"actor_identity"`

	// Authority scope that permits this command.
	AuthorityScope AuthorityScope `json:"authority_scope"`

	IdempotencyKey string `json:"idempotency_key"`

	CreatedAt time.Time `json:"created_at"`
}

// DesiredState represents the operator-desired lifecycle state.
type DesiredState struct {
	SchemaVersion string `json:"schema_version"`

	WorkflowID         WorkflowID     `json:"workflow_id"`
	DesiredCommand     ControlCommand `json:"desired_command"`
	ControlRequestID   ControlRequestID `json:"control_request_id"`
	Generation         int64          `json:"generation"`

	// Cancellation precedence: true when cancel wins over pause/resume.
	CancelPrecedence bool `json:"cancel_precedence"`

	CreatedAt time.Time `json:"created_at"`
}

// RestartProvenance records the source of a restarted run.
type RestartProvenance struct {
	SchemaVersion string `json:"schema_version"`

	SourceRunID             RunID        `json:"source_run_id"`
	SourceWorkflowID        WorkflowID   `json:"source_workflow_id"`
	SourceInvocationID      InvocationID `json:"source_invocation_id"`
	SourceDeploymentID      DeploymentID `json:"source_deployment_id"`
	SourceDeploymentVersion string       `json:"source_deployment_version"`
	OriginalInputDigest     string       `json:"original_input_digest"`
	RestartedAt             time.Time    `json:"restarted_at"`
}

// ---------------------------------------------------------------------------
// Active-time ledger
// ---------------------------------------------------------------------------

// ActiveTimeLedger tracks active execution time for a workflow.
type ActiveTimeLedger struct {
	SchemaVersion string `json:"schema_version"`

	// Total consumed active time (RUNNING + PAUSE_REQUESTED).
	ConsumedMs int64 `json:"consumed_ms"`

	// Currently running segment start (nil if frozen/paused).
	RunningSegmentStartMs *int64 `json:"running_segment_start_ms,omitempty"`

	// Frozen state: when PAUSED or NEEDS_REPLAN, the consumed time at freeze.
	FrozenConsumedMs int64 `json:"frozen_consumed_ms,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Limit amendment
// ---------------------------------------------------------------------------

// LimitAmendment represents an administrative limit ceiling amendment.
type LimitAmendment struct {
	SchemaVersion string `json:"schema_version"`

	AmendmentID              LimitAmendmentID `json:"amendment_id"`
	WorkflowID               WorkflowID       `json:"workflow_id"`
	ExpectedAuthorityGeneration int64         `json:"expected_authority_generation"`

	// Absolute, increase-only values (optional, zero=unchanged).
	NewMaxActiveDurationMs  int64 `json:"new_max_active_duration_ms,omitempty"`
	NewCurrentAttemptLeaseMs int64 `json:"new_current_attempt_lease_ms,omitempty"`
	NewMaxLLMSpendDecimal   string `json:"new_max_llm_spend_decimal,omitempty"`

	// Before/after ceiling snapshot.
	BeforeMaxActiveDurationMs  int64  `json:"before_max_active_duration_ms"`
	BeforeMaxAttemptLeaseMs    int64  `json:"before_max_attempt_lease_ms"`
	BeforeMaxLLMSpendDecimal   string `json:"before_max_llm_spend_decimal"`
	AfterMaxActiveDurationMs   int64  `json:"after_max_active_duration_ms"`
	AfterMaxAttemptLeaseMs     int64  `json:"after_max_attempt_lease_ms"`
	AfterMaxLLMSpendDecimal    string `json:"after_max_llm_spend_decimal"`

	// Spend reservation snapshot at amendment time.
	ConsumedActiveTimeMs int64  `json:"consumed_active_time_ms"`
	ReservedSpendDecimal string `json:"reserved_spend_decimal"`

	Reason      string    `json:"reason"`
	ActorIdentity string   `json:"actor_identity"`
	IdempotencyKey string  `json:"idempotency_key"`
	NewAuthorityGeneration int64 `json:"new_authority_generation"`
	CreatedAt   time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Workflow types
// ---------------------------------------------------------------------------

// WorkflowRecord is the durable record of a workflow.
type WorkflowRecord struct {
	SchemaVersion string `json:"schema_version"`

	WorkflowID       WorkflowID     `json:"workflow_id"`
	WorkflowKind     string         `json:"workflow_kind"` // standalone, pipeline, parent_child
	InvocationID     InvocationID   `json:"invocation_id"`
	DeploymentID     DeploymentID   `json:"deployment_id"`
	Status           WorkflowStatus `json:"status"`
	Generation       int64          `json:"generation"`

	// Immutable policy and catalog snapshot refs.
	PolicyDigest       string `json:"policy_digest"`
	CatalogSnapshotRef string `json:"catalog_snapshot_ref,omitempty"`

	// Aggregate ceilings.
	MaxActiveDurationMs int64  `json:"max_active_duration_ms"`
	MaxAttemptLeaseMs   int64  `json:"max_attempt_lease_ms"`
	MaxLLMSpendDecimal  string `json:"max_llm_spend_decimal"`

	// Authority generation for limit amendments.
	AuthorityGeneration int64 `json:"authority_generation"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	TerminatedAt *time.Time `json:"terminated_at,omitempty"`
	TerminalReason *FailureReason `json:"terminal_reason,omitempty"`
}

// WorkflowPolicySnapshot contains immutable workflow-policy references.
type WorkflowPolicySnapshot struct {
	SchemaVersion string `json:"schema_version"`

	PolicyDigest       string `json:"policy_digest"`
	CatalogSnapshotRef string `json:"catalog_snapshot_ref,omitempty"`
	MaxActiveDurationMs int64  `json:"max_active_duration_ms"`
	MaxAttemptLeaseMs   int64  `json:"max_attempt_lease_ms"`
	MaxLLMSpendDecimal  string `json:"max_llm_spend_decimal"`
}

// ---------------------------------------------------------------------------
// Pipeline node/stage types
// ---------------------------------------------------------------------------

// PipelineNode represents a stage in a pipeline workflow.
type PipelineNode struct {
	SchemaVersion string `json:"schema_version"`

	NodeID   NodeID     `json:"node_id"`
	WorkflowID WorkflowID `json:"workflow_id"`
	Status   NodeStatus `json:"status"`
	RunID    RunID      `json:"run_id,omitempty"`

	// Stage order (0-based).
	StageOrder int `json:"stage_order"`

	// Package reference for this stage.
	PackageName    string `json:"package_name"`
	PackageVersion string `json:"package_version"`

	// Handoff from previous stage (nil for first stage).
	IncomingHandoffID *HandoffID `json:"incoming_handoff_id,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// NodeStateTransition records a node state transition event.
type NodeStateTransition struct {
	SchemaVersion string `json:"schema_version"`

	NodeID    NodeID     `json:"node_id"`
	FromState NodeStatus `json:"from_state"`
	ToState   NodeStatus `json:"to_state"`
	Reason    string     `json:"reason,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// MCP Service types
// ---------------------------------------------------------------------------

// MCPServiceBinding represents a binding to an MCP service.
type MCPServiceBinding struct {
	SchemaVersion string `json:"schema_version"`

	ServiceID  ServiceID      `json:"service_id"`
	WorkflowID WorkflowID     `json:"workflow_id"`
	Status     ServiceStatus  `json:"status"`

	// Package identity.
	PackageName    string `json:"package_name"`
	PackageVersion string `json:"package_version"`

	// Logical service name within the package.
	ServiceName string `json:"service_name"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ServiceLease represents a lease on an MCP service instance.
type ServiceLease struct {
	SchemaVersion string `json:"schema_version"`

	LeaseID    LeaseID    `json:"lease_id"`
	ServiceID  ServiceID  `json:"service_id"`
	WorkflowID WorkflowID `json:"workflow_id"`

	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LeaseToken string    `json:"lease_token"`
}

// ServiceHealthSummary is a compact health status for a service.
type ServiceHealthSummary struct {
	SchemaVersion string `json:"schema_version"`

	ServiceID   ServiceID     `json:"service_id"`
	Status      ServiceStatus `json:"status"`
	LastChecked time.Time     `json:"last_checked"`
	LastHealthy *time.Time    `json:"last_healthy,omitempty"`
	ErrorCount  int           `json:"error_count"`
	LastError   string        `json:"last_error,omitempty"`
}

// ---------------------------------------------------------------------------
// Handoff types
// ---------------------------------------------------------------------------

// HandoffEnvelope represents a handoff between stages with structured context
// and artifact references.
type HandoffEnvelope struct {
	SchemaVersion string `json:"schema_version"`

	HandoffID          HandoffID          `json:"handoff_id"`
	WorkflowID         WorkflowID         `json:"workflow_id"`
	SourceNodeID       NodeID             `json:"source_node_id"`
	TargetNodeID       NodeID             `json:"target_node_id"`

	// Structured context (JSON).
	ContextJSON string `json:"context_json"`

	// Artifact references.
	ArtifactRefs []ArtifactRef `json:"artifact_refs,omitempty"`

	// Classification is at least the most restrictive of producer declaration,
	// context, and referenced artifacts.
	Classification DataClassification `json:"classification"`

	CreatedAt time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Child batch types
// ---------------------------------------------------------------------------

// ChildBatch represents a batch of child runs spawned by a parent.
type ChildBatch struct {
	SchemaVersion string `json:"schema_version"`

	ChildBatchID ChildBatchID    `json:"child_batch_id"`
	WorkflowID   WorkflowID      `json:"workflow_id"`
	ParentNodeID NodeID          `json:"parent_node_id"`
	Status       ChildBatchStatus `json:"status"`

	// Spawn request details.
	SpawnRequest ChildSpawnRequest `json:"spawn_request"`

	// Join policy.
	JoinPolicy JoinPolicy `json:"join_policy"`

	// Children allocated to this batch.
	ChildRunIDs []RunID `json:"child_run_ids,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChildSpawnRequest describes a request to spawn child runs.
type ChildSpawnRequest struct {
	SchemaVersion string `json:"schema_version"`

	ChildPackageName    string `json:"child_package_name"`
	ChildPackageVersion string `json:"child_package_version"`
	MaxFanOut           int    `json:"max_fan_out"`
	MaxConcurrency      int    `json:"max_concurrency"`
	InputJSONTemplate   string `json:"input_json_template"`
}

// JoinPolicy describes how child results are joined.
type JoinPolicy struct {
	SchemaVersion string `json:"schema_version"`

	// join_all: wait for all, all_first: first result triggers join.
	Mode string `json:"mode"` // join_all, all_first
}

// ChildResult represents the result of a single child run.
type ChildResult struct {
	SchemaVersion string `json:"schema_version"`

	ChildResultID ChildResultID `json:"child_result_id"`
	ChildBatchID  ChildBatchID  `json:"child_batch_id"`
	ChildRunID    RunID         `json:"child_run_id"`
	Status        RunStatus     `json:"status"`
	OutputJSON    string        `json:"output_json,omitempty"`
	FailureReason *FailureReason `json:"failure_reason,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Run types
// ---------------------------------------------------------------------------

// RunRecord is the durable record of a run.
type RunRecord struct {
	SchemaVersion string `json:"schema_version"`

	RunID      RunID      `json:"run_id"`
	WorkflowID WorkflowID `json:"workflow_id"`
	Status     RunStatus  `json:"status"`

	// Polymorphic run kind.
	RunKind string `json:"run_kind"` // standalone, pipeline_stage, parent, child, mcp_service

	// Immutable policy and catalog snapshot refs.
	PolicyDigest       string `json:"policy_digest"`
	CatalogSnapshotRef string `json:"catalog_snapshot_ref,omitempty"`

	// Node that owns this run (for pipeline stages).
	NodeID *NodeID `json:"node_id,omitempty"`

	// Aggregate ceilings (narrowed from workflow-level).
	MaxActiveDurationMs int64  `json:"max_active_duration_ms"`
	MaxAttemptLeaseMs   int64  `json:"max_attempt_lease_ms"`
	MaxLLMSpendDecimal  string `json:"max_llm_spend_decimal"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	TerminatedAt *time.Time `json:"terminated_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Attempt types
// ---------------------------------------------------------------------------

// AttemptRecord represents a single attempt within a run.
type AttemptRecord struct {
	SchemaVersion string `json:"schema_version"`

	AttemptID AttemptID     `json:"attempt_id"`
	RunID     RunID         `json:"run_id"`
	WorkflowID WorkflowID   `json:"workflow_id"`
	Status    AttemptStatus `json:"status"`

	// Attempt number (1-based).
	AttemptNumber int `json:"attempt_number"`

	// Failure information.
	FailureReason  *FailureReason     `json:"failure_reason,omitempty"`
	FailureScope   *FailureScope      `json:"failure_scope,omitempty"`
	RecoveryDisposition *RecoveryDisposition `json:"recovery_disposition,omitempty"`
	ResumeCapability *ResumeCapability `json:"resume_capability,omitempty"`

	// Lease.
	Lease *AttemptLease `json:"lease,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	TerminatedAt *time.Time `json:"terminated_at,omitempty"`
}

// AttemptLease represents a lease on an attempt.
type AttemptLease struct {
	SchemaVersion string `json:"schema_version"`

	LeaseID    LeaseID   `json:"lease_id"`
	AttemptID  AttemptID `json:"attempt_id"`
	RunID      RunID     `json:"run_id"`

	// Lease duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LeaseToken string    `json:"lease_token"`
}

// ---------------------------------------------------------------------------
// Model requirements, candidates, and route decision
// ---------------------------------------------------------------------------

// ModelRequirements describes the minimum requirements for a model candidate.
type ModelRequirements struct {
	SchemaVersion string `json:"schema_version"`

	CapabilityTier string `json:"capability_tier"` // basic, standard, advanced
	ContextTokens  int    `json:"context_tokens"`
	Features       []string `json:"features,omitempty"` // chat, structured_json, reasoning_effort
}

// Candidate describes a model candidate for routing.
type Candidate struct {
	SchemaVersion string `json:"schema_version"`

	ID               string   `json:"id"`
	Role             string   `json:"role"`             // primary, recovery
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	Location         string   `json:"location"`         // local, cloud
	Credential       string   `json:"credential,omitempty"`
	UpstreamProviders []string `json:"upstream_providers,omitempty"`
	Endpoint         string   `json:"endpoint,omitempty"`
	AuthNone         bool     `json:"auth_none,omitempty"`
}

// RouteDecision records the routing decision for a model call.
type RouteDecision struct {
	SchemaVersion string `json:"schema_version"`

	ModelCallID    ModelCallID `json:"model_call_id"`
	AttemptID      AttemptID   `json:"attempt_id"`
	RunID          RunID       `json:"run_id"`

	CandidateID       string `json:"candidate_id"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	AttemptedRecovery bool   `json:"attempted_recovery"`
	Succeeded         bool   `json:"succeeded"`
	FailureReason     *FailureReason `json:"failure_reason,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Credential/provider availability
// ---------------------------------------------------------------------------

// WorkflowCredentialRecord tracks credential/provider availability for a workflow.
type WorkflowCredentialRecord struct {
	SchemaVersion string `json:"schema_version"`

	WorkflowID   WorkflowID `json:"workflow_id"`
	TargetID     string     `json:"target_id"`
	Provider     string     `json:"provider"`

	// Typed availability state.
	Available  bool   `json:"available"`
	Generation int64  `json:"generation"`
	Scope      string `json:"scope"` // workflow, run, attempt

	// Source of the availability record.
	SourceNodeID  NodeID    `json:"source_node_id,omitempty"`
	SourceAttempt AttemptID `json:"source_attempt_id,omitempty"`

	// Typed cause of unavailability.
	Cause   string    `json:"cause,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// ---------------------------------------------------------------------------
// Usage and cost
// ---------------------------------------------------------------------------

// NormalizedUsage represents normalized model usage for a call.
type NormalizedUsage struct {
	SchemaVersion string `json:"schema_version"`

	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// Cost represents a monetary cost.
type Cost struct {
	SchemaVersion  string `json:"schema_version"`
	AmountDecimal  string `json:"amount_decimal"`
	Currency       string `json:"currency"`
}

// ---------------------------------------------------------------------------
// Progress and checkpoint
// ---------------------------------------------------------------------------

// ProgressSummary describes progress within an attempt.
type ProgressSummary struct {
	SchemaVersion string `json:"schema_version"`

	ModelCallsCompleted int `json:"model_calls_completed"`
	ToolCallsCompleted  int `json:"tool_calls_completed"`
	ActionsSinceCheckpoint int `json:"actions_since_checkpoint"`
	ActionsWithoutProgress int `json:"actions_without_progress"`
}

// CheckpointSummary describes a checkpoint.
type CheckpointSummary struct {
	SchemaVersion string `json:"schema_version"`

	CheckpointID   CheckpointID `json:"checkpoint_id"`
	AttemptID      AttemptID    `json:"attempt_id"`
	RunID          RunID        `json:"run_id"`
	ActionCount    int          `json:"action_count"`
	TotalModelCalls int         `json:"total_model_calls"`
	CreatedAt      time.Time    `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Artifact reference
// ---------------------------------------------------------------------------

// ArtifactRef is an immutable logical reference to an artifact.
// It NEVER exposes a host/container path.
type ArtifactRef struct {
	SchemaVersion string `json:"schema_version"`

	ArtifactID ArtifactID `json:"artifact_id"`
	WorkflowID WorkflowID `json:"workflow_id"`
	NodeID     *NodeID    `json:"node_id,omitempty"`
	RunID      RunID      `json:"run_id"`
	AttemptID  AttemptID  `json:"attempt_id"`

	// Immutable logical reference (e.g., "output.json", "checkpoint.bin").
	LogicalRef string `json:"logical_ref"`

	// Digest (SHA-256 hex).
	Digest string `json:"digest"`

	// Byte size.
	ByteSize int64 `json:"byte_size"`

	// Media type (e.g., "application/json").
	MediaType string `json:"media_type"`

	// Schema reference (when declared).
	Schema string `json:"schema,omitempty"`

	// Classification.
	Classification DataClassification `json:"classification"`

	CreatedAt time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Budget summaries
// ---------------------------------------------------------------------------

// TimeBudgetSummary describes time usage within an attempt.
type TimeBudgetSummary struct {
	SchemaVersion string `json:"schema_version"`

	AttemptDurationMs int64 `json:"attempt_duration_ms"`
	RunActiveTimeMs   int64 `json:"run_active_time_ms"`
	WorkflowActiveTimeMs int64 `json:"workflow_active_time_ms"`
	RemainingMs       int64 `json:"remaining_ms"`
}

// LLMBudgetSummary describes LLM budget usage.
type LLMBudgetSummary struct {
	SchemaVersion string `json:"schema_version"`

	TotalTokens        int64  `json:"total_tokens"`
	InputTokens        int64  `json:"input_tokens"`
	OutputTokens       int64  `json:"output_tokens"`
	TotalCostDecimal   string `json:"total_cost_decimal"`
	RemainingCostDecimal string `json:"remaining_cost_decimal"`
	ModelCalls         int    `json:"model_calls"`
}

// ---------------------------------------------------------------------------
// Attempt report
// ---------------------------------------------------------------------------

// AttemptReport is the portable report for a single attempt.
type AttemptReport struct {
	SchemaVersion string `json:"schema_version"`

	RunID              RunID              `json:"run_id"`
	AttemptID          AttemptID          `json:"attempt_id"`
	Status             AttemptStatus      `json:"status"`
	Reason             *FailureReason     `json:"reason,omitempty"`
	FailureScope       *FailureScope      `json:"failure_scope,omitempty"`
	RecoveryDisposition *RecoveryDisposition `json:"recovery_disposition,omitempty"`
	ResumeCapability   *ResumeCapability  `json:"resume_capability,omitempty"`

	Progress           *ProgressSummary     `json:"progress,omitempty"`
	Checkpoint         *CheckpointSummary   `json:"checkpoint,omitempty"`
	Artifacts          []ArtifactRef        `json:"artifacts,omitempty"`
	Time               *TimeBudgetSummary   `json:"time,omitempty"`
	LLMBudget          *LLMBudgetSummary    `json:"llm_budget,omitempty"`
	RouteDecisions     []RouteDecision      `json:"route_decisions,omitempty"`
	RecommendedActions []string             `json:"recommended_actions,omitempty"`
	EvidenceRefs       []string             `json:"evidence_refs,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Workflow report
// ---------------------------------------------------------------------------

// WorkflowReport is the portable report for a completed workflow.
type WorkflowReport struct {
	SchemaVersion string `json:"schema_version"`

	WorkflowID              WorkflowID       `json:"workflow_id"`
	WorkflowKind            string           `json:"workflow_kind"`
	RequestedDeploymentRef  string           `json:"requested_deployment_ref"`
	ResolvedDeploymentID    DeploymentID     `json:"resolved_deployment_id"`
	ResolvedDeploymentVersion string          `json:"resolved_deployment_version"`
	ResolvedDeploymentDigest string           `json:"resolved_deployment_digest"`
	InvocationID            InvocationID     `json:"invocation_id"`

	Nodes          []PipelineNode        `json:"nodes,omitempty"`
	ActiveNodeIDs  []NodeID              `json:"active_node_ids,omitempty"`
	ServiceBindings []MCPServiceBinding  `json:"service_bindings,omitempty"`
	Handoffs       []HandoffEnvelope     `json:"handoffs,omitempty"`
	ChildBatches   []ChildBatch          `json:"child_batches,omitempty"`

	AggregateLimits *WorkflowPolicySnapshot `json:"aggregate_limits,omitempty"`
	AggregateUsage  *AggregateUsageSummary  `json:"aggregate_usage,omitempty"`
	ActiveTime      *ActiveTimeLedger       `json:"active_time,omitempty"`
	LimitAmendments []LimitAmendment        `json:"limit_amendments,omitempty"`
	ControlHistory  []ControlRequest        `json:"control_history,omitempty"`
	TerminalReason  *FailureReason          `json:"terminal_reason,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// AggregateUsageSummary aggregates usage across an entire workflow.
type AggregateUsageSummary struct {
	SchemaVersion       string `json:"schema_version"`
	TotalModelCalls     int    `json:"total_model_calls"`
	TotalInputTokens    int64  `json:"total_input_tokens"`
	TotalOutputTokens   int64  `json:"total_output_tokens"`
	TotalCostDecimal    string `json:"total_cost_decimal"`
	TotalActiveTimeMs   int64  `json:"total_active_time_ms"`
}

// ---------------------------------------------------------------------------
// Transition errors
// ---------------------------------------------------------------------------

// TransitionError is returned when an invalid state transition is attempted.
type TransitionError struct {
	Resource  string      `json:"resource"`
	FromState interface{} `json:"from_state"`
	ToState   interface{} `json:"to_state"`
	Message   string      `json:"message"`
}

func (e *TransitionError) Error() string {
	return e.Message
}

// NewTransitionError creates a new TransitionError.
func NewTransitionError(resource string, from, to interface{}) *TransitionError {
	return &TransitionError{
		Resource:  resource,
		FromState: from,
		ToState:   to,
		Message:   "invalid state transition: cannot move " + resource + " from " + fmtState(from) + " to " + fmtState(to),
	}
}

func fmtState(s interface{}) string {
	if str, ok := s.(fmt.Stringer); ok {
		return str.String()
	}
	return "UNKNOWN"
}