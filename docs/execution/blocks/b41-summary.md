# Block 41 — Durable Routed Run Golden Proof, Hardening, and v0.5.0 Release

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Target release:** stable `v0.5.0` — Durable Routed Run
**Depends on:** B26–B40 complete and `make block40-gate` green
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65
**Release authority:** The orchestrator may prepare release artifacts, but it
must not create or push `v0.5.0` without explicit founder approval.

## Outcome

B41 proves and releases one coherent Hermes-authored, independently operated
Routed Run product.

At block completion:

- v0.2.3 legacy single-model agents and stable v0.3/v0.4 deployments still
  install and run with their approved semantics after upgrade to v0.5.
- The B28 portability conformance slice passes on Docker and has recorded
  local-Kubernetes evidence plus the managed-substrate decision (Kubernetes
  per D67; Cloudflare rejected).
- The B29 runtime profile, real invocation stream, durable replay, interactive
  wait/wake, and activation/resource measurements pass under disconnect,
  restart, cancellation, and slow-consumer faults.
- Hermes registers approved agents and MCP services in the B31 private catalog;
  a runtime orchestrator resolves and pins a worker, verifier, and testing role
  without gaining unconstrained authority.
- B32 two-sided agent communication and encrypted artifact transfer pass with
  A2A-compatible task/event semantics and cross-tenant denial.
- A worker requests a logical route without embedding a provider or model.
- `local-first` and `cloud-cost-first` both run through the same deterministic
  selector and supervisor.
- One eligible model-call failure is recovered exactly once on an approved
  target, and the recovery target remains sticky for that attempt.
- One whole-worker failure can return a safe checkpoint through the control
  API and continue once inside the existing authority envelope.
- Agents run beyond every former client/daemon/harness timeout, across many
  turns, up to an explicit operator-approved maximum active duration with no
  hidden platform lifetime ceiling.
- Every standalone run, pipeline stage, parent, child, and recovery attempt in
  a workflow shares one enforced LLM-spend ceiling and one accumulated
  active-time ceiling.
- Hermes can author, test, package, and create an immutable deployment, then
  exit before AgentPaaS CLI/API/cron-style invocation begins.
- Accepted invocations pin the exact deployment once, are idempotent, and obey
  receiver-local top-level per-deployment concurrency without a hidden queue;
  every nested stage/service/child package is pinned before admission.
- Cancel and linked new-run restart preserve hard-limit and audit
  semantics. Cooperative pause/resume and scoped pre-terminal amendments
  are deferred per Fix 5; their requests fail closed with
  `feature_not_enabled`.
- A worker calls an approved MCP service running in a separate AgentPaaS
  container through a logical binding and protected workflow network.
- A three-stage pipeline invoked with Hermes absent advances and transfers
  bounded durable context across separate containers.
- A parent spawns separate leaf-child containers, joins durable ordered
  results, collates them itself, and continues with Hermes absent.
- Leases, fencing, stall detection, no-progress controls, and crash recovery
  are proven under adversarial timing.
- Input, cache, output, reasoning/request fees where available, reservations,
  and unreconciled charges are represented truthfully.
- The ordinary customer-feedback report demo finishes after labelled fault
  injection and reports the exact operational and cost history.
- Weather, multi-turn reasoning/tool, interactive streaming, MCP,
  pipeline, and orchestrator/worker/verifier/testing reference fixtures cover
  the supported agent feature matrix.
- A clean Hermes profile on a fresh macOS environment can configure and deploy
  a Routed Run from public instructions; the deployment then runs through
  AgentPaaS without that Hermes session.
- The founder has explicitly approved the public model roster and final time
  defaults from live evidence.
- The release either publishes measured activation/latency evidence with no
  SLA, or records explicit A3 approval for every public SLO, always-ready
  guarantee, or default warm-pool policy.
- Release binaries, Homebrew installation, SDK, plugin, schemas, docs,
  changelog, and tag all agree on v0.5.0.

B41 does not add a verifier, learned router, context compactor, automatic task
decomposer, provider marketplace, external orchestrator adapter, general DAG,
recursive child graph, or a second recovery cascade.

## Release-block rules

### This is a proof and closure block

B41 may:

- Add missing tests, fixtures, fault injection, evidence capture, and release
  automation.
- Fix defects found by the proof matrix.
- Improve wording and onboarding required to make the approved capability
  understandable.
- Make backward-compatible changes needed to close a failed B26–B40 contract.

B41 may not silently:

- Expand the approved v0.5 product scope.
- Weaken a release contract to make a failing test pass.
- Turn a manual or live gate into a skipped PASS.
- Select public models or timeout defaults before the approval checkpoints.
- Publish a latency SLO, always-ready claim, managed activation guarantee, or
  default warm-pool policy before the conditional A3 checkpoint.
- Add a semantic judge and call it testing.
- Publish a release because automated CI alone is green.

Any material behavior not already authorized by B26–B40 is a spec deviation.
The orchestrator must record the issue, present the smallest options and their
trade-offs, and wait for an explicit decision.

### One consolidated release evidence file

Keep committed release evidence in:

```text
docs/release/v0.5.0-evidence.md
```

Do not create one Markdown file per test, model, run, or participant. Large
raw logs, screenshots, provider responses, and machine-readable traces belong
in CI/release artifacts referenced by digest and URL from the consolidated
file.

The evidence file uses exactly four states:

- `NOT RUN`
- `PASS`
- `FAIL`
- `BLOCKED`

“Skipped,” “not available,” and “manual follow-up” are not PASS.

Every evidence row records:

- Proof ID.
- Git commit.
- UTC date and environment identifier.
- Test mode: deterministic, Docker, live, or clean-machine.
- Exact command or Hermes action.
- Expected observation.
- Sanitized observed result.
- PASS/FAIL/BLOCKED state.
- Fault-injection label, when applicable.
- Artifact URL/digest or concise inline evidence.
- Open defect or approval reference.

Never commit credentials, credential values, raw authorization headers,
complete user prompts, sensitive model responses, hidden reasoning, private
endpoint URLs, or unsanitized environment dumps.

### Deterministic proof and live proof are different

Deterministic CI proves:

- State-machine behavior.
- Candidate eligibility and tie-breaking.
- Exact replay and bounded recovery.
- Cost arithmetic and reservations.
- Timing transitions under a fake clock.
- Attempt fencing.
- Authority and prompt-injection boundaries.
- Long-duration/multi-turn state with a fake clock and real-container bounded
  longevity tests.
- Cross-container MCP identity/authority and no synthetic-success fallback.
- Pipeline handoff and parent/child spawn/join idempotency across crashes.
- Immutable deployment/alias lifecycle and direct invocation idempotency.
- Cancel/restart states and races, frozen-time/spend invariants for
  `NEEDS_REPLAN`, and typed `feature_not_enabled` zero-mutation refusals of
  pause/resume/amendment (Fix 5).
- Backward compatibility.

Live testing proves:

- Adapter conformance against selected current endpoints.
- Provider usage/cost field interpretation.
- Real local and cloud latency behavior.
- Error normalization through a labelled fault proxy.
- Hermes usability on the release candidate.

Live model output is not the oracle for deterministic CI. A fluent report does
not make a test pass. The live test passes from operational state, expected
artifact presence/shape, policy evidence, and exact ledger invariants.

## Required proof matrix

The consolidated evidence file must contain every row below.

