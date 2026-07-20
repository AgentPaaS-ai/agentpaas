# Block 40 — Hermes Authoring, Packaging, Deployment, and Operations Skills/UX

**Status:** EXECUTION-READY SPEC — annotated per Fix 5 (2026-07-19)
**Date:** 2026-07-18
**Fix 5 note:** cooperative pause/resume and limit amendments are deferred
to a post-v0.5 minor release. In this spec, the pause/resume/amendment UX
conversations (T04 items 7-8, recommendation rows, T05 amendment items, and
transcript matrix items 19-25 where they exercise pause/resume/amendment)
become typed `feature_not_enabled` handling: Hermes explains the control is
not yet available and never implies the run was paused or amended. Cancel,
restart, and one failure continuation UX ship in full. Deferred UX
conversations remain below as the approved design for the follow-on
release.
**Target release:** `v0.5.0-rc.1` GitHub prerelease; stable v0.5 closes at B41
**Depends on:** B34, B35, and B39 complete; `make block39-gate` green
**Must complete before:** B41
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65

## Outcome

B40 makes the cumulative v0.5 runtime usable through the supported AgentPaaS build
experience:

> Hermes + AgentPaaS skills + AgentPaaS SDK for authoring, testing, packaging,
> and deployment;
> AgentPaaS CLI/API + AgentPaaS runtime for independent execution.

At block completion, a practitioner can use Hermes to:

- Choose one supported execution shape: standalone worker, worker plus governed
  MCP service, linear pipeline, or one-level parent/leaf tree.
- Inspect/query the private B31 catalog, submit signed registration proposals,
  promote approved component versions, and choose bounded worker/verifier/
  testing capabilities without exposing runtime endpoints.
- Declare the B29 runtime profile, streaming/guardrail mode, interactive inbox,
  and activation class, with `on_demand` as the ordinary-worker default.
- Generate AgentPaaS SDK workers and strict workflow definitions.
- Configure approved model routes, credentials, network access, active-time and
  LLM-spend ceilings, and receiver-local concurrency.
- Validate and test the package, including progress/checkpoint behavior and
  pattern-specific contracts.
- Package and create an immutable AgentPaaS deployment version.
- Create or move an audited alias such as `production/customer-report`.
- Receive short CLI, API, cron, and Kubernetes invocation instructions.
- Inspect deployment, invocation, run, workflow, cost, route, and audit state.
- Request cancel, pause, resume, restart, one eligible failure continuation,
  alias rollback, or deactivation through the same authenticated AgentPaaS
  control plane.
- Propose a limit increase and, only after explicit user confirmation, submit a
  scoped append-only amendment.
- Explain completion, recovery, failure, and exact metered LLM spend without
  claiming semantic correctness or savings.
- Follow durable progress/output events by cursor, reconnect without creating a
  second run, and distinguish platform startup latency from provider latency.

The decisive release proof is that Hermes can be terminated after deployment
and the exact deployment can still be invoked through the AgentPaaS CLI/API.
External cron, Kubernetes, and other processes may trigger the same API. They
do not coordinate stages, relay MCP calls, spawn children, carry results, or
become runtime orchestrators.

A later Hermes session may attach to an existing run by ID, inspect structured
state and audit evidence, or submit an authorized control request. The
original authoring session is never session-owner of the deployed runtime.

B40 does not add Codex, Cursor, generic MCP operator, LangChain, or
bring-your-orchestrator adapters. It does not add an always-on Hermes process.

## Locked responsibility boundary

| Component | v0.5 responsibility |
|---|---|
| Hermes | Supported interactive authoring, testing, packaging, deployment, and optional operations client |
| AgentPaaS skills | Teach Hermes the approved SDK, workflow, deployment, invocation, inspection, and control contracts |
| AgentPaaS CLI/API | Durable deployment and control surface used equally by Hermes, a human operator, cron, Kubernetes, or another process |
| AgentPaaS runtime | Owns invocation admission, exact-version pinning, containers, MCP, pipeline progression, parent/leaf scheduling, data transfer, routing, active time, spend, fencing, and evidence |
| External caller | Supplies a trigger or authenticated control request; never becomes workflow coordinator |

The supported authoring path is Hermes. The supported runtime path is
AgentPaaS. A workflow authored elsewhere is not a v0.5 compatibility promise,
but direct invocation of a valid deployed AgentPaaS artifact is.

Deployment references follow B26:

```text
customer-report@1.2.0
production/customer-report -> customer-report@1.2.0
```

An accepted invocation records both the requested reference and resolved exact
version/digests. Alias changes affect future admissions only.

## UX principles

### One question at a time

Preserve the existing Hermes plugin tone:

- Ask one short question.
- Do not show YAML unless the user asks.
- Do not expose internal ports, capability tokens, or store keys.
- Do not dump the full model catalog.
- Explain each material decision in ordinary language.
- Present one final deployment card and one short proof command.

### Build, deploy, then run independently

The default successful conversation ends with:

```text
Deployed: customer-report@1.2.0
Alias: production/customer-report
Execution: AgentPaaS-managed
Start: agentpaas invoke production/customer-report --input request.json
Hermes required while running: no
```

The exact command syntax must match the implemented B26 CLI. The transcript
above describes intent and is not permission to invent a conflicting command.

Hermes may offer to run a test invocation, but deployment completion must not
be conflated with a Hermes-owned run.

### Pattern, not provider, is the first routing question

For each worker that uses a logical model route:

