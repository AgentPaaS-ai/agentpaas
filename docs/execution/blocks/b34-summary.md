# Block 34 — Runtime-Native Sequential Agent Pipelines

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Target release:** v0.4.0
**Depends on:** B33 complete; `make block33-gate` green
**Must complete before:** B35, B36, and B40
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65

## Outcome

B34 proves that AgentPaaS can execute a declared sequence of agents without
using Hermes, another agent, or the caller as the runtime message bus. At block
completion:

- Every pipeline stage runs as its own AgentPaaS worker container with its own
  gateway, policy, identity, lease, run record, and evidence.
- The AgentPaaS runtime durably schedules the next stage only after the prior
  stage has committed a valid terminal result and handoff.
- A bounded, versioned handoff envelope moves explicit context and artifact
  references between adjacent stages.
- Stage roles resolve through B31 constraints and pin exact component digests
  before admission. Stage messages, terminal events, and artifact transfer use
  B32 rather than a second pipeline-specific transport.
- No credential, hidden model context, raw container filesystem path, or
  unapproved capability can cross a stage boundary.
- Restarting the daemon cannot duplicate a completed stage or lose a committed
  handoff.
- The controller subscribes to durable task events and never polls containers
  to decide that a stage completed.
- A failed, cancelled, or expired stage stops the pipeline with a truthful,
  inspectable terminal result. A B39-eligible `NEEDS_REPLAN` pauses at the same
  stage; it never advances. v0.4 does not silently skip, compensate, or branch.
- The signed pipeline is an immutable B26 deployment. Hermes may author, test,
  package, and deploy it, then must be absent before a CLI/API/cron-style
  invocation. The pipeline completes without Hermes relaying data or advancing
  stages.
- The scheduler has a durable desired-state seam: once `PAUSE_REQUESTED`
  commits, an active stage may finish and commit its handoff, but the next
  stage cannot launch; B39 activates public pause/resume control.

B34 is deliberately a linear, fail-fast primitive. It is not a general DAG,
event bus, distributed transaction engine, or multi-agent conversation
framework. B35 adds one bounded fan-out/fan-in primitive on top of the same
durable workflow state.

## Locked v0.4 pipeline boundary

The supported workflow shape is:

```text
trigger -> stage 1 -> stage 2 -> ... -> stage N -> workflow result
```

The v0.4 contract is intentionally narrow:

1. A pipeline has between 2 and 16 statically declared stages.
2. Each stage references an installed, signed AgentPaaS worker package by
   logical package identity and immutable digest. A pipeline may not supply an
   arbitrary image or command.
3. Stage order is fixed in the signed workflow snapshot. There are no runtime
   branches, loops, dynamic stage insertion, or backward edges.
4. Exactly one stage is active at a time. A stage may use B33 MCP services, but
   may not spawn child agents. Pipeline plus parent/child composition is
   deferred; B35 is a separate workflow kind in v0.4.
5. The pipeline is fail-fast. Only `SUCCEEDED` advances. `FAILED` (including
   reason `POLICY_DENIED`), `CANCELLED`, `EXPIRED`, and `BUDGET_EXCEEDED`
   terminate the workflow. `NEEDS_REPLAN` is always nonterminal, frozen, and
   non-advancing at the same stage. B34 reserves that state but neither
   originates nor continues it; B39 activates the eligible transition and one
   explicit failure continuation while every downstream stage remains blocked.
6. A stage retry or later B39 continuation remains the same logical stage and
   run; it never creates a second downstream handoff.
7. The final stage returns the public workflow result. Intermediate stage
   results remain evidence and are not presented as overall success.
8. Every stage receives only the immediately preceding handoff plus immutable
   workflow metadata. It does not receive another stage's hidden SDK memory,
   provider transcript, credentials, or complete run store.

Any request requiring a branch, cycle, human approval step, compensation, or
dynamic graph fails validation with a stable unsupported-shape reason. Those
features belong after v0.4 evidence exists.

## Locked handoff contract

B34 activates the B26 `HandoffEnvelope` through the B32 message/artifact
broker as the only supported stage-to-stage
data path. Its canonical representation contains at least:

```yaml
schema_version: agentpaas.workflow.handoff/v1
workflow_id: wf_...
handoff_id: ho_...
from_node_id: stage_research
to_node_id: stage_write
producer_run_id: run_...
producer_attempt_id: attempt_...
producer_result_digest: sha256:...
sequence: 1
created_at: 2026-07-16T00:00:00Z
classification: internal
context:
  schema: example/research-notes/v1
  value: {}
artifacts:
  - artifact_id: artifact_...
    owner_node_id: stage_research
    owner_run_id: run_...
    immutable_ref: artifacts/research-notes.json
    digest: sha256:...
    media_type: application/json
    size_bytes: 1234
    classification: internal
```