| ID | Proof | Required result |
|---|---|---|
| RR-01 | Prior-release compatibility | v0.2.3 legacy projects plus stable v0.3/v0.4 catalog, A2A, MCP, pipeline, and parent/child deployments upgrade and run with their approved semantics |
| RR-02 | Logical route | Worker source has no target name; signed route resolves an approved target |
| RR-03 | Deterministic selector | Candidate exclusions, choice, and tie-break are stable over repeated runs |
| RR-04 | Local-first | Approved local primary fails through labelled injection; approved cloud target completes |
| RR-05 | Cloud-cost-first | Economical cloud primary fails through labelled injection; approved recovery tier completes |
| RR-06 | Multi-turn progress | Worker performs multiple model/tool phases and advances authenticated checkpoints |
| RR-07 | Call recovery | Normalized request is replayed once, partial output is discarded, recovery is sticky |
| RR-08 | Worker continuation | `NEEDS_REPLAN` reaches the control API; one allowed operator action resumes from a safe checkpoint and a second failure continuation is denied |
| RR-09 | Attempt fencing | Expired attempt cannot make governed calls or commit artifacts |
| RR-10 | Shared budget | Every call, model-using service, stage, parent, child, and attempt reserves/reconciles against one exact workflow LLM spend limit and shared workflow token totals; spend or token exhaustion denies the next physical request pre-network |
| RR-11 | Cost dimensions | Supported usage dimensions, basis, source, freshness, and uncertainty are visible |
| RR-12 | Credential/quota | Authentication failure excludes the target and either an independent approved target recovers or terminal reauthentication is reported; quota failure likewise recovers independently or terminates as `QUOTA_EXHAUSTED` |
| RR-13 | Loop guardrails | Repeated action and no-checkpoint advancement stop at deterministic bounds |
| RR-14 | Crash recovery | Daemon restart preserves valid state, fences ambiguity, and does not double-charge or replay |
| RR-15 | Completion truth | No correctness judge; completion and under-limit wording remain operationally precise |
| RR-16 | Hermes authoring path | Clean profile authors, tests, packages, and deploys; a later session can inspect/control an independently invoked run |
| RR-17 | Fresh install | Public installation path reaches one Routed Run proof on a fresh macOS environment |
| RR-18 | Release artifacts | Version, checksums, SBOM, signatures, Homebrew package, docs, and tag agree |
| RR-19 | Authorized-duration execution | Real 6-minute and 30-minute runs plus fake-clock 24-hour/100-turn run complete without a hidden timeout; accumulated active-time ceiling and cancel still win |
| RR-20 | Container MCP service | Worker calls a real approved MCP service in a separate AgentPaaS container, and a service-originated routed model call uses shared selection/recovery/spend/token/fencing; identity, policy, lease, network, restart, and evidence hold |
| RR-21 | Runtime-native pipeline | With Hermes absent before invocation, three separate stage containers exchange bounded handoffs/artifacts in order; daemon restart creates no duplicate stage |
| RR-22 | Parent/child collation | With Hermes absent before invocation, parent and three separate leaf children spawn/join under aggregate limits; parent collates ordered durable results with no duplicate/orphan child |
| RR-23 | Independent deployment lifecycle | Hermes authors/tests/packages/deploys an immutable exact deployment with exact nested package identities and audited alias, exits, and CLI/API/cron-style invocation succeeds; promotion, rollback, and deactivation affect only allowed future admissions |
| RR-24 | Invocation safety | Across all four topologies, same caller/key/canonical intent returns one invocation; changed ref/input/initial ceiling/authority option conflicts; top-level per-deployment concurrency returns retryable `ALREADY_RUNNING`, frozen state releases and resume reacquires the slot, and no hidden queue exists |
| RR-25 | Operator lifecycle | Cancel, linked restart, and typed `feature_not_enabled` refusals of pause/resume/amendment requests pass fencing, authorization, idempotency, and zero-mutation tests; pause/resume and amendments themselves are deferred per Fix 5 and proven post-v0.5 |
| RR-26 | Runtime portability | One exact signed package passes the B28 conformance slice on Docker and the recorded Kubernetes proof; the managed-substrate decision (Kubernetes per D67; Cloudflare rejected) and remaining constraints are explicit |
| RR-27 | Runtime profiles, streaming, and durable waits | Buffered compatibility, real model/invocation streaming, cursor replay after disconnect/restart, interactive inbox wakeup, guardrail modes, and profile mismatch denial pass |
| RR-28 | Component catalog and capability resolution | Hermes proposes signed agent and MCP packages; authorized promotion creates tenant-scoped catalog records; constrained role queries resolve deterministically and pin exact versions |
| RR-29 | Secure A2A and artifact transfer | A2A-compatible delegation uses logical identities, two-sided communication policy, durable completion events, and encrypted brokered artifacts; forged, undeclared, direct, and cross-tenant paths fail closed |
| RR-30 | Activation efficiency and latency | On-demand, cached-cold, warm, and resident modes are measured where supported; dormant authority/resource invariants hold; p50/p95/p99 platform overhead is separated from provider/tool time |
| RR-31 | Expanded agent feature coverage | Weather baseline, multi-turn reasoning/tool, interactive streaming, MCP service, pipeline, and orchestrator/worker/verifier/testing fixtures exercise the supported profile and coordination matrix |

Critical deterministic Routed Run scenarios must pass three consecutive runs
from clean state (`pass^3`). Retries performed by the test harness do not count
as a pass and must be disabled.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Freeze proof fixtures and release evidence contract | B40 | all RR rows represented; gate cannot mislabel missing evidence |
| 2 | T02 Build full-stack deterministic Routed Run/workflow golden suite | T01 | RR-02–RR-13, RR-15, RR-19–RR-25, and deterministic portions of RR-26–RR-31 pass |
| 3 | T03 Prove compatibility, migration, and rollback safety | T01, B26 | RR-01 and state/API upgrade matrix pass |
| 4 | T04 Run adversary, crash, concurrency, and resource hardening | T02–T03 | RR-09, RR-13, RR-14 and RR-19–RR-31 fault matrices pass |
| 5 | T05 Run live target conformance and approve public roster | T02, B36–B38 | candidate comparison complete; founder approval A1 recorded |
| 6 | T06 Calibrate time defaults and activation/latency claims | T05, B28, B29, B39 | founder approval A2 recorded; A3 is approved for each public commitment or release wording says measured/no SLA |
| 7 | T07 Prove Hermes authoring, independent invocation, and lifecycle demos | T05–T06, B40 | RR-04, RR-05, RR-08, RR-16, and RR-19–RR-31 live transcripts pass |
| 8 | T08 Truth-sync product docs, examples, and Golden Loop | T05–T07 | public claims and instructions match evidence |
| 9 | T09 Build and test the release candidate on a fresh install | T01–T08 | RR-17 passes; RR-18 pre-publish checklist is complete while row remains NOT RUN |
| 10 | T10 Run final gate, obtain publish approval, release, and verify | T01–T09 | tag and post-publish RR-18 checks pass |

## T01 — Freeze proof fixtures and the release evidence contract

### Goal

Make missing, contradictory, or selectively reported release evidence
machine-detectable before running the expensive proof matrix.

### Required work

1. Create the single consolidated evidence file with:
   - Release metadata.
   - RR-01–RR-31 table.
   - Automated gate results.
   - Live target candidate table.
   - Timing calibration table.
   - Runtime portability/substrate comparison and decision reference.
   - Streaming, activation, and platform-overhead baseline tables.
   - Catalog/A2A/artifact-policy proof references.
   - Founder approval A1.
   - Founder approval A2.
   - A3 public latency/activation claim disposition and approval when required.
   - Clean-machine result.
   - Known defects and risk disposition.
   - Publish authorization and post-release verification.
2. Add a small evidence validator that fails when:
   - A required RR ID is absent or duplicated.
   - A required field is empty.
   - A FAIL/BLOCKED/NOT RUN row is treated as release-ready.
   - An approval has no explicit decision, approver, and date.
   - A faulted run lacks a visible `INJECTED_FAULT` marker.
   - A live result has no commit/catalog/policy snapshot identity.
3. Add sanitized fixture data for the customer-feedback report:
   - A deterministic set of feedback records.
   - Stable expected artifact names and structural requirements.
   - No expected semantic prose or LLM-authored “correct answer.”
4. Freeze fake target IDs and prices independently from the future public
   roster. Suggested IDs:
   - `fixture.local.primary`
   - `fixture.cloud.economy`
   - `fixture.cloud.capability`
   - `fixture.cloud.independent`
5. Add fault scenarios for:
   - Timeout before headers.
   - Timeout after partial body.
   - Retryable service failure.
   - Rate limit.
   - Authentication failure.
   - Quota exhaustion.
   - Context rejection.
   - Structured-output rejection.
   - Unknown charge after timeout.
   - Daemon restart during a long turn, MCP call, pipeline handoff, child
     allocation, child join, and parent continuation.
   - Invalid/missing handoff, forged MCP service identity, recursive child,
     and aggregate workflow limit exhaustion.
   - Alias promotion during active invocation, deactivation, duplicate/
     conflicting invocation admission, and deployment-concurrency overlap.
   - Cancel versus late success, restart version choice, and
     pause/resume/amendment requests that must fail closed with
     `feature_not_enabled` and zero mutation (Fix 5).
   - Stream disconnect, slow consumer, partial-output failure, forged cursor,
     duplicate terminal delivery, and interactive-input wakeup races.
   - Unauthorized catalog proposal/promotion, capability downgrade, mutable
     alias re-resolution, forged A2A sender, disallowed receiver, direct
     endpoint bypass, artifact-grant replay, and cross-tenant access.
6. **STRETCH (Fix 7, 2026-07-19):** offline-bundle. If release timeline is
   healthy, add a separate network-enabled `make block41-offline-bundle`
   preparation job that resolves only lockfile/pinned inputs and emits one
   content-addressed bundle plus manifest (Go module cache, pinned Python
   deps, tool binaries/plugins, pinned OCI archives, local vuln/OSV
   snapshots, tool/version manifest, no credentials). If the timeline is
   tight, this is the first item to cut: `block41-ci` then runs with network
   and pinned toolchain versions recorded in the evidence file, and the
   offline reproducibility claim is dropped from the release notes. The
   v0.5.0 release must not slip for the offline bundle.
7. Add `make block41-ci` for reproducible automated tests. When the stretch
   offline bundle exists, it runs with network denied and only the verified
   bundle available; otherwise it runs with network and pinned tool
   versions, and the evidence file records which mode was used.
8. Add `make block41-gate` as the pre-publish release gate:
   - It runs `make block41-ci`.
   - It validates RR-01–RR-17 and RR-19–RR-31, A1, A2, the A3 claim
     disposition/conditional approval, live proof,
     fresh-install proof, and
     the RR-18 pre-publish artifact checklist.
   - It requires RR-18 itself to remain `NOT RUN` until public artifacts and
     the tag can actually be verified.
   - It never tags, pushes, signs, or publishes.
