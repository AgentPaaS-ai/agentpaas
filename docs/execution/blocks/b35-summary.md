# Block 35 — Parent/Child Agent Fan-Out, Fan-In, and Collation

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Identity-source note (Fix 3, 2026-07-19):** B31 is reduced to a registry
read API + promotion bit. B35 orchestrator role selection at authoring time
consumes `registry list`/`registry show` (promoted packages, declared
capability metadata); child allowlists remain statically declared in the
signed workflow and pin exact digests at B26 admission. No capability
schema matching is required through v0.5.
**Target release:** stable `v0.4.0` — Governed Multi-Agent Workflows
**Depends on:** B34 complete; `make block34-gate` green
**Must complete before:** B36, B38, B39, and B40
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65

## Outcome

B35 proves a second runtime-native multi-agent pattern: one parent agent may
spawn a bounded set of approved child agents, wait for their durable results,
collate those results itself, and continue its work. At block completion:

- The parent and every child run in separate AgentPaaS worker/gateway
  containers with independent identity, policy, lease, run state, and evidence.
- Child creation is requested through a small authenticated SDK contract and
  executed by AgentPaaS; it is not implemented with Docker access, OS process
  creation, or a Hermes tool call.
- Worker, verifier, and testing roles may resolve from B31 capability
  constraints, but every chosen child is pinned to an exact approved digest
  before spawn and remains inside the signed candidate/resource envelope.
- Spawn and join are durable and idempotent across daemon or parent restarts.
- Spawn/join uses B32 A2A-compatible tasks, ordered events, and artifact grants;
  the parent suspends at a safe wait and wakes without status polling.
- The parent receives only bounded child result envelopes and approved artifact
  references, in deterministic request order.
- The parent—not AgentPaaS and not an LLM verifier—collates the results and
  decides its next application action.
- Workflow active time, concurrency, container count, resource, authority, and
  later B38 spend limits bound the entire family.
- Child agents are leaves in v0.4. A child cannot recursively spawn another
  child or construct an arbitrary graph.
- The parent/child workflow is an immutable B26 deployment and can be invoked
  through CLI/API with Hermes absent before start.
- B39 can pause the family safely: no unstarted child launches, active children
  may finish and store results, and the parent cannot continue until resume.

B35 is not a general swarm framework, consensus protocol, recursive actor
runtime, or verifier. It adds one comprehensible fan-out/fan-in primitive that
can be measured and secured before expanding the graph model.

The `parent_child` coordination kind is separate from B34 `pipeline` in v0.4.
The parent may use B33 MCP services, but it is not also a pipeline stage and
children cannot advance a stage graph.

## Locked v0.4 parent/child shape

```text
                     +-> child 1 -+
parent -> spawn/join +-> child 2 -+-> ordered child results -> parent continues
                     +-> child N -+
```

The supported contract is:

1. The parent declares an allowlist of child worker package identities in its
   signed manifest. Immutable deployment creation resolves each allowlist entry
   to an exact installed version/digest, and admission pins that snapshot.
   Runtime requests select a logical entry from the pinned allowlist; they
   cannot supply images, commands, aliases, policies, credentials, or arbitrary
   package paths.
2. One spawn request contains 1–8 children. The workflow policy sets a maximum
   active-child concurrency no greater than 4. Lower signed workflow/package
   resource limits win; internal children do not consume additional top-level
   deployment-admission slots.
3. Each child receives one bounded canonical JSON input envelope plus approved
   read-only artifact references. It does not inherit parent memory,
   credentials, gateway handles, filesystem, or full transcript.
4. All requested children are required in v0.4. The batch joins successfully
   only when every child succeeds. A terminal child failure terminates the
   batch and parent attempt with a typed reason after outstanding children are
   cancelled/fenced. There is no partial-success policy in this release.
5. Child result order is the original request order, never completion order.
6. The parent explicitly calls `join_children()` and then collates the returned
   results in its own code/model loop. AgentPaaS does not synthesize a summary
   or judge semantic correctness.
7. A parent may create at most one active child batch at a time and at most two
   batches during one attempt. These fixed safety bounds can become policy
   fields only after v0.4 operational evidence.
