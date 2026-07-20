# Block 26 — Durable Deployment, Invocation, Run, and Workflow Contracts/State Foundation

**Status:** IMPLEMENTED — retained as the binding completion record
**Date:** 2026-07-16
**Last reconciled:** 2026-07-18
**Target release:** v0.3.0
**Depends on:** v0.2.3 tag and all existing release gates green
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D33
**Must complete before:** B27 and every later release-train block

**Post-block compatibility:** Approved decisions D34–D65 apply prospectively.
B28 must adapt these completed deployment/invocation/state contracts behind
portable tenant-aware ports rather than reopening or replacing B26 history.

## Outcome

B26 establishes the stable, backward-compatible contracts that every durable
deployment, invocation, run, workflow, and Routed Run component uses. At
block completion:

- Existing v0.2.3 single-model agents still pack and run unchanged.
- A new worker may name a logical model route in `agent.yaml`.
- Signed `policy.yaml` can declare a two-tier approved model route and Routed
  Run limits.
- Run, attempt, lease, checkpoint, artifact, failure, route-decision, usage,
  cost, attempt-report, workflow, stage, service, handoff, child-batch, and
  child-result schemas are versioned.
- Immutable deployment versions, mutable audited aliases, direct invocation,
  invocation idempotency/concurrency, deactivation, and retained-history
  contracts are versioned.
- The control/operator APIs can represent cancel, `PAUSE_REQUESTED`, `PAUSED`,
  resume, new-run restart, `NEEDS_REPLAN`, one failure continuation, and
  append-only limit amendments without yet implementing the full supervisor.
- Protected local state stores persist deployment, alias, invocation,
  amendment, control transition, run, attempt, workflow, handoff, service, and
  parent/child metadata atomically.
- Later blocks can implement SDK progress, selection, recovery, spend, and
  supervision without redefining core state.

B26 is a contract and persistence block. It does not advertise working
long-running execution, cross-container MCP, pipelines, parent/child spawn,
model fallback, worker recovery, or cost optimization.

## Why this block is first

The v0.2.3 runtime has one `llm` configuration, in-memory run tracking, no
immutable deployment/alias identity, no durable invocation idempotency, no
attempt identity, no lease, no workflow identity, no durable handoff, and no
portable attempt report. Implementing long-running supervision, direct
deployment invocation, operator lifecycle, service discovery, pipelines,
child scheduling, routing, or watchdogs directly in those structures would
create incompatible state and force later rewrites.

B26 freezes the legacy path and creates explicit seams before behavior is
added.

## Locked design decisions

### Single-agent configuration remains in the two existing project files

Do not introduce `routes.yaml`, `models.yaml`, or another required project
file in v0.3.

A multi-agent workflow is a distinct signed artifact and may contain one
optional `workflow.yaml`. Ordinary agent projects do not need it. Do not hide
pipeline/child authority in prompts or trigger payloads.

`agent.yaml` names the logical route:

```yaml
llm:
  route: worker.general
```

The legacy form remains valid:

```yaml
llm:
  provider: openrouter
  model: provider/model
  credential: openrouter-key
```

`route` is mutually exclusive with `provider`, `model`, and `credential`.

`policy.yaml` owns approved targets, cloud-transfer authority, minimum
requirements, and runtime limits:

```yaml
version: "1.1"

llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: 2.00

routed_run:
  model_call_timeout: 2m
  stall_timeout: 2m
  attempt_lease: 10m
  max_active_duration: 20m
  recovery_margin: 2m
  max_llm_calls: 40
  max_model_recoveries_per_attempt: 1
  max_worker_retries: 1
  max_identical_tool_actions: 3
  max_actions_without_progress: 10

model_routes:
  worker.general:
    pattern: cloud-cost-first
    cloud_transfer: allowed
    minimum:
      capability_tier: standard
      context_tokens: 32768
      features: [chat]
    candidates:
      - id: primary-cloud
        role: primary
        provider: openrouter
        model: provider/economical-model
        upstream_providers: [provider-endpoint-a]
        credential: openrouter-key
        location: cloud
      - id: recovery-cloud
        role: recovery
        provider: openrouter
        model: provider/capable-model
        upstream_providers: [provider-endpoint-b]
        credential: openrouter-key
        location: cloud
```

The example model names are placeholders, not release defaults.
The numeric time and spend values are synthetic schema examples, not release
defaults. B30/B39/B41 calibration and explicit approval own the published
values.

Policy schema `1.0` remains the immutable legacy form. Routed fields require
policy schema `1.1`; the new parser accepts both versions, preserves v1.0
canonical digests, and rejects routed fields labelled as v1.0 or unknown
future versions with a clear typed error.

### Route vocabulary

Use these v0.3 enums:

- `pattern`: `local-first` or `cloud-cost-first`
- `role`: `primary` or `recovery`
- `location`: `local` or `cloud`
- `cloud_transfer`: `allowed` or `denied`
- `capability_tier`: `basic`, `standard`, or `advanced`
- `feature`: `chat`, `structured_json`, or `reasoning_effort`
- `effort`: `standard` or `high`

New values require additive schema work and tests. Unknown values fail closed.

Route IDs are 1–128 ASCII characters and match
`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`. Candidate IDs are route-local,
1–64 ASCII characters, and use the same grammar. Normalize nothing: reject
uppercase, Unicode/confusables, leading/trailing separators, and duplicate
byte strings rather than silently rewriting signed authority.

A routed v0.3 project compiles exactly the one route named by
`agent.yaml`. Its policy contains that one executable route with 2–64
candidates. The map-shaped schema preserves a future multiple-route seam, but
v0.3 rejects extra executable routes rather than carrying unused authority.

### Candidate contract

Every candidate has:

- Stable route-local `id`.
- `role`, `provider`, `model`, and `location`.
- `credential` for authenticated targets.
- `upstream_providers` for an aggregator target: a non-empty bounded list of
  exact catalog-known upstream endpoint slugs. It is required for routed
  OpenRouter candidates, forbidden for direct/local adapters, and part of
  signed authority.
- Optional `endpoint` only for an explicitly supported custom provider type.
- Optional `auth: none` only for an explicitly approved local endpoint.

Known cloud models are resolved against the versioned catalog in B36. Custom
local OpenAI-compatible targets supply administrator-asserted metadata through
the B36 catalog overlay; B26 reserves the schema field but does not implement
selection.

An aggregator candidate always names one model. `models`, `fallbacks`,
auto-router aliases, and provider routing outside `upstream_providers` are not
v0.3 authority. If an upstream family slug could expand to unreviewed variants,
validation must require an exact reviewed endpoint slug or split the envelope
into explicit candidates.

`upstream_providers` contains 1–8 unique entries. Each is 1–128 safe ASCII
characters, must exactly match a catalog identity, and is not normalized.
Reject whitespace, controls, URL syntax, backslashes, dot segments, empty
segments, and values that the current adapter/catalog resolves as a broader
provider family rather than the reviewed endpoint envelope. The list is an
allowlist set, not a priority order; canonicalization sorts it bytewise after
rejecting duplicates.