1. Ask whether local execution is desired.
2. Choose:
   - `local-first`, or
   - `cloud-cost-first`.
3. Confirm whether that node's work may be sent to approved cloud models.
4. Identify approved targets and credential labels available to the receiver.
5. Ask for the workflow LLM spend ceiling and maximum active duration, or
   offer only release-approved defaults.
6. Ask one concrete timing question for known long local tool/test work.
   Otherwise use the A2-approved attempt-lease recommendation.
7. Explain that an attempt lease can yield a safe operator decision inside the
   current active-time/spend envelope; it is not the workflow's lifetime.

Do not force local setup on cloud-only users. Do not claim that prompt text
alone reveals reliable task complexity.

### Exact model choice remains runtime-owned

Hermes configures an approved candidate pool and minimum capability. It never
pins an exact recovery target in a run-control request.

```text
Allowed control: capability_up
Denied control: retry on provider/model-name
```

An optional technical inspection view may show exact catalog entries and the
runtime's actual decision. Worker source and normal user prompts remain
provider-neutral.

### Automatic call recovery is not a Hermes decision

B37 may replay one eligible normalized model call on another approved target.
Hermes can report that event later, but it neither approves nor relays the
call.

### Whole-worker continuation is optional operator control

A run enters `NEEDS_REPLAN` only when the runtime has fenced the failed
attempt, retained a safe checkpoint, and enough current active time/spend
remain for one allowed continuation.

An authenticated operator, directly or through a Hermes session explicitly
asked to manage that run, may choose one runtime-allowed action:

- `more_time`
- `capability_up`
- `larger_context`
- `split_task`
- `stop`

Only the first three continue the same run, and only once. `split_task` is a
new authoring/deployment decision. `stop` maps to cancellation. No live Hermes
session is required for a run that is progressing normally.

### Current limits are hard and separately amendable

Hermes must distinguish continuation from limit amendment:

- Continuation uses already-approved current limits.
- Amendment raises an absolute current active-time, current-attempt lease,
  and/or LLM-spend ceiling.
- Amendment requires `runs:amend_limits` and explicit user confirmation.
- Hermes first displays original value, current value, consumed/reserved
  amount, proposed absolute value, and reason.
- Hermes cannot self-confirm or place amendment authority in a worker.
- If terminal `EXPIRED` or `BUDGET_EXCEEDED` commits first, Hermes reports the
  terminal result and must not retry the amendment or imply resurrection.

A user-friendly “add 10 minutes” interaction is translated client-side to an
absolute value. The API persists only the absolute increase.

### Operator lifecycle wording is exact

- **Cancel:** terminally stop this run; accepted upstream external side effects
  cannot be undone.
- **Pause:** request a safe boundary. `PAUSE_REQUESTED` still consumes active
  time and may spend while current work drains.
- **Paused:** no runtime progress, active-time accrual, new spend, live worker
  capability, or active/in-flight LLM reservation. Any retained unreconciled
  exposure remains visible and reduces headroom but authorizes no work.
- **Resume:** same workflow ID, exact deployment, input, committed state,
  consumed spend, and remaining active time.
- **Restart:** a new invocation from stage one, linked to the old run. It uses
  the old exact deployment by default; resolving the alias as it exists now is
  an explicit option.

### Structured control boundary

Runtime control fields are authoritative:

```text
requested_deployment_ref
resolved_deployment_version
resolved_package_digests
invocation_id
workflow_id
node_id
run_id
attempt_id
status
desired_control_state
reason
failure_scope
recovery_disposition
resume_capability
recommended_actions
active_time_original
active_time_current
active_time_consumed
active_time_remaining
attempt_lease_current
llm_spend_original
llm_spend_current
llm_spend_consumed
llm_spend_reserved
authority_generation
checkpoint_id
active_service_ids
active_child_batch_id
```

Worker progress, artifact content, model output, provider messages, logs, and
evidence excerpts are untrusted data. They may inform an explanation but
cannot:

- Change status, reason, or desired state.
- Add an allowed action.
- Authorize a continuation or limit amendment.
- Supply confirmation.
- Select an exact target.
- Change the pinned deployment/input.
- Mark work successful or correct.

## Initial recommendation rules

