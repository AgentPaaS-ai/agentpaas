# Block 29 — Agent Runtime Profiles, Durable Events, Streaming, and Efficiency

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Target release:** v0.3.0
**Depends on:** B28 complete; `make block28-gate` green
**Must complete before:** B30 and every registry/coordination/routing block
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D52, D55–D65

## Outcome

B29 replaces the current partial streaming scaffolding with a durable agent
runtime contract and a measured activation model. It defines which features an
agent requires, which features a runtime/model adapter supplies, and how
clients observe work without owning its lifetime.

At block completion:

- A versioned Agent Runtime Profile declares baseline and optional execution
  features; incompatible packages, deployments, and targets fail before start.
- Existing `agent.llm(prompt, ...)` remains a compatible buffered call.
- A normalized multi-role model-call envelope supports structured messages,
  structured output, tool-call parts, reasoning controls/usage, cancellation,
  deadlines, and provider-extension metadata inside signed policy.
- A streaming SDK call yields ordered model/output/tool/usage events and
  commits one canonical final result.
- `InvokeStream` invokes the real durable admission path and then subscribes to
  the accepted run; it never manufactures success or makes the connection the
  run owner.
- Run, progress, model-output, task-input, wait, control, and terminal events
  are durable, ordered, resumable after a cursor, and replayable after daemon
  restart.
- Client disconnect does not cancel work. Explicit authenticated cancel,
  deadline, lease, policy, or budget exhaustion does.
- An interactive inbox can durably add approved input/approval events to an
  existing task and wake it at a safe boundary.
- Activation is explicit: `on_demand`, `warm`, or `resident`. Ordinary agents
  default to `on_demand`; availability never implies a permanently running
  process.
- Platform and provider latency are traced separately and a repeatable
  performance/efficiency baseline is recorded.

B29 does not implement long-running proof, a component catalog, A2A delegation,
MCP service routing, pipelines, fan-out, model selection, or recovery. It
provides the contracts those blocks consume.

## Current behavior that must be characterized and replaced

- Python `agent.llm()` returns one complete response; the harness buffers the
  full provider body.
- The protobuf `InvokeStream` method does not invoke the production run path.
- SSE/event-bus state is in memory, has bounded subscriber channels that can
  drop events, and cannot resume across daemon restart.
- B27 progress/checkpoint state is durable, but external event subscription is
  not the durable source of truth.
- The current run creates agent/gateway containers and networks per invocation
  and removes them at terminal cleanup; there is no warm pool.

Tests must freeze these observations before replacement so no prototype path is
mistaken for a completed contract.

## Agent Runtime Profile

Every package declares `runtime_profile_version` plus required features. The
baseline v0.3 profile contains:

- bounded structured input and output;
- multi-role messages (`system`, `developer`, `user`, `assistant`, `tool`);
- buffered model calls and provider usage;
- semantic progress, checkpoints, artifacts, cancellation, and deadlines;
- governed HTTP and MCP calls;
- ordered durable run events.

Negotiated optional features include:

- `model_streaming` and `tool_call_streaming`;
- `structured_output` with an explicit JSON schema;
- `reasoning_controls`, reasoning-token accounting, and provider-approved
  reasoning summaries, never raw chain-of-thought;
- `interactive_inbox` and durable external waits;
- `multimodal_artifact_parts`;
- bounded concurrent model/tool calls;
- provider extensions under a namespaced, policy-reviewed field.

A deployment records its required profile. A model/runtime/component record
declares supported features. Admission and later catalog resolution use set
inclusion; marketing names or silent best-effort downgrade are forbidden.

## Locked SDK compatibility and streaming contract

The legacy method remains:

```python
result = agent.llm(prompt, **options)
```

It is a buffered compatibility wrapper over the normalized call envelope. The
additive streaming method is:

```python
for event in agent.llm_stream(prompt=None, messages=None, **options):
    handle(event)
```

Exactly one of `prompt` or `messages` is supplied. Event kinds are versioned
and minimally include:

- `response_started`;
- `output_delta`;
- `tool_call_delta`;
- `usage_update`;
- `response_completed`;
- `response_failed`.

Every event has logical call ID, physical request ID where applicable, sequence,
timestamp, target identity when resolved, and bounded payload. The harness
assembles the authoritative result, verifies the terminal usage/result, and
commits it once. An interrupted stream can resume observation after a cursor;
it does not blindly repeat an external request.

## Streaming security

- Raw credentials never enter an event or SDK response.
- Input/output/token limits are enforced incrementally.
- Backpressure has explicit byte/event/time bounds; a slow observer cannot
  exhaust trusted memory or block terminal persistence.
- A strict whole-response guardrail selects `buffered_release` and does not
  claim token streaming.
- `incremental_release` is permitted only when every configured response
  filter declares stream-safe incremental semantics.
- Cancellation, fencing, or budget exhaustion closes the upstream request,
  commits a typed terminal event, and rejects late deltas.
- Partial output is marked uncommitted and cannot become checkpoint input,
  recovery replay context, or a successful result.