8. A child is marked `leaf=true`; any child spawn call is denied before a
   container is created.
9. Parent continuation in B39 may resume from a safe checkpoint and recover the
   existing batch/result. It may not silently create a new batch for the same
   spawn intent.

## Locked SDK contract

The parent-facing API is intentionally small:

```python
batch = agent.spawn_children(
    idempotency_key="research-regions-v1",
    children=[
        {"worker": "region-researcher", "input": {"region": "west"}},
        {"worker": "region-researcher", "input": {"region": "east"}},
    ],
)

results = agent.join_children(batch.id)
# Parent code/model collates results and continues.
```

Rules:

1. `idempotency_key` is required, scoped to workflow/parent run/logical spawn
   site, and limited to a canonical safe character set and length.
2. Before spawn, the parent must have a current accepted B27 safe checkpoint.
   The runtime—not worker-authored checkpoint metadata—atomically binds that
   checkpoint ID, the canonical spawn digest, the idempotency key, and parent
   attempt/generation to `CHILD_BATCH_INTENT`. B39 continuation can therefore
   recover already-created children without trusting prose in a checkpoint.
3. Worker names resolve only to exact package identities/digests already pinned
   in the signed parent allowlist snapshot. The SDK cannot override models,
   policies, networks, deadlines, aliases, or credentials.
4. Child input is canonical JSON limited to 256 KiB per child and 512 KiB per
   batch, excluding artifact content. Reserved control fields, known local
   credential fingerprints, and credential/capability representations are
   rejected. This is not a claim of semantic DLP for arbitrary JSON.
5. Repeating an identical spawn key and payload returns the existing batch.
   Reusing the key with different canonical content fails closed.
6. `join_children()` is a governed long-running SDK operation. It emits
   authenticated B27 activity while waiting, observes cancellation/active-time,
   and returns only after a durable batch terminal state.
7. Successful results contain child ID, package digest, run/attempt IDs,
   result digest, bounded structured result, artifact references, and
   provenance. No child credential or hidden context is exposed.
8. `join_children()` is idempotent; subsequent calls return the same terminal
   result digest.

The SDK talks only to the local trusted harness. Extend the B30 authenticated
attempt control journal/mailbox with bounded, sequenced spawn/join requests and
durable responses (or use an equivalently protected harness-to-daemon
channel). Python never receives a daemon socket, Docker socket, scheduler
capability, journal key, or direct child endpoint.

## Locked scheduling and lifecycle protocol

1. In one B26 `ApplyTransition`, validate the current accepted safe checkpoint,
   persist `CHILD_BATCH_INTENT` with checkpoint ID/canonical spawn digest/key/
   limits/generation, allocate stable child node/run IDs and launch keys in
   request order, and advance the batch to `ALLOCATED`. No child launches
   before that commit.
2. Each child uses the B26 node lifecycle `PENDING -> READY -> LAUNCHING ->
   RUNNING -> terminal`; a scheduler marks only bounded-capacity children
   `READY`, then claims one by atomically creating its attempt/lease/job.
3. The batch uses only the B26 batch lifecycle: `INTENT`, `ALLOCATED`,
   `RUNNING`, `PAUSE_REQUESTED`, `PAUSED`, `JOINING`, `STOPPING`, and one
   terminal state. Per-child node/run states are not invented batch states.
4. Launch children through the normal B30 AgentPaaS container path, respecting
   signed workflow child/container capacity. Internal child runs do not consume
   additional top-level deployment-admission slots.
5. Record each child run and attempt under the parent workflow, but keep its
   policy, lease, artifacts, progress, and terminal result independent.
6. Reconcile batch `ALLOCATED` plus per-child `READY`/`LAUNCHING` state from
   durable jobs/container labels before creating resources after restart.
7. Persist each terminal child result once. Late events from fenced attempts
   cannot overwrite it.
8. When every child succeeds, atomically persist an ordered immutable
   `ChildBatchResult` and wake the parent join operation.
9. On first required child failure, parent cancellation, active-time
   exhaustion, or lease revocation, move the batch to `STOPPING`, stop launching
   `PENDING`/`READY` children, cancel/fence active children, then commit the
   truthful terminal batch state.
