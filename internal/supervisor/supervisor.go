// Package supervisor implements the durable, request-context-independent
// lifecycle supervisor for long-running invoke jobs (B30-T05).
//
// The Supervisor is the operational-truth authority for a run after durable
// admission (B26 state + B27 authenticated progress). It drives every state
// transition through compare-and-swap (CAS) writes on the durable store, never
// mutating state without a generation check. It is independent of any CLI /
// gRPC request context: it runs as a daemon-level component that the daemon
// invokes after admission and that the daemon re-invokes on restart for
// reconciliation.
//
// Liveness model (b30-summary.md T05):
//   - Accepted authenticated activity: progress/heartbeat with a valid HMAC,
//     model/HTTP/MCP start/end, checkpoint/artifact commit, and the terminal
//     job-result event.
//   - NOT accepted as progress: stdout/stderr spam, process existence, or
//     unauthenticated file writes.
//
// Stall model:
//   - A stall timer per attempt fires when (now - lastActivity) exceeds the
//     stall timeout AND no in-flight governed operation is active.
//   - While a governed operation (model/HTTP/MCP) is in flight, the stall
//     deadline is bounded by the operation deadline (the min of the operation
//     timeout, attempt-lease remaining, and active-time remaining), not the
//     raw stall timeout.
//
// Finalization model:
//   - Success is finalized ONLY from a verified InvokeJobResult event for the
//     active lease. A container exiting zero is NOT sufficient.
//   - Finalization, cancellation, and cleanup are idempotent under races via
//     CAS generations and a terminal-event fence (the control journal).
//
// Restart reconciliation (b30-summary.md T05 reconcile):
//   - Revoke any ambiguous active lease (an active lease with no committed
//     terminal event), mark FAILED with reason "daemon_restart".
//   - Ingest any already-committed terminal result/checkpoint; never blindly
//     re-invoke work.
//   - Preserve safe checkpoint/artifact state for B39 continuation.
//   - Conservatively close an interrupted active-time segment exactly once;
//     never double-charge or forgive active time.
//   - Never accrue time while the durable workflow is fully PAUSED or
//     NEEDS_REPLAN; never leave a frozen workflow with an active lease,
//     capability, or active/in-flight reservation.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// Sentinel errors. Callers MUST NOT infer behavior from error strings; match
// with errors.Is.
var (
	// ErrAttemptNotFound is returned when the supervisor has no record of the
	// attempt (it was never claimed or has been cleaned up).
	ErrAttemptNotFound = errors.New("supervisor: attempt not found")
	// ErrLeaseMismatch is returned when an event references a lease that is not
	// the active lease for the attempt (a late or forged event).
	ErrLeaseMismatch = errors.New("supervisor: lease mismatch")
	// ErrInvalidHMAC is returned when a progress or result event carries an
	// HMAC that does not verify against the attempt's control key.
	ErrInvalidHMAC = errors.New("supervisor: invalid HMAC")
	// ErrAlreadyTerminal is returned when a terminal transition is requested on
	// an attempt that is already in a terminal state. It is NOT an error for
	// idempotent finalization: Finalize/Cancel return nil when the existing
	// terminal state matches the request.
	ErrAlreadyTerminal = errors.New("supervisor: attempt already terminal")
	// ErrNotVerifiedResult is returned when a success finalization is requested
	// without a verified InvokeJobResult event for the active lease.
	ErrNotVerifiedResult = errors.New("supervisor: success requires verified result event")
	// ErrInvalidArgument is returned for malformed supervisor inputs.
	ErrInvalidArgument = errors.New("supervisor: invalid argument")
	// ErrDigestMismatch is returned when a ResultEvent's ResultDigest does not
	// match the SHA-256 of its StructuredResult.
	ErrDigestMismatch = errors.New("supervisor: result digest mismatch")
	// ErrContextBoundExceeded is returned when a durable worker exceeds its
	// configured context bound (F27).
	ErrContextBoundExceeded = errors.New("supervisor: context bound exceeded")
)

