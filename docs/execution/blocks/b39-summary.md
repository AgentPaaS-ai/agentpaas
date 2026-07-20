# Block 39 — Integrated Supervision, Guardrails, Operator Control, and Continuation

**Status:** EXECUTION-READY SPEC — trimmed per architecture audit Fix 5
(2026-07-19)
**Date:** 2026-07-18; revised 2026-07-19
**Trim note:** v0.5 ships cancel, one failure continuation, fencing,
guardrails, and structured reports. Cooperative pause/resume (T07 pause/
resume portions) and administrative limit amendments (T07 amendment
portions, B38 AmendLimit activation) are DEFERRED to a post-v0.5 minor
release. B26 schemas already reserve `PAUSED`/`NEEDS_REPLAN` states and
amendment records, so deferral is additive, not a schema break. The
amendment-vs-exhaustion, pause-drain fencing, and resume capacity races are
removed from the v0.5 proof surface. Everything below is annotated where
the trim applies.
**Target release:** v0.5.0
**Depends on:** B27–B38 complete; all corresponding gates green
**Must complete before:** B40 and B41
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65 as
narrowed by Fix 5 (D32/D33 amendment and pause/resume portions deferred)

## Outcome

B39 combines the B30 durable execution path, B33 service lifecycle, B34/B35
workflow patterns, B37 call router, B38 workflow ledger, and B27 checkpoint
protocol into one Routed Run state machine. It extends those proven primitives;
it must not replace them with a second supervisor or scheduler. At block
completion:

- A routed workflow owns one accumulated active-time ledger and one shared B38
  LLM spend ledger; each has an original and current ceiling plus append-only
  administrative amendments. A standalone run is a one-node workflow.
- Each stage, parent, child, service, run, and worker attempt owns the scoped
  lease and isolated Docker resources defined in B30–B35.
- Model calls, governed tools, progress, checkpoints, and artifacts are tied
  to the active lease.
- Model-call timeout, worker stall, attempt lease, and maximum active duration are
  separate and consistently enforced.
- Authenticated progress and governed activity drive liveness.
- Repeated actions and actions without checkpoint advancement stop
  deterministically.
- Attempt termination produces a complete structured report.
- Runs with a valid safe checkpoint and enough current time/spend can enter
  `NEEDS_REPLAN`; active-time and spend accumulation stop there.
- Authenticated CLI/API, or Hermes after explicit user confirmation, may
  continue the same run once with `more_time`,
  `capability_up`, or `larger_context`.
- The old attempt is fenced and fully stopped before the second starts.
- Operators can cancel terminally, request cooperative pause, resume from a
  safe boundary, or restart as a new run pinned to the original exact
  deployment by default.
- A scoped `runs:amend_limits` action can atomically raise absolute active-time,
  current-attempt lease, and/or LLM spend ceilings before terminal exhaustion;
  no agent or ordinary invocation token can do so.
- A daemon restart reconciles workflow, service, stage, child, and attempt
  state and never blindly repeats side effects or relaunches completed nodes.
- B29 activation policy is enforced: ordinary workers scale to zero after
  terminal or safe external-wait boundaries; warm sandboxes retain no task,
  route, network, or credential authority; resident services are explicit and
  continuously metered.
- B29/B32 durable subscriptions wake waits, joins, approvals, and completion;
  supervisor correctness contains no constant status polling or tmux session.
- Existing legacy runs remain on their v0.2.3 execution path.

B39 is the first block in which all already-proven runtime mechanisms and the
model router operate together. It is still not publicly released until B40
authoring/lifecycle UX and B41 proof gates pass.

## Locked run and attempt model

### Routed workflow and run

A routed workflow begins when B26 atomically admits an invocation of an active
exact/alias deployment. The B26 workflow record pins:

- Requested deployment reference, resolved exact deployment/version/digests,
  invocation/input digest, idempotency identity, and optional
  `restarted_from` provenance.
- Workflow snapshot plus every agent/image/lock and policy digest.
- Route and catalog snapshot.
- Workflow input digest.
- Original/current LLM token/spend, active-time, and aggregate resource
  ceilings plus authority/amendment generation.

Every routed worker run pins its workflow/node identity and attempt purpose:
`INITIAL`, `FAILURE_CONTINUATION`, or `OPERATOR_PAUSE_RESUME`. At most one
`FAILURE_CONTINUATION` is allowed. Administrative pause/resume may create a
new fenced execution attempt but never resets failure-continuation, call,
guardrail, spend, active-time, or workflow counters. Whole-worker failure
continuation is available to a standalone run, active pipeline stage, or
parent run. Leaf children receive B37 call-level recovery but no independent
whole-worker failure continuation; a terminal child failure follows B35's
all-required rule.

`RUNNING` and `PAUSE_REQUESTED` accrue active time. Fully `PAUSED` and
`NEEDS_REPLAN` do not. Queueing/launching nodes, active child joins, and active
MCP operations accrue according to the B30 segment ledger. No transition
resets consumed time or spend.

### Worker attempt

Each attempt has:

- Random attempt ID and lease ID.
- Attempt lease start/expiry.
- Model-call timeout and stall timeout.
- Attempt-local selected/sticky target state.
- Authenticated progress journal.
- The run-level B27 writable artifact workspace, remounted only after the old
  attempt is fenced, plus read-only B34/B35 workflow artifact projections.
- Independent agent/gateway containers and networks.

An attempt ends in:

- `SUCCEEDED`
- `NEEDS_REPLAN`
- `FAILED`
- `FENCED`
- `CANCELLED`

An operator pause fences the active attempt with reason `OPERATOR_PAUSE` only
after a validated safe checkpoint/boundary; the run/workflow, not that attempt,
holds `PAUSED`.

Returning from `@agent.on_invoke` is structurally `SUCCEEDED` only after any
required B34 handoff or B35 batch contract is satisfied. No correctness
verifier is called.

### Active resource limits

B39 reuses B30–B35 deployment and workflow counters. It does not reinterpret
the existing maximum of three active runs as three workflows with unlimited
children. Every worker/gateway pair and B33 service counts toward the aggregate
active-container ceiling, while each active worker attempt also consumes an
active-attempt slot. A run in `PAUSED` or `NEEDS_REPLAN` has no running
containers/services/capabilities or active/in-flight LLM reservation and does
not consume an active-attempt or deployment-concurrency slot. Retained B38
unreconciled exposure may remain visible and reduce headroom, but it authorizes
no request. Resume must atomically reacquire capacity or leave the run frozen.

## Locked time semantics

For every routed workflow node:

