# AgentPaaS

## Run any AI agent safely and efficiently—with credentials, costs, and network access under control.

AI can now build useful agents and integration workflows quickly. Operating
them safely is still difficult. They contain AI-generated code, call changing
model and SaaS endpoints, use valuable credentials, can run for many turns,
and may be changed by untrusted input after deployment.

AgentPaaS is the platform for building, deploying, and operating governed AI
agents. AgentPaaS skills guide Hermes while a practitioner plans, creates,
tests, packages, and deploys an agent, pipeline, or one-level agent tree.
Generated workers use the AgentPaaS SDK. After deployment, AgentPaaS runs the
artifact independently: a cron job, Kubernetes, another process, or the
AgentPaaS CLI can invoke it through the AgentPaaS API without a live Hermes
session. The runtime isolates workers, coordinates long-running and
multi-container execution, resolves approved capabilities from a private
component catalog, enforces their approved authority, routes model calls and
agent-to-agent tasks, brokers artifacts, supervises execution, and records
what happened.

The foundational promise is:

> Even if an agent is compromised, it cannot silently take credentials or
> communicate outside the authority its administrator approved.

The cumulative v0.5.0 resilience promise is:

> A long-running AgentPaaS task can continue across many turns and governed
> containers, survive an eligible model failure, remain inside its approved
> authority and active-time limit, never authorize an LLM call beyond its
> approved spend limit, and report exactly what happened.

AgentPaaS does not initially promise that the answer is factually correct, that
the work product is high quality, or that its routing has already found the
globally optimal model. Those claims require different evidence and remain
outside the first Routed Run release.

## What the product does

AgentPaaS packages and runs an agent inside an isolated container with no
direct route to the internet. Model, HTTP, and MCP traffic crosses governed
interfaces and a policy-enforcing gateway. Credentials stay outside the agent
source, package, trigger payload, and Python worker. The runtime applies them
only inside trusted request handling.

Signed packages carry source, requirements, policy, identity, provenance,
SBOMs, and verification material—not live credentials. A receiver inspects
the requested authority, maps logical credential requirements to local
credentials, approves the policy, and runs the same governed artifact.

For longer-running work, AgentPaaS adds a managed execution contract:

- No hidden one-minute or five-minute platform ceiling. A task may run for the
  operator-authorized active duration, with explicit cancellation, pause, and
  liveness contracts.
- One routed workflow containing one or more bounded worker runs and attempts;
  a standalone agent is a one-node workflow.
- AgentPaaS-managed MCP services that run in separate governed containers.
- A tenant-private catalog of signed agent and MCP packages, distinct from the
  directory of currently active instances.
- Constrained capability discovery that records the query and pins one exact,
  tested package version before admission.
- A2A-compatible agent tasks and events plus MCP-compatible tool/service calls,
  with AgentPaaS state remaining authoritative.
- Two-sided communication authorization: both the sender's egress and the
  receiver's ingress must permit the exact logical edge.
- Encrypted brokered artifact transfer using scoped, expiring grants rather
  than shared writable mounts or raw object-store credentials.
- Durable ordered event streams, cursor replay, and interactive wait/wakeup;
  clients can disconnect without owning or polling the run.
- Explicit `on_demand`, `warm`, and `resident` activation, with ordinary
  workers dormant until invoked.
- Native sequential pipelines whose context and artifacts are transferred by
  the runtime rather than relayed through Hermes.
- Bounded parent/child execution in which a parent agent can start approved
  child agents in separate containers, await them, collate their results, and
  continue.
- Logical model routes rather than provider names embedded in worker code.
- An approved local and/or cloud model pool.
- A shared LLM spend/token limit across all workflow calls, model-using
  services, stages, parents, children, and attempts.
- Separate model-call, stall, attempt-lease, and hard active-time controls.
- Heartbeats, semantic progress, portable checkpoints, and artifact
  references.
- Automatic recovery for eligible individual model-call failures.
- A structured attempt report when an authenticated operator must continue
  within the existing envelope, create a revised deployment, or stop.
- A tamper-evident audit chain plus durable ledgers for workflow transitions,
  service calls, handoffs, child results, model choices, failures, costs,
  checkpoints, attempts, and terminal results.

## The integrated AgentPaaS stack

| Layer | Responsibility |
|---|---|
| **Hermes** | User-facing authoring, testing, packaging, deployment, and optional operations environment |
| **AgentPaaS skills for Hermes** | Teach Hermes how to build and test workers, create immutable deployments, manage aliases, inspect runs, and request allowed control actions |
| **AgentPaaS SDK** | Gives workers governed buffered/streaming model, HTTP, MCP, A2A task, durable event/wait, progress, checkpoint, artifact, child-agent, and completion interfaces |
| **AgentPaaS CLI and API** | Register and query packages; deploy, promote, invoke, subscribe, signal, inspect, cancel, pause/resume, continue, restart, amend authorized limits, roll back, and deactivate without making the caller the runtime coordinator |
| **AgentPaaS catalog and gateways** | Store tenant-scoped signed agent/MCP metadata separately from live instances; resolve exact compatible capabilities; enforce every ingress, egress, message, and artifact grant |
| **AgentPaaS runtime** | Resolves immutable deployments; activates, isolates, and supervises workers and services; executes linear pipelines and bounded leaf-child batches; transfers bounded handoffs/artifacts; selects approved model targets; enforces credentials, communication policy, leases, active-time limits, concurrency, and budgets; records durable events and evidence |

Hermes owns the interactive authoring experience: business goal, build-time
task plan, worker and workflow creation, testing, packaging, deployment, and
optional later diagnosis. It calls AgentPaaS lifecycle and control interfaces;
it does not remain attached to deployed execution.

The runtime owns package/capability resolution, exact eligible model selection,
model switching, worker attempts, leases, active-time limits, budgets,
mechanical state, active-instance discovery, MCP service calls, A2A tasks,
pipeline stage transitions, parent/child scheduling, durable context and
artifact transfer, and structured evidence. The same evidence is available
through the API, CLI, or a later Hermes session. Hermes does not start
implicitly with a cron/API invocation and never sits in the middle of an
approved message, stage transition, MCP call, or parent/child data transfer.

There is no “bring your own runtime orchestrator” adapter through v0.5.0. External
systems may invoke and inspect a deployed AgentPaaS artifact, but they do not
schedule its stages, relay its context, spawn its children, or replace its
runtime. AgentPaaS workers and workflows are authored through Hermes using
AgentPaaS skills and the SDK, then operated by AgentPaaS itself.

Supporting pitch:

> Build and deploy with Hermes; run independently with AgentPaaS under
> governed, resilient execution across approved local and cloud models.

## Why this matters

### Safer than running AI-generated code directly

Agents are useful because they act. That also makes the workload, its inputs,
its dependencies, and its future behavior untrusted. AgentPaaS places the
agent inside a deterministic authority boundary and keeps credentials and
network access under runtime control.

### More resilient than hard-coding one model

The cheapest or strongest model changes frequently. A local model may be
adequate for one worker and fail on another. A cloud model can time out, hit a
context limit, exhaust a quota, or become unavailable mid-task. AgentPaaS lets
workers request a logical route and lets the runtime select and, when allowed,
replace the exact target without changing worker code.

### More useful than cost-per-call reporting

The meaningful unit is the routed workflow, not one API request. A standalone
run is simply a one-node workflow. A more expensive
model can finish in fewer turns; a cheaper model can loop until it costs more.
AgentPaaS records total calls, turns, input, cached input, output, elapsed time,
failed attempts, and metered LLM spend across the complete workflow.

### Portable rather than platform-captive

An AgentPaaS package carries the worker and its declared requirements. The
receiver maps the signed candidate envelope to their approved local endpoints,
provider subscriptions, and credentials. A different provider, model,
endpoint, or authority requires an explicit reconfiguration/fork and new
approval; the runtime never substitutes it silently. OpenRouter can expose a
broad cloud pool, direct providers can participate, and local
OpenAI-compatible runtimes can participate without making any one provider
the architecture.

