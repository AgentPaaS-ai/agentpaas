// Package runtime provides the container RuntimeDriver abstraction, Docker
// implementation, ownership naming/labels, crash reconciliation, activation
// policy enforcement, durable inbox/approval stores, model-call envelopes,
// streaming event validation, guardrail selection, and performance harness
// utilities.
//
// # RuntimeDriver
//
// RuntimeDriver abstracts create/start/stop/remove for containers and networks,
// plus exec, logs, and stats. DockerRuntime applies P1 hardening defaults
// (non-root user, read-only rootfs where applicable, cap drop, no-new-privileges,
// pids limits) and manages gateway/agent/MCP networks.
//
// # Ownership and reconcile
//
// Containers and networks are labeled with AgentPaaS ownership metadata so
// ReconcileAfterCrash can discover orphans and avoid touching unrelated
// resources (ErrNotOwned).
//
// # Activation and zero authority
//
// Activation policies cover on_demand, warm, and resident modes. Warm idle
// sandboxes must satisfy ZeroAuthorityInvariant (no residual lease, route
// capability, or applied credentials). Resident mode requires explicit
// authorization and is never inferred.
//
// # Adjacent runtime services
//
// DurableInboxStore and ApprovalStore provide WAL-backed per-tenant messaging
// and human-approval flows. ModelCallEnvelope and StreamEvent types define
// validated LLM I/O contracts with credential/CoT leakage checks.
package runtime