```text
effective model-call deadline =
min(model_call_timeout, attempt_lease_remaining, active_time_remaining)

effective invoke deadline =
min(attempt_lease_remaining, active_time_remaining)
```

These conflicting fixed limits were removed and regression-tested in B30:

- Daemon auto-invoke context fixed at 2 minutes.
- Inner `urllib` invoke request fixed at 60 seconds.
- Harness invoke default of 5 minutes.
- Harness wall budget default of 120 seconds.
- Model client timeout fixed at 120 seconds.

The workflow owns exactly one accumulated active-time segment. While workflow
state is `RUNNING` or `PAUSE_REQUESTED`, elapsed time advances once even if a
parent, several children, and an MCP service overlap. Node/attempt leases and
operation deadlines are still independent, but their overlapping durations are
never summed into the workflow ceiling.

B39 must consume the B30 active-time/deadline API and tests. It may not add a router,
provider, MCP, pipeline, child-join, plugin, or CLI timeout that can become a
shorter hidden lifetime ceiling. Legacy defaults remain compatible. Routed
execution derives all limits from the current authority generation, B30
active-time ledger, and authoritative daemon clock.

Public CLI/API invocation starts a run asynchronously and polls structured
state with Hermes absent. A Hermes lifecycle client uses the same API; no
client subprocess timeout is the worker attempt lease.

## Locked liveness semantics

These update mechanical activity:

- Authenticated `agent.progress(...)`.
- Model call start/end.
- Governed HTTP start/end.
- Governed MCP start/end.
- Valid artifact commit/checkpoint.

Plain stdout/stderr and an open process do not prove liveness.

A progress heartbeat is not necessarily semantic advancement. Semantic
advancement requires a new safe checkpoint whose completed/remaining/action
or artifact digest differs from the previous safe checkpoint.

## Locked guardrail fingerprints

Use canonical, redacted action fingerprints:

- LLM: route ID + prompt digest + requirements digest.
- HTTP: method + normalized URL + body digest + credential ID.
- MCP: server ID + tool + canonical input digest.
- Artifact commit: relative path + digest.
- Pipeline handoff: from/to node + handoff/result digest.
- Child spawn: child package list + canonical batch request digest.
- Child join: batch ID + terminal result-set digest.

The repeated-action guard counts identical governed actions since the most
recent semantic advancement. The no-progress guard counts all governed actions
since semantic advancement.

No raw prompt, body, credential, or MCP input enters the fingerprint ledger.

## Locked fencing sequence

Before another attempt starts:

1. Atomically mark the old lease revoked in durable state.
2. Cancel the invoke/model/tool contexts.
3. Stop the old gateway and agent containers.
4. Verify containers are stopped/removed and networks detached.
5. Reject/ignore every late progress record.
6. Only then create a new attempt and lease.

If resource stop/fencing cannot be confirmed, continuation or transition to
fully `PAUSED` fails closed.

Cancellation cannot undo an external side effect already accepted upstream.
The attempt report must state this limitation. Arbitrary idempotency for
external actions is deferred.

## Operator lifecycle and limit-amendment contract

All controls use B26 authenticated, generation-checked, idempotent requests.
An ordinary invoke credential cannot call administrative controls.

### Cancel

- Accepted from `RUNNING`, `PAUSE_REQUESTED`, `PAUSED`, or `NEEDS_REPLAN`.
- Atomically sets terminal cancellation intent before any new launch.
- Revokes workflow/node/service/attempt leases, cancels contexts, fences/stops
  active containers, reconciles reservations, and records `CANCELLED`.
- Wins deterministically over pause, resume, continuation, amendment, and late
  success. Repetition returns the same result.

### Pause

- Atomically changes desired state to `PAUSE_REQUESTED`; every scheduler checks
  it before a new stage, child, service, continuation, or attempt launch.
- Already-active attempts/children and already-bound services may continue
  governed model/tool work under their existing lease and limits only to reach
  the approved safe boundary. The pause request grants no new authority and is
  exposed through the progress/control channel.
- A pipeline active stage may finish and commit its handoff, but the atomic
  transition records workflow `PAUSED` rather than next-stage `READY`.
- A standalone or active parent pauses at its next accepted B27 safe
  checkpoint; then its attempt is fenced with `OPERATOR_PAUSE`.
- Active B35 children may finish and commit results. Unstarted children do not
  launch and the parent does not wake/continue.
- `PAUSE_REQUESTED` continues active-time and spend accounting while existing
  work drains. A failure remains failure. If no safe boundary arrives, the
  state stays requested until cancel or another terminal guardrail wins.
- `PAUSED` commits only after all workflow-owned worker/service resources and
  capabilities are stopped/fenced and every LLM reservation is committed,
  released, or converted to conservative unreconciled exposure. Only then do
  time and new spend stop.

### Resume

- Accepted only from `PAUSED`; `NEEDS_REPLAN` uses continuation, not resume.
- Revalidates exact deployment integrity/availability, original input,
  policy/route/catalog snapshots, credentials, safe checkpoint/handoff/child
  state, current ceilings, and B26 deployment/workflow concurrency.
  Admission-only deactivation does not cancel, re-resolve, or by itself block
  this accepted invocation.
- Capacity acquisition and state transition are atomic. `ALREADY_RUNNING`
  leaves the workflow `PAUSED` with time still frozen.
- A pipeline launches only its deferred next stage. A parent launches only
  unstarted children or resumes the parent from retained results. A
  standalone/parent checkpoint resume creates purpose
  `OPERATOR_PAUSE_RESUME` after old-attempt fencing.
- It preserves active-time/spend/recovery/control counters and uses a fresh
  lease/capability generation.

### Restart

- Creates a new B26 invocation/workflow/run; it never changes the source run's
  terminal identity or limits.
- If the source is active/frozen, cancellation and full fencing commit before
  new admission.
- Default deployment/input are the source run's exact pinned version and
  original input. Current alias resolution requires an explicit option.
- Starts from stage one without implicit checkpoint import, receives new
  separately approved initial ceilings/idempotency key, and records
  `restarted_from`.

### Amend current limits

- Accepted only from `RUNNING`, `PAUSE_REQUESTED`, `PAUSED`, or
  `NEEDS_REPLAN`, only with `runs:amend_limits`, explicit user confirmation,
  reason, expected authority generation, and idempotency key.
- Uses absolute increase-only values for maximum active duration, current
  attempt lease, and/or B38 LLM spend ceiling. The CLI may calculate a new
  absolute value from user-friendly “add” input; the API/store never persists
  a relative increment.