### Simple enough to prove

The initial public proof is not an abstract routing benchmark. It is an
ordinary task that finishes after an injected primary-model failure and
produces a short operational result:

```text
Customer-feedback report produced
Primary target timed out
AgentPaaS recovered on another approved target
Total metered LLM spend: $1.37
$0.63 under the $2.00 LLM spend limit
```

“Under budget” demonstrates control. It is not described as savings or
optimization until AgentPaaS has a measured comparison baseline.

## The wedge

AgentPaaS is not another visual workflow builder, connector catalog, standalone
agent framework, or generic AI gateway. Its wedge is the complete trust and
execution path:

**Hermes authoring -> AgentPaaS worker/workflow -> signed immutable deployment
-> receiver-approved authority -> CLI/API/cron invocation -> isolated
execution -> governed model and tool calls -> bounded recovery -> verifiable
evidence.**

AI gateways, sandbox providers, authentication products, MCP servers, and
connectors may be useful inputs. AgentPaaS differentiates by binding them to a
portable worker, its receiver-specific authority, its multi-attempt execution,
and its evidence.

## Who starts with it

The first user is an AI practitioner or platform engineer building an agent in
Hermes on a Mac. They can install AgentPaaS without an AgentPaaS cloud account,
build and test a worker or workflow, approve its destinations and model pool,
deploy an immutable version, invoke it later from CLI/API/cron, inspect
failures and costs, and share the signed package without sharing credentials.

Two initial proof patterns serve different audiences:

- `local-first` for indie developers, privacy-conscious users, and teams with
  available local compute.
- `cloud-cost-first` for developers and platform teams that want economical
  cloud execution with an approved higher-capability recovery tier.

Neither local execution nor a particular cloud provider is required for the
product to succeed.

## Open-source and commercial direction

The local runtime, enforcement path, package format, policy and routing
schemas, SDK, CLI, Hermes skills, local model router, audit evidence, and
golden demos remain open source. This is the trust and adoption engine.

After v0.3, the primary commercial direction is a managed AgentPaaS service:
customers pay AgentPaaS to help package and deploy their agents into a
tenant-isolated PaaS. Hosted onboarding, private component/model catalogs,
fleet management, metering/billing, dashboards, SSO/RBAC, approval workflows,
backups, compliance evidence, support, and service commitments build on the
same open contracts. A customer-owned-cloud adapter may follow where demand
requires it; it is not the mandatory first commercial step.

The local product is not intentionally weakened to create a paid security
tier.

---

# High-level roadmap

The executable release sequence lives in `docs/roadmap.md` and
`docs/execution/blocks/`. This section explains product maturity rather than
repeating every engineering task.

## Foundation — shipped through v0.2.3

AgentPaaS already proves the basic governed lifecycle:

- Local macOS runtime and Docker isolation.
- Default-deny egress through a gateway.
- Credential values kept outside agent code and trigger payloads.
- Policy, token budgets, rate limits, guardrails, and audit evidence.
- Signed packaging, SBOMs, publisher identity, TOFU trust, bundle inspection,
  receiver-local credential mapping, install, fork, and provenance.
- Hermes-assisted build, run, inspection, diagnosis, sharing, and receiving
  flows.
- Golden, manual, and adversarial release gates.

This baseline has primarily exercised simple, short-lived agents with one LLM
configuration. It does not yet prove long-running multi-turn recovery.

## v0.3.0-alpha.1 — Durable Agent Runtime preview

After B30, publish a GitHub prerelease—not the stable Homebrew default—that
lets developers try real buffered/streaming model calls, durable cursor replay,
interactive wait/wakeup, explicit activation modes, independent CLI/API
invocation, and long-running multi-turn work that outlives Hermes and client
connections. The public demonstration is: start an agent, disconnect, return
later, and recover its ordered evidence and durable result.

## v0.3.0 — Agent Registry and Secure Delegation

After B32 and its release-closure gate, ship the next stable release. It
includes the B26/B27 durable foundations, B28 portability evidence, B29 runtime
profiles/streaming/events/activation, B30 long-running execution, B31 private
agent/MCP catalog, and B32 secure A2A tasks plus encrypted artifact transfer.

The stable launch claim is:

> Build an agent with Hermes, register its signed package, discover it by
> capability, pin an exact tested version, delegate a task securely, transfer
> an encrypted artifact, and receive a durable completion event—all locally
> today.

Docker remains the only supported production runtime. Kubernetes and
Cloudflare results are portability/feasibility evidence, not shipped adapters.
Managed-PaaS design-partner work may begin after this release using the B28
decision while the local release train continues.

## v0.4.0 — Governed Multi-Agent Workflows

After B35 and its release-closure gate, ship AgentPaaS-container MCP services,
runtime-native three-stage pipelines, and bounded parent/leaf-child fan-out and
fan-in. The public demonstration uses separate orchestrator, worker, verifier,
testing-agent, and MCP-service packages; Hermes exits before invocation and is
never the runtime message or file relay.

## v0.5.0-rc.1 and v0.5.0 — Durable Routed Run

After B40, publish a release candidate containing deterministic model routing,
one bounded model-call recovery, shared workflow LLM spend enforcement,
integrated supervision/control, and the complete Hermes authoring/operations
UX. After B41 passes the cumulative RR-01–RR-31 proof and final publish gate,
ship stable v0.5.0.

The stable v0.5 claim is that a long-running governed workflow can select only
inside its approved model envelope, survive one eligible model failure, remain
inside shared time/spend/authority limits, and report exactly what happened.

Every stable release has its own closure gate: cumulative compatibility and
migration tests, clean installation, signed binaries/checksums/SBOM, Homebrew
verification, truth-synced docs and limitations, one memorable end-to-end
quickstart, attached evidence, zero open P0/P1 defects, and explicit approval
for the exact release commit. Alpha and RC tags remain GitHub prereleases and
must not silently replace the stable installation channel.

## After v0.3.0 — managed PaaS validation and adoption

The next commercial milestone is a managed, tenant-isolated AgentPaaS service
using the same signed package, SDK, policy, catalog, event, artifact, and
gateway contracts as the local runtime. The B28 evidence chooses whether the
first managed substrate is Cloudflare, Kubernetes, or another backend; product
APIs do not expose substrate objects.

Early managed pilots and local practitioners should establish:

- Whether practitioners can author and deploy through Hermes, then invoke and
  operate Routed Run independently without expert help.
- Whether AgentPaaS-assisted packaging and deployment removes enough operating
  burden for customers to pay for a managed service.
- Whether the selected substrate meets tenant isolation, activation latency,
  durability, metering, and unit-economics requirements.
- Which failure classes occur in real work.
- Which attempt reports lead to useful replans.
- Whether total run cost, turns, and recovery evidence change model-selection
  decisions.
- Whether platform teams embed the SDK and skills in their own AgentPaaS
  workers.

Only after this evidence should AgentPaaS consider cross-task learning,
task-complexity prediction, automatic decomposition, outcome verification, or
claims of model-cost optimization.

## Customer-owned cloud deployment

Offer a customer-owned-cloud adapter after the managed substrate is proven, or
earlier only for a concrete design partner. Preserve the same portable ports
and public contracts for runtime, secret storage, identity, component/model
catalogs, events, artifacts, audit, and deployment.

Do not build multi-cloud parity, a large Terraform product, or Kubernetes
objects into the public agent contract.

## Managed AgentPaaS service

Build the first managed PaaS after v0.3 from the B28 substrate decision and
the same open-source data plane. Add organizational operations and commercial
service ownership without replacing or weakening the local security contract.
The managed service is the primary commercial hypothesis, not a distant option;
it still advances through evidence and bounded pilots rather than a speculative
multi-cloud control-plane build.

---

# Approved decision register

This section is the single decision record for the pitch and Routed Run
direction. A separate model-routing decision file is intentionally not
maintained.

## D1. Lead with control, then prove optimization

The approved top-level message is:

> Run any AI agent safely and efficiently—with credentials, costs, and network
> access under control.

For the current product, “efficiently” means bounded and observable execution:
time, calls, tokens, LLM spend, recovery, and safe failure. It does not yet
mean that AgentPaaS has proven an automatic optimizer.

The claim progression is:

1. Control execution.
2. Measure execution.
3. Recover execution.
4. Optimize execution.
5. Prove savings against a baseline.

## D2. Hermes authors; AgentPaaS operates independently

People who choose AgentPaaS build AgentPaaS workers and workflows through
Hermes. AgentPaaS skills teach Hermes how to construct SDK workers, test them,
package them, create immutable deployments, manage deployment aliases, and
use the runtime lifecycle contract.

Hermes is not a deployed runtime dependency. After authoring, testing,
packaging, and deployment, an AgentPaaS deployment can be invoked through the
AgentPaaS CLI or authenticated API by cron, Kubernetes, or another process.
AgentPaaS coordinates MCP service
calls, pipeline stage transitions, parent/child scheduling, model recovery,
checkpoints, and data handoffs. A later Hermes session may inspect or control
the run through the same AgentPaaS interfaces.

Generic customer runtime orchestrators, arbitrary externally built agents,
Codex/Cursor adapters, and dedicated LangChain or LlamaIndex integrations are
not v0.5.0 scope. Calling the deployment API does not make the caller an
AgentPaaS runtime orchestrator.

## D3. The first routing milestone is resilience

The first milestone is:

> A multi-turn AgentPaaS worker survives a primary model failure, continues
> through one eligible recovery selection, remains inside enforced
> guardrails, and reports the complete attempt and cost history.

Long-running multi-turn execution, checkpoint recovery, and fallback must be
proven before broader optimization claims.

Model-routing implementation does not begin until AgentPaaS has first passed
the long-running worker, inter-container MCP, native pipeline, and bounded
parent/child foundation gates.

## D4. Routed Run does not certify correctness

v0.5.0 has no general verifier and no LLM judge.

“Completed” means the worker returned its requested result and exited
successfully while remaining inside operational constraints. It does not mean
that AgentPaaS independently verified factual correctness or quality.

Application-specific deterministic checks, an optional LLM verifier, and
workflow outcome contracts may be added later as patterns. They are not part
of the router’s general success definition.

## D5. One routing engine supports multiple policy patterns

The engine is a policy-controlled two-tier route. Initial templates are:

```text
local-first:
approved local primary pool -> approved cloud recovery pool

cloud-cost-first:
approved economical cloud primary pool -> approved higher-capability cloud recovery pool
```

Later reliability-first, compliance-first, residency-first, or latency-first
patterns should reuse the same interfaces.

OpenRouter is an important adapter and source of models. It is not an
architectural dependency and its unrestricted auto-router is not the v0.5
selector.

## D6. Workers request logical routes, not exact models

A worker requests a logical route such as `worker.general`. Route policy
declares the approved candidate pool and the minimum context, capability,
feature, location, credential, time, and budget constraints.

The exact target is selected by the runtime. A routed worker cannot override
the chosen provider or model by supplying `model=` to `agent.llm()`. The
existing override remains available only in the legacy single-model path for
backward compatibility.

Per-call requirements may narrow the signed route—for example, requesting
structured JSON, a larger minimum context, or `effort: high`—but may not add a
provider, credential, location, or capability outside it.

## D7. The first selector is deterministic and explainable

The selector:

1. Removes targets forbidden by policy or cloud-transfer rules.
2. Removes targets without an available independent credential or explicitly
   approved unauthenticated local endpoint.
3. Removes targets that do not meet context, feature, effort, capability, time,
   or output requirements.
4. Removes targets whose price cannot be bounded under the active LLM spend
   limit.
5. Selects the eligible target with the lowest conservative metered LLM cost
   in the required primary or recovery tier.
6. Uses stable candidate ID ordering as the final tie-breaker.

Every decision records the considered candidates, exclusion reasons, selected
target, catalog version, and remaining budget. No hidden historical score,
prompt classifier, or LLM judge participates in v0.5.

Time eligibility means that the bounded call can still fit inside the current
lease, remaining active-time limit, and recovery margin. v0.5 records actual
target latency but does not rank models by an unproven latency predictor or
promise a latency SLA.

The model pool is an administrator-approved and tested roster, even when a
provider exposes thousands of models.

An aggregator candidate is also bounded. It names one model and a signed
allowlist of acceptable upstream provider endpoints. Aggregator model
fallbacks are disabled; provider routing cannot leave that allowlist. The
runtime records the returned model/upstream identity when the adapter exposes
it, and a mismatch outside approved aliases is a terminal contract violation.
This preserves OpenRouter as a useful adapter without delegating AgentPaaS
model selection or policy authority to it.

## D8. Routed Run is one product with two recovery boundaries

Model-call routing and whole-worker supervision ship together. They are not
separate levels, SKUs, or public milestones.

Every routed workflow creates:

- A workflow ID plus node/run/attempt IDs; a standalone run uses one node.
- An immutable route-policy and catalog snapshot.
- One current LLM spend ceiling and active-time ceiling shared by all stages,
  parents, children, calls, and attempts, plus an append-only amendment chain.
- Scoped service, node, run, and attempt leases.
- Per-worker progress/checkpoint channels and isolated artifact workspaces,
  with bounded workflow handoff/result references.
- A model-call, route-decision, usage, cost, and attempt ledger.
- A structured result for CLI/API consumers and optional Hermes inspection.

### Model-call recovery

When an eligible individual call fails:

1. Preserve the request through the last completed call boundary.
2. Discard uncommitted partial output.
3. Classify the observable failure.
4. Select one eligible recovery target.
5. Replay the normalized request exactly once.
6. Keep the recovery target sticky for the remainder of that worker attempt.

The router does not start an uncontrolled cascade through every available
model.

### Whole-worker recovery

When a worker stalls, loops, exhausts its attempt lease, or otherwise cannot
finish while enough approved time and LLM budget remain, the runtime fences
the attempt and returns a checkpoint and attempt report through the control
API.

An authenticated operator, directly or through Hermes, may choose:

- `more_time`
- `capability_up`
- `larger_context`
- `split_task`
- `stop`

For v0.5.0, at most one operator-directed whole-worker continuation is
allowed. The control request changes only the allowed attempt requirement;
the router still selects the exact target.

`split_task` may cause a practitioner to create and deploy a new workflow
envelope through Hermes and is not a continuation action. Inside an already
approved workflow, the AgentPaaS runtime starts declared pipeline stages or
parent-requested children and transfers their bounded results.

If the workflow consumes its current active-time or LLM spend ceiling before
an authorized amendment commits, the run is terminal. It does not enter
`NEEDS_REPLAN`, and no control client may resurrect or amend it afterward.

## D9. Time controls are separate

A single timeout is inadequate for agent work. Routed Run distinguishes:

- **Model-call timeout:** one provider request stopped responding.
- **Stall timeout:** the worker stopped heartbeating or producing observable
  governed activity.
- **Attempt lease:** the active execution time granted to the current worker
  attempt inside the workflow limit.
- **Hard workflow/run active-time limit:** the maximum accumulated running
  time currently approved for the workflow; a standalone run is a one-node
  workflow.

Reaching a five-minute lease while still making meaningful progress is not
proof that the model is incapable. The runtime should stop at the lease,
preserve a safe checkpoint, and expose an operator decision when the remaining
active time and LLM spend envelope can support one continuation.

Final defaults will be calibrated against real local and cloud tests before
release. They remain configurable and must leave a recovery margin before the
active-time limit. Before the one whole-worker failure continuation is used,
normal work must leave that margin unconsumed. Call-level model recovery stays
inside the current call/attempt envelope and does not reserve it again. When an
authorized `FAILURE_CONTINUATION` starts, the margin is released once for that
final attempt; no second continuation reserve exists.