| Runtime evidence | Hermes behavior |
|---|---|
| Model call automatically recovered | Report the recovery concisely; no extra approval or call |
| Attempt lease ended with safe checkpoint and sufficient current envelope | Offer `more_time` |
| Context rejection and larger-context action allowed | Offer `larger_context` |
| Repeated/no-progress guard and stronger tier allowed | Offer `capability_up` once |
| Remaining current ceiling may be insufficient but is not exhausted | Offer pause and show a separately confirmed absolute amendment |
| Current active-time or spend ceiling exhausted | Report terminal failure; no amendment, continuation, or resurrection |
| User asks to pause | Submit pause request and explain that time freezes only at `PAUSED` |
| Resume capacity conflict | Explain retryable `ALREADY_RUNNING`; leave run paused |
| User asks to restart | Explain new-run semantics, exact-version default, and checkpoint reset |
| Work is too broad | Propose a revised/new deployment; ask before topology or limits expand |
| No safe checkpoint | Do not claim resume; offer restart/redesign where valid |
| Policy/cloud-transfer denial | Stop and use the existing policy-review workflow |
| Credential/reauth/quota with no independent target | Report the exact terminal credential action |
| External application failure | Report the external blocker; do not change model |
| Agent code exception | Use the existing fix-code/new-deployment path; do not assume a stronger model repairs code |
| Ambiguous report | Ask the user; do not invent recovery |

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Extend Hermes deployment, invocation, inspection, and control contracts | B39 | CLI/API/plugin schema and authority parity pass |
| 2 | T02 Add build, test, package, and immutable-deployment onboarding | T01, B27–B39 | all four supported shapes produce valid exact deployments |
| 3 | T03 Prove independent invocation and deployment lifecycle UX | T01, T02 | CLI/API/cron-style invokes work after Hermes exits |
| 4 | T04 Add attachable monitoring, recovery, lifecycle control, and amendment UX | T01, T03 | existing runs can be inspected/controlled by a later session |
| 5 | T05 Enforce approval, scope, and prompt-injection boundaries | T01–T04 | self-confirmation/authority-confusion negatives pass |
| 6 | T06 Update generated agent/workflow authoring patterns | T02, B27–B39 | generated packages use only approved SDK/runtime contracts |
| 7 | T07 Implement simple user-facing deployment and run results | T03–T06 | exact wording/cost/version fixtures pass |
| 8 | T08 Run deterministic Hermes transcripts and prepare B41 live matrix | T01–T07 | clean-profile author/package/deploy/exit/invoke/reattach proofs pass; live fixtures handed forward |
| 9 | T09 Block gate and adversary review | T01–T08 | `make block40-gate` passes |

## T01 — Extend Hermes deployment, invocation, inspection, and control contracts

### Goal

Expose B26/B39 durable APIs through thin Hermes tool adapters while preserving
the AgentPaaS CLI/API as the source of truth.

### Required work

1. Inventory every current plugin tool and CLI contract before adding a name.
   Extend an existing tool when its semantics remain clear; add a narrow tool
   only where deployment/control cannot be expressed safely.
2. Add typed deployment operations for:
   - Create exact immutable deployment from validated package/workflow
     snapshot.
   - List/show exact versions and requested/resolved digests.
   - Create or atomically move an alias.
   - Show alias history.
   - Roll back an alias by moving it to a prior exact version.
   - Deactivate an exact version.
   - Reject destructive purge in v0.5.
3. Extend `agentpaas_trigger_invoke`, or its B26-equivalent existing surface,
   to accept:
   - Requested exact/alias deployment reference.
   - Typed input/payload reference and digest.
   - Required durable idempotency key for API/scheduler calls.
   - Optional CLI-generated ad hoc key.
   - Optional narrower initial active-time, attempt-lease, and spend ceilings
     within the deployment's configured maxima. Omission uses the deployment
     values; invocation can never broaden them.
   Return asynchronously with invocation/workflow/run IDs, requested reference,
   and resolved exact version/digests.
4. Keep development `agentpaas_run` behavior compatible, but never use a local
   project path as the identity of a deployed production run.
5. Add typed control operations for:
   - Failure continuation with allowed action.
   - Cancel (the durable semantics behind existing `agentpaas_stop`).
   - Pause.
   - Resume.
   - Restart, including explicit `resolve_current_alias`.
   - Absolute increase-only limit amendment.
6. Require operation-specific authority:
   - Invoke credentials cannot perform administrative lifecycle operations.
   - Workers/model-route capabilities cannot invoke any operator control.
   - `runs:amend_limits` is distinct from ordinary run control.
7. Require expected authority generation, idempotency key, reason, and explicit
   confirmation evidence where the B39 contract requires them.
8. Extend status/summarize/timeline/explain/next-action and audit fixtures with
   deployment, invocation, control, amendment, workflow, service, handoff,
   batch, child, run, and attempt fields.
9. Separate trusted runtime control fields from untrusted evidence before the
   data reaches Hermes.
10. Remove any plugin assumption that a successful `agentpaas_run` makes the
    Hermes session the owner of the deployed run. Session-local tracking may
    remain only as a convenience index.
11. Keep operator schema compatibility additive and preserve every legacy tool
    invocation/response fixture.
12. Do not create a public “generic orchestrator” tool. Every adapter is a
    deployment, trigger, inspection, or control client over the same
    AgentPaaS API.

### Likely files

- `integrations/hermes-plugin/schemas.py`
- `integrations/hermes-plugin/tools.py`
- `integrations/hermes-plugin/contracts.py`
- `integrations/hermes-plugin/__init__.py`
- plugin contract/adversary tests
- CLI/operator contract tests

### Tests to write first

- Exact deployment, alias, promotion/rollback, deactivation schemas.
- Immutable deployment rejects in-place content/version mutation.
- Invocation returns requested and resolved exact references.
- Same idempotency key/request returns the same invocation; changed payload or
  reference conflicts.
- Exact model/provider/endpoint fields rejected from continuation.
- Invoke token denied cancel/pause/resume/restart/amendment.
- Ordinary control token denied `runs:amend_limits`.
- Absolute amendment requires explicit confirmation and generation.
- Duplicate control/amendment returns original result; changed replay
  conflicts.
- Untrusted evidence cannot overwrite any control field.
- Legacy operator/plugin fixtures remain valid.
- Session-local run ownership is not required to inspect/control an authorized
  run.
- Existing tool registration remains complete and unique.

### Exit gate

Hermes can call every required deployment and operations API through typed
thin adapters, but the same operation remains available through AgentPaaS
CLI/API with Hermes absent.

## T02 — Add build, test, package, and immutable-deployment onboarding

### Goal