### Runtime limits

All duration fields use Go duration strings in policy and normalized
milliseconds in API/state.

B26 validates relationships but does not lock release defaults. At minimum:

- Every configured duration must be positive and bounded.
- `model_call_timeout <= attempt_lease`.
- `stall_timeout <= attempt_lease`.
- `attempt_lease < max_active_duration`.
- `recovery_margin` must be positive and less than `max_active_duration`.
- `attempt_lease + recovery_margin <= max_active_duration` for the initial
  attempt.
  Before the one whole-worker `FAILURE_CONTINUATION` is consumed, ordinary work
  must leave this minimum time for fencing and that continuation. Call-level
  model recovery stays inside its already authorized call/attempt envelope and
  neither reserves nor subtracts a second margin. When the failure continuation
  is atomically admitted, the margin is released once into that final attempt's
  remaining active time; no margin is held for a second continuation. It never
  extends or resets the active-time ceiling.
- `max_model_recoveries_per_attempt` must be `0` or `1`.
- `max_worker_retries` must be `0` or `1`.

Final defaults are approved after B30 foundation calibration and B39/B41
integration/live regression.

`max_active_duration` is accumulated active execution time, not an absolute
wall timestamp. `RUNNING` and `PAUSE_REQUESTED` consume it. Fully `PAUSED` and
`NEEDS_REPLAN` do not. B30 implements the accounting and B39 integrates it.
One workflow has at most one open active-time segment: parallel parent, child,
stage-support, or MCP-service activity advances elapsed workflow active time
once, never once per node. Attempt leases and operation timers remain
independent and may overlap.

The policy carries initial ceilings. A B39 administrative amendment is a
receiver-local, append-only authority overlay on one non-terminal workflow;
it never rewrites the signed package or original snapshot.

### Immutable deployment, alias, and invocation contract

Do not add another required project source file. Deployment is receiver-local
operational state created from an already installed, verified package or
workflow artifact.

An exact deployment identity contains:

- Deployment ID.
- Package/workflow name and semantic version.
- Immutable bundle/package/workflow, policy, image/lock, and provenance
  digests.
- For a workflow deployment, the exact installed version/digest of every
  statically declared stage, MCP service, and child-allowlist package.
- Deployment generation and `ACTIVE|INACTIVE` status.
- Receiver-local `max_concurrent_runs`, default `1` and bounded by the
  runtime-wide capacity.
- Created/activated/deactivated actor and audit references.

An alias is a mutable, generation-checked pointer such as
`production/customer-report -> customer-report@1.2.0`. Alias promotion and
rollback are the same atomic operation with different target versions. Every
accepted invocation records both the requested exact/alias reference and the
resolved exact deployment identity and its nested package snapshot. Alias
mutation never changes that record, and no stage/service/child package alias is
resolved after admission. An alias resolving to an inactive or missing
deployment fails closed.

The durable invocation request contains:

```text
schema_version
requested_deployment_ref
bounded_input_json
input_digest
initial_max_active_duration_ms
initial_attempt_lease_ms
initial_max_cost_usd_decimal
creation_options_digest
idempotency_key
caller_identity
```

The API requires a caller-supplied idempotency key. The CLI generates and
prints a random key for ad hoc use when omitted; scheduled examples require a
stable event-derived key. Reject duplicate JSON object keys and compute
`input_digest` from the bounded canonical JSON representation. Canonicalize all
creation-time options and reject unknown or duplicate fields. Scope lookup to
caller identity plus idempotency key (and receiver/tenant namespace when one
exists), then store a canonical invocation-intent digest over the requested
deployment reference, input, effective initial active-time/attempt-lease/spend
ceilings, and every option that can change execution or authority. A
canonical-equivalent retry returns the same workflow/run ID even if the alias
moved afterward. Reuse with any changed intent field returns
`IDEMPOTENCY_CONFLICT`; timestamps, display labels, and transport metadata that
cannot affect execution are explicitly excluded from the digest.

After idempotency lookup, admission atomically counts slot-holding top-level
workflows for the resolved exact deployment. One accepted workflow holds one
slot while `PENDING`, `RUNNING`, or `PAUSE_REQUESTED`; terminal states release
it. `PAUSED` and `NEEDS_REPLAN` release it and resume must reacquire one.
Internal stage, service, and child runs do not consume another top-level slot
for the same accepted workflow; signed workflow aggregate capacity governs
them. A distinct request over `max_concurrent_runs` returns retryable
`ALREADY_RUNNING` and is not persisted as accepted queued work; v0.3 has no
hidden top-level invocation queue. Explicit visible `READY`/`ALLOCATED` stages
or children inside an already accepted workflow are workflow state, not
top-level admission backlog.

Deactivation prevents new invocation but does not cancel an accepted run.
There is no destructive delete/purge API in v0.3. Historical deployment,
alias, invocation, result, cost, and audit records remain readable.

### Operator control and limit-amendment contract

Reserve these desired/observed lifecycle states now:

```text
RUNNING -> PAUSE_REQUESTED -> PAUSED -> RUNNING
RUNNING|PAUSE_REQUESTED|PAUSED|NEEDS_REPLAN -> CANCELLED
terminal or active source run -> separate restarted run
```

Control commands are authenticated, generation-checked, idempotent, and
audited. Cancellation is terminal. Pause is cooperative: it sets durable
desired state immediately, blocks every new node/child/continuation launch,
and reaches `PAUSED` only after B39 proves the appropriate safe boundary and
fences/stops workflow-owned execution resources. `PAUSE_REQUESTED` still
consumes active time and may spend while already-active work drains. Fully
`PAUSED` and `NEEDS_REPLAN` consume no active time, permit no new model/tool
work, have no active/in-flight LLM reservation, and retain no live
workflow-owned worker/service container or capability. Unknown provider
billing may remain as immutable unreconciled exposure that reduces available
headroom but authorizes no work.

Resume preserves the workflow ID, exact deployment, input, current authority
generation, accumulated spend, consumed active time, checkpoint/handoff/child
state, and recovery counters. It reacquires concurrency and revalidates the
pinned deployment's integrity/availability, credential, policy,
checkpoint/artifact, and resource state. Because resume is not a new
invocation, admission-only deactivation does not cancel, re-resolve, or by
itself block the accepted run.

Restart is a new invocation with a new idempotency key and workflow/run ID.
The default target is the source run's exact deployment version and original
input. It starts from stage one and records `restarted_from`; it does not
implicitly import a checkpoint. A caller must explicitly request current
alias resolution if it wants potentially different code. Any non-terminal
active or frozen source is cancelled/fenced before the new run is admitted.

A limit amendment contains absolute, increase-only values:

```text
amendment_id
workflow_id
expected_authority_generation
new_max_active_duration_ms       # optional
new_current_attempt_lease_ms     # optional
new_max_llm_spend_decimal        # optional
reason
idempotency_key
actor_identity
```