"Run for as long as needed" means there is no hidden product ceiling. Every
run still has an explicit operator-approved active-time limit, which may be
hours or days, plus cancellation and liveness controls. Attempt leases can be
continued once within that limit while progress and a safe checkpoint exist.
`PAUSED` and `NEEDS_REPLAN` do not consume active time; `PAUSE_REQUESTED` does
until a safe boundary is committed. No model/tool work or spend may occur
while fully paused. An authorized operator may raise a non-terminal run's
current time or spend ceiling through an append-only audited amendment. If the
current ceiling is consumed before that amendment commits, the run is
terminal.

## D10. `agent.progress(...)` is the minimum worker recovery contract

The primary new SDK method combines heartbeat, progress, semantic checkpoint,
artifact references, and safe-resume declaration:

```text
phase
completed_work
remaining_work
artifact_references
last_committed_action
safe_to_resume
```

The runtime automatically records mechanical state it can observe:

- Completed model-call boundaries.
- Target selections, failures, and routing decisions.
- Usage, metered LLM spend, and elapsed time.
- Governed HTTP and MCP receipts.
- Durable artifact metadata and workspace state.
- Heartbeats, attempts, leases, and guardrail events.

On a resumed attempt, the first `agent.progress(...)` response returns the
latest accepted resume checkpoint and attempt metadata. This avoids adding a
second mandatory recovery API while keeping resume state outside user trigger
payloads.

AgentPaaS does not claim to transfer hidden chain-of-thought,
provider-internal reasoning state, continuation IDs, provider prompt caches,
arbitrary process memory, or uncommitted partial output.

If no valid checkpoint exists, AgentPaaS fails the worker terminally and does
not blindly restart work that may already have caused side effects.

## D11. Attempts are fenced

Every worker attempt receives a lease identity. When an attempt ends or
recovery begins, the lease is revoked before a new attempt is allowed to make
model, HTTP, MCP, credentialed, or artifact-commit calls.

This prevents an old timed-out worker and a new recovery worker from acting at
the same time. Reusing the same artifact workspace is permitted only after the
old attempt is fenced and its container/network resources are stopped.

Idempotency keys for consequential external actions remain a future
enhancement. v0.5 avoids blind whole-task replay when side effects are
uncertain.

## D12. One shared hard LLM spend limit covers the workflow

The first financial budget covers metered LLM spend only. It is owned by the
workflow and therefore includes standalone calls, pipeline stages,
model-using MCP services, parent/child runs, model-call recovery, and worker
continuation:

```text
LLM spend limit                $2.00
Initial model calls            $0.54
Recovery model calls           $0.83
Total metered LLM spend        $1.37
Under limit                    $0.63
```

There is no protected fallback reserve in v0.5. Before each call, AgentPaaS
reserves a conservative maximum based on the input and output cap. After the
call, it reconciles provider-reported usage or cost and returns unused
reservation.
If actual provider cost exceeds that conservative reservation, AgentPaaS shows
the overage truthfully and blocks every later model call; “hard” describes
authorization, not a claim that a provider can never report variance.

The cost model supports:

- Input tokens.
- Cached-input tokens.
- Cache-write tokens where a provider reports or prices them separately.
- Output tokens.
- Provider-reported reasoning tokens and cost where available.
- Per-request charges where applicable.
- Metered, subscription, and local cost bases.

Cache state is not assumed to transfer between models. Preflight calculations
use uncached input unless the provider contract guarantees otherwise.

Local compute and subscription plans may have zero marginal metered LLM spend,
but AgentPaaS does not call them free or include infrastructure cost. Unknown
or stale price data cannot be used for a bounded lowest-metered-cost decision
unless an administrator supplies an explicit approved price record.

## D13. Credential, subscription, and quota failures may use another target

A provider authentication, quota, subscription, or token failure does not
automatically fail the whole task if another approved target has an independent
valid credential and still satisfies policy, time, and budget constraints.

The failed target/credential pair is marked unavailable for the remainder of
the workflow wherever the same approved identity appears. AgentPaaS does not
repeatedly retry the same expired credential in another stage or child.

If no independently authorized candidate remains, the result is terminal
`REAUTH_REQUIRED`, `QUOTA_EXHAUSTED`, or `NO_ELIGIBLE_TARGET`. An operator may
later correct the deployment or credentials and invoke a new run. Automatic
OAuth reauthentication and token refresh are not v0.5 scope.

## D14. Recoverable failures are objective and bounded

Initial model-recoverable conditions include:

- Provider timeout or connection failure.
- Rate limit or retryable provider service error.
- Provider quota/subscription exhaustion when another credentialed target
  exists.
- Context-window rejection when a larger-context candidate exists.
- Output-limit rejection when a compatible target and spend reservation exist.
- Repeated malformed JSON only when the worker explicitly requested
  structured JSON.

Initial terminal stops or explicit operator decisions include:

- LLM spend exhausted.
- Active-time limit exhausted.
- Policy or cloud-transfer denial.
- No eligible independently credentialed target.
- External application failure unrelated to the model.
- Agent exception without a safe checkpoint.
- User cancellation.

Without outcome verification, AgentPaaS does not automatically downgrade below
the declared capability minimum merely because a target is cheaper.

## D15. Loop protection is mechanical, not semantic

v0.5 does not need to understand every possible reasoning loop. It must ensure
that no loop runs indefinitely.

Initial guardrails are:

- Maximum metered LLM spend.
- Maximum accumulated workflow/run active time.
- Maximum LLM calls or iterations.
- Maximum identical repeated governed tool actions.
- Maximum governed actions without checkpoint advancement.
- Stall timeout.
- One model-recovery selection per worker attempt.
- One operator-directed whole-worker continuation.
- Lease revocation when an attempt ends.
- Maximum pipeline stages and handoff bytes.
- Maximum child fan-out, fixed leaf-only depth, and concurrently active
  containers.
- One workflow-level active-time and aggregate LLM spend envelope shared by
  pipeline stages and child runs.

## D16. Operators use structured evidence to choose the next action

The AgentPaaS API and CLI expose an attempt report such as the following.
AgentPaaS skills teach Hermes to interpret the same report:

```text
status: NEEDS_REPLAN
reason: ATTEMPT_TIME_EXHAUSTED

progress:
  phase: test_suite_running
  completed_work: 4 files modified, 3 tests added
  remaining_work: finish tests and summarize
  safe_to_resume: true

time:
  model_activity: 2m10s
  tool_activity: 2m50s

checkpoint: cp-184
spent: $0.31
remaining_llm_budget: $1.69
```

The initial recommendation rules are:

| Evidence | Operator action |
|---|---|
| Active progress at lease expiry | `more_time` |
| Context-window rejection | `larger_context` |
| Repeated unsuccessful cycles without progress | `capability_up` or `split_task` |
| Task is too broad for the remaining envelope | Propose a new or revised workflow envelope; ask before authority or aggregate budget expands |
| Provider outage, timeout, or rate limit | Router handles automatically when possible |
| Current LLM spend or active-time ceiling exhausted | Terminal failure; no continuation or amendment |
| Remaining current ceiling is likely insufficient | Explicitly pause or atomically approve a higher ceiling before terminal exhaustion |
| Policy, credential, or external-system blocker | `stop` or correct the deployment and invoke a new run |

This is same-task adaptation using explicit evidence. It is not cross-task
machine learning.

## D17. Public proof uses ordinary work and labelled fault injection

The first reference agent produces a customer-feedback report through several
model turns and emits progress checkpoints between phases.

Two runs prove one engine:

### Local-first

```text
Customer-feedback report produced
Attempted on an approved local endpoint
Injected local model timeout
Recovered on an approved cloud target
Cloud metered LLM spend: $0.37
$0.63 under the $1.00 LLM spend limit
```

### Cloud-cost-first

```text
Customer-feedback report produced
Started on an approved economical cloud target
Injected eligible model failure
Recovered on an approved higher-capability cloud target
Total metered LLM spend: $1.37
$0.63 under the $2.00 LLM spend limit
```