Let a practitioner turn an ordinary request into one valid deployed
AgentPaaS artifact without needing to understand internal schemas.

### Required work

Update the AgentPaaS Hermes skills to perform this sequence:

1. Classify the requested runtime shape as exactly one of:
   - Standalone worker.
   - Worker plus B33 MCP service.
   - B34 linear pipeline.
   - B35 parent plus bounded leaf children.
   Explain the proposed shape in one sentence and obtain confirmation before
   introducing multi-agent execution.
2. Reject or simplify DAG branches, cycles, recursive children, grandchildren,
   dynamic undeclared workers, compensation semantics, or other post-v0.5
   shapes.
3. For each routed worker, choose `local-first` or `cloud-cost-first`, confirm
   cloud-transfer permission, and identify receiver-approved candidate targets.
4. Discover credential labels only. Never ask for or place key values in chat,
   generated files, tool arguments, checkpoints, or transcript fixtures.
5. Validate every target:
   - Catalogued cloud endpoint/model/capability/pricing.
   - Explicit administrator metadata for custom local OpenAI-compatible
     endpoints.
   - Credential label where required.
   - Network/egress and cloud-transfer authority.
   - Compatible context/capability floor.
6. Ask for one shared workflow LLM-spend ceiling, maximum active duration, and
   initial attempt lease. Use only A1/A2 defaults that have passed B41.
7. Ask whether concurrent invocations are safe. Default
   `max_concurrent_runs` to one; require explicit configuration and explanation
   before raising it.
8. For B33–B35, summarize stage/child/service/container count and
   artifact/handoff/result bounds before generation.
9. Generate only required files:
   - One `agent.yaml`, `policy.yaml`, and SDK implementation per worker/service
     package.
   - Strict `workflow.yaml` for MCP bindings, linear stage order, or parent
     child allowlist and aggregate limits.
   - B27 progress/checkpoint calls and pattern-specific B33/B34/B35 SDK calls.
10. Validate schemas and policy, run unit/contract tests, build/install each
    package, and execute one bounded development test. A workflow is not
    deployable if its checkpoint, handoff, spawn/join, MCP, route, budget, or
    concurrency contract fails.
11. Package immutable content with an exact semantic version and pin:
    - Package/image/lock digests.
    - Workflow snapshot digest.
    - Policy/route/catalog requirements.
    - Supported input schema.
    - Initial limits and concurrency.
12. Create the exact deployment through B26. Never overwrite an existing exact
    version.
13. Offer an alias. Moving an existing alias requires showing current target,
    proposed exact target, and that only future admissions change.
14. Present one concise deployment card:

```text
Deployed: customer-report@1.2.0
Alias: production/customer-report
Shape: three-stage pipeline in separate AgentPaaS containers
Route: approved economical cloud pool with higher-capability recovery
Cloud transfer: allowed
LLM spend ceiling: $2.00
Maximum active duration: 20 active minutes
Maximum concurrent runs: 1
```

The values above are fixtures, not public defaults.

15. End with direct invocation examples and explain that Hermes may now exit.
16. Preserve the simple legacy single-model project path, but label it as
    development execution rather than immutable deployment when applicable.

### Tests to write first

- Local-first and cloud-cost-first onboarding.
- User declines local.
- User denies cloud transfer.
- Missing credential terminal handoff.
- Uncatalogued cloud target rejected instead of accepting asserted pricing.
- Custom local endpoint requires explicit administrator metadata.
- No raw secret enters transcript or generated package.
- Standalone package validates and deploys.
- MCP service/client package compiles logical binding with no raw port.
- Three-stage pipeline fixes order and compatible handoff schemas.
- Parent/three-child package fixes allowlist, leaf-only rule, shared limits,
  all-required result rule, and concurrency.
- Unsupported DAG/recursive-child request is narrowed, not emitted.
- Duplicate exact version with changed content fails.
- Alias move shows future-only semantics.
- Default concurrency is one; unapproved increase is not emitted.
- Generated deployment card matches durable API state exactly.

### Exit gate

A clean Hermes profile can create, test, package, and deploy each supported
shape as an immutable AgentPaaS deployment without silently expanding
topology, routing, credentials, network, time, spend, or concurrency.

## T03 — Prove independent invocation and deployment lifecycle UX

### Goal

Make “build with Hermes; run independently with AgentPaaS” an executable
contract rather than marketing language.

### Required work

1. For every supported shape, finish deployment, persist evidence, then
   terminate the authoring Hermes process before invocation.
2. Invoke the deployment through:
   - AgentPaaS CLI.
   - Authenticated AgentPaaS API.
   - A scheduler-style request with a stable idempotency key.
   At least one proof must use an alias and one an exact version.
3. Provide copyable examples for external cron and Kubernetes jobs that call
   the AgentPaaS invocation API/CLI. These examples supply triggers only; they
   do not poll-and-relay workflow data.
4. Prove admission semantics:
   - Alias resolves exactly once.
   - Requested reference and exact version/digests are returned and audited.
   - Every stage/service/child package identity is exact in the admitted
     snapshot and is never re-resolved later.
   - Active alias movement cannot change a running or historical invocation.
   - Same key plus same request returns the same run.
   - Same key plus changed reference/input/initial ceiling or another
     execution/authority-bearing creation option conflicts.
5. Prove receiver-local concurrency:
   - Default one overlapping distinct invocation returns retryable
     `ALREADY_RUNNING`.
   - No hidden queue or later surprise start is created.
   - A `PAUSED` run does not consume a slot; resume must atomically reacquire
     it.
