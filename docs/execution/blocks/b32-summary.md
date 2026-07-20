# Block 32 — Secure Task Delegation, Events, and Artifact Transfer (Simplified)

**Status:** EXECUTION-READY SPEC — simplified per architecture audit Fix 4
(2026-07-19)
**Date:** 2026-07-19 (revised)
**Target release:** stable `v0.3.0` — Agent Registry and Secure Delegation
**Depends on:** B31 (reduced) complete; `make block31-gate` green
**Must complete before:** B33–B35 and integrated supervision
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D43–D52 as
narrowed by this revision; D68 (one gateway per run)

## Why this block is simplified

The original B32 built three layers of agent-to-agent security: the
signed workflow snapshot with per-run gateways (already shipped), a
two-sided sender-egress/receiver-ingress edge compiler, and an A2A wire
-protocol adapter plus a ten-dimension encrypted grant broker. The audit
finding: the first layer already provides the security property. Per D68,
every run has its own gateway on its own internal network; the workflow
snapshot pins exact package digests; the trusted harness/gateway path
resolves logical identities to real endpoints that agent code never sees.
The second and third layers duplicate that guarantee at the application
layer for a multi-tenant, multi-organization scenario that does not exist
in v0.3.

The simplification keeps the complete user-visible and security-relevant
surface: logical task delegation, trusted endpoint resolution, durable
completion events, and digest-bound artifact transfer. It removes the A2A
wire adapter (no external partners), the two-sided edge compiler (single
workflow-snapshot check carries the same property), and the grant broker
(digest-bound artifact references through the trusted path suffice).

## Outcome

At block completion:

- A caller creates a durable task for an exact B31-registered, promoted
  package through the AgentPaaS SDK/control API and immediately receives
  durable IDs.
- The signed workflow snapshot pins caller, callee, exact package digests,
  capability/operation, input/output/data constraints, deadlines, budgets,
  and artifact audiences. Both sides' authority is checked against that one
  immutable snapshot; either side's absence stops dispatch.
- Agent code uses logical identities only (`agent.delegate(...)`). It never
  receives an IP, DNS name, container address, port, or capability token.
- All agent-to-agent traffic crosses the caller's and callee's own gateway
  sidecars over a workflow-scoped internal network with a per-binding
  unguessable capability enforced and stripped by trusted components (the
  B33 T04 network model, generalized).
- Files move as immutable, digest-verified artifact references through the
  trusted broker path — committed by the producer, authorized by the
  workflow snapshot, projected read-only to the consumer. No shared
  writable mounts, no raw storage credentials, no endpoint exposure.
- Completion commits one terminal task transition and a durable outbox
  event; waiting orchestrators suspend and wake from the B29 event store
  cursor without polling or tmux.
- Disconnect, retry, daemon restart, and duplicate delivery preserve one
  task/result and ordered resumable observation.

B32 does not implement A2A wire-protocol conformance, Agent Card exchange,
public federation, open inbound discovery, a public marketplace, recursive
swarms, arbitrary negotiation, or generic DAGs.

## What is NOT in this block (removed by Fix 4)

- **A2A wire adapter** (former T05). No external A2A partners exist. The
  canonical AgentPaaS task/message/event records are the only contract;
  they may be mapped to A2A later if a partner appears.
- **Two-sided edge compiler** (former T02). Sender authorization and
  receiver authorization are both evaluated against the single immutable
  workflow snapshot: the caller's signed workflow must name the binding,
  and the callee's signed policy must accept that caller. Audit shows both
  decisions. No separate edge graph is compiled or versioned.
- **Grant broker** (former T04 grant machinery). Artifact transfer uses
  digest-bound references with workflow-scoped audience, expiry, and
  read-only projection (the B34 T06 model). The ten-dimension grant
  record (tenant, workflow, sender, receiver, purpose, digest,
  classification, size, operation, expiry, use count) is reduced to:
  artifact ID, digest, workflow, producer, audience, expiry, classification.

## Canonical task and delegation contract

AgentPaaS records are authoritative for: tenant, workflow, task,
caller/callee deployment and attempt identities, communication snapshot
generation, task state, lease, idempotency, cancellation, messages,
artifact references, usage/budget, terminal result, ordered events, and
audit evidence.

The worker-facing API:

```python
task = agent.delegate(
    capability="report.verify",      # logical binding from signed workflow
    message={"role": "user", "parts": [...]},
    idempotency_key="...",
)

for event in task.events(after_sequence=0):
    handle(event)
```

The SDK sends no endpoint. The caller's harness holds trusted invoke state
from the daemon: the workflow snapshot with the pinned binding, the
workflow-scoped network alias, and the per-binding capability. The caller
gateway attaches the capability; the callee gateway validates it against
workflow ID, package digest, tool/operation, and both leases, then strips
it before dispatch. Python on either side never sees the alias, the
capability, or any address.

## Message and prompt contract

- Messages use explicit roles and bounded typed parts.
- Free-form prompt text is untrusted content, not policy.
- System/developer authority comes from the callee package and deployment
  snapshot, never from caller-supplied text.
- Control capabilities, raw secrets, hidden reasoning, provider
  continuation identifiers, uncommitted stream deltas, and process memory
  are forbidden in messages.
- Every message records sender, intended recipient/task, sequence, schema,
  content digest, size, timestamp, and provenance; sensitive bodies stay
  out of ordinary audit.
- Schema validation proves form, not semantic correctness.