- Atomically appends before/after values and actor/usage/reservation evidence,
  advances authority generation, and updates active timers/reservation
  capacity without restarting a running worker.
- A current-attempt-lease increase updates the active lease while running. In
  `PAUSED` or `NEEDS_REPLAN` it constrains the next authorized
  resume/continuation attempt and does not launch one.
- Exact replay returns the original amendment. Changed replay, decrease,
  overflow, missing scope/confirmation, worker/trigger request, or terminal
  run fails closed.
- Amendment and active-time/spend exhaustion race on one state/authority
  generation. If terminal exhaustion commits first, the run cannot be amended
  or resurrected.

## Continuation contract

A continuation is accepted only when:

- Run status is `NEEDS_REPLAN`.
- No continuation has already been used.
- Latest checkpoint is `safe_to_resume` and passes digest/snapshot checks.
- Old attempt is fenced.
- Current active time and LLM spend remain after any committed amendment.
- Recovery action is allowed and does not broaden signed policy.
- Request is idempotent.

Actions:

- `more_time`: grants a new attempt lease within remaining current active time.
- `capability_up`: raises the minimum by one configured tier; selector chooses
  the exact target.
- `larger_context`: raises the minimum to the next eligible context boundary
  supported by the approved recovery pool.

Attempt-2 target behavior is deterministic:

- `more_time` keeps the original capability/context floor and uses the normal
  primary phase, excluding every target/credential already marked unavailable
  for the workflow. It does not force an unnecessary model change.
- `capability_up` and `larger_context` use the recovery candidate role with
  the raised requirement.
- Exact selection still happens on the first real normalized call; no control
  client supplies a target.
- The failure-continuation attempt receives its own allowance of at most one
  automatic call-level recovery, as specified by per-attempt policy. It is
  still the only failure-driven whole-worker continuation even if prior
  operator pause/resume segments exist.

`split_task` is an authoring decision that proposes a new/expanded deployment
or independently approved runs outside the current continuation. Hermes may
help create it, but it does not continue this run and is distinct from a
predeclared B35 child batch. `stop` maps to cancellation.

For an active B34 pipeline stage, `NEEDS_REPLAN` pauses the workflow without
advancing the next stage. Continuing the stage retains the same workflow node
and committed incoming handoff. Only its eventual success may advance the
pipeline. For a parent, continuation recovers an existing B35 batch/result by
spawn idempotency key; it may not duplicate children. Leaf-child attempts are
not continuable in v0.5.

Provider, endpoint, credential, cloud transfer, topology, or aggregate
resource expansion is never performed by this continuation API. Time/spend
ceiling increases use the separate administrative amendment contract and
never occur implicitly as part of continuation.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Activate routing on the B30–B35 durable state machines | B30–B38 | standalone, pipeline, parent, and child calls use one integrated path |
| 2 | T02 Bind routed operations to B30 time controls | T01 | fake-clock and real slow-call tests prove no new hidden ceiling |
| 3 | T03 Integrate B30 liveness/stall across routed operations | T01, T02, B27 | model/MCP/pipeline/join activity matrix passes |
| 4 | T04 Implement repeated-action/no-progress guardrails | T03 | exact fingerprint and checkpoint-advancement tests pass |
| 5 | T05 Enforce hierarchical lease fencing across all governed surfaces | T01–T04 | stale workflow/node/attempt can perform zero post-fence actions |
| 6 | T06 Produce workflow/node/attempt reports and terminal mapping | T02–T05 | every reason maps to one honest scope and action |
|| 7 | T07 Activate cancel and restart; DEFER pause/resume and limit amendments | T05, T06 | cancel/restart state-race matrix passes; pause/resume and amendment paths return typed `feature_not_enabled` |
|| 8 | T08 Activate one idempotent eligible-node failure continuation | T05–T07 | same node gets one safe recovery attempt; child/second denied |
| 9 | T09 Reconcile integrated workflow crashes and resources | T01–T08 | no blind stage/child/call replay, false pause, or orphan |
| 10 | T10 Calibrate timeout/lease/active-time defaults on all supported shapes | T02–T09 | evidence report produced; final defaults await approval |
| 11 | T11 Block gate and adversary review | T01–T10 | `make block39-gate` passes |

## T01 — Activate routing on the durable workflow/run/attempt state machines

### Goal

Wire B36–B38 routing into the B30 durable supervisor and B33–B35 workflow
controllers while leaving legacy execution untouched. Do not introduce a new
run store, supervisor, workflow scheduler, service manager, or child scheduler.

### Required work

1. Reuse the B30 `Supervisor` interface and durable invocation job/control
   journal as the only worker execution path for routed nodes.
2. For an initial routed standalone run or workflow node:
   - Consume the exact deployment/invocation identity already admitted by B26;
     never resolve its alias again.
   - Validate route/catalog/credential/current-spend/current-active-time
     preflight.
   - Load the B26-admitted workflow/node/run and durable `READY` intent; never
     create a second run.
   - Claim `READY` atomically with creation of attempt 1, its lease, and job.
   - Preserve the already returned invocation/workflow/run IDs; expose the
     attempt ID asynchronously after claim.
   - Provision and invoke in a supervisor goroutine.
3. Move per-attempt resource creation/cleanup out of the monolithic `Run`
   handler into explicit lifecycle methods.
4. Pass immutable route snapshot, B38 workflow budget ledger reference,
   workflow/node/attempt metadata, progress journal state, and artifact mount
   to the harness.
5. Enable B37 routed `agent.llm()` only on this path.
6. Persist every state transition before publishing it.
7. Publish event-bus and audit records after durable state succeeds.
8. Reuse one idempotent finalizer per attempt/run plus the B33–B35 service,
   stage, batch, and workflow finalizers.
9. Keep `trackedRun` compatibility adapter only for legacy runs; do not make
   it the Routed Run source of truth.
10. A routed agent with no invocation result cannot be marked succeeded by
    `Stop`.
11. Enforce B35 workflow aggregate counters and active-attempt limits
    atomically before every routed node, stage, service, and child launch.
12. Exercise five paths through the same integration seam: one standalone
    worker, one model-using B33 MCP service, one B34 pipeline stage, one B35
    parent call, and one B35 leaf-child call. Prove service-originated model
    routing/recovery uses the caller workflow ledger and fences with the
    service lease; the child path must fail closed for whole-worker
    continuation.
13. Exercise initial public CLI/API invocation with Hermes absent and prove
    ordinary invoke authority cannot call lifecycle/admin-amendment internals.

### Likely files