6. Prove lifecycle:
   - Promotion moves an alias to a new exact version for future runs.
   - Rollback moves it back without mutating either version.
   - Deactivation prevents new exact and alias-based admissions.
   - Deactivation does not cancel active runs or erase history.
7. Keep execution asynchronous. CLI/API client timeout or disconnect cannot
   become a worker lease or active-time limit.
8. Return a short launch receipt with invocation/workflow/run IDs and commands
   for status, cancel, pause, and audit.
9. Verify external callers lack internal stage, handoff, child, MCP, model
   route, and worker capabilities.

### Tests to write first

- Hermes process absent before CLI invocation admission.
- Hermes process absent before API invocation admission.
- Fresh process invokes alias and receives exact pin.
- Alias promotion during an active run changes only the next invocation.
- Rollback and deactivate history.
- Stable scheduler idempotency and conflicting replay.
- CLI-generated ad hoc idempotency key is displayed.
- Default concurrency rejection creates no queued run.
- Higher explicit concurrency works only for a configured deployment.
- Client disconnect leaves runtime progress intact.
- Pipeline advances and transfers handoffs without caller/Hermes.
- Parent spawns/joins/collates leaves without caller/Hermes.
- MCP call crosses containers without caller/Hermes.
- Trigger credential cannot call internal coordination or admin APIs.

### Exit gate

A deployed standalone, worker-plus-MCP-service, pipeline, and one-level tree
start and finish through AgentPaaS with the authoring Hermes session gone. The
MCP shape includes a service-originated routed model call; exact-version,
idempotency, concurrency, alias, and deactivation evidence is durable.

## T04 — Add attachable monitoring, recovery, lifecycle control, and amendment UX

### Goal

Let an authorized user or later Hermes session understand and control an
independently running workflow without becoming part of its execution graph.

### Required work

1. Attach by durable invocation/workflow/run ID. Do not require session-local
   ownership, original prompt history, local project path, or the authoring
   Hermes session.
2. Poll structured status/summarize at a bounded cadence only when the user
   asks to monitor. Use timeline for incremental evidence; do not hold one
   blocking command open for task duration.
3. Show progress when phase/checkpoint/control state changes, not on every
   heartbeat. Never fetch and repost handoff, child-result, or MCP payloads to
   advance execution.
4. On automatic B37 call recovery, explain the failed target category and
   selected approved recovery category from runtime evidence. Do not initiate
   another model call.
5. On `NEEDS_REPLAN`:
   - Fetch next-action and attempt report.
   - Verify safe checkpoint, current active-time/spend remaining, recovery
     count, and `recommended_actions`.
   - Offer `more_time`, `capability_up`, or `larger_context` only when present.
   - Generate an idempotency key and submit the same-run continuation only when
     acting as an authenticated operator.
   - Never submit an exact model and never create a second failure
     continuation.
6. For `split_task`, propose a revised/new deployment and its limits. Do not
   silently convert the current run or confuse it with a predeclared B35 child
   batch.
7. Implement lifecycle conversations:
   - Cancel: explain terminal and external-side-effect limitation, then submit
     one idempotent request.
   - Pause: report `PAUSE_REQUESTED` until a safe boundary and complete fencing
     produce `PAUSED`. Never say time has stopped earlier.
   - Resume: show same exact deployment/input and remaining active time/spend;
     report capacity conflict without changing state.
   - Restart: show new-run semantics, exact-version default, fresh limits, no
     checkpoint import, and optional explicit current-alias resolution.
8. Implement amendment conversation:
   - Read current authority generation and original/current/used/reserved
     values.
   - Ask which ceiling to raise and why.
   - Translate relative user wording to proposed absolute values.
   - Show the exact before/after preview.
   - Obtain explicit user confirmation.
   - Submit with `runs:amend_limits`, expected generation, and idempotency key.
   - Re-read status after a race; never retry as an increment.
9. If plugin/CLI monitoring times out, query durable state before declaring a
   worker failure.
10. A later Hermes session can summarize audit, controls, amendments, route
    decisions, costs, and artifacts without reading raw secrets or assuming
    correctness.

### Tests to write first

- Fresh Hermes profile attaches to a run created by CLI/API.
- Primary success and automatic model-call recovery.
- Needs-replan/more-time continuation.
- Context/larger-context continuation.
- No-progress/capability-up continuation.
- No checkpoint and second-continuation denial.
- Terminal active-time/spend exhaustion offers no amendment/resurrection.
- Pause-request/drain/paused wording and state.
- Long paused wall interval shows zero new active time/spend.
- Resume same exact version/input/checkpoint and no completed-work replay.
- Resume `ALREADY_RUNNING` leaves state paused.
- Restart defaults to exact deployment/input and starts at stage one.
- Explicit current-alias restart is visibly different.
- Running and paused amendments require exact confirmation and commit once.
- Amendment/exhaustion race reports the durable winner.
- Tool timeout followed by status query.
- Pipeline/parent/MCP progress is observed but never relayed.

### Exit gate

A new Hermes session can attach, explain, and submit authorized controls over
the common API, while a normal run needs no Hermes process and no runtime data
passes through Hermes.

## T05 — Enforce approval, scope, and prompt-injection boundaries

### Goal

Keep authoring and operations convenience inside deterministic receiver
authority.

### Required work