The demo verifies runtime completion, artifact creation, routing evidence,
guardrails, and cost arithmetic. It does not use an LLM judge or present the
report as independently correctness-certified.

## D18. Preserve scale seams without building a substrate control plane

The v0.3–v0.5 train ships locally but must not require an agent-domain rewrite
to become a managed PaaS. It preserves:

- Logical model routes instead of provider names in worker code.
- Candidate-target and provider-adapter interfaces.
- A replaceable selector with a deterministic first implementation.
- A normalized buffered and streaming model-call envelope.
- A versioned capability and price catalog outside selector logic.
- Durable run, attempt, checkpoint, artifact, and cost-store interfaces.
- Explicit leases and fencing tokens.
- A decision ledger explaining every selection.
- A deterministic policy gate every future selector must obey.
- Structured runtime-to-operator feedback shared by CLI, API, and Hermes.

The first storage implementation may use protected local files and SQLite.
The interfaces must allow a future transactional database, durable event/
artifact stores, and distributed lease manager without changing the SDK or
policy contract.

The same rule applies to workflow coordination: component catalog, runtime
directory, event/artifact stores, workflow scheduler, child-run scheduler, and
aggregate budget ledger are interfaces with local implementations, not
assumptions embedded directly in Docker, Kubernetes, or Cloudflare calls.

## D19. Authority remains deterministic

Routing may choose only inside the administrator-approved envelope. It cannot
silently add a provider, endpoint, credential, cloud location, MCP tool,
network destination, write permission, time, or spend authority.

Receiving a package does not turn portability into authority substitution.
The receiver may map logical credential requirements and approve candidate
endpoints already represented by the signed envelope. Changing the provider,
model, endpoint, or candidate pool requires an explicit reconfiguration/fork,
policy review, and new signed package identity or provenance step.

An authenticated operator may continue a run within its remaining current
limits. A separately scoped administrative control may append an explicit,
idempotent increase to active time, current attempt lease, or LLM spend before
the run becomes terminal. The amendment records exact before/after values,
actor, reason, usage, and time. It does not change provider, credential,
network, MCP, workflow topology, code, or any other authority. Workers,
prompts, ordinary invocation credentials, and Hermes without explicit user
confirmation cannot authorize an amendment.

## D20. Connector breadth is deferred; A2A and MCP interoperability are narrow

AgentPaaS should consume APIs, CLIs, MCP servers, and third-party connectors
where useful, but it does not compete on connector count.

MCP is the interoperability boundary for tools and services. A2A-compatible
adapters are the interoperability boundary for agent tasks, messages, and
events. Both enter through AgentPaaS gateways and map onto canonical
AgentPaaS identities, tasks, leases, events, policies, and artifacts; neither
protocol becomes a second source of truth.

v0.3 includes the narrow A2A path and v0.4 adds the narrow AgentPaaS-native
MCP path. A broad public
marketplace, arbitrary host MCP processes, unconstrained external agents, and
external runtime-coordinator adapters remain deferred. Direct invocation and
lifecycle control through the AgentPaaS CLI/API are first-class and are not
such an adapter.

## D21. Evidence precedes learned routing

The cumulative v0.5 ledger intentionally captures the future learning dataset: route,
candidate exclusions, target, failure, tokens, cache use, cost, latency,
turns, attempt outcome, and explicit user/operator action.

It does not use that history to make v0.5 decisions.

Learned selection, task similarity, task-complexity inference, historical
model scores, turn-count prediction, automatic decomposition, and quality
escalation remain deferred until:

1. The deterministic router has real adoption.
2. Outcome labels are trustworthy.
3. A static baseline exists.
4. Savings and quality can be measured honestly.
5. The learned proposal remains inside the deterministic envelope.

## D22. Managed PaaS follows a bounded portability gate

The sequence is:

1. Prove the complete local Hermes authoring + SDK + independent AgentPaaS
   deployment/runtime experience.
2. Before the remaining release-train control-plane work, run one bounded portability
   slice on Docker, local Kubernetes, and Cloudflare and isolate every
   substrate-specific implementation behind explicit ports.
3. Ship v0.3, v0.4, and v0.5 on Docker without claiming production Kubernetes
   or Cloudflare support.
4. Use the evidence to select one managed substrate and validate paid managed
   pilots after v0.3.
5. Add a customer-owned-cloud adapter when actual demand justifies it.

Cloudflare, Kubernetes, or another platform may become the first managed
adapter. None is the AgentPaaS architecture, and a service mesh is optional
infrastructure rather than product authority.

## D23. Explicitly deferred through v0.5.0

- General-purpose correctness or quality verification.
- LLM-as-a-judge routing.
- Learned or predictive model selection.
- Automatic prompt/task complexity inference.
- Cross-task performance memory and fuzzy task matching.
- Automatic context summarization or compaction.
- Runtime-selected task decomposition. A parent agent may request only
  explicitly approved child agents; the runtime does not invent the child
  plan.
- Composing parent/child fan-out inside a pipeline stage; v0.4 proves the two
  coordination kinds separately, while either may use B33 MCP services.
- Multiple recovery cascades through many models.
- Unrestricted use of every model exposed by an aggregator.
- Automatic capability downgrade below declared requirements.
- Savings claims without a measured baseline.
- Automatic OAuth reauthentication or refresh.
- Generic third-party runtime-orchestrator integrations; direct AgentPaaS
  deployment invocation from external schedulers is supported.
- Arbitrary externally built agent compatibility for Routed Run.
- Full enterprise SaaS action approvals, OAuth broker redesign, generic
  third-party MCP packaging, broad data-flow attestations, or the former B27
  enterprise mega-demo.
- Production Kubernetes/Cloudflare adapters, hosted multi-tenancy, billing,
  fleet management, and public catalog federation. Their contracts and
  feasibility are addressed in v0.3; their managed production implementation
  follows it.

## D24. Release-time decisions that still require explicit approval

The engineering plan is complete, but these values are deliberately not guessed
months in advance:

1. The recommended real local/cloud model roster used in public examples,
   including the catalog price-validity/freshness policy applied to it.
2. The final default model timeout, stall timeout, attempt lease, maximum
   active duration, and recovery margin.
3. Any public latency SLO or managed activation guarantee. v0.3 records
   cold-image, cached-cold, warm, and resident baselines first; a later claim
   requires an explicit evidence-backed approval.

The release train requires deterministic synthetic tests first, then live
calibration, then explicit founder approval before v0.5 model defaults or
names are published. The schemas and runtime are not blocked from being built
because both are configurable and tested independently of the final roster.

## D25. Long-running means authorized duration without hidden ceilings

The current one-request invocation path is replaced for durable runs. No CLI,
plugin, daemon helper, harness default, provider client, CPU rlimit, or socket
read timeout may impose a shorter undocumented lifetime than the effective
active-time and attempt limits.

The release must prove:

- A real 6-minute, 20-plus-turn task lasting longer than every current fixed
  timeout.
- A 30-minute real-time release-lane soak with repeated model/tool turns and
  checkpoints.
- An accelerated fake-clock 24-hour/100-turn run.
- Daemon restart, cancellation, stall, lease expiry, and cleanup during a
  long-running task.

This does not promise infinite execution. The active-time limit is explicit,
configurable, and operator-approved; AgentPaaS proves that it—not an accidental
transport timeout—controls termination. Fully paused and `NEEDS_REPLAN` time
is excluded; drain-to-pause time remains included.

## D26. AgentPaaS containers can expose and consume governed MCP services

An AgentPaaS package may declare an MCP service entrypoint. Another approved
AgentPaaS agent may call it by logical service ID and allowed tool name. The
runtime resolves the endpoint, creates the workflow-scoped internal network,
applies a run-scoped service capability invisible to Python, enforces caller
and service leases, bounds request/response sizes and time, and records the
call.

The service has its own image, policy, credentials, lease, health, and
container. Caller credentials and authority are never inherited by the
service. The Python worker never receives a raw container IP, Docker name, or
service capability. If the real MCP router is unavailable, the call fails
closed; a synthetic success response is never permitted on this path.