- existing B30 supervisor/routed-run packages
- existing B33 service and B34/B35 workflow-controller packages
- `internal/daemon/control_handlers.go`
- `internal/daemon/stub_handlers.go`
- runtime resource manager
- daemon tests

### Tests to write first

- Initial routed run state sequence.
- Success return creates terminal result and cleans resources.
- Agent exception maps to failure.
- Duplicate finalization.
- Concurrent active-attempt limit.
- Failure at every resource-creation stage leaves no orphan.
- State write failure prevents event/resource progression.
- Legacy run path fixture remains unchanged.
- Pipeline stage retains its incoming handoff and advances only after success.
- Parent resumes an existing batch/result without duplicating children.
- Concurrent sibling calls share the B38 workflow ledger.
- A model-using MCP service routes and recovers through B36–B38, shares the
  caller workflow spend/token ledger, and cannot call after service fencing.

### Exit gate

A routed standalone worker, model-using MCP service, pipeline stage, parent,
and leaf child each complete through their existing durable state; all share
the correct workflow controls/ledger and all resources are reclaimed.

## T02 — Bind routed operations to B30 active-time controls

### Goal

Make every routed model, MCP, pipeline, child-join, and continuation operation
consume B30's authoritative active-time/deadline API without introducing a new
premature kill.

### Required work

1. Reuse B30's injected `Clock` and timer factory in router/integration tests.
2. Derive effective deadlines from the locked formulas and current workflow
   active time remaining.
3. Assert the routed daemon and inner invoke-helper remain free of fixed
   lifetime timeouts already removed in B30.
4. Configure the harness invoke deadline from attempt lease/active time.
5. Configure B37 model client from model-call/effective deadline.
6. Ensure a model-call timeout does not automatically end the worker when B37
   can recover.
7. Ensure attempt lease expiry cancels current model/tool work and ends the
   attempt.
8. Ensure current workflow active-time exhaustion always wins terminally
   across an active stage, child batch, service call, or `PAUSE_REQUESTED`.
   Fully `PAUSED` and `NEEDS_REPLAN` stop the active-time segment and do not
   expire merely because wall time passes.
9. Enforce one recovery-margin state machine: before
   `FAILURE_CONTINUATION`, normal calls must leave the configured margin; B37
   call recovery does not reserve it again; continuation admission releases it
   exactly once into the final attempt; no second margin remains. Preflight
   rejects a policy with no possible margin.
10. Record workflow/node/operation scope, which clock boundary fired, and exact
    observed/limit durations.
11. Use monotonic time for duration decisions and UTC wall time for evidence.
12. When an authorized T07 time amendment commits during `RUNNING`, update the
    current active-segment deadline atomically without forgiving consumed
    time. If exhaustion commits first, cancellation/fencing continues and the
    amendment is rejected.

### Tests to write first

- Slow model below call timeout succeeds.
- Model call timeout routes within same attempt.
- Active worker below lease continues.
- Lease expires before active-time ceiling.
- Active-time ceiling is consumed during model call/tool work/pause-request
  drain.
- Arbitrary wall-clock advance while fully paused/needs-replan consumes zero;
  resume continues with the same remaining active time.
- Stall and lease fire simultaneously with deterministic precedence.
- Cancellation wins over recovery.
- No 60-second/2-minute fixed limit remains on routed path.
- Pipeline stage and child join cross the old limits while progress continues.
- Active-time exhaustion cancels parent, active children, and bound MCP services.
- Normal call admission preserves recovery margin and B37 call recovery does
  not double-reserve it.
- Running time amendment/exhaustion race has one durable winner.
- Legacy default timings unchanged.

### Exit gate

Each timer can be triggered independently in fake-clock tests and measured in
one bounded real integration test.

## T03 — Integrate B30 liveness and stall across routed operations

### Goal

Preserve B30's distinction between a legitimately slow worker and a stalled
one while routing, MCP calls, handoffs, child spawn/join, and continuation are
active.

### Required work

1. Feed B37 model, B33 MCP, B34 handoff, and B35 spawn/join events into the B30
   supervisor's existing authenticated governed-activity channel.
2. Track:
   - Last mechanical activity.
   - Last heartbeat.
   - Last safe semantic checkpoint.
   - Time by model, HTTP, MCP, and other worker activity where observable.
3. Reset stall timer only on an accepted event from the active lease.
4. At stall timeout:
   - Revoke/fence the attempt.
   - Use latest safe checkpoint if present.
   - Report `STALL_TIMEOUT`.
   - Set `NEEDS_REPLAN` only when remaining envelope and resume capability
     permit it; otherwise fail terminally.
5. A long model call is governed by the model-call timeout and counts as
   active while in flight; do not fire stall first unless configured
   incorrectly.
   Every governed HTTP/MCP operation must likewise have an effective timeout
   bounded by the attempt lease and active time remaining; an in-flight event cannot
   suppress stall forever.
6. A B35 join counts as activity only while the durable batch is changing or
   the bounded join heartbeat is accepted; it cannot suppress the workflow
   active-time limit. Waiting on a child or unhealthy MCP service cannot
   suppress stall forever.
7. A long local tool phase requires progress heartbeats. Document this in the
   SDK and Hermes authoring template.
8. Ignore stdout spam, unauthenticated journal writes, and late lease events.
9. Bound heartbeat rate and coalesce display events without losing the latest
   durable state.

### Tests to write first

- Regular heartbeat prevents stall.
- Governed model/HTTP/MCP activity prevents stall.
- Valid pipeline handoff and changing child-batch state count as governed
  activity; an unchanged join loop eventually stalls or reaches its operation
  deadline.
- stdout-only loop stalls.
- Forged progress does not prevent stall.
- Heartbeat without safe checkpoint yields no resume claim.
- Safe checkpoint yields `NEEDS_REPLAN`.
- Event at boundary has deterministic ordering.
- Excessive heartbeat flood bounded.

### Exit gate

The B30 watchdog neither kills an active governed routed/workflow operation
prematurely nor lets an idle, unchanged-join, or spam-only worker run
indefinitely.

## T04 — Implement repeated-action and no-progress guardrails

### Goal

Stop mechanical loops without an LLM verifier.

### Required work

1. Generate the locked fingerprint for every governed action.
2. Track:
   - Consecutive/rolling identical count since semantic advancement.
   - Total governed action count since semantic advancement.
3. Reset both only after an accepted safe checkpoint with changed semantic or
   artifact digest.