It is accepted only in `RUNNING`, `PAUSE_REQUESTED`, `PAUSED`, or
`NEEDS_REPLAN`, only through administrative `runs:amend_limits` authority, and
only before terminal exhaustion commits. A new request must increase at least
one value; exact replay returns its original result, while a new unchanged or
decreasing request fails. The atomic
transition records original/current values, consumed active time, reconciled
and reserved spend, actor, reason, time, and the new authority generation.
Ordinary invoke credentials, workers, SDK calls, model text, artifacts, and
Hermes without explicit user confirmation cannot submit it.

An amended current-attempt lease updates the live lease in `RUNNING` or
`PAUSE_REQUESTED`. In `PAUSED` or `NEEDS_REPLAN` it is the maximum lease for
the next authorized resume/continuation attempt; the amendment itself creates
no attempt and consumes no active time.

### One run, multiple attempts

A continuation keeps the original run ID and creates a new attempt ID. It is
not represented as an unrelated new run.

Initial continuation actions are:

- `more_time`
- `capability_up`
- `larger_context`

`split_task` is not a continuation action. A practitioner may create and
deploy a new workflow envelope through Hermes, but B34 and B35 let AgentPaaS
advance declared stages and approved children without an authoring client.
`stop` maps to terminal cancellation. A failure continuation attempt, an
administrative pause/resume execution segment, a pipeline stage, and a child
run are different durable concepts and must not be conflated. Pause/resume
never resets the one allowed failure-continuation counter.

### Workflow, stage, service, and child hierarchy

Use one explicit hierarchy:

```text
workflow
  run (standalone, pipeline stage, parent, child, or MCP service)
    attempt
```

- A standalone invocation still receives a workflow ID so later accounting
  and storage do not need a migration.
- A linear pipeline contains ordered stage nodes. Each stage points to one
  run and commits at most one accepted handoff to the next stage.
- An MCP service is a run with `run_kind: mcp_service`; callers refer to a
  logical service binding, never a container address.
- A parent run may own child batches. v0.3 child runs are leaf runs and cannot
  create another child batch.
- Every member shares the workflow active-time and aggregate resource/spend
  ledgers; a member may narrow them but only an authenticated workflow-level
  amendment may raise the current ceiling.
- The workflow active-time ledger counts elapsed `RUNNING`/
  `PAUSE_REQUESTED` time once even when parent, children, or services overlap;
  it is not a sum of per-node elapsed time.

Define stable IDs for workflow, node/stage, service binding, handoff, child
batch, and child result now. B33–B35 implement their behavior.

### Durable local state layout

The first store uses:

```text
~/.agentpaas/state/deployments/
  deployments/<deployment_id>.json
  aliases/<escaped_alias>.json
  invocations/<caller_scope>/<idempotency_digest>.json
  transactions/

~/.agentpaas/state/runs/<run_id>/
  run.json
  attempts/<attempt_id>.json
  controls.jsonl
  checkpoints/
  artifacts/
  ledger.jsonl
  invoke-response.json

~/.agentpaas/state/workflows/<workflow_id>/
  workflow.json
  events.jsonl
  transactions/
  nodes/<node_id>.json
  services/<service_id>.json
  handoffs/<handoff_id>.json
  child-batches/<batch_id>.json
  amendments/<amendment_id>.json
  controls.jsonl
  ledger.jsonl
```

`events.jsonl`/`transactions` are the authoritative atomic workflow-transition
journal for the first local implementation. Materialized node/service/handoff/
batch files are rebuildable views. The workflow `ledger.jsonl` is the B38 cost
ledger; the run-level ledger remains operational call/attempt evidence and
must not become a second budget authority.

All directories are `0700`; state files are `0600`. Writes use temp file,
fsync, rename, and parent-directory fsync. User-controlled IDs and paths never
become raw path components.

Every durable JSON or JSONL envelope carries an explicit state schema version.
The store has one ordered migration registry rather than migration logic
scattered through readers. Unknown newer versions fail closed before mutation.
Supported migrations are idempotent, stage and fsync replacements, retain a
recoverable backup until the run/workflow-root migration commits, and resume safely
after interruption. These rules are required even though v0.3 starts with a
local file store; a future transactional store must preserve the same
observable contract.

Audit remains the tamper-evident evidence chain. The run/workflow stores are
operational state, not a replacement for audit.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Freeze and characterize v0.2.3 | v0.2.3 | legacy fixtures plus current timeout/resource/MCP/workflow gap tests are recorded |
| 2 | T02 Add project and policy schemas | T01 | strict route, workflow, service, handoff, child, active-time, and spend schemas pass |
| 3 | T03 Define deployment/invocation/run/workflow domain contracts | T02 | Go/JSON golden schemas, lifecycle hierarchy, controls, and enum tests pass |
| 4 | T04 Extend trigger/control/operator contracts | T03 | deployment/invocation/control/amendment APIs, generated-code drift, and CLI JSON parity pass |
| 5 | T05 Implement protected deployment/run/workflow stores | T03 | admission/idempotency/alias/control atomicity, permissions, race, transaction, and crash tests pass |
| 6 | T06 Wire compatibility-safe daemon/CLI skeleton | T04, T05 | legacy run unchanged; deployment/control requests represented but unfinished execution fails closed |
| 7 | T07 Block gate and adversary review | T01–T06 | `make block26-gate` and adversary matrix pass |

Do not start B27 until T07 is recorded complete.

## T01 — Freeze and characterize the v0.2.3 baseline

### Goal

Create executable evidence of behavior that later schema and state changes may
not break.

### Required work

1. Add immutable fixtures for:
   - A legacy project with singular OpenRouter LLM config.
   - A legacy direct-provider project.
   - A project with no LLM.
   - A v0.2.3 policy with token budget, rate limit, provider lock,
     guardrails, transformations, and observability.
   - A v0.2.3 installed bundle/manifest fixture.
2. Record expected:
   - Strict YAML parse result.
   - Canonical policy digest.
   - Agent lock representation.
   - Pack/validate result.
   - Invoke payload shape.
   - CLI `run --json`, `summarize --json`, `timeline --json`, and
     `next-action --json` shapes.
3. Add a legacy SDK test proving:
   - `agent.llm(prompt)` works.
   - `agent.llm(prompt, model=...)` still forwards the override on the legacy
     path.
   - No progress/checkpoint call is required.
4. Add an upgrade test that reads the v0.2.3 fixture without rewriting it.
5. Add characterization tests that prove and document—not yet fix—the current
   boundaries:
   - Two sequential `agent.llm()` calls work inside one invocation.
   - A normal daemon invocation crossing approximately 60 seconds fails at the
     inner blocking request.
   - The 2-minute daemon, 5-minute harness, 120-second harness budget, and
     120-second model-client limits conflict.
   - The fixed 30-second CPU rlimit and zero child-process rlimit are present.
   - Trigger calls create independent runs with no workflow/handoff identity.
   - No immutable deployment/alias, durable invocation idempotency,
     per-deployment concurrency, pause/resume/restart, or limit-amendment
     contract exists.
   - Production does not install the `mcpmanager.Router`; a managed MCP call
     cannot be claimed from the synthetic fallback.
6. Record exact source locations and observed failure envelopes so B30/B33
   cannot declare success by changing only tests or comments.