9. Add `make block41-postrelease-gate`:
   - It reruns the evidence validator in post-release mode.
   - It requires RR-01–RR-31 all PASS.
   - It verifies that RR-18 names the approved tag, commit, public artifact
     digests, workflow/deployment/control results, and Homebrew smoke.
10. Keep B26–B40 cumulative gates callable separately so a failure can be
   localized.

### Tests to write first

- Evidence validator accepts one complete synthetic fixture.
- Missing/duplicate RR ID.
- Invalid state and case variation.
- FAIL or BLOCKED hidden in prose.
- Approval with no explicit approver/date.
- Public latency/always-ready/warm-pool claim with no explicit A3 approval;
  measured/no-SLA wording passes without inventing an approval.
- Reused artifact digest for two incompatible runs.
- Validator expects exactly RR-01 through RR-31 and rejects an unknown/gapped
  proof ID.
- Fault result with no injection marker.
- Secret sentinel and authorization-header redaction.
- `block41-ci` has no network or credential dependency.
- Missing, stale, wrong-commit, wrong-platform, tampered, or incomplete offline
  bundle fails before any gate command; a download attempt fails the test.
- Vulnerability scanners consume the manifested local snapshots and report
  their effective times/digests.
- `block41-gate` remains red on a pristine pre-approval evidence template.
- Pre-publish mode accepts only RR-18=`NOT RUN`, never RR-01–RR-17 or
  RR-19–RR-31.
- Post-release mode fails until RR-18 is PASS.

### Likely files

- `docs/release/v0.5.0-evidence.md`
- `scripts/verify-v050-release-evidence.*`
- offline-bundle builder/verifier, pinned tool manifest, and CI artifact wiring
- `test/routedrun/fixtures/**`
- `Makefile`
- release-evidence validator tests

### Exit gate

Every release claim has one proof ID, every absent manual decision blocks the
gate, and deterministic fixtures do not contain public model defaults.

## T02 — Build the full-stack deterministic Routed Run/workflow golden suite

### Goal

Prove the complete SDK-to-runtime path for standalone and supported
multi-container workflows without relying on a live model’s availability or
output quality.

### Required work

1. Build a scripted OpenAI-compatible fake target service with:
   - Multiple target identities and credentials.
   - Deterministic success responses and usage fields.
   - Controllable latency through a fake clock or explicit test barrier.
   - Every T01 failure mode.
   - Request fingerprint capture.
   - A hard assertion on calls made after lease revocation.
2. Route traffic through the real harness/gateway boundary. Do not test the
   golden proof by calling the selector directly.
3. Run the reference customer-feedback worker through:
   - Load fixture data.
   - Classify/summarize batches over multiple calls.
   - Write an intermediate artifact.
   - Emit a safe checkpoint.
   - Perform a later model/tool phase.
   - Write the final report artifact.
   - Return operational completion.
4. Implement the deterministic local-first run:
   - Local fixture target selected first.
   - Labelled timeout injected.
   - Exact call replayed once on approved cloud fixture.
   - Cloud fixture remains sticky for later calls in attempt 1.
5. Implement the deterministic cloud-cost-first run:
   - Economy fixture selected by exact cost ordering.
   - Labelled eligible failure injected.
   - Capability recovery fixture selected.
   - No third target is called.
6. Implement whole-worker continuation:
   - The `INITIAL` attempt commits a safe checkpoint.
   - Attempt lease expires while legitimate progress is visible.
   - Runtime fences it and returns `NEEDS_REPLAN` through the control API.
   - An authenticated operator fixture chooses one allowed action.
   - The single `FAILURE_CONTINUATION` attempt resumes from the accepted
     checkpoint.
   - Completed work and governed side effects are not repeated.
   - Any `OPERATOR_PAUSE_RESUME` segments do not reset the
     failure-continuation count.
7. Assert the complete ledger:
   - Route snapshot.
   - Considered/excluded candidates.
   - Model-call fingerprints.
   - Failure class and scope.
   - Reservation and reconciliation entries.
   - Shared workflow token totals and a pre-network token-exhaustion denial.
   - Progress/checkpoint sequence.
   - Attempt transition and fencing sequence.
   - Artifact digest/ownership.
   - Final status.
8. Add pass^3 execution from a fully isolated temporary AgentPaaS home.
9. Assert zero fixture-server, container, network, run-lock, or temporary
   credential residue after each run.
10. Keep structured JSON validation limited to the explicit structured-output
    fixture. Do not add a report-quality judge.
11. Run B30 longevity fixtures through the real async container path:
    - A real 6-minute, 20-plus-turn worker in the normal deterministic suite.
    - A real 30-minute worker in the release/overnight lane.
    - A fake-clock 24-hour/100-turn run in CI.
   - Explicit accumulated active-time ceilings and cancellation controls that
     terminate each fixture when set below consumed active work.
   - A long `NEEDS_REPLAN` interval that consumes no active time or new spend.
12. Run one real B33 MCP service container and client worker container through
    initialize, exact tool-list readiness, call, restart, lease expiry, and
    cleanup. In a deterministic variant, have the service itself make a routed
    fake-model call and prove B36 selection, one B37 recovery, shared B38 spend/
    token accounting, and service-lease fencing. Remove or bypass the real
    router and require the proof to fail; a synthetic success is never
    acceptable.
13. Run a B34 three-stage fixture in separate worker/gateway pairs. Commit a
    bounded structured handoff and immutable artifact at each boundary,
    start it by direct deployment invocation with Hermes absent, restart the
    daemon after one handoff, and prove every stage executes once in order.
14. Run a B35 parent plus three leaf children in separate worker/gateway pairs.
    Complete children out of order, return them to the parent in request order,
    restart after the durable batch result commits but before parent delivery,
    and prove the resumed parent receives the same result without respawn. A
    separate active-child restart must fence/fail safely without blind replay.
    In the successful path, the parent—not the runtime—collates and continues
    without duplicate/orphan children and with Hermes absent before
    invocation.
15. For the pipeline and parent/child fixtures, assert one workflow
    accumulated active-time ledger that advances once for each elapsed active
    interval regardless of parallel node count, one B38 spend/token ledger,
    aggregate resource bounds, authority narrowing, and complete deployment/
    invocation/workflow/node/service/batch/run/attempt provenance.
16. Prove the B26 deployment lifecycle:
    - Create exact `customer-report@1.2.0` and alias
      `production/customer-report` through a clean Hermes authoring fixture.
    - Terminate that Hermes process before invoking through CLI and API.
    - Return and audit requested alias plus exact resolved version/digests.
    - Promote the alias to `@1.3.0` while a `@1.2.0` run is active; prove the
      active run stays pinned and only a later admission resolves `@1.3.0`.
    - Roll back the alias and deactivate an exact version without cancelling
      active work or deleting history.
17. Prove invocation admission:
    - Run the shared B26 admission-conformance suite through public CLI/API for
      standalone, MCP-client, pipeline, and parent/child deployments.
    - Same caller/key/canonical intent returns the same invocation.
    - Changed reference, input, initial active-time/lease/spend ceiling, or
      authority-bearing creation option conflicts before resource creation.
    - Alias movement cannot change the top-level or nested exact identities of
      an accepted workflow.
    - Default `max_concurrent_runs=1` rejects distinct overlap with retryable
      `ALREADY_RUNNING` and creates no queued launch.
    - An explicitly concurrency-safe deployment can run at its configured
      higher bound.
    - Internal stages/children do not consume additional top-level slots;
      `PAUSED`/`NEEDS_REPLAN` release and resume atomically reacquires one.
18. Prove B39 controls:
    - Cancel wins over late worker/model success.
    - Restart creates one linked new invocation on the original exact version
      and input by default, with no implicit checkpoint import.
    - Pause, resume, and limit-amendment requests fail closed with typed
      `feature_not_enabled` and zero mutation, including correctly scoped
      amendment requests (Fix 5 deferral).
    - Terminal `EXPIRED`/`BUDGET_EXCEEDED` commits at the exact configured
      ceiling and cannot be amended or resurrected.
19. Run the B28 portability fixture with the identical package digest and
    contract assertions on Docker and the recorded Kubernetes proof
    adapter (Cloudflare rejected per D67); verify tenant separation and
    preserve the signed substrate decision record.
20. Prove B29 profile negotiation, buffered compatibility, real model and
    invocation streaming, cursor replay after disconnect/daemon restart,
    durable interactive wait/wake, guardrail modes, and zero-authority
    on-demand dormancy.
21. Prove B31 Hermes registration proposals, authorized promotion, unified
    agent/MCP catalog queries, deterministic constrained role resolution, and
    exact package/profile pinning before admission.
22. Prove B32 A2A-compatible task delegation, two-sided communication-policy
    denial, event-driven completion, and encrypted brokered artifact grants
    without direct addresses, shared writable mounts, raw storage credentials,
    tmux coordination, or correctness polling.
23. Run the expanded reference-agent matrix: weather baseline, multi-turn
    reasoning/tool worker, interactive streaming worker, MCP service/client,
    pipeline, and orchestrator with separate worker/verifier/testing roles.

### Tests to write first