10. Parent death does not transfer authority to children. Outstanding children
   are cancelled/fenced by default; already committed child results remain for
   inspection and possible B39 idempotent continuation.
11. Cleanup removes every child worker/gateway container and transient mount;
    committed reports/artifacts remain inspectable workflow history, with no
    automated purge or retention lifecycle in v0.4.
12. After durable `PAUSE_REQUESTED`, launch no unstarted child. Active children
    may reach terminal results, which remain committed. Do not wake/continue
    the parent. Reach `PAUSED` only after every active child/parent/service is
    fenced/stopped and no active/in-flight reservation remains. Retained
    unreconciled exposure may remain in the ledger but authorizes no work.
    Resume launches only the unstarted children or wakes the parent from the
    retained ordered state; it never respawns a completed child.

## Authority and aggregate limits

Every child receives the intersection of:

- Workflow authority and remaining active time.
- Parent manifest's child declaration.
- Child package's own signed manifest and policy.
- The per-request child input/artifact selection.
- Signed workflow/package child concurrency, container, CPU, memory, PID, and
  network ceilings.

Authority can narrow but cannot expand through spawning. The parent cannot
delegate a credential, network destination, MCP service, model route, or data
classification it does not possess and the child does not independently
declare.

The workflow enforces aggregate counters for:

- Active and total containers, including every worker/gateway and B33 service.
- Active and total child runs.
- Accumulated workflow active time—one elapsed clock while the workflow is
  `RUNNING`/`PAUSE_REQUESTED`, not a sum across parallel children—and
  per-operation deadlines/attempt leases.
- CPU, memory, PIDs, artifact bytes/count, and handoff/result bytes.
- B38 LLM token/spend reservations and reconciled charges.

B35 implements non-monetary aggregate counters and a fail-closed spend seam.
Model-enabled child execution remains gated until B38 supplies the shared
workflow ledger and B39 activates integrated execution.

## Child result and collation boundary

The runtime verifies structural completion, not semantic truth. It guarantees:

- The approved child package ran and reached a terminal state.
- The result came from the authenticated current attempt.
- The result and artifacts satisfy declared schemas, sizes, classifications,
  digests, and authority.
- Every required child result exists exactly once and is returned in stable
  order.

The runtime does not determine whether research is insightful, code is correct,
or a summary is good. The parent agent owns domain-level collation and any
subsequent checks. A future verifier pattern can consume these same durable
results without becoming a v0.4 platform dependency.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Freeze child manifest, input, and result contracts | B26, B34 | canonical fixtures and unsupported-graph negatives pass |
| 2 | T02 Implement authenticated spawn/join SDK operations | T01, B27 | checkpoint, identity, idempotency, and child-leaf tests pass |
| 3 | T03 Implement durable child-batch state and scheduling | T01–T02 | fan-out survives daemon restart without duplicate children |
| 4 | T04 Enforce container, authority, and aggregate bounds | T03 | separate-container and confused-deputy adversary tests pass |
| 5 | T05 Implement deterministic join and parent wake-up | T03–T04 | ordered all-required results and race tests pass |
| 6 | T06 Reconcile failure, parent death, cancel, pause/resume, and cleanup | T03–T05 | lifecycle/control fault matrix converges without orphans or duplicate children |
| 7 | T07 Build independently invoked parent collation proof and operator evidence | T02–T06 | CLI/API deployment starts with Hermes absent; parent collates durable child results |
| 8 | T08 Run block gate and adversary review | T01–T07 | `make block35-gate` green and handoff recorded |

Do not start B36 until T08 is complete.

## Expected implementation areas

Extend B26/B30/B34 components rather than creating a second workflow control
plane. Expected areas include:

- B26 workflow/child contracts, store, and atomic transition journal.
- A focused child controller such as `internal/workflow/children/**`.
- `internal/daemon/**` allocation, scheduling, reconciliation, and cleanup.
- `internal/harness/**` authenticated spawn/join request/response transport.
- `python/agentpaas_sdk/**` parent APIs, leaf-role enforcement, and tests.
- `internal/runtime/**` independent container identities and aggregate
  reservations.