## D27. Sequential pipelines are runtime-native

A signed workflow definition may declare an ordered list of AgentPaaS agent
stages. Each stage runs in its own agent and gateway containers. On successful
completion, AgentPaaS durably commits a bounded handoff envelope containing
structured JSON, artifact references and digests, provenance, and explicit
stage status before starting the next stage.

Hermes builds, tests, packages, and deploys the workflow. After deployment, the
AgentPaaS CLI/API may start it without Hermes. AgentPaaS does not copy stage
output through an authoring client or require one to remain online to advance
the sequence. v0.4 supports a linear, fail-fast pipeline with bounded stages;
arbitrary DAGs, cycles, parent/child fan-out inside a stage, compensation
engines, and implicit transfer of process memory or hidden model state remain
deferred.

## D28. Parent agents can perform bounded fan-out and fan-in

A parent worker may use AgentPaaS SDK spawn/join operations to request a
bounded set of policy-approved child agents. Every child receives a separate
run, lease, agent container, gateway container, policy, and durable result.
Spawn is idempotent, join is restart-safe, and the parent receives only
bounded child result envelopes and artifact references before collating and
continuing.

The parent may not name arbitrary images, grant its own credentials or
network authority to a child, raise the workflow time/spend limits, or create an
unbounded recursive tree. v0.4 proves one parent with a bounded set of leaf
children. Every declared child is required for batch success, results are
returned in request order, and a child failure cancels/fences the remaining
batch. Leaf children may use bounded model-call recovery, but do not receive a
separate whole-worker continuation in v0.4. Partial success, deeper recursive
spawning, and arbitrary dynamic graphs are deferred.

## D29. Workflow authority and budgets are inherited by narrowing

Pipelines and parent/child runs share one workflow ID, accumulated active-time
ledger, aggregate active-container limit, and—after the cost ledger is
enabled—one metered LLM spend ledger. A stage or child may receive a smaller
sub-limit but cannot expand the workflow envelope. Only an authenticated
operator limit amendment may raise the workflow-level current ceiling.

Active time is elapsed workflow-active time, counted once while the workflow is
`RUNNING` or `PAUSE_REQUESTED`. Concurrent parents, children, and services do
not multiply that clock; their attempt leases and operation deadlines remain
independent. Fully `PAUSED` and `NEEDS_REPLAN` intervals add zero active time.

Every stage and child still runs under its own signed policy. Authority is the
intersection of the workflow edge, the caller or parent spawn declaration,
and the callee package policy. Context transfer does not transfer credentials,
leases, model-route capabilities, or network authority.

## D30. Deployments are immutable versions behind audited aliases

Hermes is the required release-train environment for authoring, testing, packaging, and
deploying an agent, pipeline, or one-level parent/child workflow. A deployment
resolves to an exact installed package or workflow version plus immutable
signed digests, for example:

```text
customer-report@1.2.0
production/customer-report -> customer-report@1.2.0
```

When an immutable workflow deployment is created, every declared stage, MCP
service, and child-allowlist entry is resolved to an exact installed package
version and signed digest in that snapshot. An accepted invocation records both
the requested reference and the exact resolved deployment, including those
nested identities. Promotion or rollback atomically changes an alias for future
invocations only; no stage, service, or child is re-resolved after admission,
and an alias change never alters an active or historical run.

Deactivation prevents new invocations of the exact deployment, including
through an alias, but does not kill active runs. Cancellation is explicit.
Packages, results, costs, and audit history remain inspectable. Destructive
purge and automated retention policy are deferred beyond v0.5.

## D31. Deployed work is invoked directly and idempotently

The AgentPaaS CLI and authenticated API can invoke an active deployment
without Hermes. External cron, Kubernetes, and another process use that same
API; they provide input and receive a durable run/workflow ID immediately.
They do not relay internal context or coordinate runtime stages and children.

Every durable API invocation supplies an idempotency key. Retrying the same key
with the same canonical invocation intent returns the original run ID. That
intent includes the requested deployment reference, canonical input, initial
maximum active duration, initial attempt lease, mandatory `max_cost_usd`, and
every other creation-time option that can change execution or authority.
Changing any of those fields is a conflict. Lookup is scoped to authenticated
caller plus receiver/tenant namespace, so unrelated callers may safely use the
same key. The CLI generates and displays a key for an ad hoc invocation when
the user does not supply one, while scheduler documentation requires a stable
caller-generated key.

Each deployment has receiver-local `max_concurrent_runs`, default one. It counts
accepted top-level workflows for that exact deployment, not every internal
stage or child as another top-level admission. A workflow holds one slot while
`PENDING`, `RUNNING`, or `PAUSE_REQUESTED`; it releases the slot while fully
`PAUSED`, in `NEEDS_REPLAN`, or terminal, and resume must atomically reacquire
one. The limit is checked atomically with idempotency and workflow creation. A
distinct invocation above the limit receives a visible retryable
`ALREADY_RUNNING` response; v0.3 does not hide accepted work in an implicit
queue. An operator may explicitly configure a higher limit for
concurrency-safe deployments. Visible `READY`/`ALLOCATED` stages or children
inside one already accepted workflow are governed by its aggregate capacity
and are not a hidden queue of top-level invocations.

## D32. Current time and spend ceilings are hard but explicitly amendable

The runtime may never authorize active work beyond the currently committed
workflow active-time ceiling or a physical LLM request whose conservative
reservation would cross the current spend ceiling. If either authorized
ceiling is consumed before a limit amendment commits, the workflow stops and
fences terminally as `EXPIRED` or `BUDGET_EXCEEDED` and cannot be resurrected.
If provider-reported actual cost later exceeds its conservative reservation,
AgentPaaS records the real overage, blocks every later model call, and becomes
terminal; it never clamps or hides a provider-side variance it could not have
prevented.

Before terminal exhaustion, an authenticated administrative control with
`runs:amend_limits` authority may atomically raise an absolute active-time
ceiling, current attempt lease, LLM spend ceiling, or a combination. v0.5
supports increases only. Requests carry an idempotency key and record actor,
reason, old/new limits, usage, active reservations and unreconciled exposure at
approval, time, and the resulting authority generation. A normal invoke
credential cannot amend limits. Hermes may propose the operation but cannot
execute it without explicit user confirmation. A worker, model response,
checkpoint, artifact, or prompt can never authorize it.

When an attempt is active, a current-attempt-lease increase updates that
attempt's effective lease. In `PAUSED` or `NEEDS_REPLAN`, where no attempt may
remain active, it sets the maximum lease for the next authorized
resume/continuation attempt and does not itself launch work or consume time.

Amendment is accepted while `RUNNING`, `PAUSE_REQUESTED`, `PAUSED`, or
`NEEDS_REPLAN`. An atomic state-generation race decides whether amendment or
terminal exhaustion wins. `PAUSED` and `NEEDS_REPLAN` consume no active time
and permit no new model/tool spend; accumulated spend is preserved. Raising a
time/spend ceiling changes no code, route, provider, credential, network, MCP,
or workflow topology authority.

`NEEDS_REPLAN` exists only when a verified safe checkpoint and enough current
or explicitly amended time/spend remain for one allowed continuation. It is
never emitted after terminal time or spend exhaustion. Before it commits, every
workflow-owned worker/service container and capability is fenced or stopped
and every active LLM reservation is committed, released, or converted to a
retained unreconciled exposure; the state never parks a live resource between
decisions.

## D33. Operators can cancel, pause, resume, and restart truthfully

Cancellation is immediate and terminal: AgentPaaS blocks new work, revokes
leases, fences active workers, stops workflow-owned containers and services,
and records `CANCELLED`. It cannot undo an external side effect that was
already accepted.

Pause is cooperative and occurs at the next safe execution boundary, never by
silently freezing an arbitrary container:

- A pipeline may finish and durably commit its active stage, but AgentPaaS
  does not launch the next stage.