- Primary success with no recovery.
- Shared workflow token-total exhaustion denies a request before network
  activity and leaves spend/token ledgers exact.
- Local-first timeout and one recovery.
- Authentication and quota terminal branches are distinct; quota with no
  independent candidate is `QUOTA_EXHAUSTED`, not reauthentication.
- Service-originated routed model call shares selector, recovery, spend/token
  ledger, and service fencing.
- Cloud-cost-first service failure and one recovery.
- Timeout after partial bytes; no partial output reaches worker.
- Exact normalized request fingerprints match across replay.
- Recovery target sticky across later calls.
- Third physical call denied.
- Multi-turn progress sequence and checkpoint authentication.
- Failure continuation and operator resume both skip already committed work;
  operator resume does not reset the failure-continuation allowance.
- Shared spend across attempts.
- Unknown timeout ends its active reservation and remains as inert
  unreconciled exposure that reduces headroom.
- Candidate becomes unaffordable after earlier spend.
- Completion result exists but is not labelled verified/correct.
- Real 6-minute/20-turn and fake 24-hour/100-turn execution crosses every
  former fixed timeout.
- Real 30-minute release-lane execution has continuous bounded evidence and no
  open client request.
- Long `NEEDS_REPLAN` fake-clock and bounded real-time intervals add zero
  active time and new spend.
- MCP router/service missing, wrong identity, expired lease, forbidden tool,
  and oversize response fail closed.
- Pipeline handoff schema/secret/digest/size failures block the next stage.
- Child completion permutations preserve request order; recursive spawn and
  aggregate overrun fail closed.
- Hermes authoring process is absent before standalone, pipeline, and tree
  invocation.
- Deployment alias pin/promotion/rollback/deactivation fixtures.
- Invocation exact/conflicting idempotency and no-hidden-queue concurrency.
- Cancel/restart state and race fixtures, plus `feature_not_enabled`
  zero-mutation fixtures for pause/resume/amendment (Fix 5).

### Required proof output

Populate RR-02–RR-13, RR-15, and deterministic happy-path portions of
RR-19–RR-31. Include concise deployment/alias/invocation/control/amendment/
workflow/ledger/handoff/batch excerpts and artifact digests, not full model
text.

### Exit gate

All critical golden scenarios pass three clean repetitions, with identical
route decisions and exact cost arithmetic.

## T03 — Prove compatibility, migration, and rollback safety

### Goal

Ensure Routed Run is additive to v0.2.3, stable v0.3, and stable v0.4 rather
than a replacement that breaks existing agents, workflows, or local state.

### Required work

1. Use the immutable B26 v0.2.3 fixtures to test:
   - Singular `llm.provider/model/credential` project.
   - Legacy `agent.llm(..., model=...)`.
   - Policy without routed fields.
   - Existing installed bundle and install manifest.
   - Existing operator schema responses.
   - Existing Hermes build/run/share/receive tool calls.
2. Run legacy fixtures on:
   - The current v0.5 binary with an empty home.
   - A copied v0.2.3 home upgraded in place.
   - v0.2.3, v0.3, and v0.4 installed bundles loaded by v0.5.
3. Verify additive schema behavior:
   - Older clients ignore new fields.
   - New clients accept older responses.
   - No field is removed, renamed, or silently reinterpreted.
4. Exercise state migration interruption at every durable write boundary:
   - Before migration.
   - During temporary write.
   - Before atomic rename/commit.
   - Immediately after commit.
   Include deployment versions, alias history, invocation idempotency indexes,
   concurrency admissions, active-time segments, control journals, and limit
   amendments—not only legacy run records.
5. Make migration idempotent and preserve a recoverable backup where the
   established home-store pattern requires it.
6. Define honest rollback behavior:
   - Never claim that an older daemon can understand later routed state.
   - A failed downgrade must refuse clearly without mutating v0.5 state.
   - Document restore-from-backup or use of a separate home.
   Distinguish binary/state rollback from an ordinary deployment-alias
   rollback, which remains a supported v0.5 future-admission operation.
7. Verify mixed legacy and routed runs can coexist without:
   - Shared-budget cross-contamination.
   - Route state appearing in legacy output.
   - Legacy exact-model overrides entering routed mode.
8. Re-run pack, export, inspect, install, fork, and provenance golden paths for
   a legacy bundle.
9. Verify v0.3 and v0.4 deployment/control records survive a v0.5 daemon
   upgrade/restart:
   - Exact deployments and alias history retain digests.
   - Admitted runs retain the original resolved exact version.
   - Idempotency replay still returns the original invocation.
   - `PAUSED`/`NEEDS_REPLAN` retain frozen active time and spend.
   - Current absolute amended ceilings and authority generation restore once.

### Tests to write first

- Every B26 compatibility fixture.
- Upgrade empty, legacy-only, routed-only, and mixed homes.
- Interrupted and repeated migration.
- New daemon/old CLI and new CLI/old fixture response.
- Routed fields absent from legacy behavior.
- Legacy cost report unchanged.
- Unsafe downgrade refused with state intact.
- Existing signed bundle digest and provenance verification.
- Exact-deployment/alias/invocation/control/amendment migration fixtures.
- Upgrade while a run is `RUNNING`, `PAUSE_REQUESTED`, `PAUSED`, and
  `NEEDS_REPLAN`.

### Exit gate

RR-01 passes, every migration is atomic/idempotent, and any unsupported
downgrade is explicit rather than destructive.

## T04 — Run adversary, crash, concurrency, and resource hardening

### Goal

Prove the recovery machinery cannot broaden authority, double-execute an
expired attempt, lose budget state, or leak local runtime resources.

### Required adversary matrix

1. Candidate and policy attacks:
   - Unsigned target injected after run start.
   - Route/catalog swap after snapshot.
   - Exact provider/model/endpoint override from worker or Hermes.
   - Aggregator model fallback or routing outside the signed upstream-provider
     allowlist.
   - Missing/mismatched returned aggregator model or a reported upstream
     outside the allowlist.
   - Cloud target selected while cloud transfer is denied.
   - Unauthenticated private endpoint without explicit policy.
2. Credential attacks:
   - Credential value in route decision, error, progress, artifact metadata,
     audit, or transcript.
   - Failed credential retried repeatedly.
   - Credential label changed between snapshot and call.
3. Checkpoint/artifact attacks:
   - Worker writes directly to the progress journal.
   - Forged HMAC, sequence, attempt ID, safe-resume field, or artifact digest.
   - Symlink/path traversal/hard-link artifact.
   - Artifact commit after fencing.
4. Lease and race attacks:
   - Old worker and new attempt race model/HTTP/MCP calls.
   - Heartbeat arrives while revocation is committing.
   - Attempt 1 exits successfully after it has already been fenced.
   - Duplicate continuation with same and different idempotency keys.
5. Spend attacks:
   - Concurrent calls oversubscribe remaining budget.
   - Crash after reservation but before provider response.
   - Duplicate usage reconciliation.
   - Negative, overflowing, fractional, stale, or unknown price values.
   - Provider-reported cost conflicts with calculated usage.
6. Prompt/control attacks:
   - Progress, provider error, artifact, and model output ask an operator/
     Hermes client to expand limits, approve cloud, choose a target, retry
     again, pause/resume/restart, or mark success.
7. Loop attacks:
   - Same governed action repeated to the exact bound.
   - Semantically different text with identical governed action fingerprint.
   - Heartbeats continue while checkpoint/action progress does not.
8. Crash points:
   - Before/after workflow catalog and node route-snapshot commit.
   - Before/after model reservation.
   - During progress journal ingest.
   - During checkpoint commit.
   - During fencing.
   - Between initial-attempt termination and failure-continuation start.
   - During final result commit.
9. Long-running and active-time attacks:
   - Reintroduce a shorter plugin, CLI, daemon, HTTP, harness, model, or
     container timeout than the effective operation/active-time boundary.
   - Heartbeat/stdout spam extends the current workflow active-time ceiling.
   - Any code path enters `PAUSE_REQUESTED` or `PAUSED` (reserved states must
     be unreachable in v0.5 per Fix 5).
   - `NEEDS_REPLAN` retains a live capability/active reservation or accrues
     active time/new spend; retained unreconciled exposure is inert.
   - Cancellation during a model/MCP/join operation leaves live work.
   - CPU/PID defaults terminate legitimate long work or permit unbounded host
     process creation.
10. MCP service attacks:
    - Synthetic success when no real router/service is connected.
    - Wrong service generation, forged capability, undeclared tool, direct
      worker-to-service port, cross-workflow service call, oversize payload,
      and service lease expiry/restart race.
11. Pipeline attacks:
    - Hermes/worker directly triggers the next stage.
    - Forged/duplicate/oversize/secret-bearing handoff, artifact substitution,
      declassification, shared writable mount, concurrent stages, and daemon
      crash around stage-success/handoff/next-ready commit.
12. Parent/child attacks:
    - Undeclared package, image/command override, recursive child, ninth child,
      excess concurrency, duplicate idempotency key with changed input,
      confused-deputy authority, forged/late result, parent death, and restart
      during allocation/join/cleanup.