- `api/**`, `internal/operator/**`, and `internal/cli/**` additive family
  inspection.
- `test/workflow/**` or the repository-equivalent fan-out, crash, race,
  isolation, and adversary fixtures.
- `Makefile` and CI/release-artifact wiring for the block gates.

## T01 — Freeze child manifest, input, and result contracts

**Goal:** Establish exact supported shapes and stable failure reasons before
enabling child creation.

**Instructions:**

1. Extend strict worker manifest fixtures with child allowlists, immutable
   package identities, accepted input/result schemas, artifact classes, and
   maximum requested child count.
2. Add canonical one-, two-, eight-child and two-sequential-batch fixtures.
3. Add invalid fixtures for zero/nine children, too much concurrency, unknown
   package, mutable image/command, recursive child, nested graph, missing safe
   checkpoint, missing/invalid idempotency key, duplicate key with different
   payload, and a third batch.
4. Freeze canonical `ChildBatch`, `ChildSpec`, `ChildResult`, and
   `ChildBatchResult` encodings and digests.
5. Include oversize input/result, reserved fields, credential/capability
   values, path traversal, bad artifact digest, declassification, forged IDs,
   and non-canonical JSON.
6. Freeze standalone, pipeline, and B33 service compatibility fixtures.

**Tests:**

- Strict schema/canonicalization goldens.
- Fuzz/property tests for child input and result decoding.
- Graph-depth and leaf enforcement negatives.
- Backward-compatibility tests for agents with no child declaration.

**Done when:** Every v0.4-supported and rejected parent/child request has an
owner, stable reason, and canonical fixture.

## T02 — Implement authenticated spawn and join SDK operations

**Goal:** Expose child coordination without exposing the container runtime or
privileged orchestration APIs to the parent.

**Instructions:**

1. Add `spawn_children()` and `join_children()` to the Python SDK and harness
   RPC with strict typed responses.
2. Bind calls to workflow, parent node/run/attempt, lease, generation, and
   parent/leaf role through the authenticated harness channel.
3. Require and validate the latest accepted B27 safe checkpoint; compute the
   spawn digest from canonical request data and atomically associate its ID,
   key, parent generation, and digest with the batch. Do not require or trust a
   worker-authored spawn-intent metadata field in the checkpoint.
4. Resolve worker aliases exclusively against the signed allowlist; ignore or
   reject SDK fields attempting to set images, commands, policy, route,
   credential, active-time/spend ceilings, network, or runtime flags.
5. Canonicalize requests, enforce input/artifact limits, and compute the
   idempotency digest before durable allocation.
6. Make the SDK poll/wait path emit governed activity and react immediately to
   cancellation, lease loss, and effective operation/active-time deadline.
7. Reject spawn from a child, stale parent attempt, terminal parent, or parent
   with an active batch.
8. Return stable typed failures that B39 can classify without parsing strings.

**Tests:**

- SDK/Go schema parity and serialization goldens.
- Forged identity, stale lease, replay, key collision, and override negatives.
- Parent checkpoint and post-resume idempotency tests.
- Join cancellation/active-time/activity tests lasting beyond legacy timeouts.

**Done when:** The parent can request only pre-authorized logical work and has
no direct path to Docker, host execution, or another child's controls.

## T03 — Implement durable child-batch state and scheduling

**Goal:** Allocate and launch each child exactly once logically across crashes.

**Instructions:**

1. Implement the exact B26 child-batch transitions `INTENT -> ALLOCATED ->
   RUNNING|PAUSE_REQUESTED -> PAUSED|JOINING|STOPPING -> terminal` and the
   exact per-child B26 node/run transitions. Do not add `queued`, `active`, or
   `launching` as batch enum values.
2. Atomically bind the accepted checkpoint/spawn digest and allocate stable
   child node/run IDs plus request-order indexes before any launch.
3. Derive stable launch idempotency keys from workflow, parent run, batch,
   child ID, and generation.
4. Use B30 durable invocation jobs for each child and signed workflow capacity
   to decide which `PENDING` child becomes `READY`; claim creates exactly one
   attempt/lease/job without recreating its run.
5. Reconcile jobs and container labels after daemon restart before launching a
   missing child.
