# Block 30 — Long-Running Multi-Turn Execution and Proof

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Target release:** `v0.3.0-alpha.1` GitHub prerelease; stable v0.3 closes at B32
**Depends on:** B29 complete; `make block29-gate` green
**Must complete before:** B31 and every catalog/coordination/routing block
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65

## Outcome

B30 proves that AgentPaaS can run a real agent for its explicitly authorized
duration and across many model/tool turns without an accidental shorter
platform ceiling.

At block completion:

- The durable path does not hold one daemon, plugin, `docker exec`, HTTP, or
  socket request open for the lifetime of the worker.
- An active AgentPaaS deployment can be invoked directly through CLI/API with
  Hermes absent; the accepted invocation pins the B26 exact deployment and
  returns durable IDs promptly.
- One authoritative daemon clock and active-time ledger derive the attempt
  lease, maximum active duration, stall timer, and every governed-operation
  deadline.
- The fixed daemon 2-minute, inner 60-second, harness 5-minute, harness
  120-second wall budget, and model-client 120-second limits do not control a
  durable run.
- The fixed 30 CPU-second and zero-process rlimits are replaced by explicit
  policy/container resource controls.
- A worker performs at least 20 explicit model/tool turns, carries its own
  bounded context, emits authenticated progress, commits checkpoints and
  artifacts, and returns a durable result.
- The reference worker uses both buffered and B29 streaming model calls,
  survives observer disconnect/reconnect, and waits for one durable inbox
  event without polling or retaining a lifetime-spanning client request.
- A real run longer than six minutes and a real soak longer than 30 minutes
  pass from clean state.
- Fake-clock tests represent at least 24 hours and 100 turns without overflow,
  timer drift, duplicate finalization, or resource leakage.
- Daemon restart, cancellation, lease expiry, stall, process exit, and
  active-time exhaustion produce deterministic durable state and cleanup.
- Existing v0.2.3 agents remain on the legacy synchronous path until they opt
  into durable execution.

B30 is a mandatory foundation gate. B31–B35 and the model router may not begin
until the real-time long-run evidence is PASS. This block does not implement
cross-container MCP, pipelines, child agents, model selection, or model
fallback.

## Definition of “run for as long as needed”

The product does not promise infinite execution. It promises that termination
is controlled by explicit authority rather than an undocumented implementation
timeout.

- Every durable run has an operator-approved `max_active_duration` and an
  accumulated active-time ledger.
- The platform may expose an administrator-configured maximum initial
  duration, but it is visible in validation and inspection; it is not a hidden
  code constant.
- An attempt lease may be shorter than the active-time ceiling; B39 may later
  grant the one approved failure continuation or an explicitly authorized
  limit amendment before terminal exhaustion.
- `RUNNING` and `PAUSE_REQUESTED` consume active time. Fully `PAUSED` and
  `NEEDS_REPLAN` do not and must have no live worker/service capability or
  active/in-flight LLM reservation. Retained unreconciled exposure may remain
  inert. B30 implements/test-drives this accounting seam; B39 activates the
  complete control behavior.
- Cancellation always remains available.
- Stall and no-progress controls remain required.
- A configured run lasting hours or days uses the same state machine as a
  six-minute run; no request lifetime is used as a proxy for task lifetime.

## Current defects this block must close

The B26 characterization suite freezes these current observations:

| Layer | Current behavior | Required durable behavior |
|---|---|---|
| Daemon auto-invoke | Fixed two-minute context | Derived from attempt lease/active-time remaining |
| Inner invoke helper | `urlopen(..., timeout=60)` waits for final response | No lifetime-spanning HTTP read |
| Harness invoke | Five-minute default | Receives the authoritative effective active-segment deadline |
| Harness budget | 120-second default | Uses pinned durable-run policy, never an independent default |
| Model client | 120-second fixed timeout | Uses effective model-call/active-time deadline |
| Python worker | 30 CPU-second rlimit | Explicit CPU/container policy; no hidden cumulative kill |
| Python worker | `RLIMIT_NPROC=0` | Explicit bounded PID policy; no accidental tool breakage |
| Run state | In-memory `trackedRun` | B26 durable run/attempt store is authoritative |
| Client behavior | Blocking command interpreted as run lifetime | Direct deployment start returns IDs; status/result are queried asynchronously with Hermes absent |