### Likely files

- `test/compat/v0.2.3/**`
- `internal/pack/*_test.go`
- `internal/policy/*_test.go`
- `internal/daemon/control_handlers_*_test.go`
- `internal/cli/operator_json_test.go`
- `python/agentpaas_sdk/tests/**`

### Tests to write first

- Fixture digest/golden tests.
- Legacy policy canonicalization test.
- Legacy `buildInvokePayload` shape test.
- Legacy CLI JSON required-fields test.
- Old installed bundle read/run-resolution test.
- Focused long-duration characterization test with an injected clock or
  bounded 61-second manual run.
- MCP production-wiring negative and synthetic-fallback detection test.
- Deployment/alias/control/amendment API absence characterization fixtures.

### Exit gate

All fixtures pass on unmodified v0.2.3 behavior before T02 begins. The handoff
record includes fixture hashes so later “updating the golden” cannot silently
erase a compatibility regression.

## T02 — Add strict durable-run, route, service, and workflow schemas

### Goal

Represent logical routes and signed execution limits without changing the
legacy schema.

### Required work

1. Extend `pack.LLMConfig` with `Route`.
2. Reject:
   - `route` combined with `provider`, `model`, or `credential`.
   - Empty or unsafe route IDs.
   - Exact model override configuration on a routed project.
3. Extend `policy.Policy` with:
   - `RoutedRun *RoutedRunPolicy`
   - `ModelRoutes map[string]ModelRoute`
   - `LLMBudget.MaxCostUSD`
   Parse `max_cost_usd` through an exact decimal type (up to nano-USD
   precision), preserve a canonical decimal representation, and reject
   exponent notation, negatives, NaN/Inf, excess precision, and overflow. Do
   not introduce a `float64` enforcement field that B38 must later replace.
   Put the scalar/parser/formatter and checked integer primitives in a small
   dependency-neutral package such as `internal/money`; B36 and B38 must reuse
   it.
4. Add explicit policy-schema validation:
   - Existing policies remain `version: "1.0"`.
   - Routed fields require `version: "1.1"`.
   - Both known versions parse strictly.
   - Unknown versions and v1.0/routed-field mixtures fail closed.
   - Every routed policy explicitly supplies `llm_budget.max_cost_usd`.
     Zero is a valid hard limit for a route restricted to zero-marginal
     candidates; absence is not an unlimited routed budget.
5. Define the nested route/candidate/requirement structs from the locked
   schema above.
6. Add strict validation:
   - The route named in `agent.yaml` exists.
   - Exactly one v0.3 executable route exists and it is the named route.
   - Candidate count is 2–64.
   - Candidate IDs are unique and safe.
   - At least one primary and one recovery candidate exist when automatic
     recovery is enabled.
   - `local-first` primary candidates are local and recovery candidates are
     cloud.
   - `cloud-cost-first` candidates are cloud and recovery capability is not
     below the route minimum.
   - Cloud candidates are rejected when `cloud_transfer: denied`.
   - Authenticated candidates reference a declared credential.
   - OpenRouter candidates have a bounded, duplicate-free
     `upstream_providers` allowlist; direct/local candidates do not.
   - `auth: none` is accepted only for local custom endpoints.
   - Custom/private endpoints require explicit private-network egress policy.
   - Limits satisfy the relationships in this block.
7. Include the new fields in canonicalization, policy digest, lock signing,
   bundle inspection, policy delta, and install consent output.
8. Keep absent routed fields semantically identical to v0.2.3.
9. Add the strict optional `workflow.yaml` envelope used by B33–B35:
   - `workflow_id` is generated at runtime, never configured.
   - `kind`: `standalone`, `pipeline`, or `parent_child`. `standalone` is used
     only when one worker needs workflow-scoped B33 services; ordinary
     one-worker projects still need no `workflow.yaml`.
   - The coordination kinds are mutually exclusive in v0.3: a `pipeline`
     stage cannot spawn children, and a `parent_child` workflow cannot also
     declare pipeline stages. B33 service bindings may be used by any kind.
   - Ordered pipeline stages reference exact installed package name/version
     and immutable signed bundle/package digest, never a raw image or command.
   - MCP service bindings reference an exact AgentPaaS service package and
     allowed logical service ID.
   - Parent child allowlists name exact child identities, maximum fan-out,
     maximum concurrency, and leaf-only depth.
   - Workflow maximum active duration, handoff byte limit, artifact limit, active
     container limit, and aggregate token/LLM-spend fields are explicit.
   - Handoff/context/artifact declarations use only
     `public|internal|confidential|restricted` in the locked order; adjacent
     stages may preserve or raise classification and may never declassify.
   - Unknown fields, duplicate IDs, cycles, more than the v0.3 stage/fan-out
     bounds, and authority expansion fail closed.
10. Add `agent.yaml` `kind: worker|mcp_service` and a strict MCP service
    declaration for service packages. Legacy absence means `worker`.
11. Include workflow/service/child authority in signing, bundle inspection,
    consent, policy delta, install, and provenance output.

### Likely files

- `internal/pack/detect.go`
- `internal/pack/lock.go`
- `internal/policy/schema.go`
- `internal/policy/validate.go`
- `internal/policy/canonical.go`
- `internal/policy/digest.go`
- `internal/policy/compiler.go`
- `internal/money/**`
- `internal/install/**`
- bundle/inspection policy renderers and tests

### Tests to write first

- Valid local-first and cloud-cost-first parse fixtures.
- Every mutual-exclusion and unknown-enum negative.
- v1.0 legacy, v1.1 routed, v1.0/routed mismatch, and unknown-version tests.
- Missing primary/recovery candidate negatives.
- Extra route, too few candidates, and too many candidates.
- Missing routed `max_cost_usd`, explicit zero limit, and malformed decimal
  cases.
- Cloud-transfer denial negative.
- Duplicate candidate ID and unsafe route ID negatives.
- Credential reference and private endpoint negatives.
- Missing/duplicate/unsafe aggregator upstream, unreviewed provider-family
  expansion, and upstream fields on direct/local candidates.
- Canonicalization order invariance.
- Digest changes for authority/limit expansion.
- Legacy digest fixture remains unchanged.
- Valid three-stage pipeline and one-parent/three-leaf-child fixtures.
- Pipeline cycle/duplicate stage/unsafe identity/too-many-stage negatives.
- MCP service/client binding mismatch and undeclared tool negatives.
- Parent arbitrary-image, recursive child, fan-out/concurrency expansion, and
  workflow-budget expansion negatives.

### Exit gate

Valid route policies round-trip deterministically, every invalid policy fails
before packing, the consent card shows the route and candidate authority, and
all legacy fixtures remain byte-for-byte stable where promised.

## T03 — Define durable deployment, invocation, run, and workflow domain contracts

### Goal

Create one internal model used by daemon, harness, CLI, operator responses,
audit renderers, and tests.

### Required work

Create a focused package, suggested `internal/routedrun/`, containing:

1. Stable IDs:
   - `DeploymentID`
   - `InvocationID`
   - `ControlRequestID`
   - `LimitAmendmentID`
   - `WorkflowID`
   - `NodeID`
   - `ServiceID`
   - `HandoffID`
   - `ChildBatchID`
   - `ChildResultID`
   - `ArtifactID`
   - `RunID`
   - `AttemptID`
   - `LeaseID`
   - `CheckpointID`
   - `ModelCallID`
2. Run statuses:
   - `PENDING`
   - `RUNNING`
   - `PAUSE_REQUESTED`
   - `PAUSED`
   - `NEEDS_REPLAN`
   - `SUCCEEDED`
   - `FAILED`
   - `CANCELLED`
   - `BUDGET_EXCEEDED`
   - `EXPIRED`
3. Workflow/node/service/batch statuses:
   - Workflow: `PENDING`, `RUNNING`, `PAUSE_REQUESTED`, `PAUSED`,
     `NEEDS_REPLAN`, `SUCCEEDED`, `FAILED`, `CANCELLED`, `EXPIRED`,
     `BUDGET_EXCEEDED`.
   - Node: `PENDING`, `READY`, `LAUNCHING`, `RUNNING`, `PAUSE_REQUESTED`,
     `PAUSED`, `NEEDS_REPLAN`, `SUCCEEDED`, `FAILED`, `CANCELLED`, `SKIPPED`.
   - Service: `DECLARED`, `STARTING`, `READY`, `UNHEALTHY`, `FENCED`,
     `STOPPING`, `STOPPED`, `FAILED`.
   - Child batch: `INTENT`, `ALLOCATED`, `RUNNING`, `PAUSE_REQUESTED`,
     `PAUSED`, `JOINING`, `STOPPING`, `SUCCEEDED`, `FAILED`, `CANCELLED`.
   Unknown or cross-object transitions fail closed.
4. Attempt statuses:
   - `PENDING`
   - `RUNNING`
   - `NEEDS_REPLAN`
   - `SUCCEEDED`
   - `FAILED`
   - `FENCED`
   - `CANCELLED`
5. Failure reasons:
   - `MODEL_TIMEOUT`
   - `MODEL_CONNECTION_FAILED`
   - `MODEL_RATE_LIMITED`
   - `MODEL_SERVICE_ERROR`
   - `MODEL_CONTEXT_LIMIT`
   - `MODEL_OUTPUT_LIMIT`
   - `MODEL_MALFORMED_JSON`
   - `MODEL_IDENTITY_MISMATCH`
   - `MODEL_AUTH_UNAVAILABLE`
   - `MODEL_QUOTA_EXHAUSTED`
   - `NO_ELIGIBLE_TARGET`
   - `ATTEMPT_TIME_EXHAUSTED`
   - `STALL_TIMEOUT`
   - `NO_PROGRESS_GUARDRAIL`
   - `REPEATED_ACTION_GUARDRAIL`
   - `LLM_BUDGET_EXHAUSTED`
   - `ACTIVE_TIME_EXHAUSTED`
   - `POLICY_DENIED`
   - `EXTERNAL_DEPENDENCY_FAILED`
   - `AGENT_EXCEPTION`
   - `CHECKPOINT_UNAVAILABLE`
   - `DAEMON_RESTARTED`
   - `USER_CANCELLED`
   - `MCP_SERVICE_UNAVAILABLE`
   - `MCP_PROTOCOL_ERROR`
   - `HANDOFF_MISSING`
   - `HANDOFF_INVALID`
   - `CHILD_SPAWN_DENIED`
   - `CHILD_BATCH_FAILED`
   - `WORKFLOW_RESOURCE_EXHAUSTED`
   - `PAUSE_BOUNDARY_UNAVAILABLE`
6. Failure scope:
   - `model_call`
   - `worker`
   - `budget`
   - `policy`
   - `credential`
   - `external`
   - `platform`
   - `workflow`
   - `mcp_service`
   - `handoff`
   - `child_batch`
7. Recovery disposition:
   - `not_needed`
   - `auto_recovered`
   - `needs_replan`
   - `terminal`
8. Data classifications with the strict least-to-most-restrictive order
   `public < internal < confidential < restricted`. Unknown values fail closed;
   v0.3 has no declassification transition.
9. Immutable artifact/handoff contracts:
   - `ArtifactRef` contains artifact ID, owning workflow/node/run/attempt,
     immutable logical reference, digest, byte size, media type, schema where
     declared, and classification. It never exposes a host/container path.
   - A handoff's classification is at least the most restrictive of its
     producer declaration, context, and referenced artifacts. A downstream
     stage may preserve or raise it, never lower it.
   - Artifact and handoff bytes remain retained as run/workflow history in
     v0.3; no automated retention or destructive purge state is invented.
10. Structs for:
   - Immutable deployment record, status, exact artifact identity, receiver
     concurrency limit, and mutable alias record.
   - Invocation request/receipt, requested reference, resolved exact and nested
     identities, canonical invocation-intent digest, caller, initial ceilings,
     and durable idempotency record.
   - Desired-state/control request and terminal/new-run restart provenance.
   - Active-time ledger with consumed/running-segment/frozen-state fields.
   - Limit amendment, administrative scope, before/after ceilings, spend
     reservation snapshot, actor/reason, and authority generation.
   - Workflow record and immutable workflow-policy snapshot refs.
   - Pipeline node/stage and state transition.
   - MCP service binding, service lease, and health summary.
   - Handoff envelope with structured context and artifact references.
   - Parent/child batch, spawn request, join policy, and child result.
   - Run record and immutable policy/catalog snapshot refs.
   - Attempt record and lease.
   - Model requirements.
   - Candidate and route decision.
   - Workflow-scoped target/credential/provider availability record with
     typed cause, source node/attempt, generation, and scope.
   - Normalized usage and cost.
   - Progress and checkpoint summary.
   - Artifact reference.
   - Time and LLM budget summary.
   - Attempt report.
11. State-transition validation. Illegal transitions return typed errors and
   make no mutation.
12. Canonical JSON serialization with bounded strings and arrays.

13. Deployment statuses `ACTIVE|INACTIVE`, typed admission outcomes
    `ACCEPTED|IDEMPOTENT_REPLAY|ALREADY_RUNNING|IDEMPOTENCY_CONFLICT`, control
    commands `CANCEL|PAUSE|RESUME|RESTART|CONTINUE|AMEND_LIMITS`, and explicit
    authority scopes. Unknown values fail closed.

The attempt report must include:

```text
schema_version
run_id
attempt_id
status
reason
failure_scope
recovery_disposition
resume_capability
progress
checkpoint
artifacts
time
llm_budget
route_decisions
recommended_actions
evidence_refs
```

Workflow reports additionally include:

```text
workflow_id
workflow_kind
requested_deployment_ref
resolved_deployment_id
resolved_deployment_version
resolved_deployment_digest
invocation_id
nodes
active_node_ids
service_bindings
handoffs
child_batches
aggregate_limits
aggregate_usage
active_time
limit_amendments
control_history
terminal_reason
```

`resume_capability` is one of `safe_checkpoint`, `restart_only`, or `none`.
v0.3 does not claim arbitrary process resume.

