// Package supervisor implements the durable, request-context-independent
// lifecycle supervisor for long-running invoke jobs.
//
// The Supervisor is the operational-truth authority for a run after durable
// admission. It drives every state transition through compare-and-swap (CAS)
// writes on the durable store, never mutating state without a generation
// check. It runs as a daemon-level component invoked after admission and
// re-invoked on restart for reconciliation — independent of any CLI/gRPC
// request context.
//
// # Liveness model
//
// Accepted authenticated activity includes progress/heartbeat with a valid
// HMAC, model/HTTP/MCP start/end, checkpoint/artifact commit, and the terminal
// job-result event. Stdout/stderr spam, mere process existence, and
// unauthenticated file writes do NOT count as progress.
//
// # Stall model
//
// A stall timer per attempt fires when (now - lastActivity) exceeds the stall
// timeout AND no in-flight governed operation is active. While a governed
// operation (model/HTTP/MCP) is in flight, the stall deadline is bounded by the
// operation deadline (min of operation timeout, attempt-lease remaining, and
// active-time remaining), not the raw stall timeout alone.
//
// # Finalization model
//
// Success is finalized ONLY from a verified InvokeJobResult event for the
// active lease. A container exiting zero is NOT sufficient. Finalization,
// cancellation, and cleanup are idempotent under races via CAS generations and
// a terminal-event fence (the control journal).
//
// # Restart reconciliation
//
// On reconcile, the supervisor revokes ambiguous active leases (no committed
// terminal event) and marks FAILED with reason daemon_restart; ingests any
// already-committed terminal result/checkpoint without blindly re-invoking
// work; preserves safe checkpoint/artifact state; closes interrupted
// active-time segments exactly once; and never accrues time while the durable
// workflow is fully PAUSED or NEEDS_REPLAN.
//
// ReferenceWorker provides a deterministic multi-phase test worker that emits
// signed progress/checkpoint/result events for conformance tests.
package supervisor