Deleting a test, increasing only one constant, or calling the current blocking
path “long-running” does not satisfy this block.

## Locked durable invocation architecture

The durable path uses a job protocol rather than one long HTTP response:

1. The authenticated API/CLI request enters B26 `AdmitInvocation`, which
   validates the complete canonical invocation intent, resolves and pins the
   active exact deployment and nested snapshot, enforces top-level
   default-one/configured concurrency, and atomically creates the invocation,
   workflow, standalone node/run, and durable `READY` launch intent. It creates
   no attempt, lease, container, or duplicate orchestration run. The admission
   receipt returns invocation/workflow/run IDs immediately. No Hermes process
   or plugin request participates.
2. The supervisor claims the `READY` intent by CAS and atomically creates the
   initial attempt, lease, and `InvokeJob` while moving the node to
   `LAUNCHING`. A crash before claim leaves one claimable intent; a crash after
   claim finds the same attempt/job.
3. The daemon writes a canonical trusted invocation envelope atomically under
   the attempt control directory. It contains requested/resolved deployment,
   invocation/run/attempt/lease identity, input digest and bounded payload,
   active-time/current-segment values, progress journal configuration,
   artifact root, and compatibility-safe SDK configuration. It contains no
   raw credential value.
4. The control directory is mounted so the root harness can read it but the
   non-root Python worker cannot. Provider credentials continue through the
   existing protected side channel.
5. Container startup launches exactly one durable invoke job. The harness
   acknowledges job start through an authenticated control journal and then
   executes the synchronous Python handler internally.
6. The harness appends authenticated state/result records: `accepted`,
   `started`, progress references, `succeeded`, `failed`, or `cancelled`.
7. The daemon supervisor observes the host-side journal, B27 progress journal,
   container status, and durable store. It does not wait on worker stdout or a
   final HTTP response.
8. The final result is written atomically and identified by digest. The daemon
   verifies identity, lease, sequence, and digest before marking success.
9. Cancellation/revocation reaches the harness through a bounded control
   signal plus container stop fallback.
10. The legacy `/invoke` path remains for v0.2.3 compatibility but is never used
   by a durable run.
11. An idempotent retry returns the original receipt without another job.
    `ALREADY_RUNNING`, inactive deployment, or changed-key payload creates no
    job, run, container, hidden queue entry, or success audit.

If implementation finds that a protected local Unix socket is safer than the
control journal for cancellation, it may be used only if the same properties
hold: bounded request lifetime, no Python access to control capability,
durable status, restart reconciliation, and no host/network exposure. Record
the deviation before implementation.

## Locked multi-turn semantics

- A turn is one completed governed model or tool call plus its resulting
  worker state transition.
- Buffered `agent.llm()` and streaming `agent.llm_stream()` use the B29
  normalized envelope. A stream is observational; only the committed terminal
  result advances worker state.
- Reasoning models may expose effort, usage dimensions, and approved summaries,
  but AgentPaaS never requests or persists raw chain-of-thought.
- The worker owns explicit context. It may carry prior messages or a bounded
  transcript in its prompt/checkpoint; AgentPaaS does not claim hidden memory
  migration or automatic compaction.
- The reference worker must prove that later turns depend on explicitly
  committed earlier-turn state rather than making unrelated repeated calls.
- Raw chain-of-thought, provider continuation IDs, and uncommitted partial
  output are never checkpointed.
- A context that exceeds policy must fail explicitly; this block does not add
  automatic summarization.

## Authoritative task order

|| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
|| 0 | T00 Close the synthetic MCP success fallback (fail-closed) | B29 | no-router managed MCP call returns typed error; synthetic path exists only behind explicit test mode |
|| 1 | T01 Convert characterization into mandatory regression tests | B26, T00 | every current hidden ceiling has a named failing durable-path test |
| 2 | T02 Implement direct durable asynchronous deployment invocation/job protocol | T01 | CLI/API start with Hermes absent returns promptly; 90-second worker completes without open client request |
| 3 | T03 Unify active-time, operation-deadline, and cancellation controls | T02 | running/frozen fake-clock matrix and no-fixed-timeout scan pass |
| 4 | T04 Replace hidden Python resource rlimits | T02 | CPU/PID policy tests and security regressions pass |
| 5 | T05 Implement durable supervisor, liveness, and terminal reconciliation | T02–T04, B27 | progress/result/restart races are deterministic |
| 6 | T06 Build the real multi-turn reference worker | T03–T05 | 20+ dependent turns, checkpoints, artifacts, and explicit context pass |
| 7 | T07 Prove daemon restart, cancellation, active-time exhaustion, and cleanup | T05, T06 | every fault point preserves honest state and zero orphan resources |
| 8 | T08 Run accelerated, six-minute, and 30-minute longevity matrices | T01–T07 | all required long-run evidence is PASS, never skipped |
| 9 | T09 Block gate and adversary review | T01–T08 | `make block30-gate` passes |

## T00 — Close the synthetic MCP success fallback (fail-closed)

**Priority:** P0 — executes before all other B30 tasks.

**Origin:** ap-thinker architecture audit (2026-07-19), risk R5. The shipped
v0.2.3 harness returns a synthetic `{ok: true}` when `agent.mcp()` is called
and no `mcpmanager.Router` is installed in production. This is the same
defect class as B20 C8 (fake LLM response): false success on a governed
path. B33 closes it with the real router in v0.4, but the README already
implies governed tool calls, and B30–B32 would otherwise ship a durable
runtime on top of a known synthetic-success path. Closure is pulled forward,
mirroring how B20 pulled B19 T1 forward.

### Spec

1. In the harness MCP RPC handler: when no router is installed, return a
   typed structured error (`agentpaas_mcp_service_not_enabled` or
   `MCP_ROUTER_UNAVAILABLE`) instead of the synthetic result. B26 already
   reserves the not-enabled error name.
2. Restrict the synthetic response to explicit test mode only, using the
   same pattern as `AGENTPAAS_TEST_FAKE_LLM=1` (B20 T07). Tests that need
   the fake set the flag; production builds never fabricate success.
3. Existing external stdio/HTTP MCP compatibility (already supported by
   policy) is unaffected. Only the no-router managed-service branch changes.
4. Update `docs/known-limitations.md`: the current "may return a synthetic
   result" wording becomes "fails closed with a typed not-enabled error
   until B33 wires the production router."

### Tests to write first

- Managed `agent.mcp()` call with no router fails closed with the typed
  error; no `{ok: true}` reaches the worker.
- The synthetic payload string cannot appear on the production path (source
  scan plus runtime fixture).
- Explicit test-mode flag enables the fake for fixtures that need it.
- Existing external stdio/HTTP MCP fixtures remain green.
- Audit records the typed denial, not a fabricated success.

### Exit gate

No governed MCP call can return a synthetic success in production. Small,
self-contained diff; no dependency on B28 ports or B30 runtime work.



### Goal

Turn every current accidental lifetime limit into an executable requirement.

### Required work

1. Add a durable-run fixture whose handler sleeps past 60 seconds and then
   returns a deterministic result.
2. Add a fixture crossing two minutes while emitting B27 progress.
3. Add a fake-clock fixture crossing five minutes and 120 seconds at every
   layer.
4. Add a CPU-bound fixture that consumes more than 30 CPU seconds under an
   explicitly approved resource envelope.
5. Add a bounded child-process fixture proving the configured PID allowance,
   without weakening container isolation or permitting host escape.