// DurableStore is the subset of routedrun.LocalStore the supervisor drives
// state transitions through. Every mutating method uses compare-and-swap on a
// generation; the supervisor NEVER mutates durable state without a CAS.
type DurableStore interface {
	// Run lifecycle.
	GetRun(ctx context.Context, runID routedrun.RunID) (*routedrun.RunRecord, error)
	GetRunGeneration(ctx context.Context, runID routedrun.RunID) (int64, error)
	UpdateRun(ctx context.Context, run *routedrun.RunRecord, expectedGeneration int64) error

	// Attempt lifecycle.
	CreateAttempt(ctx context.Context, attempt *routedrun.AttemptRecord) error
	GetAttempt(ctx context.Context, attemptID routedrun.AttemptID) (*routedrun.AttemptRecord, error)
	GetAttemptGeneration(ctx context.Context, attemptID routedrun.AttemptID) (int64, error)
	UpdateAttempt(ctx context.Context, attempt *routedrun.AttemptRecord, expectedGeneration int64) error
	ListAttempts(ctx context.Context, runID routedrun.RunID) ([]*routedrun.AttemptRecord, error)

	// Workflow lifecycle (for active-time accounting).
	GetWorkflow(ctx context.Context, workflowID routedrun.WorkflowID) (*routedrun.WorkflowRecord, error)
	UpdateWorkflow(ctx context.Context, wf *routedrun.WorkflowRecord, expectedGeneration int64) error

	// Active-time ledger.
	GetActiveTimeLedger(ctx context.Context, workflowID routedrun.WorkflowID) (*routedrun.ActiveTimeLedger, error)
	GetActiveTimeLedgerGeneration(ctx context.Context, workflowID routedrun.WorkflowID) (int64, error)
	PutActiveTimeLedger(ctx context.Context, workflowID routedrun.WorkflowID, ledger *routedrun.ActiveTimeLedger, expectedGeneration int64) error

	// Checkpoints (preserved on restart).
	SaveCheckpoint(ctx context.Context, cp *routedrun.SemanticCheckpoint) error
	GetLatestCheckpoint(ctx context.Context, attemptID routedrun.AttemptID) (*routedrun.SemanticCheckpoint, error)
}

// ResultStore persists the terminal InvokeJobResult for a run. It is the
// protected result store referenced by the T02 types. The supervisor writes a
// result ONLY after the durable state transition (CAS on the attempt) commits.
type ResultStore interface {
	// SaveInvokeJobResult persists a terminal result. It MUST be idempotent: a
	// second call for the same run with the same terminal status is a no-op.
	SaveInvokeJobResult(ctx context.Context, result *routedrun.InvokeJobResult) error
	// GetInvokeJobResult loads the terminal result for a run. Returns
	// routedrun.ErrNotFound when no result has been committed.
	GetInvokeJobResult(ctx context.Context, runID routedrun.RunID) (*routedrun.InvokeJobResult, error)
}

// ControlJournalFactory opens (or creates) the per-attempt control journal.
// The supervisor uses the journal to append ACCEPTED / STARTED / PROGRESS_REF /
// SUCCEEDED / FAILED / CANCELLED events and to replay them on restart.
type ControlJournalFactory interface {
	// OpenControlJournal returns a control journal for the given run/attempt.
	// The returned journal must be closed by the caller.
	OpenControlJournal(runID routedrun.RunID, attemptID routedrun.AttemptID) (ControlJournalHandle, error)
}

// ControlJournalHandle is the per-attempt control journal interface the
// supervisor uses. It mirrors routedrun.ControlJournal's Append/Read/Close.
type ControlJournalHandle interface {
	Append(event routedrun.InvokeJobEvent) error
	Read(fromSeq int64) ([]routedrun.InvokeJobEvent, error)
	Close() error
}

// AuditLogger publishes audit/timeline events. The supervisor publishes ONLY
// AFTER the durable state commit (CAS success) succeeds, never before.
type AuditLogger interface {
	Append(record audit.AuditRecord) error
}

// noopAuditLogger is the default audit logger when none is configured. It
// silently drops events so the supervisor can run in tests without an audit
// chain. Production wiring MUST inject a real AuditWriter.
type noopAuditLogger struct{}

// noopAuditLogger.Append discards the audit record and always succeeds.
func (noopAuditLogger) Append(audit.AuditRecord) error { return nil }

// GovernedOperationKind enumerates the in-flight governed operations whose
// presence exempts an attempt from the stall timer (bounded by the operation
// deadline). These are the authenticated model / HTTP / MCP operations whose
// start/end the supervisor tracks.
type GovernedOperationKind int

