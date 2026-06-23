// Package operator provides the stable machine-readable diagnosis and
// repair-hint layer consumed by the AgentPaaS CLI, dashboard, and Block 13
// MCP/Hermes integrations.
//
// The operator contract is the retroactive invariant for Blocks 1-10: every
// human-facing command that inspects, diagnoses, or repairs an agent project
// exposes a --json output backed by the versioned schemas defined in this
// package. The schemas are the contract; text output is a rendered view, not
// the source of truth.
//
// # Methods
//
// The package defines request/response types for the seven P1 operator
// methods:
//
//   - ValidateAgentProject — project readiness check
//   - SummarizeRun — final result summary for a completed run
//   - ExplainFailure — root-cause diagnosis for a failed run
//   - ExplainPolicyDenial — identify the blocking policy rule
//   - RecommendPolicyPatch — propose a safe policy patch with risk level
//   - GetRunTimeline — chronological event list for a run
//   - NextAction — recommend the next operator action
//
// # Error categories
//
// Every diagnosis method returns a stable ErrorCategory from the fixed enum.
// Categories are part of the versioned contract: adding a new category is a
// backward-compatible schema extension, but renaming or removing one is a
// breaking change.
//
// # Evidence refs
//
// Diagnosis responses include EvidenceRef values that point to auditable
// artifacts: run_id, audit sequence ranges, policy rule ids, span/log ids,
// redacted excerpts, and verification commands. Tools resolve these refs to
// retrieve the underlying data; the refs themselves never contain secret
// values.
//
// # Confirmation protocol
//
// Trust-boundary changes (policy patches, credential bindings, direct
// leases, handoff triggers, exposed listeners, retention purges, destructive
// operations) cannot be applied by an agentic tool without explicit
// user/daemon confirmation. The confirmation protocol is encoded in the
// ConfirmationRequirement type: a response with RequiresConfirmation=true
// includes a ConfirmationID, RiskLevel, rationale, and evidence refs. The
// agentic tool returns the proposal; the daemon/UI/CLI confirmation path
// applies the change.
package operator