4. Stop at:
   - `max_identical_tool_actions`
   - `max_actions_without_progress`
   - `max_llm_calls`
   - existing token/spend limit
   `max_llm_calls` counts logical SDK calls. Record physical requests
   separately; because only one call-level recovery is allowed per attempt,
   physical requests are additionally bounded by logical calls plus one per
   attempt.
   B34 handoff commits and B35 spawn batches retain their own exact-once and
   count bounds; repeated rejected calls still count toward no-progress.
5. Emit:
   - `REPEATED_ACTION_GUARDRAIL`
   - `NO_PROGRESS_GUARDRAIL`
6. Include the redacted fingerprint type/count, not raw input, in reports.
7. If a safe checkpoint and envelope remain, report `NEEDS_REPLAN`; otherwise
   terminal failure.
8. Do not interpret text similarity, reasoning content, factual quality, or
   whether a repeated action was “smart.”
9. A checkpoint that repeats identical semantic/artifact state does not reset
   counters.

### Tests to write first

- Repeated identical HTTP/MCP/LLM fingerprints.
- Repeated identical handoff/spawn/join fingerprints.
- Different body/prompt digest is not identical.
- Alternating two-action loop reaches no-progress bound.
- Safe changed checkpoint resets.
- Duplicate checkpoint does not reset.
- Artifact digest advancement resets.
- Raw prompt/body/input absent from report.
- Bound off-by-one behavior.
- Worker cannot set counters through payload.

### Exit gate

Every configured loop has a deterministic maximum, and no semantic judge is
used.

## T05 — Enforce hierarchical lease fencing

### Goal

Prevent overlap and post-expiry action by stale workflow, node, service,
batch, run, or attempt generations.

### Required work

1. Check active lease in:
   - Model call entry and before recovery.
   - HTTP and credentialed HTTP.
   - MCP.
   - Progress/checkpoint.
   - Artifact metadata commit.
   - MCP service registration/call.
   - Pipeline handoff/advance.
   - Child allocation/spawn/join/result delivery.
2. On lease expiry/revocation:
   - Return `LEASE_EXPIRED`.
   - Cancel active contexts.
   - Reject late journal records.
3. Reuse the B30 locked attempt fencing sequence and cascade workflow/node
   revocation through B33 services, B34 active stage, and B35 parent/children.
4. Verify Docker stop/remove and network detach before another attempt.
5. Rotate:
   - Attempt lease ID.
   - Progress journal key.
   - Model-route capability.
   for the replacement attempt.
6. Never mount the old attempt’s control sidecar into the new attempt.
7. Preserve only approved durable checkpoints/results/artifacts after stop is
   confirmed. Never remount another node's writable artifact directory.
8. If any old resource cannot be fenced, mark run failed/platform-blocked and
   do not continue.
9. Record a fencing audit sequence and evidence refs.

### Tests to write first

- Every governed RPC after revocation.
- In-flight model/HTTP/MCP cancellation.
- Late progress and artifact commit.
- Old model-route capability against new gateway.
- New attempt blocked until old resource confirmation.
- Stop failure prevents continuation.
- Revocation/terminal race.
- Cross-run lease/capability.
- Old workflow/node generation attempts MCP call, handoff, child spawn, join,
  or result delivery.
- Workflow fence cancels active stage, children, and bound services before any
  continuation starts.
- No control token in Python/env/logs.

### Exit gate

Adversary tests demonstrate zero accepted governed actions at every descendant
scope after the applicable fence point.

## T06 — Produce workflow/node/attempt reports and map outcomes

### Goal

Give CLI/API clients, operators, and an optional Hermes session complete
objective evidence at the correct deployment, invocation, workflow, node, and
attempt scope without requiring log scraping.

### Required work

1. Populate the B26 `AttemptReport` on every terminal path and derive durable
   node/run/workflow reports from B33–B35 state.
2. Include:
   - Requested deployment reference, resolved exact version/digests, invocation
     identity, input digest, and `restarted_from` provenance.
   - Status/reason/scope/disposition.
   - Desired control state and append-only cancel/pause/resume/restart history.
   - Latest semantic progress.
   - Safe checkpoint/resume capability.
   - Artifacts.
   - Original/current maximum active duration, consumed/frozen/remaining
     active time, current-attempt lease, and exact boundary.
   - Original/current workflow spend/token ceiling, amendment history,
     active reservations, unreconciled exposure, and node contribution.
   - Model decisions/failures/sticky target.
   - Guardrail counts.
   - Authority generation and concurrency/capacity disposition.
   - Allowed recommended actions.
   - Evidence refs.
3. Use this outcome matrix:

| Condition | Run outcome |
|---|---|
| Handler returned | `SUCCEEDED` |
| Non-final stage returned without valid handoff | node/workflow failure `HANDOFF_MISSING`; do not advance |
| Pipeline stage needs replan + safe checkpoint/current envelope | `NEEDS_REPLAN` at the same node; next stage remains blocked and active time/spend are frozen |
| Required child fails | batch and parent attempt fail with causal child reason; siblings cancel/fence |
| Parent resumes with existing child batch/result | reuse same batch/result; never respawn |
| Leaf child requests whole-worker continuation | denied/terminal under v0.5 child policy |
| Attempt lease ends + safe checkpoint + enough current time/spend for one continuation | `NEEDS_REPLAN`, recommend `more_time` |
| Context failure + safe checkpoint + larger approved target | `NEEDS_REPLAN`, recommend `larger_context` |
| No-progress/repeated action + safe checkpoint + stronger target | `NEEDS_REPLAN`, recommend `capability_up` or `split_task` |
| Objective lease/no-progress boundary plus remaining-work evidence | `NEEDS_REPLAN`; a client may propose `split_task`, but worker prose alone cannot authorize it |
| Pause requested while work is draining | `PAUSE_REQUESTED`; active time and any real spend continue |
| Valid pause boundary plus complete fencing/reservation reconciliation | `PAUSED`; no active capability/resource, active-time accrual, or new spend |
| Resume cannot reacquire concurrency/capacity | remains `PAUSED` and returns retryable `ALREADY_RUNNING`/capacity reason |
| Authorized increase commits before exhaustion | same run continues under a new authority generation; report old/new absolute values |
| Current active-time ceiling commits first | terminal `EXPIRED`; amendment/continuation cannot resurrect |
| Current spend ceiling commits first | terminal `BUDGET_EXCEEDED`; amendment/continuation cannot resurrect |
| Policy failure or user cancel | terminal; cancellation is `CANCELLED` |
| Restart accepted | source is terminal/fenced as required; a new invocation is linked by `restarted_from` |
| Credential/quota with no independent target | terminal reauth/quota reason |
| Aggregator model or reported-upstream identity mismatch | terminal provider-contract/policy violation |
| External dependency failure | terminal external blocker |
| Agent/code exception with safe checkpoint | terminal code-failure report; use the existing fix-code/new-run path, not automatic capability escalation |
| Agent exception without safe checkpoint | terminal |
| Platform/fencing failure | terminal |