13. Workflow aggregate attacks:
    - Stage or child creates a fresh spend or active-time ledger.
    - Concurrent children oversubscribe spend/container/artifact bounds.
    - Workflow fence leaves a service, stage, child, network, mount, or active
      reservation alive; retained unreconciled exposure must remain inert.
14. Deployment and invocation attacks:
    - Mutate an immutable exact deployment or reuse a version with new digest.
    - Re-resolve an alias after admission, resume, or continuation.
    - Deactivated exact version accepts a new exact/alias invocation.
    - Same caller/key with changed ref/input/initial ceiling/creation option
      reaches resource creation.
    - Distinct overlap bypasses `max_concurrent_runs` or enters a hidden queue.
    - External trigger obtains workflow-internal or administrative authority.
15. Lifecycle attacks:
   - Cancel loses to late success, continuation, or restart.
   - Pause request mutates state instead of returning `feature_not_enabled`.
   - Resume request mutates state instead of returning `feature_not_enabled`.
   - Amendment request (any scope, including valid `runs:amend_limits`)
     mutates state instead of returning `feature_not_enabled`.
   - Restart silently imports a checkpoint, resolves the current alias, or
     creates multiple new runs.
   - Terminal exhaustion accepts any control other than inspection.
16. Streaming and wait attacks:
    - Forged/stale cursor, slow consumer, subscriber reconnect storm, partial
      delta after fencing, duplicate terminal event, hidden-reasoning exposure,
      interactive message replay, and input that attempts to expand authority.
17. Catalog and A2A attacks:
    - Unauthorized Hermes promotion, cross-tenant discovery, false capability,
      profile downgrade, post-admission version swap, forged sender/receiver,
      direct endpoint bypass, undeclared edge, completion-event spoofing, and
      artifact-grant replay or confused deputy.
18. Activation and portability attacks:
    - Dormant/warm worker retains task authority, credential, egress, or CPU;
      activation mode is silently upgraded; adapter bypasses a portable port;
      substrate identity or network policy becomes public agent authority.

### Soak and cleanup work

1. Run at least 25 sequential deterministic Routed Runs.
2. Prove default top-level deployment concurrency one, then run three
   concurrent Routed Runs only against a deployment explicitly configured and
   tested as concurrency-safe with `max_concurrent_runs=3`.
3. Run at least five sequential MCP workflows, five three-stage pipelines, and
   five parent/three-child workflows from clean state.
4. Run one mixed-concurrency soak containing a standalone run, pipeline, and
   parent/child workflow under each exact deployment's top-level admission
   ceiling plus the runtime-wide aggregate capacity.
5. Kill/restart the daemon at each crash point.
6. After bounded reconciliation, assert:
   - No active attempt has two valid leases.
   - No governed action occurs from a revoked lease.
   - No reservation is silently released when charge is unknown.
   - No artifact has two owners/committers.
   - No orphaned routed worker/gateway container or run network remains.
   - No orphaned MCP service, stage, child, workflow network, handoff mount, or
     aggregate reservation remains.
   - No paused/needs-replan workflow retains a live execution capability or
     receives a new active-time/spend entry.
   - No idempotency admission, restart, control, amendment, or active-time
     segment is duplicated.
   - State files retain required permissions.
   - Audit and decision chains verify.
7. Run with the Go race detector and repeat state-machine stress tests enough
   to expose ordering faults without relying on wall-clock sleeps.

### Tests to write first

Every adversary case above must begin as a failing focused test or explicit
fault fixture. Do not rely on a single broad red-team script that cannot show
which invariant failed.

### Exit gate

RR-09, RR-13, RR-14, and adversary portions of RR-19–RR-31 pass; existing
network, credential invisibility, sharing, provenance, and audit red-team
suites remain green; zero P0/P1 security defect is open.

## T05 — Run live target conformance and approve the public roster

### Goal

Select a small current, tested public roster from evidence without coupling
the architecture to one provider or publishing an untested “best model”
claim.

### Candidate-research rules

At execution time:

1. Review current official provider/model documentation and current price
   pages. Record source URL and effective/retrieval date.
2. Prefer primary sources. An aggregator listing is acceptable for its own
   offered price/route but not as proof of another provider’s independent
   contract.
3. Keep the candidate set intentionally small:
   - At least one explicitly approved local OpenAI-compatible target for the
     local-first proof.
   - At least one economical cloud target.
   - At least one higher-capability or larger-context cloud target.
4. OpenRouter may supply one or more candidates, but at least one direct/local
   adapter path must remain proven so OpenRouter is not an architecture
   dependency.
5. Use only credential labels stored through AgentPaaS. Never put a key in a
   command, transcript, fixture, evidence file, or model catalog.
6. Do not include a target merely because it is new or benchmark-leading.
   It must pass the adapter and operational conformance matrix.

### Required candidate report

For each candidate, record:

- Stable candidate ID.
- Provider and model identifier.
- Adapter and endpoint type.
- Signed aggregator upstream-provider allowlist, when applicable.
- Local, cloud, region/residency information where known.
- Credential requirement and whether it is independent from other targets.
- Declared context and output limits.
- Structured-output and effort support.
- Input, cached-input, cache-write, output, reasoning, and request prices where
  defined.
- Cost basis: metered, subscription, or local.
- Price source, effective/retrieval time, and freshness.
- Usage/cost fields actually observed.
- Returned model identity and returned upstream identity (or explicit
  `not_reported`) for aggregator targets.
- Timeout/rate-limit/auth/quota/context error normalization result.
- Role in `local-first`, `cloud-cost-first`, or neither.
- Known caveats, deprecation notices, and unstable fields.

### Required live conformance

Before any paid/live call, present the candidate endpoints, credential labels,
data classification, fault-proxy design, maximum metered test spend, and
cleanup plan. Obtain explicit one-time authorization for that live-test
envelope covering the planned T05–T07, T09, and post-publish T10 calls, with
stage-level spend caps. If the later work falls outside that envelope or the
authorization has expired, obtain a new one before calling. This operational
authorization is not A1 roster approval and does not authorize publication.

1. One successful buffered call and one successful streaming call with ordered
   deltas, final usage, and a canonical committed result.
2. One multi-turn worker sequence.
3. Explicit structured JSON when the target claims support.
4. Usage and provider-reported cost reconciliation.
5. Long input/output caps without intentionally exhausting a costly context
   window.
6. Labelled timeout/service/auth/quota/context faults through the controlled
   proxy; do not wait for or manufacture a real provider outage.
7. No recovery outside the signed candidate pool.
8. Aggregator calls use one model, carry the signed upstream-provider
   allowlist, return the expected model identity, and never report an upstream
   outside the envelope. Missing upstream response metadata is recorded as
   `not_reported`, not inferred.
9. No more than the explicitly approved live-test spend envelope. Record
   actual metered spend.

### Approval checkpoint A1 — mandatory stop

The orchestrator must present:

- The passing candidates.
- Excluded candidates and objective reasons.
- Recommended local-first and cloud-cost-first roles.
- Current price/capability trade-offs.
- Proposed price-validity/freshness policy for the public catalog.
- Known operational risks.
- The exact proposed public defaults/examples.

It must then wait for explicit founder approval. Silence, prior architecture
approval, or a green test is not model-roster approval.

Record the approved roster verbatim in the A1 section of the consolidated
evidence file. Only then may implementation defaults and public docs name
those models.

### Exit gate

RR-04/RR-05 live target prerequisites pass and A1 is explicitly approved.

## T06 — Calibrate time defaults and activation/latency claims

### Goal

Replace today’s conflicting fixed timeouts with tested defaults that permit
legitimate multi-minute work while preserving bounded recovery time, and
decide truthfully whether the release makes any public activation/latency
commitment beyond measured evidence.

### Required calibration scenarios

Run and record:

1. Fast cloud model calls.
2. Slow reasoning/effort calls.
3. Local cold-start and warmed calls.
4. A legitimate tool/test phase lasting at least five minutes with
   authenticated heartbeats and progress.
5. A worker that sends heartbeats but makes no checkpoint/action progress.
6. A worker that sends neither heartbeat nor governed activity.
7. A provider that never returns headers.
8. A provider that returns partial data and then stalls.
9. An attempt that reaches its lease while making progress.
10. A continuation that needs enough remaining current active time for
    fencing, launch, and completion.
11. The mandatory real 6-minute/20-plus-turn run and 30-minute release-lane
    run, each with periodic checkpoints and no lifetime-spanning client call.
12. A multi-minute B33 MCP operation, B34 pipeline stage, and B35 parent join
    with legitimate progress.
13. A current workflow active-time ceiling being consumed while parent,
    children, and MCP service are active, proving deterministic cascading
    cancellation and cleanup and proving elapsed time is charged once rather
    than once per parallel node.
14. Cancel during a long pipeline stage and one-level child batch: measure
    fencing/cleanup time and prove terminal state is truthful.
15. Pause and amendment requests during active work: typed
    `feature_not_enabled`, zero mutation, run unaffected (Fix 5).

For each scenario, separate:

- Model-call elapsed time.
- Worker/tool activity time.
- Total wall time, accumulated active time, and frozen time.
- Time between progress events.
- `PAUSE_REQUESTED` drain time and `PAUSED` interval.
- Fencing/cleanup time.
- Continuation/resume launch time.
- Remaining recovery margin.
- Original/current time and spend ceilings, usage/reservation at amendment,
  and authority generation.