const (
	GovernedOpUnspecified GovernedOperationKind = iota
	GovernedOpModel
	GovernedOpHTTP
	GovernedOpMCP
)

// ProgressEvent is an authenticated progress / heartbeat event. The supervisor
// accepts it as liveness evidence ONLY when HMAC verifies against the
// attempt's control key. An empty HMAC or a mismatched HMAC is rejected
// (TestForgedProgressRejected).
type ProgressEvent struct {
	// AttemptID is the attempt this progress pertains to.
	AttemptID routedrun.AttemptID
	// LeaseID is the active lease for the attempt. Must match the supervisor's
	// recorded active lease or the event is rejected (late/forged).
	LeaseID routedrun.LeaseID
	// Sequence is the monotonic progress sequence. Must be strictly greater
	// than the last accepted sequence (no replay, no gaps).
	Sequence int64
	// Timestamp is the event timestamp (wall).
	Timestamp time.Time
	// Phase is an opaque phase label from the worker.
	Phase string
	// HMAC is the hex-encoded HMAC-SHA256 over the canonical event fields,
	// keyed by the attempt's control key. Empty or invalid => rejected.
	HMAC string
}

// ResultEvent is the terminal job-result event. The supervisor finalizes
// success ONLY from a verified result event for the active lease. A container
// exiting zero does NOT synthesize a ResultEvent.
type ResultEvent struct {
	// AttemptID is the attempt this result pertains to.
	AttemptID routedrun.AttemptID
	// LeaseID is the active lease for the attempt. Must match.
	LeaseID routedrun.LeaseID
	// RunID is the run this result finalizes.
	RunID routedrun.RunID
	// WorkflowID is the workflow this result finalizes.
	WorkflowID routedrun.WorkflowID
	// InvocationID is the invocation this result finalizes.
	InvocationID routedrun.InvocationID
	// TerminalStatus is the terminal outcome.
	TerminalStatus routedrun.InvokeJobResultStatus
	// StructuredResult is the bounded structured result JSON.
	StructuredResult string
	// ResultDigest is the canonical digest of StructuredResult.
	ResultDigest string
	// ArtifactReferences are relative paths under the run artifact root.
	ArtifactReferences []string
	// HMAC is the hex-encoded HMAC-SHA256 over the canonical event fields,
	// keyed by the attempt's control key. Empty or invalid => rejected.
	HMAC string
}

// CheckpointEvent is an authenticated checkpoint/artifact commit. Committing a
// checkpoint counts as accepted activity (it is durable forward progress).
type CheckpointEvent struct {
	AttemptID  routedrun.AttemptID
	LeaseID    routedrun.LeaseID
	Checkpoint *routedrun.SemanticCheckpoint
	HMAC       string
}

// ClaimOptions configures a Claim.
type ClaimOptions struct {
	// StallTimeoutMs is the per-attempt stall timeout. If 0, defaults to
	// routedrun.DefaultStallTimeoutMs.
	StallTimeoutMs int64
	// ModelCallTimeoutMs is the per-attempt model-call timeout (operation
	// deadline ceiling). If 0, defaults to routedrun.DefaultModelCallTimeoutMs.
	ModelCallTimeoutMs int64
	// AttemptLeaseMs is the attempt lease duration. If 0 the attempt has no
	// lease (stall/active-time still apply).
	AttemptLeaseMs int64
}

// Supervisor is the durable, request-context-independent lifecycle supervisor.
//
// All public methods are safe for concurrent use. State transitions go through
// CAS on the DurableStore; audit events are published only after the CAS
// commits; finalization/cancellation/cleanup are idempotent under races.
type Supervisor struct {
	store    DurableStore
	results  ResultStore
	journals ControlJournalFactory
	clock    routedrun.Clock
	timer    routedrun.Timer
	audit    AuditLogger

	// stateRoot is the durable store root, used for the result store and any
	// filesystem-backed components.
	stateRoot string

	mu      sync.Mutex
	trackers map[routedrun.AttemptID]*attemptTracker
}

// SupervisorOption configures a Supervisor.
type SupervisorOption func(*Supervisor)