Rules:

1. `context.value` is canonical JSON and is limited to 256 KiB after canonical
   serialization. There is no arbitrary pickle, executable object, or opaque
   provider blob.
2. Large data moves through immutable artifact references, not by inflating the
   context envelope. The default aggregate artifact-reference metadata limit
   is 64 KiB; artifact content remains governed by B27 limits and workflow
   policy.
3. The producer declares the context schema. The consumer stage declaration
   lists accepted schema identifiers. A mismatch fails before starting the
   consumer.
4. The runtime supplies identity, digests, sequence, and timestamps. Worker
   code cannot forge them.
5. Reserved credential/control fields, known credential fingerprints,
   capability tokens, container paths, daemon control fields, and reserved
   keys are rejected before commit. AgentPaaS does not claim semantic DLP for
   arbitrary JSON; workers never receive credential values through the SDK in
   the first place.
6. The handoff is immutable after commit. A digest mismatch, duplicate ID with
   different content, or second commit from the same terminal stage fails
   closed.
7. Downstream artifact mounts are read-only. A stage writes only within its own
   B27 attempt artifact namespace.
8. Labels/classifications may remain the same or become more restrictive
   downstream according to B26's strict classification order. Every artifact
   entry is a complete B26 immutable `ArtifactRef`; a pipeline may not
   declassify data or expose a host/container path.

The worker SDK exposes two intentionally small operations:

```python
incoming = agent.workflow_input()
agent.commit_handoff(
    schema="example/research-notes/v1",
    context={"summary": "..."},
    artifacts=[artifact_ref],
)
```

`workflow_input()` is read-only: it returns `None` for pipeline stage one,
returns the validated preceding handoff for later stages, and fails with typed
`WORKFLOW_CONTEXT_UNAVAILABLE` outside a pipeline. `commit_handoff()` is valid
only for a non-final pipeline stage, only once, and only while that stage owns a
live lease; final-stage or non-pipeline use fails with typed
`HANDOFF_NOT_ALLOWED`. A non-final stage that returns success without a valid
handoff is converted to `FAILED/HANDOFF_MISSING`; it never advances.

Despite the SDK name, `commit_handoff()` durably stages an authenticated
candidate. It becomes the accepted immutable handoff only in the atomic stage
success/handoff/next-ready transition. If the handler later fails or the lease
is fenced, the candidate is retained only as failed-attempt evidence and is
never exposed downstream.

The SDK talks only to the local trusted harness. Extend the B30 authenticated
attempt control journal/mailbox for bounded handoff candidate and response
records, or use an equivalently protected harness-to-daemon channel. Never
mount the daemon socket, workflow-store path, journal key, or scheduler
capability into Python.

## Locked scheduling and commit protocol

AgentPaaS is the scheduler and durable courier:

1. During immutable deployment creation, compile/sign the pipeline snapshot
   and resolve every stage package to an exact installed version/digest.
2. B26 `AdmitInvocation` pins that snapshot and atomically precreates the
   workflow plus every fixed stage node/run: stage one is `READY`, later stages
   are `PENDING`, and no attempt/lease/container exists yet.
3. The scheduler claims the precreated `READY` node by CAS and atomically
   creates its attempt, lease, and launch job while moving it to `LAUNCHING`;
   it never creates a second stage run.
4. Accept a handoff candidate only from the authenticated current attempt and
   validate policy, schema, bounds, artifact digests, and lease.
5. On successful stage completion, atomically persist the terminal stage
   result, committed handoff, and `STAGE_READY(next)` under one workflow CAS
   transition.
6. Only after that durable transition may the scheduler launch the next stage.
7. Use a stable idempotency key derived from workflow ID, node ID, and stage
   generation. Reconciliation after a crash must discover an existing launch
   instead of starting a duplicate.
8. Fence the previous stage before granting the next stage access to its
   handoff and artifacts.
9. On the final stage, atomically persist its result and the workflow terminal
   result. No handoff is required from the final stage.
10. On cancellation or active-time exhaustion, revoke the workflow lease,
    fence the active stage and services, and prevent every future stage from
    starting.