- Workflow/node/service/child scope and aggregate cleanup time.

Do not label a percentile from an inadequate sample. Show the raw sample
count and range; use percentiles only when the sample size supports them.

### Default-relationship invariants

Whatever values are proposed must satisfy:

- Plugin/CLI wait behavior cannot terminate the underlying run.
- Model-call timeout is independent from stall timeout.
- Progress and authenticated governed activity can maintain liveness.
- Heartbeats alone cannot defeat no-progress controls indefinitely.
- Attempt lease leaves time to fence and perform the one allowed
  continuation.
- Maximum active duration is an accumulated current hard ceiling, not an
  absolute wall-clock deadline.
- `RUNNING` consumes active time; `NEEDS_REPLAN` does not. `PAUSED`/
  `PAUSE_REQUESTED` are reserved states never entered in v0.5 (Fix 5).
- Fully frozen (`NEEDS_REPLAN`) states allow no model/tool work, new spend,
  live execution capability, or active/in-flight LLM reservation; retained
  unreconciled exposure remains visible and inert.
- Continuation and daemon restart never reset consumed active time or spend.
- Terminal `EXPIRED`/`BUDGET_EXCEEDED` cannot be amended or resurrected;
  amendment requests fail closed with `feature_not_enabled` (Fix 5).
- No plugin, CLI, daemon, transport, harness, provider, pipeline, join, or MCP
  wait imposes a shorter hidden lifetime ceiling.
- Every blocking operation is bounded by its effective operation deadline and
  current active time remaining even when the workflow is configured for
  hours/days of active work.
- Before failure continuation, normal call admission leaves the recovery margin
  intact and B37 call recovery does not double-reserve it. Starting the one
  `FAILURE_CONTINUATION` releases the margin exactly once for that final attempt.
- Cancel races have one durable generation winner.
- Every value remains configurable within validated bounds.

### Approval checkpoint A2 — mandatory stop

The orchestrator must present:

- Raw calibration results.
- Proposed model-call timeout.
- Proposed stall timeout.
- Proposed attempt lease.
- Proposed default maximum workflow/run active duration.
- Validated configurable maximum-active-duration range for longer hour/day
  workloads.
- Proposed recovery margin.
- The user-visible consequence of each value.
- Alternatives for users running longer coding/tool tasks.

It must wait for explicit founder approval before changing public defaults or
documentation. Record the exact approved values and date in A2.

### Tests after approval

- Approved defaults are represented once in runtime configuration.
- Every SDK/daemon/harness/CLI/plugin layer derives its wait behavior from the
  appropriate contract rather than an independent fixed timeout.
- Boundary tests at default minus/at/plus one unit.
- Real 6-minute and 30-minute progress cases survive; the fake
  24-hour/100-turn case advances deterministically.
- MCP, pipeline, and child-join long-operation cases survive while remaining
  cancellable.
- Stalled/no-progress cases stop at the approved boundaries.
- Active-time ceiling and recovery margin cannot be exceeded by rounding.
- Pause-request/paused/resume fake-clock boundaries are exact.
- A limit amendment updates the current timer/reservation capacity once;
  replay, decrease, and post-terminal mutation fail.

### Exit gate

A2 is approved, defaults have one authoritative source, and no hidden
60-second/two-minute/five-minute or workflow-operation layer contradicts them.

### Approval checkpoint A3 — conditional mandatory stop

Present the B28/B29/B41 cold-image, cached-cold, warm, and resident support,
sample counts, p50/p95/p99 AgentPaaS overhead, provider/tool latency, idle and
active resources, and completed-task cost. Then choose one of two explicit
release dispositions:

1. **Measured/no SLA:** publish the measurements and supported activation
   modes with clear environment/sample dates, while making no latency SLA,
   always-ready guarantee, managed activation promise, or default warm-pool
   policy. This disposition requires no invented approval value.
2. **Public commitment:** present the exact SLO/guarantee/default, supported
   modes/environments, capacity and billing implications, and failure
   behavior, then wait for explicit founder approval before documentation or
   defaults change.

The evidence validator fails if public wording or configuration implies the
second disposition while the A3 record lacks exact claim, approver, and date.

### A3 exit gate

Every public activation/latency statement matches the recorded disposition.
No unsupported mode is simulated, catalog availability is never described as
resident readiness, and the local v0.5 release may proceed with measured/no-
SLA wording.

## T07 — Prove Hermes authoring, independent invocation, and lifecycle demos

### Goal

Prove the simple public story: author and deploy through Hermes, run through
AgentPaaS without that session, and optionally inspect/control from a later
Hermes session.

### Required demo runs

Use a clean Hermes profile and the founder-approved roster.

#### Demo A — local-first

1. Ask Hermes to build a customer-feedback report worker.
2. Choose local execution and approve the specific cloud recovery envelope.
3. Provide credential labels through the existing terminal-safe flow.
4. Approve the LLM-spend ceiling and maximum active duration.
5. Confirm generated worker source contains a route, not target names.
6. Test, package, and deploy an exact version plus alias.
7. Terminate the authoring Hermes process before invoking the alias through
   AgentPaaS CLI.
8. Inject and visibly label one local-model timeout through the fault proxy.
9. Observe automatic call recovery to an approved cloud target.
10. Observe later calls remain sticky to the recovery target for the active
    attempt.
11. Attach from a fresh Hermes session and confirm report artifact, exact
    deployment pin, ledger, and concise under-limit result.

#### Demo B — cloud-cost-first

1. Start from a separate clean profile with no local runtime assumption.
2. Choose economical cloud primary and approved capability recovery tiers.
3. Build, test, package, and deploy; then terminate Hermes.
4. Invoke through the authenticated AgentPaaS API.
5. Inject one eligible primary failure.
6. Observe one recovery and completion.
7. Confirm no third target and no time/spend expansion.
8. Confirm output uses “under the LLM spend limit,” not savings.

#### Demo C — whole-worker replan

1. Run a legitimate multi-phase task with a safe checkpoint.
2. Reach the attempt lease while progress is visible.
3. Confirm the runtime returns `NEEDS_REPLAN` rather than guessing.
4. Attach a later Hermes session or use the equivalent CLI control and confirm
   it sees only structured allowed actions.
5. Continue the same run once inside the current envelope.
6. Confirm the `FAILURE_CONTINUATION` attempt resumes committed work and a
   second failure continuation is impossible, including after pause/resume.

#### Demo D — long-running multi-turn worker

1. Ask Hermes to build, test, package, and deploy the B30 reference worker.
2. Terminate Hermes, invoke through AgentPaaS, and let the real run exceed six
   minutes and 20 turns while checkpoints advance.
3. Attach a fresh Hermes session and recover status from durable IDs.
4. Request pause; observe the typed `feature_not_enabled` response and that
   the run continues unaffected (pause/resume deferred per Fix 5).
5. In a separate run, request a limit amendment; observe the typed
   `feature_not_enabled` response and that terminal `EXPIRED`/
   `BUDGET_EXCEEDED` still fires exactly at the configured ceiling.
6. Confirm no former 60-second/two-minute/five-minute boundary ends the run,
   while explicit cancel and a deliberately shorter maximum active duration
   still win in labelled tests.

#### Demo E — deployment, invocation, alias, and restart lifecycle

1. Deploy `customer-report@1.2.0` and point
   `production/customer-report` to it.
2. Terminate Hermes and, in separate sequential bounded cases, invoke the
   alias through CLI, API, and one scheduler-style request.
3. Retry the same scheduler idempotency key/request and receive the same run;
   prove a changed payload conflicts.
4. Start a distinct overlap and observe retryable `ALREADY_RUNNING` with no
   hidden queued start.
5. Deploy `@1.3.0` and promote the alias while the `@1.2.0` run is active.
   Confirm the active run remains `@1.2.0` and, after its slot is released,
   the next alias invocation pins `@1.3.0`.
6. Restart the original run and confirm the new invocation defaults to
   `@1.2.0`, original input, stage one, and no checkpoint import. Separately
   exercise explicit current-alias resolution.
7. Roll back the alias and deactivate one exact version; confirm active/history
   state remains and prohibited future admissions fail.

#### Demo F — MCP service and three-stage pipeline

1. Ask Hermes to build one B33 MCP service and client plus a B34 three-stage
   workflow using logical package/service names.
2. Confirm the compiled snapshot contains immutable package digests and no raw
   internal port/capability in worker source.
3. Package/deploy, terminate Hermes, and invoke the workflow through AgentPaaS
   CLI/API.
4. Confirm the worker calls the separate service container. Have that service
   make one routed model call through the approved live roster and labelled
   fault proxy; prove shared selection/recovery, spend/token accounting, and
   service-lease fencing.
5. Confirm each separate stage advances from a durable bounded handoff/artifact.
6. Attach a fresh session and inspect final provenance; Hermes must not have
   relayed any MCP payload or handoff.

#### Demo G — parent and leaf children

1. Ask Hermes to build an approved parent plus three leaf workers.
2. Confirm shared workflow spend/active-time ceilings, child allowlist,
   fan-out/concurrency bounds, and all-required behavior before deployment.