6. Add source/AST tests forbidding fixed durable-path values in:
   - daemon invoke context;
   - inner helper/socket calls;
   - harness invoke defaults;
   - model client construction;
   - plugin command waits;
   - Python resource bootstrap.
7. Keep explicit legacy-path constants allowlisted by exact file/function and
   document why they do not affect durable mode.

### Tests to write first

- 61-second durable handler currently fails and later must pass.
- 121-second progress case.
- Fixed-timeout ownership scanner positive and negative fixtures.
- Resource-limit introspection from inside the worker container.
- Legacy path remains unchanged.

### Exit gate

Every observed v0.2.3 limit has a test that fails for the intended reason on
the baseline and cannot be “fixed” by changing only a comment.

## T02 — Implement direct durable asynchronous deployment invocation

### Goal

Activate B26 deployment admission for standalone durable workers and decouple
client/control-plane request lifetime from worker lifetime. Hermes must be
absent before invocation in the proof.

### Required work

1. Wire authenticated CLI/API deployment invocation through B26
   `AdmitInvocation` before creating resources. Resolve aliases once, pin the
   exact deployment/nested snapshot, enforce active status, complete-intent
   idempotency, and receiver-local top-level concurrency atomically.
2. Return the admission receipt after its durable commit, without waiting for
   attempt creation, resource start, or final completion. Status/result APIs
   expose the initial attempt ID after the supervisor claim creates it.
3. Claim the precreated standalone `READY` node/run by CAS and create exactly
   one attempt, lease, and launch job in that transition. Never call
   `CreateRun` after admission.
4. Add versioned `InvokeJob`, `InvokeJobEvent`, and `InvokeJobResult` domain
   objects to the B26 contract package.
5. Create protected per-attempt control paths with `0700` directories and
   `0600` files, symlink-safe opens, atomic writes, sequence numbers, and
   bounded sizes.
6. Sign or HMAC control events with an attempt key unavailable to Python.
7. Extend the harness startup to consume one job envelope exactly once.
8. Add structured get/status/result APIs backed by the durable store.
9. Make duplicate startup/job delivery idempotent. A second delivery never
   invokes the handler twice.
10. Persist `accepted` before container start and `started` before Python
   handler execution.
11. Write result content to the protected result store, expose only bounded
   structured result and artifact references, and persist digest before
   terminal success.
12. Retain the legacy synchronous route behind explicit compatibility
    dispatch.
13. On an idempotent API retry, return the original receipt even if the alias
    now points elsewhere. On `ALREADY_RUNNING`, inactive deployment, or key
    conflict, create no job/resource and no hidden queue record.

### Tests to write first

- Run/start API returns within a bounded short interval for a 90-second job.
- Crash/replay after admission but before `READY` claim, and after claim but
  before resource start, yields one node/run/attempt/job and one execution.
- No long-lived `ExecWithStdin` or `urlopen` call exists on durable path.
- Duplicate job envelope starts one handler.
- Crash before/after accepted, started, result write, and terminal commit.
- Python cannot read/write control key or forge result event.
- Oversized/corrupt/traversal/symlink envelope fails before invocation.
- Legacy synchronous SDK/CLI fixtures pass unchanged.
- Hermes process absent before API/CLI invocation.
- Exact-ref and alias invocation pin the expected digest; alias movement after
  acceptance cannot alter an idempotent replay.
- Same caller/key with changed input, requested reference, initial active-time/
  lease/spend ceiling, or authority-bearing creation option conflicts before
  resource creation.
- Two simultaneous default-one invocations admit exactly one and return one
  retryable `ALREADY_RUNNING`; configured-safe concurrency admits its bound.
- The shared B26 admission-conformance suite passes for the standalone
  topology, including slot release/reacquisition and no hidden queue.

### Exit gate

A 90-second deterministic worker completes while every initiating client may
disconnect immediately after receiving invocation/workflow/run IDs; the
attempt ID becomes queryable after asynchronous launch claim.

## T03 — Unify active-time, operation-deadline, and cancellation controls