6. Persist per-child progress, checkpoint, artifacts, result, attempt report,
   and cleanup independently under the workflow hierarchy.
7. Fence terminal attempts and reject late completion from an older generation.
8. Make concurrent reconciliation safe through generation/CAS checks; only one
   scheduler owns a batch transition.

**Tests:**

- Fan-out sizes 1, 2, 8 with concurrency 1 and 4.
- Crash injection before/after intent/checkpoint association, allocation,
  `READY` claim, launch, result, and next-child transitions.
- Concurrent scheduler/CAS tests and duplicate event storms.
- A proof that every child has one logical ID and no duplicate container after
  restart.

**Done when:** All legal crash histories converge on the same child set and
request order.

## T04 — Enforce separate containers, authority, and aggregate bounds

**Goal:** Prevent parent delegation from becoming privilege amplification or an
unbounded resource multiplier.

**Instructions:**

1. Launch parent and children through separate normal worker/gateway container
   pairs and apply workflow/parent/batch/child labels without secrets.
2. Compile each child's effective authority from the documented intersection;
   never copy the parent's credentials or capability tokens.
3. Deny direct parent-child or sibling network traffic. Communication uses
   durable input/result/artifact state or separately approved B33 MCP services.
4. Enforce workflow aggregate active/total container, child, CPU, memory, PID,
   active-time, artifact, and payload limits before every allocation and launch.
5. Reserve capacity atomically so concurrent child starts cannot oversubscribe
   the bound.
6. Add the fail-closed B38 spend-authorizer seam to every child model call.
7. Refuse recursive spawn at SDK, policy, scheduler, and container-label
   reconciliation layers.
8. Release reservations exactly once on terminal reconciliation and cleanup.

**Tests:**

- Container identity/count assertions and sibling isolation tests.
- Confused-deputy attempts to delegate forbidden credentials, networks, MCP
  services, model routes, classifications, and artifacts.
- Aggregate-limit races at exact boundary and one over.
- Child recursion attempts from SDK, forged RPC, and replayed job state.

**Done when:** Fan-out cannot grant new authority or exceed the aggregate
resource envelope even under concurrent requests.

## T05 — Implement deterministic join and parent wake-up

**Goal:** Deliver one ordered, durable result set to the correct parent attempt.

**Instructions:**

1. Persist each authenticated child terminal result with canonical result and
   artifact digests.
2. Once all required children succeed, construct `ChildBatchResult` in request
   order and commit it atomically with batch success.
3. Notify the parent join channel only after durable commit; notification loss
   must be recoverable by polling state.
4. Bind result delivery to the current parent attempt/lease. A stale attempt
   cannot consume or acknowledge results.
5. On B39 parent continuation, return the existing committed batch result after
   validating the safe checkpoint and spawn idempotency key.
6. Bound returned structured result metadata; project large child outputs as
   immutable read-only artifact references.
7. Make repeated join and daemon restart return byte-identical ordered result
   metadata.
8. Record parent receipt/acknowledgment without deleting child evidence.

**Tests:**

- Children finish in every permutation; join order remains request order.
- Notification loss, duplicate notification, parent reconnect, and daemon
  restart tests.
- Old-parent-attempt and forged-child-result rejection.
- Large artifact projection and result size limits.

**Done when:** The parent receives exactly one complete result set independent
of completion timing or process restart.

## T06 — Reconcile failure, parent death, cancel, pause/resume, and cleanup

**Goal:** Ensure no child becomes orphaned and no failed batch is reported as
successful.

**Instructions:**

1. On first required child terminal failure, atomically mark the batch
   `STOPPING`, mark unstarted `PENDING`/`READY` children skipped/cancelled by
   policy, and cancel/fence active children.
2. Preserve the first causal failure and all child terminal reports; record
   cleanup errors separately without replacing the cause.
3. On parent lease loss/death, use the same cancellation/fencing path. Retain
   committed results for B39 idempotent continuation, but do not let children
   continue with orphan authority.
4. On workflow cancellation or active-time exhaustion, cancel parent, children, and
   bound B33 services under one workflow terminal reconciliation.
5. Make all cancel, fence, result, capacity-release, and cleanup steps
   idempotent.