3. Package/deploy, terminate Hermes, and invoke the parent through AgentPaaS.
4. Confirm separate child containers finish out of order while ordered durable
   results return to the parent.
5. Confirm the parent collates and continues; AgentPaaS/Hermes does not produce
   the collation.
6. Inspect cleanup and prove no child can recursively spawn.

#### Demo H — catalog discovery and secure A2A delegation

1. Use Hermes to build and test separate orchestrator, worker, verifier, and
   testing-agent packages plus one MCP service package.
2. Submit signed registration proposals and approve promotion into a private
   tenant catalog; confirm catalog presence does not start a process.
3. From the orchestrator, query bounded capability roles and resolve exact
   compatible versions under the signed workflow policy.
4. Delegate one task through the AgentPaaS gateway using logical identities and
   an A2A-compatible adapter; transfer one input artifact through the encrypted
   broker and wait on durable completion events.
5. Prove an undeclared sender/receiver edge, direct endpoint, cross-tenant
   query, and stale artifact grant all fail closed and are audited.

#### Demo I — interactive streaming and activation efficiency

1. Invoke a multi-turn streaming-capable worker through the durable admission
   path and consume ordered deltas without making the stream connection own
   the run.
2. Disconnect, reconnect after the last cursor, restart the daemon once, and
   observe no gap, duplicate committed result, or repeated external call.
3. Let the worker suspend at a safe interactive wait, submit approved input
   through the inbox, and observe event-driven wakeup without polling or tmux.
4. Run the same bounded fixture in cold-image, cached-cold, warm, and resident
   modes where supported; report p50/p95/p99 AgentPaaS overhead separately from
   provider/tool time and verify dormant/warm authority invariants.

### Transcript assertions

- Hermes asks one question at a time.
- The authoring Hermes process is absent before release-proof invocations.
- Local use is optional.
- No user is asked to select the exact recovery model.
- No raw key appears in chat/tool arguments.
- No model/provider/progress text controls authority.
- A plugin subprocess wait timeout is followed by a status query.
- Requested deployment reference and exact resolved version/digests match
  durable admission evidence.
- Invocation retries, concurrency rejections, alias moves, restart, and
  deactivation use the approved semantics.
- `PAUSE_REQUESTED` is never described as frozen; `PAUSED` proves no active
  capability/reservation or new time/spend.
- Limit amendment shows absolute old/current/new values, explicit
  confirmation, and the exhaustion-race winner.
- Every recovery is explained in ordinary language.
- Technical details remain available through inspect/timeline.
- Completion is not called verified or correct.
- The dollar amount is exact and is not called savings.
- Fault injection is obvious in every public result.
- Every service, stage, parent, and child is visibly a separate AgentPaaS
  container boundary.
- Hermes is absent before runtime start and never becomes the workflow
  coordination/data-transfer path.
- Catalog resolution records the query constraints and exact selected package;
  availability never means a resident process.
- Agent-to-agent ingress and egress both match the immutable communication
  snapshot, and every file crosses the artifact broker under a scoped grant.
- Stream disconnect/reconnect, interactive waits, and terminal notification
  are cursor/event driven; no correctness polling or tmux dependency appears.

### Exit gate

RR-04, RR-05, RR-08, RR-16, and RR-19–RR-31 are PASS in deterministic and
required live modes, with sanitized transcripts referenced from the
consolidated evidence.

## T08 — Truth-sync product docs, examples, and the Golden Loop

### Goal

Make the public promise, actual product, known limitations, and release proof
say the same thing.

### Required documentation work

Update and reconcile:

- `README.md`
- `CHANGELOG.md`
- `Agentpaas-pitch.md`
- `docs/roadmap.md`
- `docs/known-limitations.md`
- `docs/quickstart.md`
- `docs/policy-reference.md`
- `docs/how-enforcement-works.md`
- SDK usage documentation
- Hermes plugin `SKILL.md` and setup guidance
- `docs/execution/golden-loop-test.md`
- Release notes and Homebrew descriptions

### Required public content

1. Lead with:

   > Run any AI agent safely and efficiently—with credentials, costs, and
   > network access under control.

2. Explain the required stack:
   - Hermes is the supported authoring, testing, packaging, and deployment
     environment, plus an optional later operations client.
   - AgentPaaS skills guide Hermes.
   - Generated workers/services use the SDK.
   - AgentPaaS CLI/API, cron, Kubernetes, or another process can trigger a
     deployed workflow without Hermes.
   - The AgentPaaS runtime governs, routes, supervises, advances stages,
     carries handoffs, schedules approved children, and records evidence.
   - External callers and Hermes are not runtime orchestrators or message
     buses.
3. Include one simple customer-feedback report example.
4. Explain local-first and cloud-cost-first without presenting local execution
   as mandatory.
5. Define:
   - Logical route.
   - LLM spend limit.
   - Immutable deployment version and audited alias.
   - Requested deployment reference versus exact resolved version.
   - Durable invocation idempotency and visible concurrency rejection.
   - Automatic call recovery.
   - Safe checkpoint.
   - One operator-directed failure continuation.
   - Accumulated maximum active duration versus “no hidden platform ceiling.”
   - Cancel, pause/resume, restart, and explicit pre-terminal absolute limit
     amendment.
   - Logical MCP service binding.
   - Private component catalog versus live runtime directory.
   - Runtime-profile compatibility and exact capability resolution.
   - A2A-compatible agent tasks and MCP tool/service calls, both governed by
     canonical AgentPaaS state and gateway policy.
   - Two-sided communication authorization and encrypted artifact grants.
   - Durable event/cursor completion and interactive wait/wakeup without
     polling or tmux.
   - `on_demand`, `warm`, and `resident` activation semantics.
   - Linear pipeline handoff.
   - Bounded leaf-child spawn/join and parent-owned collation.
6. State exact limitations:
   - No correctness certification.
   - No learned optimization or savings claim.
   - No automatic context compaction/decomposition.
   - No automatic OAuth reauthentication.
   - No external orchestrator adapters.
   - One recovery selection per attempt and one failure-driven worker
     continuation.
   - Linear pipelines only; no general DAG/branch/cycle/compensation.
   - Leaf children only, fan-out/concurrency bounded, all children required;
     no recursive swarm or partial-success policy.
7. Publish only A1-approved targets, A2-approved time defaults, and
   activation/latency wording permitted by the A3 disposition.
8. Date/source price examples or use clearly synthetic numbers.
9. Distinguish local/subscription marginal spend from infrastructure or plan
   cost.
10. Extend the Golden Loop from the v0.2.3 lifecycle with:
    - Routed build.
    - Package, immutable deploy, and alias inspection.
    - Terminate Hermes before CLI/API invocation.
    - Scheduler/API idempotency and concurrency rejection.
    - Route/policy inspection.
    - Multi-turn run.
    - Injected recovery.
    - Checkpoint and one continuation.
    - Spend/attempt evidence.
    - Real long-running/multi-turn proof.
    - Cross-container MCP service call.
    - Safe pause, frozen time/spend, exact resume, linked restart, and an
      explicitly confirmed pre-terminal limit amendment.
    - Three-stage runtime-native pipeline with Hermes absent before invocation.
    - Parent/three-child collation with Hermes absent before invocation.
    - Hermes registration proposal, catalog promotion, constrained role query,
      and exact worker/verifier/testing-agent pins.
    - Real streaming with disconnect/reconnect, durable replay, interactive
      inbox wakeup, and one canonical terminal result.
    - Secure A2A delegation plus brokered encrypted artifact transfer with a
      labelled two-sided-policy denial.
    - Cold/cached/warm/resident performance evidence separating AgentPaaS
      overhead from provider/tool latency.
    - Existing export/receive/install/provenance lifecycle.

### Truth-language gate

Fail docs/tests for unqualified claims such as:

- “best model”
- “optimal model”
- “saved $”
- “free local model”
- “total task cost” when only LLM spend is measured
- “verified correct”
- “guaranteed quality”
- “unlimited fallback”
- “supports any orchestrator”

Contextual explanations of why these claims are not made are allowed.

### Release metadata reconciliation

Audit and resolve all version/package sources, including:

- GoReleaser build paths and ldflags.
- CLI and daemon version output.
- Hermes plugin version.
- SDK package version, if distributed independently.
- Static `Formula/agentpaas.rb` versus the actual tap/cask generated by
  GoReleaser.
- README and quickstart install command.
- Changelog and release notes.

Remove or clearly retire obsolete release templates rather than leaving a
public `0.1.0` formula alongside a v0.5.0 release path.

### Tests

- Markdown link check.
- Version-string consistency script.
- Forbidden-claim scan with explicit allowlisted explanatory contexts.
- All YAML/code snippets parse and validate.
- Copy/paste quickstart smoke on a clean home/profile.
- Legacy quickstart remains available and truthful.
- Golden Loop expected evidence maps to RR IDs.

### Exit gate

Every public claim maps to passing evidence, every current limitation is
visible, and no stale release/version path contradicts v0.5.0.

## T09 — Build and test the release candidate on a fresh install

### Goal

Test the actual artifacts and public instructions before any permanent release
tag is created.

### Required work