### Goal

Make the daemon’s pinned policy and clock authoritative at every layer.

### Required work

1. Define `TimeEnvelope` containing current maximum active duration, consumed
   active duration, optional running-segment start, attempt lease remaining,
   stall timeout, model-call timeout, lifecycle/authority generation, and
   cancellation generation.
2. Derive effective operation deadline as:

```text
min(operation_timeout, attempt_lease_remaining, active_time_remaining)
```

3. Convert that remaining duration to a per-active-segment deadline for the
   harness and governed clients; no layer invents a lifetime default.
4. Inject clock/timer interfaces into supervisor, harness integration, and
   model/tool clients.
5. Persist UTC timestamps for evidence and use monotonic time for live
   duration decisions.
6. Start/close an active-time segment atomically with observed workflow state.
   `RUNNING` and `PAUSE_REQUESTED` accrue; fully `PAUSED` and `NEEDS_REPLAN`
   do not. A daemon restart conservatively closes an interrupted active
   segment exactly once.
7. Keep at most one segment open per workflow. Future parallel parent/child or
   service activity consumes one elapsed workflow clock, not the sum of node
   intervals; per-attempt leases remain independent.
8. Define deterministic precedence when cancellation, lease, stall, process
   exit, and active-time exhaustion coincide:
   - user cancellation;
   - active-time exhaustion;
   - lease expiry;
   - stall;
   - process/provider failure.
9. A waiting status/result query has its own short client timeout and never
   cancels the run.
10. Any admin platform maximum is visible in validation/inspection and tested
   as authority, not hidden implementation behavior.
11. Reserve an atomic ceiling/authority-generation update seam for B39 limit
    amendments. B30 does not expose amendment behavior, but fake-clock tests
    must prove a new ceiling can be installed without losing consumed time.

### Tests to write first

- Minus/at/plus-one boundary for every timer.
- Client query timeout leaves run active.
- Model/tool operation cannot exceed remaining lease/active time.
- Heartbeat and timer at same fake-clock instant follow precedence.
- Long duration arithmetic does not overflow.
- Timezone/wall-clock jump does not change monotonic duration behavior.
- Long fake-clock advances while `PAUSED` or `NEEDS_REPLAN` add zero active
  time; the same advance during `PAUSE_REQUESTED` is charged.
- Pause/resume segment boundaries, daemon restart, and repeated reconciliation
  neither double-charge nor forgive active time.
- Ceiling update versus exhaustion uses one generation and has one winner.
- Search test finds no unauthorized fixed durable-path timeout.

### Exit gate

Every termination report names the authoritative boundary, configured limit,
observed duration, and remaining envelope.

## T04 — Replace hidden Python CPU and process limits

### Goal

Allow legitimate long work while preserving explicit resource containment.

### Required work

1. Remove the unconditional `RLIMIT_CPU=30` from durable workers.
2. If CPU time is bounded, expose it as signed policy and report it separately
   from accumulated workflow active time.
3. Replace `RLIMIT_NPROC=0` with a policy-derived container PID limit and
   safe default sufficient for approved local tools.
4. Keep child-agent creation out of OS process spawning; B35 uses the
   AgentPaaS control plane and separate containers.
5. Apply memory, CPU quota, PID, file-size, and disk/workspace limits through
   the runtime driver/container spec.
6. Ensure the worker remains non-root, capability-dropped, network-isolated,
   and unable to access Docker/host control sockets.
7. Record resource limit and termination reason in attempt evidence.

### Tests to write first

- CPU-bound work beyond 30 CPU seconds succeeds under an allowed envelope.
- Explicit CPU budget terminates at its configured boundary.
- Allowed bounded subprocess succeeds; PID fork bomb is contained.
- Memory, file, and artifact limits remain enforced.
- Child subprocess cannot access host network, Docker socket, control files,
  credentials, or journal keys.
- Legacy security/adversary suites remain green.

### Exit gate