### Tests to write first

- Every valid and invalid state transition.
- Enum completeness and JSON value stability.
- Oversized/unicode/control-character field rejection or sanitization.
- Deterministic canonical JSON.
- Secret sentinel rejection/redaction policy.
- Attempt report minimum required fields.
- Fuzz parse/unmarshal tests.
- Workflow/run/attempt hierarchy and referential-integrity tests.
- Pipeline transition, handoff single-commit, child spawn/join, and service
  registration transition matrices.
- Deployment alias/version pinning and active/inactive transition matrix.
- Invocation idempotency canonical-equivalent/different-input, duplicate JSON
  key rejection, changed initial-ceiling/creation-option conflicts, caller
  isolation, and top-level concurrency-admission transition matrix.
- Artifact ownership/schema/classification order, no-declassification, and
  host-path non-representation tests.
- Cancel/pause/resume/restart desired/observed state matrix, including
  cancellation precedence.
- Active-time freeze/unfreeze arithmetic and append-only limit-amendment
  generation/race fixtures.

### Exit gate

One package owns these definitions; daemon, harness, and operator packages do
not create competing enum copies.

## T04 — Extend protobuf and operator contracts additively

### Goal

Expose Routed Run state without breaking existing clients.

### Required work

1. Add `RUN_STATUS_PAUSE_REQUESTED`, `RUN_STATUS_PAUSED`,
   `RUN_STATUS_NEEDS_REPLAN`, `RUN_STATUS_BUDGET_EXCEEDED`, and
   `RUN_STATUS_EXPIRED` to the trigger/control API without renumbering existing
   values.
2. Add continuation fields to `RunRequest`:
   - `continue_run_id`
   - `recovery_action`
   - `requested_attempt_lease_ms`
3. Add to `RunResponse`:
   - `invocation_id`
   - `workflow_id`
   - optional `attempt_id` (absent in the immediate admission receipt until the
     asynchronous scheduler claim creates the attempt; present in later status)
   - `status`
   - requested and resolved deployment references.
4. Add versioned messages for attempt report, progress, checkpoint, artifact,
   time, LLM budget, and route decision.
5. Add the attempt report to `SummarizeRunResponse` and the latest relevant
   reason/action to `ExplainFailureResponse` and `NextActionResponse`.
6. Bump the additive operator schema from `1.0.0` to `1.1.0`.
7. Add the new next actions:
   - `more_time`
   - `capability_up`
   - `larger_context`
   - `split_task`
   - `stop`
8. Preserve every existing field number and JSON field.
9. Update Hermes contract fixtures only for additive parity; no Routed Run
   conversational behavior is added until B40.
10. Add versioned internal/control messages for:
    - Create/get/list deployment from an exact installed artifact.
    - Create/get/list/compare-and-swap deployment alias.
    - Deactivate deployment without cancelling active runs.
    - Invoke deployment with bounded input and idempotency key.
    - Create/get/cancel workflow.
    - Pause/resume workflow desired state.
    - Restart as a new invocation from the source exact deployment/input or an
      explicitly requested current alias.
    - Amend absolute active-time/current-attempt/spend ceilings with expected
      authority generation, reason, idempotency key, and actor scope.
    - Workflow node and stage status.
    - Service binding/readiness.
    - Handoff metadata.
    - Parent/child batch and child result.
    These fields are representational only in B26. B33–B35 activate behavior.
11. Ensure a trigger-created standalone run receives an additive workflow ID
    without changing existing CLI text or legacy semantics.
12. Define typed responses for `IDEMPOTENT_REPLAY`, `ALREADY_RUNNING`,
    `IDEMPOTENCY_CONFLICT`, `DEPLOYMENT_INACTIVE`, `RUN_TERMINAL`,
    `UNSAFE_PAUSE_BOUNDARY`, `CONCURRENCY_UNAVAILABLE`, and
    `LIMIT_AMENDMENT_DENIED`; callers must not infer these from error strings.
13. Separate invoke authority from administrative control. Reserve explicit
    `runs:control` and `runs:amend_limits` scopes in the authenticated control
    contract; do not expose the latter to Python/SDK or an ordinary trigger
    credential.
14. Make absolute ceilings exact: duration milliseconds use checked integers
    and LLM spend uses B26's decimal representation, never floats. Responses
    include original/current/consumed/reserved values and authority generation.
15. Keep B26 behavior representational: deployment invocation, control, and
    amendment requests that depend on B30/B39 return typed not-enabled errors
    without partial state or resources.

### Likely files

- `api/control/v1/control.proto`
- `api/trigger/v1/trigger.proto`
- generated protobuf files
- `internal/operator/schema.go`
- `internal/operator/categories.go`
- `internal/cli/operator_json_test.go`
- `integrations/hermes-plugin/contracts.py`
- contract-parity tests

### Tests to write first

- Proto field-number golden test.
- Old JSON fixture unmarshals into new structs.
- New response includes required attempt-report fields.
- Operator enum parity between Go and Python.
- Unknown action/failure values fail closed where control decisions are made.
- `make proto` produces no drift after generated files are committed.
- Old clients ignore workflow IDs; new clients can inspect hierarchy without
  inferring that pipeline/spawn execution is already enabled.
- Deployment exact/alias JSON/protobuf golden fixtures and field-number
  stability.
- Invoke idempotency and typed concurrency-error contract fixtures.
- Pause/resume/cancel/restart/amend request validation, missing-scope, changed
  idempotency-payload, terminal-run, and numeric overflow negatives.

### Exit gate

Old clients can ignore the new fields; new clients can distinguish deployment,
invocation, running, pause-requested, paused, needs-replan, and terminal state
and can form only typed, explicitly authorized control requests.

## T05 — Implement the protected local run/workflow stores

### Goal

Persist enough operational state for immutable deployment resolution, direct
invocation admission, operator control, and multi-attempt/multi-container
workflow execution/reconciliation without using chat history or in-memory
maps as the source of truth.

### Required work

1. Create a `DeploymentStore` interface with:
   - `CreateDeployment`, `GetDeployment`, `ListDeployments`
   - `SetDeploymentStatus`
   - `CompareAndSwapAlias`, `ResolveAlias`, `ListAliases`
   - `AdmitInvocation(request, expected_deployment_generation)` as the one
     atomic idempotency lookup, canonical-intent comparison, alias/exact and
     nested-snapshot resolution, active-status check, top-level concurrency
     check, invocation record, topology-specific workflow/node/run identity
     creation, and first durable `READY` launch-intent transaction.
   - `GetInvocationByIdempotency`, `ListInvocations`
   `AdmitInvocation` returns an existing receipt for an exact replay, rejects
   changed intent, and never creates queued state on `ALREADY_RUNNING`. For a
   standalone deployment it preallocates one node/run and marks that node
   `READY`; for a pipeline it preallocates every fixed stage node/run, marks
   stage one `READY`, and leaves later stages `PENDING`; for a parent/child
   deployment it preallocates only the parent node/run because child identities
   are created later by the atomic B35 spawn-intent transition. Admission does
   not create an attempt, lease, container, or second generic orchestration
   run. A scheduler claim later creates the initial attempt/lease/job atomically
   with `READY -> LAUNCHING`.