4. The runtime recommends only actions supported by objective fields. An
   authorized operator/client chooses among allowed actions; Hermes uses the
   same API and cannot add authority.
5. Do not mark a fluent result correct.
6. Extend summarize/explain/timeline/next-action from the same report objects,
   including deployment, invocation, control, amendment, service, stage,
   handoff, parent, batch, and child identifiers.
7. Treat agent-authored progress text as untrusted evidence; it cannot replace
   control fields or authorize continuation, lifecycle control, or amendment.
8. Map internal terminal reasons to stable user/operator categories without
   changing the ledger reason:
   - `MODEL_AUTH_UNAVAILABLE` -> `REAUTH_REQUIRED`
   - `MODEL_QUOTA_EXHAUSTED` -> `QUOTA_EXHAUSTED`
   - exhausted candidate set -> `NO_ELIGIBLE_TARGET`

### Tests to write first

- One report fixture for every outcome row.
- Workflow aggregation fixtures for standalone, pipeline, parent, child, and
  MCP-service failures.
- Aggregator identity mismatch is terminal and cannot trigger another model.
- Missing checkpoint.
- No remaining budget/time.
- Multiple allowed actions.
- Every pause state and frozen-time/spend invariant.
- Accepted/rejected amendment and amendment-versus-exhaustion race.
- Restart provenance and exact-version/default-input reporting.
- Prompt injection in progress text cannot alter action/status/reason.
- Secret and raw prompt/response redaction.
- CLI/API/operator/Hermes-client contract parity.
- Terminal state/report atomicity.

### Exit gate

An operator or authorized client can understand and control the run from
structured fields without raw logs or trusting worker prose as a command.

## T07 — Activate cancel and restart; defer pause/resume and limit amendments

**Fix 5 trim (2026-07-19):** v0.5 activates cancel and restart only.
Pause/resume and the administrative limit-amendment path are deferred to a
post-v0.5 minor release. Their request surfaces validate and return typed
`feature_not_enabled` (the B26 representational contracts already exist;
activating them is the deferred work). The `PAUSED`/`NEEDS_REPLAN` state
semantics, active-time freeze invariants, and amendment schemas remain
reserved and tested at the B26/B30 store level; the runtime never enters
`PAUSED` in v0.5, and `NEEDS_REPLAN` arises only as the T08 continuation
precondition, not via operator pause.

### Goal

Expose one durable, idempotent lifecycle-control path for independently
invoked workflows covering terminal cancellation and provenance-linked
restart. Pause/resume and limit-amendment requests fail closed with typed
not-enabled errors until their deferred activation.

### Required work (v0.5 scope)

1. Implement the B26 control journal and APIs as the sole mutation path for
   cancel, pause, resume, restart, and limit amendment. CLI and Hermes adapters
   must call these APIs rather than mutating local state.
2. Require authenticated actor, operation-specific scope, expected authority
   generation, idempotency key, request digest, reason, and explicit
   confirmation where required. Exact replay returns the original result;
   changed replay conflicts.
3. Implement cancellation:
   - Commit terminal cancellation intent before cancellation side effects.
   - Revoke all workflow/node/service/attempt capabilities and leases.
   - Cancel in-flight model/tool contexts, fence containers, and reconcile LLM
     reservations.
   - Make cancel win over late success, pause completion, resume,
     continuation, restart admission, and amendment.
   - Record that accepted upstream external side effects cannot be undone.
4. **DEFERRED (Fix 5): cooperative pause.** The pause request surface
   validates scope and idempotency and returns `feature_not_enabled`. The
   drain/fence/`PAUSED` commit semantics in the B26 schema remain reserved;
   activation is post-v0.5 work. No v0.5 path may set `PAUSE_REQUESTED` or
   `PAUSED`.
5. **DEFERRED (Fix 5): resume.** Same typed not-enabled surface. Resume
   revalidation, capacity reacquisition, and deferred-work launch activate
   with pause in the post-v0.5 release.
6. Implement restart as a new B26 invocation:
   - Cancel and fully fence an active source first.
   - Default to the source's exact pinned deployment and original input.
   - Resolve a current alias only behind an explicit option.
   - Begin at stage one with no implicit checkpoint/artifact import, new
     separately approved limits/idempotency key, and `restarted_from`.
7. **DEFERRED (Fix 5): limit amendment.** The `runs:amend_limits` surface
   validates authority and returns `feature_not_enabled`. Absolute
   increase-only amendment commits, generation races, and timer/reservation
   updates activate post-v0.5. Terminal `EXPIRED`/`BUDGET_EXCEEDED`
   remains terminal regardless.
8. Serialize control races in durable state. Define and test one winner for
   cancel versus every operation, pause versus terminal completion, concurrent
   resumes, restart versus source completion, and amendment versus
   active-time/spend exhaustion.
9. Publish audit/event records only after durable state commits. Redact
   credentials and never place administrative tokens in worker containers,
   prompts, checkpoints, or artifacts.
10. Reconcile partially completed control operations idempotently after daemon
    restart; no request may create duplicate attempts, restarts, or amendment
    increments.

### Tests to write first

- Cancel from `RUNNING` and `NEEDS_REPLAN`.
- Cancel versus late success/model response/reservation commit.
- Restart defaults to exact pinned version/input, starts at stage one, and
  records provenance.
- Explicit restart-with-current-alias resolves once; default restart never
  re-resolves.
- Pause request returns `feature_not_enabled` with zero state mutation.
- Resume request returns `feature_not_enabled` with zero state mutation.
- Amendment request returns `feature_not_enabled` with zero state mutation,
  including a request with valid `runs:amend_limits` scope.
- No code path can enter `PAUSE_REQUESTED` or `PAUSED` in v0.5.
- Ordinary invoke credentials cannot cancel/restart/pause/resume/amend.
- Daemon crash at each control journal boundary replays idempotently.

### Exit gate

CLI/API cancel and restart work with Hermes absent; cancel wins every race;
restart creates one linked new invocation on the exact pinned version by
default. Pause, resume, and amendment requests fail closed with typed
`feature_not_enabled` and zero mutation, and no v0.5 path enters
`PAUSE_REQUESTED` or `PAUSED`.