## Artifact transfer contract

A B27 workspace artifact becomes transferable after authenticated commit:

- Record: artifact ID, content digest, workflow, producer run/attempt,
  media type, size, data classification, audience (from the signed
  snapshot), and expiry.
- Consumer receives a read-only projected mount after the runtime verifies
  digest, audience, and expiry. Downstream stages cannot mutate or
  substitute provenance.
- Expired task authority prevents new reads even if an old reference is
  known.
- Shared writable volumes, raw object-store URLs/keys, and direct peer
  file servers are forbidden.

## Completion, wait, and wake

The callee commits terminal status/result/artifact references and the
terminal outbox event transactionally. Delivery is at least once;
consumers deduplicate by task/event sequence. A parent/orchestrator
subscribes by cursor, may checkpoint and release its sandbox at a safe
wait, and resumes when the durable event arrives. Tmux, shell sessions,
open HTTP requests, and status polling are never correctness dependencies.

## Task sequence

|| Task | Name | Primary result |
|---|---|---|---|
| T01 | Canonical task/message/result schemas | Task state, messages/parts/status, idempotency, fixtures |
| T02 | Snapshot-based two-sided authorization | Caller-binding and callee-policy checks against the immutable workflow snapshot; stable denial reasons; both decisions audited |
| T03 | Logical invocation and gateway enforcement | SDK/control API, run-scoped capability, no endpoint exposure, raw east-west bypass fails |
| T04 | Digest-bound artifact transfer | Commit/authorize/project read-only; digest, audience, expiry enforced; tamper/traversal fail closed |
| T05 | Event-driven wait/wake | Transactional terminal outbox, suspend/resume, disconnect/restart, no polling |
| T06 | Adversary and cumulative gates | Snapshot forgery, prompt injection, cross-run/data leak, replay, stale lease, artifact tamper |

## Required tests

- Undeclared caller, callee, version, capability, operation, data class,
  or direction fails at dispatch and at direct network attempt.
- Caller-authorized/callee-denied and caller-denied/callee-authorized both
  fail and record distinct audit decisions (two-sided evaluation against
  the snapshot).
- Registry movement after admission cannot change an accepted task.
- Agent code receives no IP, DNS name, container address, or capability.
- Prompt text naming another agent/tool/credential cannot expand authority.
- Hidden reasoning, credential sentinel, and control capability cannot
  cross a message, checkpoint, result, or artifact record.
- Artifact digest mismatch, audience mismatch, expired reference, range
  tamper, symlink/hardlink/path traversal fail closed.
- Duplicate task/message/result/event delivery produces one committed
  effect.
- Parent disconnect/restart before and after terminal commit wakes exactly
  once from a cursor and does not poll.
- Slow event/artifact consumers obey backpressure and cannot block
  terminal persistence, fencing, or cancellation.

## Exit gate

`make block32-gate` includes the B31 gate, schema/migration/race tests,
real separate-container logical invocation, gateway-bypass adversary
tests, two-run message/artifact isolation, digest/integrity fixtures, and
restart/disconnect wait/wake proof. It is **NO-GO** if any agent-to-agent
path bypasses the gateway pair, if either side's authorization is
unevaluated against the snapshot, if a file moves through a shared path or
raw storage credential, or if completion depends on polling.

## R32 — stable `v0.3.0` release-closure checkpoint

B32 closes **Agent Registry and Secure Delegation**. The release must let
a fresh user follow one public quickstart to:

1. Build and test an agent with Hermes.
2. Promote its signed package in the local registry (B31).
3. Delegate a task to it from another agent through logical identity with
   Hermes absent before invocation.
4. Transfer one digest-verified artifact read-only under a scoped audience.
5. Disconnect and receive the terminal task event after a durable cursor.
6. Observe labelled denial of an undeclared delegation and an
   audience-mismatched artifact read.

### Mandatory stable-release closure

- Run cumulative B26–B32 deterministic, race, Docker, adversary, tenant,
  migration, upgrade, rollback, restart/reconnect, activation, and cleanup
  gates. Prove v0.2.3 pack/install/run compatibility.
- Build release binaries, SDK/schemas, checksums, SBOM, signatures,
  release notes, changelog, and sanitized consolidated v0.3 evidence from
  one exact clean commit.
- Install and upgrade through the proposed stable Homebrew formula on a
  clean macOS environment; verify CLI/daemon/harness/plugin parity.
- Truth-sync README, pitch, roadmap, quickstart, policy/security docs, and
  known limitations. State that Docker alone is supported and that
  routing, recovery, MCP runtime, pipelines, and child workflows arrive
  later.
- Attach the quickstart, terminal recording, and evidence digest to the
  GitHub release.
- Require zero open P0/P1 defect. Lower-severity defects need explicit
  ship/defer disposition.
- Present exact commit, artifacts, evidence summary, open risks, and
  release commands; wait for explicit approval before creating/pushing
  `v0.3.0` or updating stable Homebrew.
- After publish, verify public assets/signatures/Homebrew/links and rerun
  the bounded quickstart. Never move/recreate a failed tag; use `v0.3.1`
  for a compatible fix.

## Handoff to B33–B35

Record task/event/artifact schema versions, snapshot authorization
contract, capability enforcement points, broker key model, SDK methods,
and cursor/dedupe rules. B33 maps MCP service calls onto the same logical
identity and snapshot authorization; B34/B35 use the same durable tasks,
messages, artifact references, and event-driven joins for stages and
children.