- A standalone or parent worker pauses at its next accepted resumable
  checkpoint.
- Active children may finish and store results, but no unstarted child or
  parent continuation launches.

The state sequence is `RUNNING -> PAUSE_REQUESTED -> PAUSED`.
`PAUSE_REQUESTED` continues consuming active time and may incur spend until a
safe boundary is committed. If no safe boundary arrives, it remains requested
until cancellation or a terminal guardrail fires. Once `PAUSED`, all
workflow-owned execution resources are fenced/stopped, active-time accounting
freezes, no new spend is permitted, and any active LLM reservation has been
committed, released, or converted to retained unreconciled exposure.

Resume keeps the same workflow ID, exact deployment version, input, committed
handoffs/results, accumulated spend, remaining active-time ceiling, and audit
history. It never resets authority or recovery counters. Resume revalidates
the pinned deployment's integrity and availability, credentials, policy,
artifacts, checkpoints, and available concurrency before launching work.
Because resume is not a new invocation, admission-only deactivation does not
cancel, re-resolve, or by itself block the accepted run.

Restart is a new invocation, not resurrection of the old run. It first
cancels and fully fences the source if it is in any non-terminal active or
frozen state, then creates a new run ID from stage one using the same exact
deployment version and original input by default. It does not
silently import a checkpoint. Using the deployment alias as it resolves now
requires an explicit option because it may select different code. The new run
receives its own separately approved active-time and LLM spend ceilings and
records `restarted_from` provenance.

## D34. Managed PaaS is the primary post-v0.3 commercial direction

After v0.3, AgentPaaS should validate a paid managed PaaS in which customers
receive help packaging and deploying their agents into a tenant-isolated
service. Local operation remains a first-class open trust and adoption path,
and a customer-owned-cloud adapter remains valuable, but neither displaces the
managed service as the primary commercial hypothesis.

## D35. AgentPaaS owns agent semantics; substrates own infrastructure mechanics

AgentPaaS is an agent-domain control plane for package/deployment identity,
capabilities, tasks, attempts, leases, communication policy, budgets, events,
artifacts, credentials, audit, and metering attribution. Docker, Kubernetes,
Cloudflare, and optional service-mesh components provide compute, networking,
storage primitives, resource isolation, and autoscaling behind adapters. Their
objects do not become the public AgentPaaS API or a second policy authority.

## D36. A portability gate precedes the remaining release-train control-plane work

B28 is inserted before long-running, catalog, coordination, and routing work.
It extracts replaceable runtime/state/event/artifact/gateway/identity/secret/
package/metering ports and proves one bounded vertical slice. It does not ship
a cloud service, production Kubernetes adapter, or service mesh in v0.3.

## D37. One conformance slice runs on Docker, Kubernetes, and Cloudflare

The portability proof uses the same exact signed package and semantic checks
on the shipped Docker adapter, a local Kubernetes environment, and a
Cloudflare feasibility environment. The proof covers admission, isolation,
governed egress, durable state/events/artifacts, restart/fencing, tenant denial,
metering, and cleanup; platform-specific rewrites or weakened assertions are
not evidence of portability.

## D38. The first managed substrate is chosen from evidence

Cloudflare is preferred only if it can support the required immutable workload,
durability, isolation, gateway, credential, event, artifact, metering, latency,
and economics contracts. Otherwise Kubernetes is the primary managed backend
and Cloudflare may serve a narrower edge role. If neither passes, the missing
contract is fixed before building more substrate-specific control-plane code.

## D39. Agent packages are substrate-neutral and immutable

The portable unit is an OCI image by digest plus a signed, versioned AgentPaaS
manifest describing runtime profile, entrypoints, capabilities, policy,
resources, activation, and provenance. Agent source and package metadata do
not contain Kubernetes YAML, Cloudflare configuration, Docker addresses, raw
credentials, or substrate control-plane assumptions.

## D40. Agents and MCP servers share one typed private component catalog

AgentPaaS maintains a tenant-scoped catalog of signed component packages whose
kind is `agent`, `mcp_service`, or a later explicitly approved type. Common
metadata covers identity, exact version/digest, publisher/provenance, runtime
profile, capabilities, policy requirements, schemas, status, and test evidence;
type-specific metadata remains explicit rather than pretending agents and
tools have identical invocation semantics.

## D41. The package catalog is separate from the live runtime directory

Catalog presence means a tested package is available to resolve; it does not
mean a process is running. A separate lease-backed runtime directory records
active/warm/resident instances and routable generations. This separation
supports scale-to-zero, prevents stale endpoint discovery, and keeps logical
identity independent from a machine, container, pod, or session address.

## D42. Hermes proposes registration; AgentPaaS authorizes promotion

After Hermes builds and tests a package, it may submit a signed registration
proposal containing manifests and evidence. AgentPaaS verifies provenance,
policy, schemas, compatibility, and tenant authority before an authorized
actor promotes the exact package into the catalog. A prompt, build profile,
agent, or ordinary invocation credential cannot self-approve availability.

## D43. Capability discovery is bounded and exact versions are pinned

An orchestrator may query for roles such as worker, verifier, or testing agent
only inside its signed workflow constraints: tenant, allowed package/publisher,
capabilities, runtime profile, data class, network region, evidence level,
cost/resource ceiling, and version policy. Resolution is deterministic and
audited, and admission pins exact package and policy digests so later catalog
changes cannot alter an accepted workflow.

## D44. Pipelines and swarms are constrained products, not arbitrary graphs

Catalog discovery enables composition but does not authorize an unconstrained
swarm. v0.4 retains linear pipelines and bounded one-level parent/leaf-child
execution with declared roles, fan-out/concurrency/aggregate limits, narrowed
authority, durable joins, and parent-owned collation. Recursive graphs,
runtime-invented decomposition, free-form peer spawning, and partial-success
semantics remain deferred.

## D45. Agents use logical identities, never machine endpoints

Agents address packages, deployments, services, roles, tasks, runs, and
artifacts through AgentPaaS logical identifiers. Only the trusted runtime
directory and gateway resolve those identifiers to current instances. Worker
code never discovers or invokes a raw localhost port, container address, pod
IP, tmux pane, or Cloudflare object URL.

## D46. Use A2A for agent interoperability and MCP for tool interoperability

A2A-compatible task/message/event adapters are used between agents; MCP is
used for tools and services. AgentPaaS canonical task, run, attempt, lease,
event, policy, and artifact records remain authoritative, and protocol adapters
map to them rather than creating parallel schedulers, identities, or truth.

## D47. External registries are discovery inputs, not trust authorities

Agent Card directories, MCP registries, OCI registries, and future ecosystem
catalogs may seed metadata or package retrieval. Imported records remain
untrusted until AgentPaaS verifies identity/provenance, pins exact content,
runs policy and profile checks, attaches local evidence, and explicitly
promotes them into the tenant catalog.

## D48. Both agent ingress and egress are pinned

Every allowed communication edge names the logical sender, logical receiver,
protocol/action, purpose, data class, size/rate/deadline bounds, and artifact
rights. A call proceeds only when the sender's egress and receiver's ingress
both authorize the same edge in the immutable workflow snapshot; either side
denies by default and every decision is audited.

## D49. All agent communications cross AgentPaaS enforcement

Agent-to-agent messages, MCP calls, model/HTTP calls, signals, event
subscriptions, and artifact transfers cross authenticated AgentPaaS gateways
or broker APIs. Direct container/pod/session networking, shared writable
volumes, host-port discovery, and side channels are not product communication
paths even when a substrate network could technically permit them.

## D50. Prompts and messages are bounded untrusted data

Agent messages use versioned envelopes with sender/receiver/task identities,
schema/content type, classification, size/rate/deadline limits, provenance, and
idempotency. Message or prompt content cannot grant a capability, change a
route, expand time/spend, amend communication policy, approve promotion, or
declare operational success; trusted control operations remain separate.

## D51. Files move through an encrypted artifact broker