2. Create a `RunStore` interface with:
   - `CreateRun`
   - `GetRun`
   - `UpdateRun`
   - `CreateAttempt`
   - `GetAttempt`
   - `UpdateAttempt`
   - `ListRuns`
   - `ListAttempts`
   - `AppendLedger`
   - `ReconcileInterrupted`
   Top-level runs are created inside `AdmitInvocation`; callers must not follow
   admission with another `CreateRun`. `CreateRun` is available only inside the
   atomic workflow transitions that create later dynamic service/child work or
   migration/legacy paths.
3. Create a `WorkflowStore` interface with:
   - `CreateWorkflow`, `GetWorkflow`, `UpdateWorkflow`, `ListWorkflows`
   - `CreateNode`, `GetNode`, `UpdateNode`, `ListNodes`
   - `RegisterService`, `UpdateService`, `ListServices`
   - `CommitHandoff`, `GetHandoff`, `ListHandoffs`
   - `CreateChildBatch`, `UpdateChildBatch`, `ListChildBatches`
   - `CommitChildResult`, `ListChildResults`
   - `RequestControl`, `GetDesiredState`, `AppendControlResult`
   - `AppendLimitAmendment(workflow_id, expected_authority_generation,
     amendment)`
   - `ApplyTransition(workflow_id, expected_generation, command)` for one
     atomic logical update spanning node/run result, handoff or child result,
     aggregate counters, and the next workflow state.
   Every mutating method uses compare-and-swap generation and idempotency
   identity where replay is possible. B34/B35 controllers use
   `ApplyTransition`; they may not compose a safety-critical transition from
   several independent file writes.
4. Implement local file stores under the locked layouts.
5. Generate IDs from cryptographically random bytes with fixed safe prefixes.
6. Treat lease IDs as opaque fencing tokens. Never accept caller-selected
   values.
7. Use compare-and-swap generation numbers to prevent lost updates.
8. Use atomic durable writes and recover safely from an orphaned temp file.
9. Enforce permissions on creation and on open; fail closed on unsafe modes.
10. Cap file sizes, record counts, string sizes, and ledger line sizes.
11. Never follow symlinks anywhere in deployment, run, or workflow trees.
12. Provide fake in-memory stores and a fake clock for later deterministic
    tests.
13. Put a schema version on every persisted state and ledger envelope. Add one
    ordered migration registry that:
    - Recognizes the current and explicitly supported older versions.
    - Fails closed on unknown/newer versions before mutating any record.
    - Applies each migration idempotently through staged, fsynced, atomic
      replacement.
    - Retains a recoverable backup until the complete run-root migration is
      committed.
    - Can resume or roll back safely after interruption at any write boundary.
14. `ReconcileInterrupted` always revokes the interrupted lease and records
    `DAEMON_RESTARTED`. In B26, with no authenticated B27 checkpoint contract
    yet, the attempt becomes terminal `FAILED`. B30 may later derive
    `NEEDS_REPLAN` only from a verified safe checkpoint plus remaining
    envelope; B39 exposes that disposition through the control API. Workflow reconciliation
    never guesses a stage handoff or duplicates a child spawn.
15. Implement `ApplyTransition` as one durable journal/WAL commit plus
    idempotent materialization (or an equivalently atomic local mechanism).
    Crash injection before and after journal fsync, commit marker, and every
    materialized write must recover to the pre-transition or committed state,
    never a mixture.
16. Make pause/control/amendment transitions part of the same workflow
    generation discipline. In particular:
    - `PAUSE_REQUESTED` commits before the scheduler may launch more work.
    - `PAUSED` or `NEEDS_REPLAN` commits only with no live workflow-owned
      worker/service container or capability and no active/in-flight LLM
      reservation; retained unreconciled exposure is permitted and authorizes
      no work.
    - Resume admission and concurrency acquisition commit atomically or leave
      the workflow paused.
    - Limit amendment and terminal active-time/spend exhaustion race on one
      authority/state generation; exactly one wins.
    - Cancel wins over pause/resume/continuation and is idempotent.
17. Persist active-time accounting as consumed duration plus at most one
    workflow running-segment start. Reconciliation closes an interrupted
    segment conservatively, never sums overlapping node intervals, and never
    charges `PAUSED` or `NEEDS_REPLAN` wall time.
18. Publish one parameterized B26 admission-conformance suite that B30–B35 must
    run against standalone, MCP-client, pipeline, and parent/child deployments.
    It covers exact/alias invocation, caller/key replay, changed ref/input/
    initial-ceiling/creation-option conflict, duplicate-key rejection, caller
    isolation, alias movement after acceptance, inactive future admission,
    default-one overlap with no queue, configured-safe concurrency, paused-slot
    release, and atomic resume reacquisition.

### Likely files

- `internal/routedrun/store.go`
- `internal/routedrun/localstore.go`
- `internal/routedrun/memorystore.go`
- `internal/routedrun/*_test.go`
- `internal/home/**` if a path helper is needed

### Tests to write first

- Create/read/update/list happy paths.
- Concurrent compare-and-swap race.
- Partial write and simulated crash recovery.
- Permission mutation to group/world readable.
- Symlink for every directory/file boundary.
- Traversal/control-character/massive ID attempts.
- Ledger truncation/malformed tail behavior.
- Current, supported-old, and unknown-newer state schema handling.
- Repeated migration and interruption at every durable write boundary.
- Restart reconciliation idempotency.
- Single-commit handoff and duplicate child-result idempotency.
- Alias compare-and-swap/promotion/rollback, exact-version pinning, inactive
  target, and alias-moved-after-idempotent-invocation tests.
- Atomic invocation same-key canonical replay, reordered-object equivalence,
  changed-ref/input/initial-ceiling/creation-option conflict, duplicate-key
  rejection, caller isolation, max-concurrency race, internal-node non-counting,
  paused-slot release, and resume-slot reacquisition.
- Crash/replay between admission commit, `READY` claim, attempt/lease creation,
  and first resource launch; no duplicate generic or topology run may appear.
- Parameterized admission-conformance fixtures for all four v0.3 topologies;
  B26 may use fake topology controllers while B30–B35 activate each real one.
- Control request replay/race matrix for cancel/pause/resume/restart.
- Limit amendment same/different-payload idempotency, increase-only numeric
  checks, scope denial, amendment-versus-exhaustion race, and crash recovery.
- Active-time segment crash accounting and frozen-state clock-jump tests.
- Workflow/run/node reference corruption and partial multi-record update.
- Atomic stage-success/handoff/next-ready and child-result/batch-terminal
  transition recovery at every journal/materialization boundary.
- Secret sentinel absent from errors and logs.

### Exit gate

`go test -race` passes under concurrent alias/invocation/control/amendment and
workflow updates; interrupted writes or migrations cannot produce false
admission, false success, or a broadened ceiling; unknown newer state is not
mutated, and every file has verified mode.

## T06 — Wire a compatibility-safe daemon and CLI skeleton

### Goal