11. If `PAUSE_REQUESTED` is durable when a stage succeeds, atomically commit
    stage result and handoff plus workflow `PAUSED`; do not mark or launch the
    next stage. If no stage is active, pause at the existing boundary.
12. Resume changes `PAUSED` to the appropriate next/active node state only
    after B26 concurrency reacquisition and B39 revalidation. Repeated
    pause/resume cannot duplicate a stage or handoff.

The workflow controller runs inside the AgentPaaS daemon/runtime. It is not a
Hermes prompt loop and does not depend on an open client request.

## Authority, budget, and provenance rules

Each stage receives the intersection of:

- Workflow-level policy and remaining active-time ceiling.
- The stage package's signed manifest and policy.
- The workflow node's declared MCP/model/network requirements.
- Remaining workflow authority and remaining aggregate resource budget.

The result is a new stage-specific capability set; authority is never copied
wholesale from the prior stage. In particular:

- Credentials are freshly resolved for the current stage and are never placed
  in a handoff.
- Network and MCP access is independently evaluated for every stage.
- The stage effective operation deadline cannot exceed workflow active time
  remaining.
- B38 later enforces one shared LLM spend ledger across all stages. Until B38,
  model-spend-enabled pipelines remain behind the not-yet-enabled gate; B34
  deterministic proofs use no-cost fake/local operations.
- Workflow evidence links every result and artifact to producer package digest,
  run, attempt, policy digest, and handoff digest.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Freeze the workflow and handoff conformance fixtures | B26, B33 | valid/invalid pipeline fixtures and legacy negatives pass |
| 2 | T02 Implement strict pipeline compilation | T01 | signed immutable snapshots and unsupported-shape tests pass |
| 3 | T03 Implement SDK input and handoff operations | T01, B27 | SDK/harness auth, schema, size, and secret negatives pass |
| 4 | T04 Implement durable linear workflow scheduling | T02–T03 | two- and three-stage fake pipelines survive daemon restart |
| 5 | T05 Enforce separate stage containers and authority | T04 | identity/network/policy isolation tests prove separation |
| 6 | T06 Implement artifact transfer and provenance | T03–T05 | read-only mounts, digest, classification, and tamper tests pass |
| 7 | T07 Reconcile failure, cancel, pause boundary, and idempotency | T04–T06 | crash/control race matrix has one deterministic paused or terminal result |
| 8 | T08 Add independent deployment invocation, operator inspection, and reference proof | T04–T07 | Hermes-absent-before-start CLI/API evidence proves native execution |
| 9 | T09 Run the block gate and adversary review | T01–T08 | `make block34-gate` green and handoff recorded |

Do not start B35 until T09 is complete.

## Expected implementation areas

Use the package locations established by B26–B33; do not create parallel
contract or store packages merely to match these suggested names. Expected
areas include:

- B26 workflow contracts/store and atomic transition journal.
- A focused linear controller such as `internal/workflow/pipeline/**`.
- `internal/daemon/**` durable scheduler/reconciliation integration.
- `internal/harness/**` authenticated handoff request/response transport.
- `python/agentpaas_sdk/**` workflow input/handoff methods and tests.
- `internal/runtime/**` container labels, mounts, networks, and cleanup.
- `api/**`, `internal/operator/**`, and `internal/cli/**` additive inspection.
- `test/workflow/**` or the repository-equivalent deterministic, Docker,
  crash, and adversary fixtures.
- `Makefile` and CI/release-artifact wiring for the block gates.

## T01 — Freeze workflow and handoff conformance fixtures

**Goal:** Convert the B26 representational contracts into executable fixtures
without changing behavior yet.

**Instructions:**

1. Add canonical fixtures for two-stage, three-stage, maximum-stage, and
   incompatible-schema pipelines.
2. Add invalid fixtures for zero/one/too-many stages, duplicate node IDs,
   branches, cycles, dynamic package references, mutable tags, undeclared MCP
   services, and unsupported compensation instructions.
3. Add canonical handoff fixtures at zero bytes, normal size, exact context
   limit, and one byte over each limit.
4. Include malicious keys, credential-looking values, path traversal,
   symlinked artifacts, digest substitution, forged producer identity,
   declassification, duplicate IDs, and non-canonical JSON.
5. Freeze v0.2.3 standalone-run fixtures and B33 service fixtures to prove
   pipeline support is additive.
6. Record exact stable failure codes and which validation layer owns each one.

**Tests:**

- Schema and canonicalization golden tests.
- Fuzz/property tests for envelope decoding and bounds.
- Legacy standalone and service regression suites.
- A negative that proves no current command can claim a pipeline completed.

