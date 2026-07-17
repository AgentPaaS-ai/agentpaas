package routedrun

import (
	"context"
)

// ---------------------------------------------------------------------------
// DeploymentStore interface
// ---------------------------------------------------------------------------

// DeploymentStore defines the durable storage interface for deployments,
// aliases, and invocation admission.
type DeploymentStore interface {
	// CreateDeployment persists a new deployment record.
	CreateDeployment(ctx context.Context, dep *DeploymentRecord) error

	// GetDeployment retrieves a deployment by ID.
	GetDeployment(ctx context.Context, deploymentID DeploymentID) (*DeploymentRecord, error)

	// ListDeployments returns all deployments.
	ListDeployments(ctx context.Context) ([]*DeploymentRecord, error)

	// SetDeploymentStatus atomically updates the deployment status.
	// The generation parameter enables compare-and-swap.
	SetDeploymentStatus(ctx context.Context, deploymentID DeploymentID, status DeploymentStatus, expectedGeneration int64) error

	// CompareAndSwapAlias atomically updates an alias record.
	// Returns an error if the generation does not match.
	CompareAndSwapAlias(ctx context.Context, alias *AliasRecord) error

	// ResolveAlias returns the deployment ID that the alias currently points to.
	ResolveAlias(ctx context.Context, alias string) (*AliasRecord, error)

	// ListAliases returns all alias records.
	ListAliases(ctx context.Context) ([]*AliasRecord, error)

	// AdmitInvocation performs the one atomic admission operation:
	// idempotency lookup, canonical-intent comparison, alias/exact and
	// nested-snapshot resolution, active-status check, top-level concurrency
	// check, invocation record creation, topology-specific workflow/node/run
	// identity creation, and first durable READY launch-intent transaction.
	AdmitInvocation(ctx context.Context, request *InvocationRequest, expectedDeploymentGeneration int64) (*InvocationReceipt, error)

	// GetInvocationByIdempotency retrieves a previous invocation result for
	// idempotency replay.
	GetInvocationByIdempotency(ctx context.Context, callerIdentity, idempotencyKey string) (*InvocationReceipt, error)

	// ListInvocations lists all invocations.
	ListInvocations(ctx context.Context) ([]*InvocationReceipt, error)
}

// ---------------------------------------------------------------------------
// RunStore interface
// ---------------------------------------------------------------------------

// RunStore defines the durable storage interface for runs and attempts.
type RunStore interface {
	// CreateRun persists a new run record.
	// Top-level runs are created inside AdmitInvocation; callers must not
	// follow admission with another CreateRun. CreateRun is available only
	// for atomic workflow transitions that create dynamic service/child work.
	CreateRun(ctx context.Context, run *RunRecord) error

	// GetRun retrieves a run by ID.
	GetRun(ctx context.Context, runID RunID) (*RunRecord, error)

	// UpdateRun atomically updates a run record.
	UpdateRun(ctx context.Context, run *RunRecord, expectedGeneration int64) error

	// CreateAttempt persists a new attempt record.
	CreateAttempt(ctx context.Context, attempt *AttemptRecord) error

	// GetAttempt retrieves an attempt by ID.
	GetAttempt(ctx context.Context, attemptID AttemptID) (*AttemptRecord, error)

	// UpdateAttempt atomically updates an attempt record.
	UpdateAttempt(ctx context.Context, attempt *AttemptRecord, expectedGeneration int64) error

	// ListRuns lists runs, optionally filtered by workflow ID.
	ListRuns(ctx context.Context, workflowID WorkflowID) ([]*RunRecord, error)

	// ListAttempts lists attempts for a run.
	ListAttempts(ctx context.Context, runID RunID) ([]*AttemptRecord, error)

	// AppendLedger appends a ledger entry for active-time/cost accounting.
	AppendLedger(ctx context.Context, runID RunID, entry string) error

	// ReconcileInterrupted handles interrupted runs: revokes the lease,
	// records DAEMON_RESTARTED, and fails the attempt.
	ReconcileInterrupted(ctx context.Context, runID RunID) error
}

// ---------------------------------------------------------------------------
// WorkflowStore interface
// ---------------------------------------------------------------------------