Agents transfer files and large data by committing immutable encrypted
artifacts to an AgentPaaS broker and sending references, never by embedding
large blobs in prompts or sharing mounts/storage credentials. Scoped
short-lived grants bind tenant, workflow, sender, receiver, purpose, digest,
classification, size, allowed operation, expiry, and use count; the broker
verifies integrity, audits access, and applies retention/deletion policy.

## D52. Completion and wakeup are durable and event-driven

Task state changes and ordered outbox events commit atomically. Callers and
agents subscribe after a cursor and receive terminal or input events through
streaming/wait primitives; disconnect/restart uses replay and deduplication.
Correctness polling, heartbeat-as-completion, open client requests, tmux panes,
and shell-session liveness are not coordination contracts.

## D53. A service mesh is optional infrastructure, not the AgentPaaS product

Kubernetes plus a mesh may add workload mTLS, transport identity, telemetry,
and network enforcement for a managed deployment. AgentPaaS still owns
capability resolution, two-sided semantic authorization, budgets, tasks,
artifacts, events, and audit. v0.3 evaluates Kubernetes without requiring a
mesh and may measure an optional ambient-mesh experiment.

## D54. Tenancy is a first-class key in every portable contract

Tenant/organization identity scopes packages, aliases, deployments, catalog
queries, tasks, runs, events, artifacts, secrets, communication edges, usage,
and audit from the first portability interface. Cross-tenant list, resolve,
invoke, signal, subscribe, download, and metering access fail closed in tests;
managed multi-tenancy is not retrofitted after local schemas harden.

## D55. The approved v0.3–v0.5 release-train sequence is B26 through B41

B26 durable deployment/state and B27 progress/checkpoint/artifact foundations
are followed by B28 portability; B29 runtime profiles, durable events,
streaming, and activation; B30 long-running proof; B31 catalog; B32 secure
A2A/artifacts; B33 MCP services; B34 pipelines; B35 parent/child; B36 model
catalog/routing; B37 call recovery; B38 shared spend; B39 supervision/control;
B40 Hermes UX; and B41 integrated proof/release. B32 closes v0.3, B35 closes
v0.4, and B41 closes v0.5, with prereleases after B30 and B40. This replaces
the former B28–B37 sequence without changing completed B26/B27 history.

## D56. Agent Runtime Profiles declare required execution features

Every package declares a versioned baseline plus required optional features
such as model/tool streaming, structured output, multi-role messages,
reasoning controls and usage, interactive input, multimodal artifact parts,
and bounded concurrency. Deployments, adapters, and resolved components must
satisfy the profile by set inclusion before start; silent best-effort downgrade
is forbidden.

## D57. Streaming is a first-class durable product capability

The existing buffered `agent.llm(...)` remains compatible, while an additive
streaming call emits ordered output/tool/usage events and exactly one canonical
final result. `InvokeStream` uses the real idempotent durable admission path;
the observer connection does not own the run, and cursor reconnect never
creates or repeats work.

## D58. Streaming is governed incrementally

Credentials and hidden chain-of-thought never enter streams. Byte/event/time
backpressure, token/spend/lease limits, cancellation, fencing, and guardrail
modes apply while bytes arrive. Strict whole-response filters use buffered
release; incremental release is allowed only for explicitly stream-safe
filters. Partial deltas remain uncommitted and cannot become a checkpoint,
recovery input, or successful result.

## D59. Interactive agents use a durable inbox and safe wait state

Authorized users or agents may append bounded messages/approvals to a task
inbox. A waiting worker commits a safe state with no dependency on an open
client connection; an on-demand sandbox may stop and later wake from the inbox
event. Input remains untrusted data and cannot mutate authority, budgets, or
policy.

## D60. Ordinary agents scale to zero; activation is explicit

`on_demand` is the default and retains no live agent process, task capability,
credential, or network authority between tasks, although an exact image may be
cached. `warm` retains a bounded idle sandbox without task authority until
admission; `resident` is an explicitly authorized, continuously metered
service/event consumer. Catalog availability never implies residency.

## D61. Efficiency and latency are release conformance dimensions

Tests separate AgentPaaS admission, queue/claim, image/cache, gateway, sandbox,
first-progress/token, event, artifact, and cleanup overhead from provider/tool
latency. They record p50/p95/p99 cold-image, cached-cold, warm, and resident
modes where supported plus idle/active resources and cost per completed task.
No public latency SLO is invented before the baseline and an explicit approval.

## D62. The cumulative Golden Loop covers the supported agent feature matrix

The cumulative v0.3–v0.5 release proof expands beyond a simple weather agent to include a
weather baseline, multi-turn reasoning/tool use, real streaming with reconnect,
interactive wait/wake, an MCP service/client, a three-stage pipeline, and an
orchestrator resolving separate worker, verifier, and testing agents. It also
proves catalog registration, exact capability pinning, A2A delegation,
two-sided denial, brokered artifacts, event completion, activation modes,
latency/resource evidence, restart, fencing, and cross-tenant isolation.

## D63. Ship the roadmap as three stable GitHub releases

The former single v0.3 big-bang release is split at coherent, independently
usable boundaries: v0.3.0 after B32 for the private component catalog and
secure A2A/artifacts; v0.4.0 after B35 for governed MCP, pipeline, and bounded
parent/child workflows; and v0.5.0 after B41 for the fully proven Durable
Routed Run. Feature additions use minor versions; patch versions remain for
compatible fixes and security updates.

## D64. Use prereleases to expose foundations without overstating stability

Publish `v0.3.0-alpha.1` after B30 so developers can try durable streaming,
reconnect, interactive waits, activation, and long-running invocation. Publish
`v0.5.0-rc.1` after B40 so practitioners can test the integrated router,
recovery, spend, supervision, and Hermes UX before the final B41 proof. Both
are GitHub prereleases and do not replace the stable Homebrew channel.

## D65. Every stable version receives a release-closure gate

Finishing an engineering block does not authorize a tag. B32, B35, and B41
each close their release with cumulative compatibility/migration proof, clean
install, signed checksummed SBOM-bearing artifacts, Homebrew verification,
truth-synced public docs and known limitations, one reproducible quickstart and
GitHub demonstration, attached sanitized evidence, zero open P0/P1 defects,
and explicit approval of the exact commit. Intermediate blocks may produce
engineering reports and demos without claiming a shipped capability.

## D66. B28 executes Kubernetes-first; Cloudflare proof deferred

Founder decision (2026-07-19): the B28 portability gate proceeds with the
local Kubernetes proof as the primary managed-substrate candidate. The
Cloudflare feasibility proof (B28 T05) is deferred from the B28 exit gate
until Kubernetes conformance is recorded and the port contracts are frozen.

This narrows D37 and D38 for B28 only. It satisfies the "founder explicitly
narrows the approved portability decision" clause in the B28 exit gate
(`docs/execution/blocks/b28-summary.md`). B29 is not blocked by a skipped
Cloudflare proof as long as:

- B28 T01–T04 and T06–T07 are complete and recorded;
- the Cloudflare deferral is recorded here (D66);
- the substrate decision record (B28 T07) states Cloudflare as a deferred
  candidate with the specific contract questions still open, not rejected;
- a later block re-opens the Cloudflare proof before any managed-service
  release decision claims parity.

Cloudflare remains a candidate under D38. This deferral does not anoint
Kubernetes as the shipped managed substrate; B28 T07 still records evidence
and the open feasibility questions for Cloudflare. The Docker adapter remains
the only shipped v0.3 production runtime.

## References informing the direction

- [OpenRouter model fallbacks](https://openrouter.ai/docs/guides/routing/model-fallbacks)
- [OpenRouter provider routing](https://openrouter.ai/docs/guides/routing/provider-selection)
- [LangGraph persistence and checkpoint recovery](https://docs.langchain.com/oss/python/langgraph/persistence)
- [Temporal Python SDK heartbeats](https://github.com/temporalio/sdk-python)
- [OWA orchestrator, worker, and verifier pattern](https://github.com/parvezsyed/agentic-swarms)