No hidden worker-bootstrap rlimit ends a valid durable run; every resource
stop maps to explicit policy evidence.

## T05 — Implement the durable supervisor and liveness reconciliation

### Goal

Use B26 state and B27 authenticated progress as operational truth.

### Required work

1. Introduce a local `Supervisor` interface and implementation independent of
   CLI request context.
2. Drive state transitions only after durable compare-and-swap writes.
3. Track accepted authenticated activity:
   - progress/heartbeat;
   - model, HTTP, and MCP start/end;
   - checkpoint/artifact commit;
   - job result event.
4. Do not count stdout/stderr spam, process existence, or unauthenticated file
   writes as progress.
5. Implement stall timer and active governed-operation exemptions bounded by
   their effective operation deadlines.
6. Finalize success only from a verified result event for the active lease.
7. Make finalization, cancellation, and cleanup idempotent under races.
8. Publish audit/timeline events only after durable state commit.
9. Reconcile daemon restart by revoking ambiguous active lease, ingesting any
   already committed result/checkpoint, and never blindly reinvoking work.
10. Preserve safe checkpoint/artifact state for later B39 continuation.
11. Reconcile active-time segment state from B26. Never accrue time while the
    durable workflow is fully `PAUSED` or `NEEDS_REPLAN`; never leave a frozen
    workflow with an active lease/capability or active/in-flight reservation.

### Tests to write first

- Heartbeat prevents stall; stdout spam does not.
- In-flight governed operation prevents stall only until operation deadline.
- Result/fence, result/cancel, and result/restart races.
- Forged/late progress and result event rejected.
- Duplicate finalizer and cleanup.
- Restart at each lifecycle boundary.
- Restart while a synthetic paused/needs-replan record has advanced wall time;
  active-time consumed remains unchanged.
- No run is marked succeeded merely because container exited zero without a
  verified result.

### Exit gate

The durable store can explain the state of every attempted long run after
client disconnect or daemon restart.

## T06 — Build the multi-turn long-running reference worker

### Goal

Prove actual dependent multi-turn work, not unrelated repeated calls or one
long sleep.

### Required work

Create a deterministic “research dossier” worker using fake local services:

1. Read a bounded fixture set.
2. Perform at least 20 governed turns across model and tool phases.
3. Explicitly carry a bounded prior-turn transcript/state into later model
   prompts.
4. Persist intermediate structured state to artifacts.
5. Emit heartbeat progress during legitimate slow phases.
6. Commit a safe checkpoint after each material phase.
7. Make at least one later phase depend on an earlier artifact digest/value.
8. Return a final structured result and artifact reference.
9. On resume input, skip already committed actions; B30 tests protocol
   behavior but does not start a second worker attempt automatically.
10. Use deterministic fake model/tool responses for assertions. No LLM judge
    or expected prose is used.

### Tests to write first

- Exact logical turn count and ordering.
- Later request contains expected explicit committed state digest/value.
- Context bound enforced and no automatic compaction claim.
- Progress/checkpoint sequence authenticated.
- Artifact digests stable.
- Raw prompts/responses absent from ordinary audit and control journal.
- Final operational result is not called verified/correct.

### Exit gate

The worker’s final result demonstrably depends on multiple earlier committed
turns and remains within configured context/resource bounds.

## T07 — Prove restart, cancellation, active-time boundaries, and cleanup

### Goal

Show that duration does not weaken lifecycle safety.

### Required work

1. Inject daemon restart during:
   - idle between turns;
   - model call;
   - local tool phase;
   - checkpoint commit;
   - final result commit.
2. Inject harness/worker exit and container runtime errors.
3. Cancel during the same phases.
4. Expire lease, stall timer, and maximum active duration independently.
5. Reconcile control/progress journals and artifacts after each fault.
6. Verify no blind replay, false success, stale lease activity, or lost
   terminal result.
7. Assert cleanup of agent/gateway containers, networks, control material,
   journal keys, and temporary credentials.