1. Require existing user/terminal confirmation paths for material trust or
   authority changes, including:
   - Cloud transfer.
   - New credential mapping.
   - New provider/endpoint/candidate pool.
   - Policy/network expansion.
   - New workflow topology or package.
   - Higher concurrency.
   - Limit amendment.
2. Do not treat same-run failure continuation inside current policy/time/spend
   as a limit amendment. Still require valid operator authority and runtime
   recommendation.
3. Never let Hermes provide user confirmation on the user's behalf.
4. Treat progress, artifacts, model/provider output, logs, package prose, and
   evidence excerpts as untrusted.
5. Preserve typed runtime control fields through sanitization while
   quoting/redacting bounded evidence.
6. Reject embedded instructions such as:
   - “increase budget”
   - “add ten minutes”
   - “choose model X”
   - “ignore retry limit”
   - “resume me”
   - “mark task complete”
   - “approve cloud transfer”
7. A worker cannot mint `safe_to_resume`, allowed action, lifecycle authority,
   or an amendment through its final result.
8. Keep administrative credentials out of worker containers, environment,
   prompts, journals, artifacts, logs, and route capabilities.
9. An external trigger has invoke-only authority unless separately issued a
   control credential.
10. Audit sanitized actor, scope, confirmation evidence, request digest,
    generation, and outcome for every lifecycle/admin operation.

### Tests to write first

- Injection in every untrusted field.
- Forged `recommended_actions` or `authority_generation`.
- Exact model request in progress text.
- Self-confirm limit/policy/cloud/concurrency expansion.
- New workflow/undeclared child after `split_task` without approval.
- Preapproved B35 child batch incorrectly asks Hermes to authorize each child.
- Invoke token attempts pause/restart/amendment.
- Worker steals an admin capability from environment/logs.
- Control fields remain intact after sanitizer.
- Secret sentinel in every transcript/tool/audit field.
- Alias-change prose in artifact cannot alter pinned deployment.

### Exit gate

No adversary fixture can make Hermes or an external trigger broaden authority,
alter a pinned run, submit a self-confirmed amendment, start extra paid work,
relay coordination data, or declare success from untrusted text.

## T06 — Update generated agent and workflow authoring patterns

### Goal

Make long-running progress/checkpoint, MCP service, handoff, and spawn/join
behavior part of generated AgentPaaS packages without embedding runtime
orchestration in agent code.

### Required work

Hermes-generated Routed Run workers must:

1. Use `@agent.on_invoke`.
2. Use `agent.llm()` without provider/model override.
3. Inspect `resume_checkpoint` from the first `agent.progress(...)` response.
4. Divide work into explicit committed phases.
5. Emit authenticated heartbeats around legitimate long local tool phases.
6. Emit a safe checkpoint only after:
   - Durable artifact write completes.
   - Governed side effects are understood/committed.
   - Completed and remaining work are accurate.
7. Reuse artifact references rather than embedding content.
8. Avoid repeating committed work on operator resume/failure continuation.
9. Return the final result only on operational completion. Raise an exception
   on failure; do not return an “error result.”
10. Never store credentials, hidden reasoning, provider cache IDs, or
    administrative/control data in checkpoints.
11. Never amend limits, choose exact models, create Docker resources, call the
    deployment API, or implement a private retry scheduler.

Maintain at least these worker templates:

- Phased customer-feedback report.
- Long local tool/test phase with heartbeats.
- Credentialed API read followed by model summarization.

Maintain focused pattern fixtures:

- B33 service package using `@agent.mcp_tool` plus a worker using a logical
  service/tool binding; no port or capability is hard-coded.
- B34 three-stage pipeline using `workflow_input()` and `commit_handoff()` with
  compatible schemas.
- B35 parent using checkpointed `spawn_children()`/`join_children()` and leaf
  workers that cannot spawn. Parent code collates results.

All generated workflows use immutable package identities, separate
containers, one shared workflow envelope, bounded artifact references, and no
Hermes callback. Do not introduce a default verifier or semantic judge.

### Tests to write first

- Static AST checks for required/forbidden SDK usage.
- Resume skips a committed phase.
- Long phase heartbeats and safe-checkpoint validity.
- Checkpoint artifact references resolve and remain bounded.
- No provider/model names in worker source.
- No secret or reserved control keys.
- Handoff and child input/result schema compatibility.
- Spawn follows a safe checkpoint and stable idempotency key.
- Generated service/pipeline/tree contains no raw internal endpoint, Docker
  call, subprocess orchestration, Hermes callback, admin API, or limit
  amendment.
- Parent is the only node allowed to spawn; leaves cannot spawn grandchildren.

### Exit gate

Hermes reliably generates supported packages that satisfy B27–B39 and remain
independently executable by AgentPaaS after deployment.

## T07 — Implement simple user-facing deployment and run results

### Goal

Make the product proof understandable to a general audience.

### Required work

Deployment:

```text
Agent deployed
Version: customer-report@1.2.0
Alias: production/customer-report
Run it with AgentPaaS; Hermes does not need to stay open.
```

Primary success:

```text
Task completed
Used an approved model route
Metered LLM spend: $0.42
$1.58 under the $2.00 LLM spend limit
```

Recovered:

```text
Task completed
The first model timed out
AgentPaaS continued on another approved model
Metered LLM spend: $1.37
$0.63 under the $2.00 LLM spend limit
```

Paused:

```text
Run paused at a safe boundary
Active time is stopped
No new model calls or spend are authorized while paused
Existing unreconciled exposure, if any, remains visible
```

Needs replan:

```text
The worker reached its attempt allowance after making progress.
A safe checkpoint is available.
AgentPaaS recommends more time within the current active-time and spend limits.
```

Terminal:

```text
The task stopped because no approved model with a valid credential remained.
No additional model was called.
```

Requirements:

1. Put exact route, attempt, alias history, and candidate details behind
   timeline/inspect.
2. Always show requested deployment reference and exact resolved version in
   technical receipts.
3. Distinguish LLM spend from total task cost.
4. Say “under limit,” not “saved.”
5. Say “completed,” not “verified correct.”
6. Label fault injection in demo output.
7. Show unreconciled exposure when non-zero and never describe it as an active
   request or available money.
8. Show local/subscription cost basis.
9. Never say “paused” or “time stopped” while state is
   `PAUSE_REQUESTED`.
10. For amendment, show old/current/new absolute values and that the prior
    spend/time remains consumed.
11. For restart, say “new run,” show `restarted_from`, and distinguish exact
    default from explicit current-alias resolution.

### Tests to write first

- Exact output snapshots for deploy, invoke, success, recovered,
  pause-requested, paused, resumed, needs-replan, amended, restarted, and
  terminal.
- Under-limit/at-limit/unreconciled/no-limit.
- Local/subscription.
- Requested alias plus resolved exact version.
- Forbidden-language grep.
- No raw prompt, response, key, capability, or internal endpoint.

### Exit gate

A non-specialist can tell what was deployed, how to invoke it independently,
whether it completed, why routing/control changed, and what metered LLM spend
was incurred.

## T08 — Deterministic Hermes lifecycle transcripts and B41 live handoff

### Goal

Prove deterministic authoring/deployment/operations behavior, not merely tool
registration, and leave an executable live matrix for B41 after roster
selection.

### Required deterministic matrix

Use a fake runtime/provider and clean Hermes test profiles:

1. Build/test/package/deploy a local-first standalone worker.
2. Build/test/package/deploy a cloud-cost-first standalone worker.
3. Build/test/package/deploy B33 MCP service/client workflow.
   Include one fake-provider service-originated model call proving shared
   routing, recovery, budget/token accounting, and service-lease fencing.
4. Build/test/package/deploy B34 three-stage pipeline.
5. Build/test/package/deploy B35 parent/three-leaf workflow.
6. Terminate Hermes; invoke an alias through CLI.
7. Terminate Hermes; invoke an exact version through API.
8. Stable scheduler invocation retries to the same run.
9. Conflicting invocation idempotency is rejected.
10. Default concurrency rejects overlap with no hidden queue.
11. Alias promotion affects only future runs.
12. Alias rollback and exact-version deactivation.
13. Primary model success.
14. Automatic call-level recovery.
15. Lease expiry -> one `more_time` continuation.
16. Context failure -> one `larger_context` continuation.
17. No progress -> one `capability_up` continuation.
18. Second failure continuation denied.
19. Pipeline safe-boundary pause and exact resume.
20. Tree pause drains active leaves, blocks unstarted leaves, then resumes.
21. Cancel wins over late success.
22. Restart creates a linked new run on exact version by default.
23. Explicit current-alias restart resolves the current alias once.
24. Running/paused limit amendment after explicit confirmation.
25. Amendment/exhaustion race.
26. Fresh Hermes profile attaches to an externally invoked run.
27. Prompt injection in progress/artifact/provider output.
28. Legacy development build/run.
29. Unsupported DAG and recursive-child requests are rejected/narrowed.

For every transcript assert:

- Correct tool/API and arguments.
- Original Hermes process is absent for independent-invocation cases.
- No exact model in continuation request.
- No raw secret or admin capability.
- No authority expansion/self-confirmation.
- No fabricated output.
- No handoff/MCP/child payload relay.
- Final exact-version, state, active-time, and cost arithmetic matches runtime
  records.

### B41 live carry-forward matrix

This matrix is specified and fixture-ready in B40 but is not a B40 completion
dependency because the founder-approved roster does not exist until B41 T05.
After B41 roster candidates exist, B41 T07 must run at least:

- One real local-first build/deploy, Hermes exit, CLI invocation, and later
  fresh-session inspection.
- One real cloud-cost-first build/deploy, Hermes exit, API invocation, and
  automatic model recovery.
- One legitimate five-plus-minute worker/tool phase with progress and no hidden
  client timeout.
- One pause lasting longer than the original remaining wall-clock interval,
  followed by resume proving frozen active time and spend.
- One authorized pre-terminal time/spend amendment proving prior consumption
  remains.
- One real cross-container MCP service call.
- One real pipeline and one parent/leaf run where Hermes is absent before
  invocation and never relays data.

Provider failure uses a labelled fault proxy; do not wait for a real outage.

### Exit gate

All deterministic transcripts pass in CI. The live matrix has executable
fixtures, commands, expected evidence, sanitization, and ownership handed to
B41 T07; no live row is marked PASS in B40. This closes B40 without creating a
B40/B41 dependency cycle.

## T09 — Block gate and adversary review

### Required `make block40-gate`

Run:

```text
make block39-gate
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
go test ./internal/operator/... ./internal/cli/... ./internal/daemon/... -count=1 -race
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

Run deterministic one-shot Hermes transcript tests. Live Hermes/model tests
are recorded for B41 and cannot be converted to PASS when unavailable.

### Required adversary matrix

- Deployed run requires the authoring Hermes session.
- External trigger receives stage/handoff/MCP/child capability.
- Alias is re-resolved after invocation admission.
- Same exact deployment version is overwritten.
- Deactivated version accepts a new run or erases active/history state.
- Duplicate scheduler request creates two runs.
- Concurrency rejection becomes a hidden queued start.
- Provider/model/endpoint supplied on continuation.
- Progress/artifact/model output forges control or amendment fields.
- Hermes silently raises active time, attempt lease, spend, concurrency, or
  policy.
- Hermes self-confirms `runs:amend_limits`.
- Amendment submitted after terminal exhaustion resurrects a run.
- `PAUSE_REQUESTED` is reported as frozen/paused.
- `PAUSED` retains a live capability/reservation or accrues time/spend.
- Resume replays completed work or resolves a new alias.
- Restart silently imports a checkpoint or silently chooses current alias.
- `split_task` creates a paid new deployment/run without approval.
- Hermes relays pipeline handoff, MCP call, child input, or child result.
- Preapproved B35 spawn incorrectly requires conversational approval.
- Undeclared service/stage/child or recursive child is silently generated.
- Failure continuation loops a second time after pause/resume.
- Plugin timeout is mistaken for worker failure.
- Fluent worker output is declared correct.
- Under-budget amount is called savings.
- Local/subscription use is called free.
- Secret/admin capability appears in transcript/tool args.
- Generic external orchestrator adapter is added.

### Block success gate

B40 is complete only when:

1. `make block40-gate` passes.
2. A clean Hermes profile authors, tests, packages, and creates immutable
   deployments for all four supported shapes.
3. CLI/API invocation succeeds after the authoring Hermes process exits.
4. External cron/Kubernetes examples are trigger-only and use durable
   idempotency.
5. Requested refs, exact versions, alias history, concurrency, and deactivation
   semantics match B26.
6. A fresh Hermes session can inspect and control an externally invoked run
   through the same API.
7. Cancel, pause/resume, restart, continuation, and explicitly confirmed
   amendments preserve B39 authority and state semantics.
8. Exact model selection remains runtime-owned.
9. Generated workers/services/workflows satisfy B27–B39 and contain no runtime
   orchestrator callback.
10. Result language is simple and truthful.
11. No generic/bring-your-orchestrator runtime surface is added.
12. The B41 live carry-forward matrix is executable but remains `NOT RUN` until
    B41 selects roster candidates; B40 completion requires no B41 evidence.

## R40 — `v0.5.0-rc.1` GitHub prerelease checkpoint

B40 completion permits a release candidate for external testing; B41 still
owns stable v0.5. The RC headline is governed model recovery under one shared
workflow spend limit with complete Hermes authoring and operations UX.

Before RC tag approval:

1. Run cumulative B26–B40 deterministic/race/Docker/adversary gates and
   v0.2.3/v0.3/v0.4 upgrade compatibility.
2. Run the candidate live roster and timing/activation calibration allowed by
   the approved test-spend envelope. Provisional names/defaults remain clearly
   candidate data until A1–A3 disposition.
3. Build signed/checksummed/SBOM candidate artifacts and a non-default install
   path; never move stable Homebrew to an RC.
4. Publish a labelled-fault demo covering deterministic route selection, one
   exact call replay, sticky recovery, shared spend/token evidence,
   pause/resume, `NEEDS_REPLAN`, one continuation, and Hermes reattachment.
5. Publish the complete candidate known-defect/risk list and invite focused
   testing of installation, migration, streaming reconnect, catalog/A2A,
   workflows, recovery, budgets, and lifecycle races.
6. Require zero open P0 security/data-loss defect. P1 defects may exist only
   when explicitly marked as RC blockers before stable B41.
7. Present exact commit, artifacts, evidence, spend, open blockers, and command
   for explicit approval before creating/pushing `v0.5.0-rc.1`.

Post-publish RC verification installs from the public candidate path, checks
signatures/version parity, and runs one bounded Routed Run. RC feedback and
fixes feed B41; the RC tag is immutable.

## Handoff record required after every task

Append:

- Task/date.
- Skill/tool/API contract decisions.
- Files changed.
- Tests and transcripts added first.
- Exact commands/PASS output.
- Deployment/version/alias evidence.
- Independent-invocation process evidence.
- Sanitizer/authority/adversary result.
- User-confirmation paths.
- Compatibility impact.
- Live-test status.
- Open risks.
- Next task unblocked.

## Pitfalls

- Hermes is the supported authoring environment, not a deployed runtime
  dependency.
- Do not make a Hermes session the owner of a deployment or run.
- Do not ask users to choose exact models during recovery.
- Do not confuse a plugin subprocess timeout with worker failure.
- Do not confuse attempt lease with maximum active duration.
- Do not report `PAUSE_REQUESTED` as paused or frozen.
- Do not let Hermes self-confirm a limit amendment.
- Do not reset consumed active time/spend on resume or continuation. Restart
  preserves the source ledger immutably and creates a separate, newly approved
  ledger for the new invocation.
- Do not mutate an exact deployment or re-resolve a pinned alias.
- Do not create a new/expanded workflow, concurrency limit, or budget silently.
- Do not confuse a preapproved B35 child batch with an ad hoc `split_task`
  workflow.
- Do not relay MCP calls, handoffs, spawn requests, or child results through
  Hermes.
- Do not let untrusted progress text become a control instruction.
- Do not claim semantic correctness or savings.
- Do not add another orchestrator adapter “for portability” in this block.