**Done when:** Every supported and rejected shape has a fixture and an expected
reason before runtime scheduling is enabled.

## T02 — Implement strict pipeline compilation

**Goal:** Turn `workflow.yaml` into one immutable, policy-bound execution plan.

**Instructions:**

1. Resolve each logical worker package to an installed signed digest.
2. Resolve stage input/output schema declarations and reject incompatible
   adjacent stages.
3. Resolve declared B33 MCP bindings per stage but do not grant them yet.
4. Canonicalize the stage list, limits, classifications, package digests,
   policy digests, and initial workflow active-time/spend ceilings.
5. Sign/hash the compiled snapshot and persist it before launch.
6. Store every exact stage deployment/package identity and digest inside the
   immutable top-level deployment so admission never consults an alias or
   mutable package reference for a later stage.
7. Reject mutable image tags, shell commands, host paths, implicit packages,
   and any field unknown to the strict schema.
8. Make recompilation with identical inputs byte-identical. A changed package,
   policy, or workflow file creates a new digest and requires a new workflow.
9. Keep generated IDs separate from signed declarative content so timestamps
   do not break deterministic snapshot tests.

**Tests:**

- Deterministic compilation and canonical digest goldens.
- Package/policy substitution and TOCTOU tests.
- Schema compatibility matrix.
- Unsupported graph-shape and unknown-field negatives.

**Done when:** A runtime can consume the snapshot without consulting mutable
workflow source or asking Hermes to reinterpret it.

## T03 — Implement SDK workflow input and handoff operations

**Goal:** Give stages one explicit, authenticated data-transfer API.

**Instructions:**

1. Add `workflow_input()` and `commit_handoff()` to the Python SDK and harness
   protocol with strict typed responses.
2. Bind each request to workflow, node, run, attempt, generation, and lease via
   the authenticated harness channel. Ignore worker-supplied identity fields.
3. Return only the current node's incoming envelope and approved read-only
   artifact references.
4. Canonicalize and validate candidate context before writing any durable
   record.
5. Scan reserved fields, known local credential fingerprints, and
   credential/capability representations using exact structural rules; do not
   use an LLM or heuristic verifier or claim arbitrary-content DLP.
6. Verify artifact ownership, completeness, digest, classification, media type,
   and size against B27 state.
7. Make repeat submission of identical bytes idempotent; reject different bytes
   for the same handoff key.
8. Return `None` for first-stage `workflow_input()`; return stable typed errors
   for standalone input access, final-stage/non-pipeline handoff commit, stale
   lease, post-terminal calls, oversize content, and schema mismatch.

**Tests:**

- SDK unit and serialization parity tests.
- Authentication, replay, stale lease, and forged identity negatives.
- Exact limit boundaries and fuzzed JSON.
- Secret/capability/path rejection and artifact tamper tests.

**Done when:** No worker-controlled field can widen authority or impersonate a
different stage, and a valid handoff is deterministic.

## T04 — Implement the durable linear workflow scheduler

**Goal:** Advance stages from durable state with exactly-once logical effects.

**Instructions:**

1. Consume the workflow and fixed stage node/run identities precreated by B26
   admission; implement CAS transitions/events for ready, launching, active,
   handoff committed, terminal, and workflow terminal states without a second
   run-creation path.
2. Generate a stable launch idempotency key per workflow node generation.
3. On `READY` claim, atomically create the initial attempt/lease/job and move
   the precreated node to `LAUNCHING`.
4. Reconcile `LAUNCHING` by labels and durable job records before creating a
   container.
5. Start only the earliest nonterminal ready node; assert at most one active
   node for this linear workflow type.
6. Commit stage success, handoff, and next-ready transition atomically. A crash
   between those logical effects must replay the transaction, not duplicate it.
7. Keep next-stage input unavailable until the previous lease is fenced and
   the handoff commit is durable.
8. Derive the public workflow status and result from state rather than a
   process-local tracker.
9. Use B30 asynchronous jobs, active-time/operation deadlines, liveness,
   cancellation, cleanup, and restart reconciliation for each stage.
10. Consume B26 desired state before every launch. If `PAUSE_REQUESTED` is
   observed, commit no new launch. On active-stage success, atomically choose
   `PAUSED` rather than `next READY`; on resume, create exactly that deferred
   ready transition.

**Tests:**

- Two-, three-, and sixteen-stage deterministic pipelines.
- Crash injection before/after every state write and container launch.
- Crash/replay after admission but before stage-one claim and after claim but
  before launch; the precreated stage run is never duplicated.