6. Reconcile containers after daemon restart by labels and generation; remove
   stale or unknown children fail closed.
7. Terminalize the parent attempt with a typed child-batch reason that B39 can
   report or continue under approved policy.
8. Assert no worker/gateway/service container or temporary mount remains after
   the cleanup deadline.
9. On durable `PAUSE_REQUESTED`, stop new child launches immediately. Let
   already-active children finish and commit results unless cancellation or a
   terminal boundary wins; never wake the parent or start collation. Reach
   `PAUSED` only with all active resources fenced/stopped and reservations
   reconciled. Resume preserves completed child IDs/results and launches only
   remaining allocated children before waking the parent exactly once.

**Tests:**

- Every child failure position with `PENDING`/`READY` and active siblings.
- Parent crash before spawn response, during join, after child success, and
  during collation.
- Cancellation/active-time/pause races with launch and result commit.
- Pause with zero/some/all children complete, pause during parent collation,
  repeated resume, and cancel-while-pausing.
- Daemon restart during stopping/cleanup and deliberately stuck container.
- Orphan scanner and aggregate reservation leak tests.

**Done when:** Fault injection always converges to one truthful batch/parent
outcome and zero unauthorized live children.

## T07 — Build the independently invoked parent collation proof and operator evidence

**Goal:** Demonstrate the user-requested pattern in understandable terms.

**Instructions:**

1. Build a reference parent that divides a simple information-gathering task
   into three approved child inputs, commits its spawn checkpoint, spawns the
   children, joins them, collates the ordered results, and produces a final
   deliverable.
2. Make each child visibly run in a separate AgentPaaS container and use a
   deterministic no-cost fixture for the mandatory gate.
3. Use Hermes/AgentPaaS skills to author, test, package, and deploy the immutable
   parent/child workflow. Terminate Hermes before invocation; start it through
   public CLI and authenticated API paths. Hermes must not relay requests or
   results.
4. Run a real long-enough variant crossing the legacy timeout. For the
   B35 restart proof, restart after the durable batch result commits but before
   the parent receives it; require the result/idempotency state to remain
   queryable with no respawn, while the interrupted parent follows B30's honest
   terminal/replan contract. B39 later activates parent continuation using
   that same result. Separately restart with active children and require safe
   fencing/failure with no blind replay.
5. Run the complete shared B26 admission-conformance suite for the parent/child
   topology: exact/alias and every pinned child identity, same-key replay,
   changed ref/input/initial-ceiling/creation-option conflict, alias movement,
   inactive future admission, default-one overlap/no queue, configured-safe
   concurrency, and paused-slot resume reacquisition.
6. Expose additive operator views for parent, batch, child statuses, active and
   `PENDING`/`READY` counts, result/artifact digests, aggregate remaining limits, causal
   failure, and cleanup.
7. Keep result display bounded and never print credential values, hidden model
   context, or full artifact content by default.
8. Include a failed-child demo that proves the parent does not collate partial
   data as success.

**Tests:**

- Clean-profile end-to-end parent/three-child proof.
- Hermes-absent-before-start successful collation plus daemon-restart durable-
  result/no-respawn proof.
- Public CLI/API B26 admission-conformance matrix for the parent/child topology.
- Container/event trace showing one parent plus separate child pairs.
- Bounded CLI/protobuf compatibility and redaction goldens.

**Done when:** A practitioner can see that AgentPaaS, not Hermes, safely ran the
family and that the parent itself performed collation.

## T08 — Block gate and adversary review

**Goal:** Make B35 a hard prerequisite for routing rather than an aspirational
multi-agent claim.

**Required commands:**

```text
make block34-gate
make block31-ci
make block31-adversary
make block31-live
make block35-gate
```

`block35-gate` must fail unless all of the following evidence exists:

1. B26–B34 cumulative gates are green.
2. The parent and every child use separate real worker/gateway containers.
3. A three-child join and parent collation complete after independent CLI/API
   invocation with Hermes absent before start.
4. Daemon and parent restart tests produce no duplicate child batch or child.
5. Child-leaf, package allowlist, authority intersection, payload/artifact, and
   aggregate-limit adversary suites fail closed.