1. Select the exact release commit and require a clean worktree.
2. Run the complete automated gate from a clean clone.
3. Run proto generation and assert zero drift.
4. Run GoReleaser config validation and a local snapshot:
   - Both supported macOS architectures.
   - Linux ARM64 harness.
   - Archives and checksums.
   - SBOM generation.
   - Signature command/config validation without publishing.
5. Install the snapshot artifact in an isolated macOS user/home or clean
   machine; do not use binaries from the source tree.
6. Verify:
   - CLI/daemon protocol compatibility.
   - Version and commit output.
   - Harness is the matching release artifact.
   - File modes and quarantine/install behavior.
   - Hermes plugin installation from the documented path.
7. On a fresh Hermes profile, follow only public instructions to author, test,
   package, and create one exact deployment plus alias. Terminate Hermes before
   invoking it through the installed AgentPaaS CLI/API, then attach a fresh
   session for inspection.
8. From installed artifacts—not source-tree binaries—run deterministic packaged
   B33 MCP, B34 pipeline, and B35 parent/child deployments with Hermes absent
   before invocation.
9. Exercise installed-product lifecycle proof:
   - Invocation idempotency and default concurrency rejection.
   - Alias promotion/rollback and exact-version deactivation.
   - Pipeline pause/resume.
   - Linked exact-version restart.
   - One explicitly confirmed pre-terminal limit amendment.
10. Separately test upgrade from published v0.2.3, stable v0.3, and stable
    v0.4 installations:
   - Existing projects, installed bundles, exact catalog registrations,
     deployments, aliases, A2A tasks/artifacts, and v0.4 workflow records
     remain.
   - Daemon restart and state migration succeed at every supported hop.
   - Legacy, catalog/A2A, MCP/pipeline/parent-child, and routed runs work with
     their approved semantics.
11. Exercise the Homebrew release path in an isolated test tap/cask or equivalent
   non-publishing dry run. Validate uninstall/reinstall as well as upgrade.
12. Record every undocumented prerequisite or confusing step as a defect.
    Fix it and repeat from clean state; do not annotate around it as a known
    manual trick unless it is an approved limitation.

### Clean-machine PASS definition

RR-17 is PASS only when:

- The environment did not have a repository checkout or development binaries
  on `PATH`.
- Public instructions were the only setup guide.
- Credentials were entered through the terminal-safe path.
- The operator deployed and independently invoked a completed Routed Run and
  could explain:
  - Why the authoring Hermes session was not required at runtime.
  - The requested deployment ref and exact resolved version.
  - Which policy pattern was used.
  - Whether recovery occurred.
  - The LLM spend limit and metered spend.
  - Where to inspect technical evidence.
  - How to cancel, pause/resume, restart, or explicitly amend a non-terminal
    limit.
- No maintainer edited generated YAML/code to make the run pass.

Record operator relationship (maintainer/non-maintainer) honestly. One fresh
environment is the v0.5 release gate; additional practitioner trials belong to
the post-v0.5 adoption phase and must not be fabricated here.

### Exit gate

RR-17 passes, the RR-18 pre-publish checklist has no failure while the RR-18
row remains NOT RUN, and no P0/P1 release defect remains.

## T10 / R41 — Close and publish stable v0.5.0

### Goal

Publish only the exact approved and proven commit, then verify what users
actually receive.

### Pre-publish checklist

1. `make block41-gate` passes from a clean clone (with the verified offline
   bundle mounted and network denied when the stretch bundle exists;
   otherwise with network and pinned tool versions recorded per Fix 7).
2. RR-01–RR-17 and RR-19–RR-31 are PASS.
3. The RR-18 pre-publish checklist has no failure and the RR-18 evidence row
   remains `NOT RUN`.
4. A1 model roster approval is recorded.
5. A2 timeout-default approval is recorded.
6. The A3 claim disposition is recorded and every public commitment has its
   required explicit approval.
7. All B26–B40 block gates pass.
8. All CI and release-verify workflows are green on the release commit.
9. Zero open P0/P1 defect.
10. Lower-severity open defects have an explicit ship/defer disposition in the
   consolidated evidence.
11. Changelog, release notes, docs, plugin, package metadata, and Homebrew
    description agree.
12. Release commit is immutable, pushed, and identified by full SHA.

### Required automated commands

`make block41-ci` must include or invoke the repository-equivalent of:

```text
buf generate && git diff --exit-code
go build ./...
go test ./... -count=1
go test -race ./...
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
osv-scanner scan -r .
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
make golden-fast
make golden-slow
AGENTPAAS_DOCKER_TESTS=1 make golden-docker
AGENTPAAS_DOCKER_TESTS=1 make e2e-network
make redteam-smoke
goreleaser check
```

When the stretch offline bundle exists, its preparation job is not part of
`block41-ci`: it runs first for the exact candidate commit, publishes the
content-addressed bundle as a CI artifact, and records its manifest digest.
`block41-ci` then runs every command above with network denied and only the
verified bundle available. When the bundle does not exist (Fix 7 fallback),
`block41-ci` runs with network and pinned tool versions, and the evidence
file states the mode explicitly.

Also run the B26–B41 focused/adversary suites and the docs/version/evidence
validators. Record exact tool versions and any required environment setup.

### Publish authorization — mandatory stop

After presenting the complete evidence summary, exact commit, open risks, and
release commands, the orchestrator must ask:

> Approve publishing commit `<full-sha>` as `v0.5.0`?

Only an explicit approval authorizes tag creation and push. Approval of A1,
A2, A3, an earlier block, or the plan itself is not publish authorization.

### Publish and post-publish verification

After approval:

1. Create the annotated `v0.5.0` tag on the approved commit.
2. Push the tag.
3. Watch every required release workflow to completion.
4. Verify GitHub release assets:
   - Expected architecture archives.
   - Checksums.
   - SBOMs.
   - Sigstore/signature material.
   - Release notes.
5. Verify checksum and signature from a separately downloaded artifact.
6. Install/upgrade through the public Homebrew command on a clean environment.
7. Verify `agentpaas version`, daemon version, protocol, and commit.
8. Install/enable the public Hermes integration and run a bounded smoke:
   - One legacy run.
   - Author/deploy one Routed Run, terminate Hermes, then invoke through the
     public CLI/API without forced recovery.
   - One deterministic/labelled recovery proof when the public demo mechanism
     is intended to ship.
   - Packaged deterministic B33 MCP, B34 pipeline, and B35 parent/child smoke
     fixtures with Hermes absent before invocation.
   - One bounded pause/resume plus invocation-idempotency/concurrency proof.
   Keep every live model call inside the still-valid operational test
   envelope; publish approval alone does not authorize additional model
   spend or data transfer.
9. Verify public links, examples, catalog/defaults, and evidence URL.
10. Mark RR-18 PASS and append post-publish results to the consolidated
    evidence on `main`.
11. Run `make block41-postrelease-gate` against the updated evidence.

If post-publish verification fails:

- Do not move or recreate the tag.
- Mark RR-18 FAIL.
- Stop promotion and document the exact affected artifact.
- Prepare the smallest patch release through a new approved release process.

### Block success gate

B41 and v0.5.0 are complete only when:

1. All RR rows are PASS.
2. A1 and A2 are explicitly approved, and A3 is approved wherever the release
   makes a public commitment rather than measured/no-SLA disclosure.
3. Publish authorization names the exact released commit.
4. Release workflows and public install verification pass.
5. No unsupported correctness, savings, or universal-orchestrator claim is
   published.
6. The complete evidence remains reproducible and sanitized.
7. Long-running, MCP-service, pipeline, parent/child, independent-deployment,
   invocation-safety, and operator-lifecycle claims each map to RR-19–RR-31
   and use the shipped artifacts.

## Handoff record required after every task

Append to the block handoff record:

- Task/date/commit.
- Proof IDs advanced.
- Tests written before implementation.
- Files changed.
- Exact commands and PASS/FAIL output.
- Faults injected and their visible labels.
- Sanitization result.
- Live metered spend, if any.
- Compatibility and migration impact.
- Defects opened/closed and severity.
- Manual gate or approval status.
- Residual risk.
- Next task unblocked.

The handoff must never say “all good” when a required live, clean-machine, or
approval row remains NOT RUN/BLOCKED.

## Pitfalls

- Do not use live model prose as the expected answer.
- Do not hide fault injection to make recovery look spontaneous.
- Do not call an under-limit amount savings.
- Do not call local/subscription execution free.
- Do not reset spend, active time, or failure-continuation count on
  continuation or operator resume.
- Do not call `PAUSE_REQUESTED` paused or freeze time before full fencing.
- Do not amend a terminal run or let Hermes self-confirm an amendment.
- Do not mutate an exact deployment, re-resolve a pinned alias, or hide a
  rejected invocation in a queue.
- Do not let a successful late exit from a fenced worker win the race.
- Do not retry release tests until one passes and report only the successful
  run.
- Do not publish a public roster from stale pricing or untested capability
  claims.
- Do not leave conflicting binary, plugin, formula, or documentation versions.
- Do not tag or publish without the final exact-commit approval.
- Do not let Hermes, an external trigger, or a test harness secretly relay MCP,
  handoff, or child-result data in a workflow proof.