## T08 — Activate one idempotent eligible-node failure continuation

### Goal

Continue the same eligible standalone, pipeline-stage, or parent run once from
a verified safe checkpoint without changing workflow identity or duplicating
handoffs/children.

### Required work

1. Activate B26 failure-continuation fields independently from T07
   administrative pause/resume attempt segments.
2. Require a client idempotency key for continuation; repeated identical
   request returns the existing attempt, while changed action conflicts.
3. Validate every locked continuation precondition.
   Reject leaf-child runs before target selection or resource creation.
4. Derive requirements:
   - `more_time`: a fresh attempt lease within the current amended
     current-attempt lease ceiling and remaining active time, using normal
     primary selection under the unchanged requirement floor.
   - `capability_up`: next capability tier and recovery-role selection.
   - `larger_context`: next eligible context boundary above observed need and
     recovery-role selection.
5. Use B36 selector; no client supplies an exact model.
6. Fence old attempt through T05.
7. Create the single `FAILURE_CONTINUATION` attempt with:
   - Same workflow/node/run, policy, image, catalog, trigger digest, B38
     workflow budget ledger, incoming handoff, and approved artifact roots.
   - New attempt/lease/journal/capability.
   - Verified resume checkpoint.
   - Existing active-time/spend/guardrail/call/control/recovery history.
   - An atomic `recovery_margin_released=true` transition tied to the consumed
     failure-continuation counter. It exposes the reserved time to this final
     attempt and cannot repeat after pause/resume or daemon restart.
8. Start only after state and resources are ready.
9. Reject:
   - A second `FAILURE_CONTINUATION`, even after any number of
     `OPERATOR_PAUSE_RESUME` segments.
   - No safe checkpoint.
   - Terminal active-time or spend exhaustion.
   - Insufficient current active time/spend for one bounded continuation.
   - Policy/catalog/image mismatch.
   - Unsupported/broadening action.
   - Leaf-child continuation.
   - Changed pipeline input/handoff or a request to skip/replace the node.
   - Parent request that conflicts with an already allocated B35 batch.
   - Any implicit policy, active-time, attempt-lease, or spend expansion;
     increases must already have committed through T07.
10. After the failure-continuation attempt reaches another recoverable
    failure, the run is terminal. Its report may recommend a new/split run,
    but it cannot return to continuable `NEEDS_REPLAN`. Administrative
    pause/resume remains lifecycle control and never grants another failure
    continuation.

### Tests to write first

- Successful `more_time`.
- Successful `capability_up` with exact selector choice.
- Successful `larger_context`.
- Same run ID/new attempt ID.
- Same checkpoint/artifacts/budget history.
- Pipeline continuation retains the same incoming handoff and produces at most
  one outgoing handoff.
- Parent continuation recovers the same child batch/result and spawns no
  duplicate child.
- Leaf-child continuation denied before model/container activity.
- Duplicate idempotency request.
- Conflicting idempotency request.
- No checkpoint, no time, no budget.
- Second failure continuation denied before model/container activity.
- The first failure continuation releases recovery margin once; its calls may
  use that remaining time, and no reserve exists for a second continuation.
- Pause/resume before and during recovery does not reset the continuation
  counter.
- Agent-supplied exact model denied.
- Policy/time/lease/budget expansion through continuation denied.
- A prior authorized amendment is honored without being repeated by
  continuation.

### Exit gate

One safe failure-driven continuation is possible and no administrative
pause/resume path creates a second one.

## T09 — Reconcile integrated workflow crashes and resources

### Goal

Compose B30–B38 reconciliation into one workflow recovery pass without
replaying a model call, completed stage, child batch, or external side effect.

### Required work

1. At daemon startup:
   - Load incomplete workflows, nodes, services, handoffs, child batches,
     runs, attempts, B38 reservations, invocation admissions, control
     journals, and limit amendments.
   - Revoke every persisted active lease.
   - Find resources by workflow/node/service/batch/run/attempt labels.
   - Stop/remove agent/gateway containers and networks.
   - Close/reconcile journals and the single workflow ledger.
2. Reconcile time/state without inventing work:
   - A persisted `RUNNING`/`PAUSE_REQUESTED` active segment closes exactly once
     at the proven resource stop/fence boundary; concurrent startup passes
     cannot double-charge it.
   - `PAUSED` and `NEEDS_REPLAN` wall-clock downtime charges no active time or
     spend. Fence any unexpectedly live resource/reservation and record a
     safety violation before leaving the state frozen.
   - An interrupted `RUNNING` attempt becomes `NEEDS_REPLAN` with
     `DAEMON_RESTARTED` only when a safe checkpoint and current envelope
     remain; otherwise it fails terminally.
   - A `PAUSE_REQUESTED` run becomes `PAUSED` only if a valid pre-crash safe
     boundary exists and full fencing/reconciliation succeeds. Never fabricate
     a checkpoint merely because the daemon restarted.
3. Never auto-resume, auto-amend, auto-restart, start a continuation, or repeat
   a completed stage/child/model
   request. B34/B35 may resume only an already-durable `READY` launch intent
   through their idempotent reconciliation rules.
4. Retain artifact/checkpoint evidence.
5. Do not count an interrupted reservation as zero; preserve/reconcile through
   B38 rules.
6. Handle crash at every lifecycle boundary idempotently.
7. Extend orphan cleanup to routed gateways, B33 services, pipeline nodes,
   child containers, transient mounts, and workflow networks.
8. Replay committed control/amendment journal results, never their relative
   user-friendly input. Restore current absolute ceilings and authority
   generation exactly once.
9. Never re-resolve a deployment alias for an admitted invocation, resume,
   continuation, or default restart.
10. A second daemon restart produces no duplicate report/event, control
    action, restart run, amendment, time segment, or spend entry.

### Tests to write first

- Crash before/after run write, resource create, invoke, checkpoint, fence,
  reservation, response, and cleanup.
- Running attempt with/without checkpoint.
- `PAUSE_REQUESTED` with/without a safe boundary.
- `PAUSED`/`NEEDS_REPLAN` downtime and unexpected orphan resources.
- Crash after committed pipeline handoff but before next-stage launch.
- Crash during child allocation/join and parent continuation.
- Crash before/after each cancel, pause, resume, restart, and amendment commit.
- MCP service and workflow-network orphan cleanup.
- Concurrent child reservations remain attached to one workflow ledger.
- Orphan gateway/network cleanup.
- Unreconciled cost preserved.
- Active segment closed once; frozen wall time never charged.
- Pinned exact deployment survives alias promotion/rollback during downtime.
- No automatic model/tool call after restart.
- Second reconciliation idempotent.