6. A failed child cancels/fences siblings and cannot produce overall success.
7. Parent death/cancellation/active-time exhaustion leaves no orphan container
   or capacity reservation.
8. Pause with active and unstarted children reaches no-live-resource
   `PAUSED`; resume does not duplicate a completed child or wake the parent
   early.
9. Result order and digest are stable under every child completion order.
10. Standalone, MCP-service, and pipeline behavior remains compatible.
11. The parent/three-child success and durable-result restart proof pass three
    consecutive clean Docker runs with test-harness retries disabled.
12. The shared B26 parent/child admission suite passes; internal children do not
    consume extra top-level slots, and no child run is duplicated between
    intent, scheduler claim, or crash reconciliation.

The adversary reviewer must specifically challenge recursive spawning,
idempotency-key collision, stale parent attempts, confused-deputy delegation,
aggregate-limit races, forged child completion, and orphan cleanup.

B35 is complete only when all gates are green, no unresolved high-severity
finding remains, and the task handoff record is committed. Only then may B36
begin model catalog and route selection work.

## R35 — stable `v0.4.0` release-closure checkpoint

B35 closes **Governed Multi-Agent Workflows**. The public claim is that an
independently invoked AgentPaaS deployment can run a governed MCP service, a
linear multi-container pipeline, and a bounded parent/leaf-child workflow
without Hermes becoming the message, file, or scheduling plane.

Before tag approval:

1. Run cumulative B26–B35 gates plus v0.2.3 and stable v0.3 install/upgrade/
   migration/rollback compatibility. Existing v0.3 catalog/A2A quickstarts must
   remain unchanged.
2. On a clean machine, use public instructions to build/package/register exact
   orchestrator, worker, verifier, testing-agent, and MCP-service packages;
   terminate Hermes before every release-proof invocation.
3. Prove one real MCP client/service call, one three-stage pipeline, and one
   parent/three-child run with exact role pins, two-sided policy, encrypted
   artifacts, durable event joins, authority narrowing, aggregate limits,
   daemon restart, and complete cleanup.
4. Build signed/checksummed/SBOM-bearing binaries, SDK/schemas, Homebrew update,
   release notes, changelog, and sanitized v0.4 evidence from one exact commit.
5. Publish one concise terminal recording and sample project for each topology,
   plus a combined orchestrator/worker/verifier/testing-agent demonstration.
6. Truth-sync all public docs and limitations. State that recursive swarms,
   arbitrary DAGs, model routing/recovery, and shared USD spend are not v0.4
   capabilities.
7. Require zero open P0/P1 defect and explicit disposition of every remaining
   release risk.
8. Present the exact commit and commands for explicit `v0.4.0` publish
   approval. After publication, verify public assets, signatures, Homebrew,
   clean install/upgrade, and demos. Fix failures with `v0.4.1`; never move the
   tag.

## Handoff record required after every task

Append one durable execution-plan handoff containing:

- Block/task ID and status.
- Commit or change-set identifier.
- Exact tests and gates run, with results and evidence paths.
- Contract/schema/API changes.
- Security, resource, and compatibility findings.
- Known gaps and deferred scope.
- Explicit next task and prerequisites.

An orchestrating model must read the prior handoff, inspect the referenced
evidence and diff, and run the task-specific preflight before changing code. A
summary alone is not proof that a task or block is complete.

## Pitfalls

- Do not give the parent Docker, host process, daemon admin, or raw child RPC
  access.
- Do not copy parent credentials or hidden context into child input.
- Do not use Hermes as a runtime spawn/join relay.
- Do not require Hermes to be alive when the deployed parent/child workflow is
  invoked.
- Do not let children recursively spawn, create arbitrary workers, or bypass
  signed allowlists.
- Do not let one successful child turn a failed batch into partial success in
  v0.4.
- Do not add an LLM judge or automatic semantic collation to the platform.
- Do not claim model-enabled shared budget safety before B38/B39.
- Do not mark the family paused while a parent/child/service capability or
  active/in-flight LLM reservation remains live, and do not respawn completed
  children on resume. Retained unreconciled exposure is inert, not a live
  reservation.