8. Repeat reconciliation twice to prove idempotency.

### Tests to write first

- Fault at every lifecycle state edge.
- SIGTERM grace then forced kill.
- Runtime stop/remove failure and retry.
- Late worker success after cancellation/fence cannot win.
- Checkpoint/result digest tamper.
- Second daemon restart produces no duplicate events or cleanup error.

### Exit gate

Every fault yields one honest terminal or resumable state and zero accepted
governed actions after lease revocation.

## T08 — Run the longevity proof matrix

### Required deterministic/accelerated proof

- Fake-clock 24-hour run.
- At least 100 turns.
- Regular progress and checkpoints.
- Model/tool delays, lease-expiry simulation, and active-time boundary.
- Long `PAUSED` and `NEEDS_REPLAN` fake intervals consume zero active time;
  `PAUSE_REQUESTED` consumes time until its boundary.
- No overflow, timer leak, unbounded goroutine/file growth, or duplicate
  finalization.

### Required real-time proof A: boundary breaker

- At least six continuous minutes.
- At least 20 dependent turns.
- Crosses 60 seconds, 120 seconds, and five minutes.
- Uses the real daemon, harness, Docker topology, progress journal, and result
  store.
- Client disconnects after start and later retrieves success.
- Stop Hermes before invocation; start the same immutable deployment once
  through the public CLI by alias and separately through the authenticated API
  by exact reference.

### Required real-time proof B: soak

- At least 30 continuous minutes.
- At least 100 governed turns with bounded cadence.
- At least ten safe checkpoints and multiple artifact updates.
- One daemon restart after a safe checkpoint; outcome follows the documented
  restart contract and does not fabricate seamless process continuation.
- Resource samples show bounded memory, PIDs, file descriptors, journal size,
  and container/network count.

### Execution rules

- `make block28-ci` runs fake-clock and short boundary tests.
- `make block28-long` runs both real-time Docker proofs and emits sanitized
  machine-readable evidence.
- `make block30-gate` requires both commands and rejects `SKIP`, missing
  Docker evidence, shortened duration, or manually edited PASS state.
- CI may schedule `block28-long` as a nightly/release job rather than on every
  unit-test commit, but B30 is not complete until it passes on the candidate
  commit.

### Exit gate

All three matrices are PASS with exact commit, environment, configured current
maximum/consumed active time, observed wall duration, turn count, checkpoint
count, terminal state, deployment identity, invocation method, and cleanup
evidence.

## T09 — Block gate and adversary review

### Required commands