// WithAuditLogger injects an audit logger. If not set, a no-op logger is used.
func WithAuditLogger(log AuditLogger) SupervisorOption {
	return func(s *Supervisor) {
		if log != nil {
			s.audit = log
		}
	}
}

// NewSupervisor constructs a Supervisor backed by the given durable store,
// result store, control-journal factory, and clock. The supervisor is
// independent of any CLI request context: callers invoke its methods from the
// daemon's post-admission path and from the restart reconciliation path.
func NewSupervisor(
	store DurableStore,
	results ResultStore,
	journals ControlJournalFactory,
	clock routedrun.Clock,
	stateRoot string,
	opts ...SupervisorOption,
) (*Supervisor, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidArgument)
	}
	if results == nil {
		return nil, fmt.Errorf("%w: nil result store", ErrInvalidArgument)
	}
	if journals == nil {
		return nil, fmt.Errorf("%w: nil control journal factory", ErrInvalidArgument)
	}
	if clock == nil {
		return nil, fmt.Errorf("%w: nil clock", ErrInvalidArgument)
	}
	s := &Supervisor{
		store:     store,
		results:   results,
		journals:  journals,
		clock:     clock,
		timer:     routedrun.SystemTimer{},
		audit:     noopAuditLogger{},
		stateRoot: stateRoot,
		trackers:  make(map[routedrun.AttemptID]*attemptTracker),
	}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// Claim acquires the active lease for an invocation's run and starts tracking
// the attempt. It is the post-admission entry point. The implementation lives
// in lifecycle.go (ClaimForRun is the lower-level entry point the daemon
// wiring uses).

// UnauthenticatedActivity records an unauthenticated activity signal
// (stdout/stderr line, process-existence poll, unauthenticated file write).
// It does NOT reset the stall timer. It exists so callers can record the
// signal for observability without it counting as progress.
func (s *Supervisor) UnauthenticatedActivity(_ context.Context, _ routedrun.AttemptID, _ string) error { // intentionally ignored (reviewed)
	return nil
}

// HandleModelStart records the start of an in-flight model call. While any
// governed operation is in flight, the stall timer is bounded by the operation
// deadline, not the raw stall timeout.
func (s *Supervisor) HandleModelStart(ctx context.Context, attemptID routedrun.AttemptID, leaseID routedrun.LeaseID) error {
	return s.handleGovernedStart(attemptID, leaseID, GovernedOpModel)
}

// HandleModelEnd records the end of an in-flight model call.
func (s *Supervisor) HandleModelEnd(ctx context.Context, attemptID routedrun.AttemptID, leaseID routedrun.LeaseID) error {
	return s.handleGovernedEnd(attemptID, leaseID, GovernedOpModel)
}

// HandleHTTPStart records the start of an in-flight HTTP operation.
func (s *Supervisor) HandleHTTPStart(ctx context.Context, attemptID routedrun.AttemptID, leaseID routedrun.LeaseID) error {
	return s.handleGovernedStart(attemptID, leaseID, GovernedOpHTTP)
}

// HandleHTTPEnd records the end of an in-flight HTTP operation.
func (s *Supervisor) HandleHTTPEnd(ctx context.Context, attemptID routedrun.AttemptID, leaseID routedrun.LeaseID) error {
	return s.handleGovernedEnd(attemptID, leaseID, GovernedOpHTTP)
}

// HandleMCPStart records the start of an in-flight MCP operation.
func (s *Supervisor) HandleMCPStart(ctx context.Context, attemptID routedrun.AttemptID, leaseID routedrun.LeaseID) error {
	return s.handleGovernedStart(attemptID, leaseID, GovernedOpMCP)
}

// HandleMCPEnd records the end of an in-flight MCP operation.
func (s *Supervisor) HandleMCPEnd(ctx context.Context, attemptID routedrun.AttemptID, leaseID routedrun.LeaseID) error {
	return s.handleGovernedEnd(attemptID, leaseID, GovernedOpMCP)
}

// Reconcile is the daemon-restart reconciliation entry point. It reads the
// control journal for the run, replays events, revokes any ambiguous active
// lease, ingests any committed terminal result/checkpoint, and never blindly
// re-invokes work. It also reconciles active-time segment state. The
// implementation lives in reconcile.go.