- Concurrent daemon reconciliation and CAS conflict tests.
- Duplicate result/handoff delivery and late old-attempt events.
- A restart test proving no completed stage reruns.
- Pause request before launch, during stage, at result/handoff commit, and
  concurrent with cancel; no next stage launches until resume.

**Done when:** Every injected crash yields one valid workflow history and at
most one logical execution of each successful stage.

## T05 — Enforce a separate container and authority boundary per stage

**Goal:** Prove a pipeline is real multi-container execution, not in-process
function chaining.

**Instructions:**

1. Launch each node through the normal AgentPaaS worker/gateway container path.
2. Label containers with workflow, node, run, attempt, package digest, policy
   digest, and lease generation; do not place secrets in labels.
3. Issue fresh stage-scoped capabilities and B33 service bindings from the
   authority intersection.
4. Prevent direct stage-to-stage network access. Data moves only through the
   durable handoff/artifact path or separately approved MCP services.
5. Prohibit shared writable volumes between stages. Use immutable artifact
   projection for approved inputs.
6. Confirm a stage cannot inspect another stage's environment, filesystem,
   gateway, credential handles, or control socket.
7. Terminate and fence one stage before starting the next, while retaining
   durable evidence and approved artifacts.

**Tests:**

- Runtime container-count and identity-label assertions.
- Cross-stage network, filesystem, credential, and gateway denial tests.
- Policy intersection tests where adjacent stages have different authority.
- Cleanup tests proving no prior worker/gateway remains active after advance.

**Done when:** Evidence proves each stage had an independent sandbox and the
handoff is the only implicit connection between them.

## T06 — Implement artifact transfer and provenance

**Goal:** Move large outputs safely without copying hidden state.

**Instructions:**

1. Promote only explicitly referenced, completed B27 artifacts into immutable
   workflow storage.
2. Persist the complete B26 `ArtifactRef`: artifact/owner identities, immutable
   logical reference, digest, media type, schema, size, and classification.
3. Materialize approved downstream artifacts read-only under a reserved SDK
   path that cannot collide with the worker's writable artifact directory.
4. Verify digest before mount and again when producing final evidence.
5. Prevent symlink, hard-link, device, traversal, and special-file escape.
6. Enforce workflow aggregate bytes and artifact count even when every stage is
   individually under its limit.
7. Delete ephemeral mounts on terminal cleanup while retaining committed
   artifacts as inspectable workflow/run history. v0.4 has no automatic purge
   or configurable retention lifecycle.

**Tests:**

- Multi-megabyte artifact handoff without context inflation.
- Digest mismatch, mutation, truncation, symlink, traversal, and classification
  negatives.
- Aggregate byte/count exhaustion and cleanup tests.
- Provenance traversal from final result to original producer attempt.

**Done when:** A downstream stage can consume an approved immutable artifact
and cannot mutate or substitute its provenance.

## T07 — Reconcile failures, cancel, pause boundary, and idempotency

**Goal:** Make every abnormal pipeline ending deterministic and truthful.

**Instructions:**

1. Map each stage terminal outcome to one workflow terminal state and stable
   reason. `NEEDS_REPLAN` is nonterminal and non-advancing; preserve every
   stage's underlying reason as evidence.
2. Never launch a downstream stage after a non-success result, invalid/missing
   handoff, cancellation, exhausted workflow active time, durable
   `PAUSE_REQUESTED`, or revoked lease.
3. Make repeated cancel, pause, resume, result, handoff, cleanup, and
   reconciliation calls idempotent.
4. On workflow cancellation, cancel/fence the active stage and all bound B33
   services, then clean up containers and temporary mounts.
5. On daemon restart, reconcile active jobs and containers before deciding to
   continue or terminate.
6. Reject late writes from an old stage attempt or old workflow generation.
7. Preserve the last committed handoff and all prior evidence on failure; never
   fabricate a final successful result from intermediate output.
8. A stage that succeeds while pause is requested commits its result/handoff
   and leaves the workflow fully `PAUSED` with no live stage/service. A stage
   failure remains failure; pause never converts it to success.

**Tests:**

- Failure at each stage position and every terminal reason.
- Cancellation during launch, MCP call, handoff commit, stage terminal commit,
  and next-stage launch.
- Daemon kill/restart at every transition.
- Duplicate/late event storms and concurrent cancel/result races.
- Pause/result/handoff/next-launch/cancel races and repeated resume after
  daemon restart.