```text
make block29-gate
make block28-ci
AGENTPAAS_DOCKER_TESTS=1 make block28-long
go test ./internal/supervisor/... ./internal/routedrun/... ./internal/harness/... ./internal/daemon/... -count=1 -race
go test ./internal/runtime/... ./internal/trigger/... ./internal/operator/... ./internal/cli/... -count=1 -race
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

### Required adversary matrix

- Reintroduce 60-second, 2-minute, 5-minute, or 120-second hidden timeout.
- Client query timeout cancels worker.
- Forge job accepted/started/result event.
- Deliver job twice.
- Move an alias between idempotent retries, reuse a key with changed input/ref/
  initial ceiling/creation option, invoke an inactive deployment, and race
  default-one top-level concurrency.
- Require a live Hermes/plugin process for invocation or status polling.
- Python reads control key or writes terminal result directly.
- Clock rollback/jump, duration overflow, and timer race.
- Charge `PAUSED`/`NEEDS_REPLAN` wall time, forgive `PAUSE_REQUESTED` time, or
  leave a frozen state with a live capability.
- stdout spam or unauthenticated heartbeat prevents stall.
- Late success after cancellation/lease expiry.
- CPU/PID exhaustion bypasses explicit policy.
- Daemon restart blindly replays handler.
- Artifact/control path traversal or symlink.
- Long-run cleanup leaks container, network, file descriptor, key, or temp
  credential.
- Long test is shortened or marked PASS without measured duration.

### Block success gate

B30 is complete only when:

0. The synthetic MCP success fallback is closed: no-router managed MCP calls
   fail with the typed not-enabled error in production (T00).
1. `make block30-gate` passes on the candidate commit.
2. The durable path contains no lifetime-spanning client request.
3. One authoritative active-time envelope controls every durable layer and
   correctly freezes fully paused/needs-replan intervals.
4. No hidden CPU/process limit ends a permitted worker.
5. The 20-turn six-minute and 100-turn 30-minute real proofs pass.
6. The accelerated 24-hour/100-turn state-machine proof passes.
7. Restart/cancel/fence/finalization races are deterministic.
8. Existing legacy agents remain compatible.
9. A CLI/API deployment invocation completes with Hermes absent before start;
   exact version, full-intent idempotency, and top-level concurrency evidence
   are durable, and the standalone B26 admission-conformance suite passes.
10. No public claim says AgentPaaS supports arbitrary-duration work before this
   evidence exists.
11. B31 is the next unblocked block; B36 model routing remains blocked until
    B35 completes.

## R30 — `v0.3.0-alpha.1` GitHub prerelease checkpoint

B30 completion permits preparation of a prerelease, not automatic publication.
The prerelease headline is:

> Start a durable streaming agent, disconnect, and return later to its ordered
> events and result—without keeping Hermes or a client request alive.

Before asking for tag approval:

1. Run B26–B30 cumulative gates, v0.2.3 compatibility/migration, race,
   adversary, real six-minute/20-turn, 30-minute release-lane, restart,
   reconnect, interactive-wait, activation, and cleanup proofs.
2. Build candidate binaries, checksums, SBOM, signatures, schema/SDK artifacts,
   and a content-addressed evidence bundle from the exact commit.
3. Provide an opt-in install path from GitHub release assets or a candidate tap;
   do not update the stable Homebrew formula or `latest` stable channel.
4. Publish a minimal durable-runtime quickstart plus a 60–120 second terminal
   recording showing invoke receipt, streaming deltas, disconnect/reconnect,
   inbox wakeup, durable result, and dormant zero-authority state.
4a. Expand the Golden Loop for v0.3 before tagging (audit risk R3): add
   phases for durable invocation with Hermes absent, disconnect/reconnect
   cursor replay, interactive inbox wakeup, and one real long-running
   multi-turn run; update `docs/execution/golden-loop-test.md` (currently
   pinned to v0.2.3) and run it clean from Phase 1. The alpha must not ship
   against a release gate that only tests the v0.2.3 product.
5. State exact omissions: no private catalog, secure A2A, MCP service runtime,
   pipeline, child graph, model routing/recovery, shared USD spend, or complete
   lifecycle UX yet.
6. Require zero open P0 security/data-loss defect. Other known alpha defects
   must be listed with severity and workaround.
7. Present the exact commit, artifacts, evidence, limitations, and publish
   command for explicit approval of the `v0.3.0-alpha.1` tag.

After publication, install from the public prerelease path on a clean machine,
verify checksums/signatures/version, rerun one bounded demo, and record the
result. A failed verification marks the prerelease failed; never move the tag.

## Handoff record required after every task

Append:

- Task/date/commit.
- Current defect closed.
- Files and contracts changed.
- Tests written before implementation.
- Exact commands and PASS/FAIL output.
- Configured and observed duration/turn/checkpoint counts.
- Fault and restart point.
- Resource/cleanup evidence.
- Compatibility impact.
- Open risks.
- Next task unblocked.

## Pitfalls

- Increasing `urlopen(timeout=60)` is not an architecture fix.
- A long sleep is not a multi-turn agent proof.
- A process that is alive is not proof of progress.
- A heartbeat is not automatically a safe checkpoint.
- Client monitoring timeout must not cancel the run.
- Do not claim seamless process-memory continuation after daemon restart.
- Do not remove resource controls; make them explicit and policy-derived.
- Do not begin model-router implementation before the foundation gate passes.