// WorkflowStore defines the durable storage interface for workflows, nodes,
// services, handoffs, and child batches.
type WorkflowStore interface {
	// --- Workflow operations ---

	// CreateWorkflow persists a new workflow record.
	CreateWorkflow(ctx context.Context, wf *WorkflowRecord) error

	// GetWorkflow retrieves a workflow by ID.
	GetWorkflow(ctx context.Context, workflowID WorkflowID) (*WorkflowRecord, error)

	// UpdateWorkflow atomically updates a workflow record.
	UpdateWorkflow(ctx context.Context, wf *WorkflowRecord, expectedGeneration int64) error

	// ListWorkflows lists all workflows.
	ListWorkflows(ctx context.Context) ([]*WorkflowRecord, error)

	// --- Node operations ---

	// CreateNode persists a new pipeline node.
	CreateNode(ctx context.Context, node *PipelineNode) error

	// GetNode retrieves a node by ID.
	GetNode(ctx context.Context, nodeID NodeID) (*PipelineNode, error)

	// UpdateNode atomically updates a node record.
	UpdateNode(ctx context.Context, node *PipelineNode, expectedGeneration int64) error

	// ListNodes lists nodes for a workflow.
	ListNodes(ctx context.Context, workflowID WorkflowID) ([]*PipelineNode, error)

	// --- Service operations ---

	// RegisterService registers an MCP service binding.
	RegisterService(ctx context.Context, svc *MCPServiceBinding) error

	// UpdateService updates a service binding status.
	UpdateService(ctx context.Context, svc *MCPServiceBinding, expectedGeneration int64) error

	// ListServices lists service bindings for a workflow.
	ListServices(ctx context.Context, workflowID WorkflowID) ([]*MCPServiceBinding, error)

	// --- Handoff operations ---

	// CommitHandoff commits a handoff envelope atomically (single-commit).
	CommitHandoff(ctx context.Context, handoff *HandoffEnvelope) error

	// GetHandoff retrieves a handoff by ID.
	GetHandoff(ctx context.Context, handoffID HandoffID) (*HandoffEnvelope, error)

	// ListHandoffs lists handoffs for a workflow.
	ListHandoffs(ctx context.Context, workflowID WorkflowID) ([]*HandoffEnvelope, error)

	// --- Child batch operations ---

	// CreateChildBatch persists a new child batch.
	CreateChildBatch(ctx context.Context, batch *ChildBatch) error

	// UpdateChildBatch atomically updates a child batch.
	UpdateChildBatch(ctx context.Context, batch *ChildBatch, expectedGeneration int64) error

	// ListChildBatches lists child batches for a workflow.
	ListChildBatches(ctx context.Context, workflowID WorkflowID) ([]*ChildBatch, error)

	// CommitChildResult commits a child result atomically.
	CommitChildResult(ctx context.Context, result *ChildResult) error

	// ListChildResults lists child results for a child batch.
	ListChildResults(ctx context.Context, childBatchID ChildBatchID) ([]*ChildResult, error)

	// --- Control operations ---

	// RequestControl submits a control request.
	RequestControl(ctx context.Context, req *ControlRequest) error

	// GetDesiredState returns the current desired state for a workflow.
	GetDesiredState(ctx context.Context, workflowID WorkflowID) (*DesiredState, error)

	// AppendControlResult appends a control result record.
	AppendControlResult(ctx context.Context, req *ControlRequest, result interface{}) error

	// --- Amendment operations ---

	// AppendLimitAmendment atomically appends a limit amendment to a workflow.
	// Uses expectedAuthorityGeneration for compare-and-swap.
	AppendLimitAmendment(ctx context.Context, workflowID WorkflowID, expectedAuthorityGeneration int64, amendment *LimitAmendment) error

	// --- Transition operations ---

	// ApplyTransition applies one atomic logical update spanning node/run
	// result, handoff or child result, aggregate counters, and the next
	// workflow state. Uses compare-and-swap generation and idempotency.
	ApplyTransition(ctx context.Context, workflowID WorkflowID, expectedGeneration int64, command string) error
}