Use the new contracts and store without claiming unfinished routing behavior.

### Required work

1. Initialize the `DeploymentStore`, `RunStore`, and `WorkflowStore` in the
   daemon.
2. Persist legacy runs as one run/one attempt while preserving their existing
   execution path.
3. Return the additive `attempt_id` and status fields.
4. Read persisted state for summarize/list/status when available; retain
   legacy audit fallback for old runs.
5. Parse continuation/control/amendment request fields but return a typed
   `FAILED_PRECONDITION` such as `routed_run_continuation_not_enabled` until
   B30/B39 implement their behavior. Validation, authentication, and typed
   not-enabled errors must occur without partial mutation.
6. For a routed project, validate and persist its signed route-policy
   reference and placeholder catalog-snapshot fields, then fail closed with a
   clear experimental/not-yet-enabled error before Docker resources are
   created. B36 resolves and persists the compiled route/catalog snapshot. Do
   not silently run the first policy candidate as a legacy agent.
7. Add compatibility-safe CLI/API skeletons for:
   - `agentpaas deploy <exact-installed-ref> [--alias <name>]
     [--max-concurrent-runs <n>]`
   - deployment list/inspect, alias set/promote/rollback, and deactivate.
   - `agentpaas run <deployment-ref> --input <json-or-file>
     [--idempotency-key <key>]`; the CLI generates and prints a key when
     omitted, while the API requires one.
   - `agentpaas run cancel|pause|resume|restart <run-id>`.
   - `agentpaas run extend <run-id> --max-active-time <duration>
     [--max-llm-spend-usd <decimal>] [--extend-current-attempt]
     --reason <text> --idempotency-key <key>`.
8. Add continuation flags to the control path:
   - `--continue <run-id>`
   - `--action <more_time|capability_up|larger_context>`
   - `--attempt-lease <duration>`
9. Do not add a separate public `route` or `recover` command.
10. Preserve current `agentpaas run <target>` output and behavior for legacy
   projects.
11. Parse and persist workflow/service/child definitions for inspect/consent,
    but fail closed before resource creation with typed not-enabled errors:
    - `agentpaas_mcp_service_not_enabled` until B33.
    - `agentpaas_pipeline_not_enabled` until B34.
    - `agentpaas_child_spawn_not_enabled` until B35.
12. Never route an MCP request through the current synthetic harness fallback
    for an AgentPaaS service binding.
13. In B26, exact deployment creation, inspect, alias mutation, and
    deactivation may work because they are state-only. Durable routed
    invocation and runtime controls must still fail closed until B30/B39. Do
    not mark a deployment invocation accepted unless its atomic admission and
    runnable foundation are enabled together.

### Tests to write first

- Legacy run still creates resources and executes.
- Routed project fails before resource creation with the typed not-enabled
  error.
- Continuation request fails without mutation.
- Existing CLI output fixtures stay valid.
- Additive JSON fields are present.
- Persisted list/summarize survives daemon restart in a test home.
- Workflow definitions inspect deterministically but cannot start early.
- Each not-enabled path creates no container/network and makes no synthetic
  MCP or handoff result.
- Immutable deploy/alias/deactivate/list state tests, including alias CAS and
  retained history.
- CLI-generated idempotency key output and API missing-key negative.
- Invoke overlap/control/amendment not-enabled paths create no accepted job,
  amendment, resource, or false audit success.

### Exit gate

The new state foundation is exercised in production code while no user can
mistake B26 for completed Routed Run support.

## T07 — Block gate and adversary review

### Required `make block26-gate`

Add a cumulative target that runs:

```text
make proto
go build ./...
go test ./internal/routedrun/... -count=1 -race
go test ./internal/policy/... ./internal/pack/... -count=1 -race
go test ./internal/operator/... ./internal/daemon/... ./internal/cli/... -count=1 -race
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

Also run the v0.2.3 compatibility fixture suite separately and print its
fixture hashes.

### Required adversary matrix

- Route/provider mutual-exclusion bypass.
- Unknown YAML fields and enum smuggling.
- Candidate ID collision and Unicode confusable IDs.
- Cloud target under denied cloud transfer.
- Undeclared credential and unauthenticated cloud candidate.
- Private endpoint without explicit private authority.
- Policy canonicalization reordering attack.
- Proto/JSON control-field injection.
- Illegal state transition.
- Lost-update race.
- Unknown-newer state, repeated migration, and interrupted migration.
- Symlink/path traversal and unsafe permissions.
- Ledger oversized line and malformed tail.
- Continuation before implementation.
- Alias retarget between invoke retry and resolution; inactive/deleted target;
  forged exact digest.
- Same caller/key with changed input/ref/initial ceiling/creation option,
  cross-caller key isolation, internal-node double-counting, and concurrent
  default-one top-level admission race.
- Ordinary invoke token, worker, model output, or forged Hermes confirmation
  attempting cancel/pause/resume/restart or `runs:amend_limits`.
- Negative/decreasing/overflow/float spend amendment, duplicate key with
  changed ceilings, and amendment-versus-terminal race.
- `PAUSED`/`NEEDS_REPLAN` marked complete with an active lease, container,
  service, capability, reservation, or running active-time segment; parallel
  nodes multiplying elapsed workflow active time.
- Secret sentinel in every error/evidence path.

### Block success gate

B26 is complete only when:

1. `make block26-gate` passes.
2. Every v0.2.3 compatibility fixture passes without unauthorized golden
   updates.
3. New schemas are signed, canonical, strict, and visible in bundle consent.
4. API changes are additive and operator schema is `1.1.0`.
5. Deployment, invocation, run, and workflow stores pass race, idempotency,
   admission, control/amendment atomicity, migration, restart, path, and
   permission tests.
6. Routed execution still fails closed as not enabled.
7. No fallback, savings, optimization, or correctness claim is added to public
   docs.

## Handoff record required after every task

Append to this file:

- Task ID and completion date.
- Decisions or deviations from this binding spec.
- Files and schemas changed.
- Compatibility fixtures and hashes.
- Tests written before implementation.
- Exact commands and PASS output.
- Adversary attempts and result.
- Migration/rollback implications.
- Unresolved risks.
- Next task now unblocked.

A skipped required test leaves the task open.

## Pitfalls

- Do not implement selection in the policy validator. B36 owns catalog
  resolution and selection.
- Do not duplicate enums across packages.
- Do not reinterpret old singular LLM config as a routed project.
- Do not silently accept route/provider combinations for convenience.
- Do not use audit JSONL as mutable operational state.
- Do not expose lease IDs as user-selectable values.
- Do not let route candidate ordering affect digests nondeterministically.
- Do not publish placeholder model names or provisional timeout values.
- Do not resolve an alias again after invocation acceptance or restart against
  a newer alias unless the operator explicitly requested current resolution.
- Do not count paused/needs-replan wall time as active time, release a paused
  run with live capabilities, or reset accumulated spend on resume.
- Do not let an ordinary invocation credential or worker-facing SDK reach the
  administrative amendment path.
- Do not implement a hidden invocation queue behind `ALREADY_RUNNING`.