**Done when:** Repeated fault runs converge to one valid durable state
(`PAUSED` or terminal as appropriate), at most one active pipeline-stage
worker, no orphaned supporting services, and no downstream execution after
failure.

## T08 — Add independent deployment invocation, operator inspection, and the reference proof

**Goal:** Make native pipeline execution understandable without requiring a
debugger or Hermes relay.

**Instructions:**

1. Extend additive CLI/operator responses to show workflow ID, snapshot digest,
   ordered nodes, active node, per-stage run/attempt/outcome, handoff digests,
   artifact references, current/consumed/remaining active time, and final
   reason.
2. Keep output bounded; provide paginated/timeline detail rather than embedding
   complete context or artifact contents.
3. Build a general-audience three-stage fixture, for example:
   `collect approved inputs -> produce a draft -> format a deliverable`.
4. Have Hermes build, test, package, and deploy an immutable version/alias using
   AgentPaaS skills, then terminate Hermes before invocation.
5. Invoke once through the public CLI and once through the authenticated API
   using stable idempotency keys. Prove requested/resolved deployment identity
   and durable workflow ID.
6. Run the complete shared B26 admission-conformance suite for the pipeline
   topology: exact/alias pinning of the top-level and every stage, same-key
   replay, changed ref/input/initial-ceiling/creation-option conflict, alias
   movement, inactive future admission, default-one overlap/no queue,
   configured-safe concurrency, and paused-slot resume reacquisition.
7. Prove the AgentPaaS daemon advances all three separate stage containers and
   returns the final deliverable.
8. Include an intentional incompatible-schema and stage-failure demo with clear
   evidence and no false success.

**Tests:**

- CLI/protobuf schema and backward-compatibility goldens.
- Bounded-output tests with the maximum stage count.
- Clean-profile end-to-end proof with Hermes absent before invocation.
- Public CLI/API B26 admission-conformance matrix for the pipeline topology.
- Container/event trace proving order and separation.

**Done when:** A practitioner can see what ran, what moved, what failed, and why
without reading daemon internals.

## T09 — Block gate and adversary review

**Goal:** Prevent pipeline work from being declared complete on unit tests or
in-process mocks alone.

**Required commands:**

```text
make block33-gate
make block30-ci
make block30-adversary
make block30-live
make block34-gate
```

`block34-gate` must fail unless all of the following evidence exists:

1. B26–B33 cumulative gates are green.
2. Real separate worker/gateway containers are observed for every stage.
3. A three-stage deployment invoked by CLI/API finishes with Hermes absent
   before start.
4. Daemon restart tests prove no stage duplicate and no lost handoff.
5. Schema, size, secret, credential, capability, path, artifact, and
   classification attacks fail closed.
6. Failure/cancellation/pause never starts the next stage; resume starts only
   the deferred stage once.
7. The shared B26 pipeline admission suite passes and no stage run is created
   twice between admission, scheduler claim, or crash recovery.
8. Standalone agents and B33 MCP services retain compatibility.
9. Evidence contains workflow, node, run, attempt, policy, package, handoff,
   artifact, and terminal provenance without secret values.
10. The successful three-stage proof and critical restart boundary pass three
   consecutive clean Docker runs with test-harness retries disabled.

The reviewer must explicitly challenge whether any data path bypasses the
handoff contract, whether Hermes is still secretly required at invocation or
runtime, whether pause can leak a next-stage launch, and whether a crash can
double-run a stage.

B34 is complete only when the required gates are green, the reviewer records
no unresolved high-severity issue, and the task handoff record is committed.

## Handoff record required after every task

Append one durable execution-plan handoff containing:

- Block/task ID and status.
- Commit or change-set identifier.
- Exact tests and gates run, with results and evidence paths.
- Contract/schema/API changes.
- Security and compatibility findings.
- Known gaps and deferred scope.
- Explicit next task and prerequisites.

An orchestrating model must read the prior handoff, verify the referenced
evidence, inspect the diff, and run the task-specific preflight before changing
code. It may not infer completion from a prose summary.

## Pitfalls

- Do not implement a pipeline as Python function calls in one container.
- Do not make Hermes or a worker poll and trigger the next stage.
- Do not pass credentials, hidden LLM transcripts, or whole run directories in
  handoffs.
- Do not treat a queue publication as stage completion; durable result and
  handoff commit are required.
- Do not add DAG branches, loops, compensation, partial success, or dynamic
  stage creation in v0.4.
- Do not claim shared model spend enforcement before B38.