- Hidden reasoning/chain-of-thought is never requested, relayed, stored, or
  exposed. Usage dimensions and approved summaries are permitted.

## Durable event and interactive-wait contract

The B28 `EventStore` is authoritative. A state transition and its outbox event
commit atomically. Delivery is at least once; consumers deduplicate by
`run_id + sequence` or `event_id`. Subscriptions accept `after_sequence` and
may use gRPC streaming, SSE, WebSocket, or an internal wait primitive without
changing semantics.

`Invoke` returns durable IDs. `InvokeStream` performs the same idempotent
admission and subscribes to that run. Reconnecting with the same invocation and
cursor never creates another run.

An approved sender can append an inbox message or approval to a task. A worker
waiting at a safe boundary has no open client request dependency. The
supervisor may stop an on-demand sandbox and later resume it from committed
state when the event arrives. Input content is untrusted data and cannot expand
authority.

## Activation contract

- `on_demand`: no live agent process, task capability, credential, or network
  authority between tasks. An exact image may remain cached.
- `warm`: a bounded idle sandbox/harness may remain ready, but it has no task
  lease, route capability, or applied credential until admission. Idle timeout,
  tenant, package digest, maximum pool size, and resource charge are explicit.
- `resident`: an explicitly authorized service/event consumer remains active
  and is continuously metered. Resident is never inferred from catalog
  availability.

The always-ready component is the authenticated AgentPaaS control/ingress
plane. Ordinary workers, verifiers, and testing agents default to scale-to-zero.

## Performance and efficiency contract

Every invocation emits separate spans/metrics for:

- authentication/policy/admission;
- queue/claim;
- image pull or cache hit;
- network/gateway preparation;
- sandbox start and harness readiness;
- time to first progress and first model token;
- each model/tool call and artifact transfer;
- wait/wakeup, child/stage transition where applicable;
- terminal-event publication and cleanup.

Record p50/p95/p99 distributions for cold-image, cached-cold, warm, and resident
modes where supported. Provider/tool time is reported separately from
AgentPaaS overhead. Also record idle/active CPU, memory, PIDs, container-seconds,
gateway overhead, bytes, stored bytes, token/cache use, and cost per completed
task. Initial numeric SLOs are approved only after the baseline exists.

## Task sequence

| Task | Name | Primary result |
|---|---|---|
| T01 | Streaming/lifecycle characterization | Tests prove the current buffered LLM, synthetic `InvokeStream`, in-memory events, and cold-per-run lifecycle |
| T02 | Runtime profile and normalized envelopes | Versioned schemas, compatibility wrappers, feature negotiation, and fail-closed validation |
| T03 | Durable event store/outbox/subscriptions | Atomic outbox, cursors, replay, dedupe, SSE/gRPC adapters, restart proof |
| T04 | Governed model streaming | SDK/harness/provider adapters, incremental usage/budget, guardrail modes, backpressure, cancel |
| T05 | Interactive inbox and suspend/wake | Durable messages/approvals, safe wait, disconnect/reconnect, no polling |
| T06 | Activation policies | On-demand default, bounded warm proof, resident contract for later service use, zero-authority idle state |
| T07 | Performance conformance harness | Cold/cached/warm traces, resource metrics, concurrency/load fixtures, baseline report |
| T08 | Integrated adversary and cumulative gate | Slow consumer, replay, forged cursor, cross-tenant subscription, partial-stream failure, cleanup |

## Required tests

- Legacy buffered SDK and v0.2.3 agents remain unchanged.
- Multi-role/structured/tool/reasoning capability negotiation fails closed when
  unsupported.
- Real `InvokeStream` and `Invoke` with one idempotency key identify one run.
- Disconnect/reconnect after every event boundary yields ordered replay and one
  terminal event.
- Daemon restart loses no committed event or inbox message.
- Subscriber overflow spills/reconnects or closes explicitly; it never silently
  drops an authoritative event.
- Slow consumer cannot block provider cancellation or terminal commit.
- Whole-response guardrail emits no early deltas.
- Incremental budget/lease exhaustion rejects every late delta and action.
- Raw credentials and hidden reasoning are absent from stream, logs, audit, and
  artifacts.
- Dormant on-demand deployment uses zero agent CPU/memory and retains no live
  task/network/credential authority after cleanup.
- Warm sandbox cannot call a model/tool/egress endpoint before task admission.

## Exit gate

`make block29-gate` includes the B28 gate, Go/Python unit/race/adversary tests,
Docker streaming and activation integration, restart/reconnect tests, and the
recorded performance baseline. It is **NO-GO** if model streaming bypasses a
configured guardrail/budget, if `InvokeStream` does not run the real admission
path, if event replay is memory-only, or if a dormant/warm worker retains task
authority.

## Handoff to B30

Record the profile/envelope/event schema versions, streaming filter modes,
activation defaults, benchmark fixtures, and measured local lifecycle
breakdown. B30 must use the durable event/job path and must test a real
multi-turn streaming-capable worker without making an observer connection the
run lifetime.