### Exit gate

Daemon restart produces an honest frozen/recoverable/terminal workflow state,
no resource orphan, no changed limit, and no repeated control, model call,
stage, child, or side effect.

## T10 — Calibrate release defaults

### Goal

Produce evidence for the founder’s timeout/default approval gate.

### Required work

Run a controlled matrix containing:

- Fast local call.
- Local cold-start call.
- Fast cloud call.
- Slow reasoning cloud call.
- Provider timeout.
- Legitimate multi-minute local tool phase with heartbeats.
- Worker stall.
- Lease expiry during active progress.
- One successful continuation.
- A long `PAUSE_REQUESTED` drain followed by a long fully `PAUSED` interval and
  resume.
- An authorized active-time/attempt-lease/spend amendment shortly before, and
  racing with, exhaustion.
- A multi-minute B33 MCP call with bounded service liveness.
- A multi-stage pipeline crossing the old client/daemon limits.
- A parent waiting on concurrent children, including one slow child.

For each candidate default, report:

- False timeout/stall count.
- Detection delay for real failure.
- Remaining recovery margin.
- Total wall time versus accumulated active time.
- Original/current limits and amendment point.
- User-visible behavior.
- Resource cleanup time.
- Behavior at workflow, node, service, parent, and child scopes.

Propose:

- Default model-call timeout.
- Default stall timeout.
- Default attempt lease.
- Default maximum active duration.
- Minimum recovery margin.

Do not change public defaults until explicitly approved. Automated tests use
injected values and do not depend on approval.

### Exit gate

The calibration report exists with raw timings and a clearly marked pending or
approved decision.

## T11 — Block gate and adversary review

### Required `make block39-gate`

Run:

```text
make block27-gate
make block30-gate
make block33-gate
make block34-gate
make block35-gate
make block36-gate
make block37-gate
make block38-gate
go test ./internal/supervisor/... ./internal/routedrun/... ./internal/daemon/... -count=1 -race
go test ./internal/harness/... ./internal/modelrouter/... ./internal/llmcost/... -count=1 -race
go test ./internal/runtime/... ./internal/trigger/... ./internal/operator/... ./internal/cli/... -count=1 -race
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

Run Docker supervisor tests with fake providers and very short injected
timings. Run the real-time calibration separately and record it; do not make
CI sleep for minutes.

### Required adversary matrix

- Fixed 60-second/2-minute timeout remains on routed path.
- Heartbeat forgery/stdout spam prevents stall.
- Duplicate checkpoint falsely resets no-progress.
- Fingerprint leaks raw action input.
- Off-by-one lets one extra call/action/retry.
- Old lease model/HTTP/MCP/progress/artifact action.
- Old gateway capability against new attempt.
- New attempt starts before old resources stop.
- Continuation resets spend/active-time/policy/catalog.
- A second failure continuation appears after operator pause/resume.
- Duplicate continuation race.
- Ordinary invoke/worker capability invokes an administrative control or
  amendment.
- `PAUSE_REQUESTED` silently freezes active time before resources stop.
- `PAUSED` retains a live worker/service capability, reservation, or accrues
  time/new spend.
- Resume replays a completed stage, child, handoff, or model call.
- Resume capacity conflict changes frozen state.
- Restart silently resolves the current alias or imports old checkpoints.
- Concurrent restart creates multiple new runs.
- Amendment decreases a ceiling, increments twice on replay, or commits after
  terminal exhaustion.
- Cancel loses to late success, resume, continuation, or amendment.
- Daemon crash triggers blind replay.
- Interrupted reservation disappears.
- Stop marks unfinished worker succeeded.
- Prompt-injected checkpoint changes control action.
- Pipeline continuation skips/replaces a stage or duplicates a handoff.
- Parent continuation respawns an existing child batch.
- Leaf child obtains any whole-worker failure continuation.
- Workflow fence leaves an MCP service, stage, child, or reservation live.
- Pipeline stage or child starts with a fresh spend ledger.

### Block success gate

B39 is complete only when:

1. `make block39-gate` passes.
2. Routed workflow, service, stage, batch, run, and attempt state compose
   durably and are race-tested.
3. A service-originated routed model call uses the same selector, recovery,
   workflow spend/token ledger, time controls, and fencing path as other nodes.
4. All four time controls behave independently and coherently.
5. Stall and loop guards are mechanical and bounded.
6. Zero governed actions are accepted after fencing.
7. Every terminal path produces a structured attempt report.
8. One safe eligible-node continuation works; a leaf-child continuation and a
   second failure continuation are impossible.
9. Cancel and linked restart pass their state/race/security matrix; pause,
   resume, and amendment requests fail closed with `feature_not_enabled`
   and zero mutation (deferred per Fix 5).
10. `NEEDS_REPLAN` consumes no active time/new spend and retains no live
   execution capability or active/in-flight reservation; retained
   unreconciled exposure is inert and still counted against headroom.
   `PAUSED`/`PAUSE_REQUESTED` are never entered in v0.5.
11. Daemon restart never blindly replays a control, amendment, model request,
    stage, child batch, or external action.
12. Legacy execution remains compatible.
13. Final timing defaults remain pending until explicit approval if not yet
    granted.

## Handoff record required after every task

Append:

- Task/date.
- State/timer/fencing decisions.
- Files changed.
- Tests added first.
- Exact commands/PASS output.
- Resource and race evidence.
- Attempt report examples.
- Adversary result.
- Compatibility impact.
- Calibration status.
- Open risks.
- Next task unblocked.

## Pitfalls

- A process that is alive is not necessarily making progress.
- A heartbeat is not necessarily a safe checkpoint.
- Do not accrue active time or authorize new spend in fully `PAUSED` or
  `NEEDS_REPLAN` state.
- Do not freeze `PAUSE_REQUESTED` before every active resource and reservation
  is reconciled.
- Do not let `Stop` turn an unfinished run into success.
- Do not start a new attempt until old resources are confirmed fenced.
- Do not promise exactly-once external side effects without idempotency.
- Do not reset spend, active time, catalog, policy, image, trigger, or
  failure-continuation state on retry or administrative resume.
- Do not let invocation/worker credentials amend limits or perform
  administrative lifecycle controls.
- Do not re-resolve an admitted deployment alias on resume or continuation.
- Do not turn loop detection into semantic judgment.
- Do not create a parallel supervisor/scheduler to integrate routing.
- Do not advance a paused pipeline stage, duplicate a child batch, or continue
  a leaf child.
