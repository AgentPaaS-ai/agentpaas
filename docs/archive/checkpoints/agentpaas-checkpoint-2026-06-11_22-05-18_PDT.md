# AgentPaaS Checkpoint

Date/time: 2026-06-11 22:05:18 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 6 planning review after the Block 5
network-topology checkpoint. The work remained in planning/spec review mode;
no implementation code has been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_22-05-18_PDT.md`

Latest committed checkpoint entering this review:
- `81adc8f docs: checkpoint block 5 network review`

## Block 6 Review Outcome

Block 6 is now scoped as the P1 harness and Python SDK contract. It is the
execution boundary for Python agent code: loading, invoking, budget
enforcement, failure reporting, developer-visible logs, and SDK-mediated
egress/tool calls.

Decisions locked:
- P1 SDK is Python first. Node SDK and Node packaging are deferred.
- Custom Dockerfile packaging is removed from P1. Python framework detection
  covers plain Python, LangGraph, and CrewAI only.
- Agent code loads once per container; invokes are serialized by default with
  `concurrency: 1`.
- `startup_timeout` covers import/readiness.
- `max_wall_clock` measures only run receive-invoke to run finishes, using a
  monotonic clock.
- `max_iterations` means agent turns. SDK-observed LLM/tool cycles count, and
  direct `agent.llm()` calls count if there is no higher-level loop.
- Token/USD enforcement uses gateway-reported best-known usage. Provider
  usage that arrives after termination is recorded as post-hoc overage in the
  audit trail.
- On budget breach: SIGTERM, 10s grace, SIGKILL, `BUDGET_EXCEEDED`, audit
  event.
- Brokered credentials are never returned to SDK callers.
- SDK exposes noncredentialed `agent.http(...)` and brokered
  `agent.http_with_credential(credential_id, ...)`.
- Gateway policy can require every outbound HTTP call to use a named
  credential binding via `egress.require_credential_binding: true`.
- Blocked egress/tool calls are visible to developers in CLI/dashboard logs
  with reason, run id, policy digest, and strict secret/payload redaction.
- MCP calls to undeclared server/tool pairs are denied before execution and
  audited; MCP input/output bodies are not logged, only hashes and metadata.

## P2 Deferrals Captured

Agent-level checkpoint/resume and half-done job recovery are explicitly
deferred to P2. P1 restarts failed runs from a fresh container and records
structured failure context outside the container. P2 must revisit long-running
or partially completed jobs, including idempotency, external side effects,
resume state, and operator-visible recovery decisions.

Automated correction/repair loops are also P2. P1 only needs enough structured
failure context to make the future loop possible: prompt/task/code failure
reasons, stderr/stdout pointers, policy decision ids, MCP/tool availability,
SaaS/upstream availability, and prior attempt context.

P2 may also use model/context-window health, performance degradation, and
turn-count guidance to decide whether to modify an agent and retry in a fresh
container until success.

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 6
- `agentpaas-execution-plan-v1.md` Block 8
- `agentpaas-prd-v4-master.md` §2.7 API surfaces
- `agentpaas-prd-v4-master.md` §2.8 Packaging pipeline
- `agentpaas-prd-v4-master.md` §2.9 agent.yaml + policy.yaml
- `agentpaas-prd-v4-master.md` §3.1 Threat model
- `agentpaas-prd-v4-master.md` §8 Success definition

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 7, the secrets broker. Pay special
attention to the local P1 direct-lease compatibility path versus the future
enterprise managed-secret posture for corporate employee machines behind VPN.

Other open items still relevant:
1. Cosign wording: "keyless local mode: signed by agent identity key" remains
   confusing and needs a precise signing story.
2. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
3. Policy schema reference: required/optional fields, defaults, unknown fields,
   wildcard behavior, CIDR/private-network behavior, credential binding
   behavior, MCP declarations.
4. Telemetry/privacy: no telemetry without opt-in; define opt-in UX and
   payload.
5. P1 must-ship vs can-slip: plan is large; decide what can slip without
   weakening the wedge.
6. Security review packet: threat model, policy, SBOM, signed audit export,
   enforcement proof, limitations.
7. Dashboard specifics: `docs/status.md` generation and GitHub API/CLI
   dependency.
8. GitHub Project setup once private repo exists.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-11 22:05:18 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `81adc8f`. Next, review Block 7: Secrets broker,
including the P1 direct-lease compatibility path and the future enterprise
managed-secret posture."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents.
