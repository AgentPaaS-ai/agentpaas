# Block 38 — Shared Workflow LLM Spend Budget and Cost Ledger

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Target release:** v0.5.0
**Depends on:** B26, B35, B36, and B37 complete; `make block37-gate` green
**Must complete before:** B39
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65

## Outcome

B38 makes the v0.5 cost claim enforceable and auditable. At block completion:

- One workflow has one current hard LLM spend ceiling shared by every standalone run,
  pipeline stage, parent, child, model call, recovery request, and worker
  attempt inside that workflow.
- Existing token limits are enforced at both node/run level and as shared
  workflow aggregates. A standalone routed run is represented as a one-node
  workflow and therefore uses the same accounting path.
- Every call reserves a conservative maximum before network activity.
- Streaming usage updates decrement the existing reservation incrementally and
  are idempotent by logical call/event sequence; they never authorize beyond
  the original reservation or committed workflow ceiling.
- Actual provider usage/cost is reconciled after success or failure.
- Unknown timeout charges convert the conservative reservation into retained
  unreconciled exposure that reduces headroom but authorizes no request.
- Input, uncached input, cached input, cache write, output, reasoning, and
  request charges are represented without double counting.
- Local and subscription targets report cost basis without being called free.
- Selector, harness, CLI, operator response, and audit use one cost
  engine and one pinned price catalog snapshot. The dashboard is disabled
  by default since v0.1.1 and is out of scope for cost surfaces (Fix 6).
- “Under budget” is computed exactly and never presented as savings.
- A scoped B39 administrative amendment may atomically raise the absolute
  current ceiling before terminal exhaustion; original limits, all amendments,
  and spend remain append-only and visible.

The first budget is explicitly **metered LLM spend**. It excludes local
hardware, infrastructure, network, storage, and paid external tools.

Every routed workflow starts with an explicit hard `max_cost_usd`, including an explicit
zero when only zero-marginal candidates are authorized. “No hard limit” is a
legacy/non-routed reporting case, not a valid v0.5 Routed Run configuration.

“Hard” means AgentPaaS never authorizes a physical request whose conservative
reservation would cross the current committed ceiling. It cannot guarantee
that a provider will never report actual cost above that reservation; any such
variance is recorded as real overage and makes the workflow terminal before
another call. An authorized human may approve a higher ceiling before the run
is terminal. An amendment is not a refund: committed spend and unreconciled
exposure are never reduced or reset. Once `BUDGET_EXCEEDED` is terminal,
amendment is rejected.

The budget owner is the durable B26 `workflow_id`, never an attempt, container,
pipeline node, or child batch. All lower-level records carry that key. Creating
a stage, child, retry, or continuation cannot mint a new ledger or reset spend.

## Locked monetary representation

Use integer nano-USD internally:

```text
1 USD = 1,000,000,000 nano-USD
```

Reasons:

- Per-token prices can be below one micro-dollar.
- Binary floating point is unsuitable for hard enforcement.
- Signed 64-bit nano-USD comfortably represents v0.5 workflow limits.

Parse external decimal strings with exact decimal arithmetic. Round
conservative estimates and positive provider-reported actual charges upward
to the next representable nano-USD. Enforcement must never round a charge
down.

API fields may retain existing `double` values for backward compatibility, but
all routed enforcement and canonical ledger fields use decimal strings and/or
integer nano-USD. Text/JSON output must derive from the exact value.

## Locked usage contract

Normalized usage contains:

```text
input_tokens_total
input_tokens_uncached
cached_input_tokens
cache_write_tokens
output_tokens
reasoning_tokens
total_tokens
usage_source: provider | estimated
provider_reported_cost
```

Invariants:

- All values are non-negative.
- Cached and cache-write tokens are explicitly related to input tokens
  according to the adapter’s documented provider semantics.
- Reasoning tokens state whether they are included in output tokens.
- Unknown fields are absent, not silently zero.
- Estimated tokens are visibly labelled.

Provider-specific ambiguity is resolved in the adapter. The generic cost
engine never guesses whether one provider’s cached-token field is inclusive.

## Locked reservation semantics

Before each physical request:

1. Estimate input tokens with B37’s verified tokenizer or byte-based upper
   bound; never use a non-conservative average-token heuristic.
2. Assume uncached input unless a guaranteed provider contract says otherwise.
3. Use the highest applicable input/cache-write rate.
4. Add the full requested output cap at the output/reasoning rate.
   - If reasoning is included in the output cap, do not add it twice.
   - If reasoning is separately billable, use the catalog’s conservative
     maximum for the selected effort.
   - If separately billable reasoning has no enforceable maximum, mark the
     target `PRICE_UNBOUNDED` under a hard spend limit.
5. Add any request fee.
6. Atomically reserve the resulting maximum.
7. Reject the request if committed spend plus active reservations plus
   unreconciled exposure
   would exceed either the node/run sublimit or shared workflow limit.

After a result:

- Prefer provider-reported cost when it is trustworthy and correlated to the
  request.
- Otherwise calculate from normalized provider usage and the pinned price
  card.
- Otherwise calculate an explicitly estimated cost.
- Commit actual cost and release unused reservation.

After a timeout/connection failure with unknown billing:

- End the active reservation and convert its full amount to immutable
  `unreconciled_exposure`.
- Count that exposure against current headroom but never treat it as an active
  request capability or reuse it for recovery.
- Show it separately in output.

If provider-reported actual cost exceeds the reservation:

- Record the variance and actual cost.
- Mark the budget exhausted/over limit.
- Block every later call.
- Never hide the overage or clamp the displayed total.

There is no protected fallback reserve in v0.5.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Implement exact money and normalized usage | B36 catalog | property/golden tests cover all price dimensions |
| 2 | T02 Parse provider usage and cost consistently | T01, B37 adapters | provider conformance fixtures pass |
| 3 | T03 Implement durable workflow reservation/reconciliation ledger | T01, B26/B35 store | atomic cross-stage/child/attempt and crash tests pass |
| 4 | T04 Enforce shared spend/token limits in model controller | T02, T03 | no request starts without reservation; recovery and every workflow node share the limit |
| 5 | T05 Expose truthful cost evidence | T03, T04 | CLI/operator/audit parity passes (dashboard excluded per Fix 6) |
| 6 | T06 Handle local, subscription, stale, and unknown pricing | T01–T05 | basis/freshness/unpriced decision matrix passes |
| 7 | T07 Block gate and adversary review | T01–T06 | `make block38-gate` passes |

## T01 — Implement exact money and normalized usage

### Goal

Create one reusable cost engine that is precise enough for enforcement and
clear enough for reporting.

### Required work

1. Complete and reuse B26’s exact `Money`/`NanoUSD` types with:
   - Checked add/subtract/multiply.
   - Decimal parse/format.
   - Upward conservative rounding.
   - Overflow detection.
2. Add normalized `Usage` and `CostBreakdown`:
   - Input.
   - Cached input.
   - Cache write.
   - Output.
   - Reasoning.
   - Request fee.
   - Total.
3. Define price-card semantics for:
   - Reasoning included in output.
   - Reasoning separately billed.
   - Maximum billable reasoning by effort.
   - Cache write absent.
   - Subscription/local mode.
   - An aggregator candidate envelope: reserve at the highest applicable rate
     across every signed upstream that could serve the request.
4. Compute:
   - Conservative maximum.
   - Actual catalog cost.
   - Variance from provider-reported cost.
5. Include source, catalog version/digest, price effective/verified time, and
   estimated/provider-reported flags.
6. Delete or delegate every remaining independent production cost formula.
7. Keep legacy dashboard float fields by converting at the final rendering
   boundary only.

### Likely files

- `internal/modelcatalog/**`
- new `internal/llmcost/**`
- `internal/llm/provider.go`
- `internal/dashboard/cost.go`
- cost tests

### Tests to write first

- Known decimal/nano-USD vectors.
- Very low per-token price.
- Large run and overflow.
- Conservative upward rounding.
- Cached-input subset.
- Cache-write separate.
- Reasoning included/separate.
- Request fee.
- Missing optional dimension.
- Provider cost variance.
- Multi-upstream aggregator envelope uses the conservative maximum rate.
- Property: total equals exact line-item sum.
- Property: conservative estimate is never below catalog cost for a usage
  vector within requested caps.

### Exit gate

All production cost output is generated through one exact engine.

## T02 — Parse provider usage and provider-reported cost

### Goal

Normalize real provider responses without double counting or fabricating
missing cache data.

### Required work

1. Extend `NormalizedModelResult` with the locked usage contract.
2. For every supported provider, add fixtures for:
   - Ordinary input/output usage.
   - Missing usage.
   - Cached input when supported/reported.
   - Cache write when supported/reported.
   - Reasoning tokens when supported/reported.
   - Provider-reported cost when available.
   - Error response containing usage/cost when available.
3. Preserve the raw provider model/request ID only as bounded non-secret
   correlation metadata.
4. Validate impossible usage:
   - Negative values.
   - Cached tokens greater than total under provider semantics.
   - Overflow.
5. Decide trust precedence per adapter:
   - Correlated provider-reported cost.
   - Provider usage plus pinned price.
   - Explicit estimate.
6. Record failed-call usage when returned.
7. Do not infer cache hits from repeated prompt digests.
8. Do not assume cache state survives a model/provider switch.

### Likely files

- provider adapter files
- provider response fixtures
- `internal/modelrouter` result handling
- tests

### Tests to write first

- One fixture for every provider/usage dimension.
- Missing usage remains estimated/unknown.
- Invalid usage rejected.
- Failed physical request with charge included.
- Model switch never reuses cache assumption.
- Provider-reported cost correlation mismatch rejected.
- Secret/raw response body absent from normalized errors.

### Exit gate

Every supported adapter either supplies trustworthy normalized usage or
explicitly reports that it cannot.

## T03 — Implement the durable budget ledger

### Goal

Serialize spend across model recovery, worker attempts, pipeline stages, and
parent/child runs with crash-safe reservations.

### Required work

1. Define a `BudgetLedger` interface:
   - `Snapshot`
   - `Reserve`
   - `Commit`
   - `Release`
   - `MarkUnreconciled`
   - `Reconcile`
   - `AmendLimit`
2. Implement it in the B26 workflow store with append-only events and a
   materialized generation/checksum. Standalone routed runs use their implicit
   one-node workflow; do not retain a second run-only enforcement ledger.
   Every event carries the B26 state schema version.
3. Ledger event types:
   - `limit_set`
   - `reservation_created`
   - `cost_committed`
   - `reservation_released`
   - `reservation_converted_to_unreconciled_exposure`
   - `provider_cost_reconciled`
   - `limit_amended`
   - `budget_exhausted`
4. Key every reservation by workflow, node, run, logical call, physical
   request, attempt, and target. Include parent batch/child identity when
   applicable, but never use it as the budget owner.
5. Atomically enforce:

```text
committed + active reservations + unreconciled exposure <= limit
```

before issuing a new reservation.
6. Persist workflow aggregate token counts plus optional node/run sublimit
   counters in the same transaction.
7. Rebuild materialized totals from ledger on startup and compare checksums.
8. A malformed/tampered ledger fails closed; it never resets spend to zero.
9. Support fake in-memory ledger/fake clock for tests.
10. No agent/trigger input can commit, release, or reconcile a reservation.
11. **DEFERRED (Fix 5, 2026-07-19):** `AmendLimit` activation. The ledger
    schema and amendment event types are implemented and tested at the store
    level, but the runtime amendment path returns `feature_not_enabled` in
    v0.5. Absolute increase-only commits, scope/confirmation checks, and the
    amendment-versus-exhaustion race activate in the post-v0.5 minor
    release. Terminal `BUDGET_EXCEEDED` remains final in all versions.
12. Same idempotency key and identical amendment returns the original result;
    changed value/reason fails conflict. Worker/model/ordinary invocation
    credentials can never call this method (enforced in v0.5 via the
    not-enabled refusal).

### Likely files

- `internal/llmcost/ledger.go`
- `internal/routedrun/localstore.go`
- daemon/run state integration
- tests

### Tests to write first

- Reserve/commit/release happy path.
- Two physical requests under one logical call.
- Cross-attempt shared limit.
- Cross-pipeline-stage shared limit.
- Concurrent sibling children share one limit and cannot double-reserve.
- Parent continuation and child-batch replay do not create a fresh ledger.
- Concurrent reservation race at boundary.
- Duplicate commit/release idempotency.
- Crash after reserve, before request.
- Crash after response, before commit.
- Timeout reservation converted to unreconciled exposure.
- Provider reconciliation later.
- Malformed/truncated/tampered ledger.
- Overflow and negative value.
- Limit cannot be raised by untrusted input.
- Authorized absolute increase before exhaustion preserves every prior charge
  and immediately changes reservation capacity.
- Amendment replay/conflict, missing scope/confirmation/reason, decrease,
  overflow, and amendment-versus-`budget_exhausted` race.

### Exit gate

Race tests prove that two requests cannot both reserve the same remaining
budget, and restart never forgets committed spend/unreconciled exposure.

## T04 — Enforce shared spend and token limits

### Goal

Connect exact accounting to B37’s physical request controller.

### Required work

1. Pin original `max_cost_usd`, aggregate token limits, node/run sublimits, and
   price catalog snapshot when the workflow starts. Every node references the
   same ledger and current authority generation; approved amendments change
   only the current workflow ceiling.
2. Before primary and recovery physical requests:
   - Check lease/active-time state.
   - Calculate maximum.
   - Reserve spend and maximum tokens.
3. Pass the approved `max_output_tokens` to the provider.
4. If the call cannot fit:
   - Return `LLM_BUDGET_EXHAUSTED`.
   - Do not issue network request.
   - Record candidate exclusion/denial evidence.
5. Reconcile every terminal network path.
6. A failed primary’s committed cost/unreconciled exposure reduces recovery
   budget.
7. Recovery selection reruns eligibility with current remaining budget.
8. Routed worker retry in B39 receives the same workflow ledger, not a reset
   budget.
9. B33 model-using MCP services, B34 stages, and B35 parent/children authorize
   every physical request through this same workflow ledger. Starting a
   service/node/child must fail closed if the ledger cannot be loaded or its
   generation cannot be advanced atomically.
10. Preserve legacy token-budget behavior for non-routed runs.
11. If actual spend crosses limit, terminate future model calls across the
    entire workflow and expose the actual overage.
12. Fully `PAUSED` and `NEEDS_REPLAN` states permit no new or active reservation
    or physical request. `PAUSE_REQUESTED` may finish already-active work until
    B39 reaches a safe boundary. Before a frozen state commits, every active
    reservation is committed, released, or converted to conservative
    unreconciled exposure.

### Tests to write first

- Primary fits and commits.
- Primary failure consumes budget; recovery still fits.
- Primary failure leaves insufficient recovery budget.
- Recovery reserve denied pre-network.
- Successful two-attempt run shares one limit.
- Two pipeline stages share one limit.
- A model-using MCP service call shares its caller workflow spend/token ledger,
  including recovery and fencing.
- Concurrent child calls race for the same remaining amount; only one fits.
- Failed child and resumed parent preserve committed spend and unreconciled
  exposure.
- `max_tokens_per_request` enforced.
- Workflow token total shared across nodes and attempts.
- No protected fallback reserve.
- Actual greater than reservation.
- Unknown timeout charge becomes inert unreconciled exposure and continues to
  reduce headroom.
- Cancellation releases only demonstrably unissued reservation.
- Pause-request drain reconciles all reservations; paused/needs-replan request
  attempts are denied without mutation.
- Authorized amendment creates room for a later request; terminal exhaustion
  followed by amendment remains denied.

### Exit gate

No routed physical request in any standalone, pipeline, parent, or child node
begins without an atomic reservation from the same workflow ledger.

## T05 — Expose truthful cost evidence

### Goal

Make the simple user-facing result traceable to exact ledger data.

### Required work

1. Extend B26 workflow/run/attempt reports with:
   - Original and current LLM spend ceilings.
   - Ordered amendment IDs, actor/reason references, before/after values, and
     authority generation.
   - Committed metered spend.
   - Active reservations.
   - Unreconciled exposure.
   - Remaining/under-limit amount.
   - Overage when present.
   - Tokens by dimension.
   - Calls/physical requests by target.
   - Cost by target and attempt.
   - Billing basis.
   - Catalog version/digest.
   - Estimated/provider-reported flags.
2. Extend:
   - `summarize`
   - `timeline`
   - Hermes contract fixtures
   - audit query rendering
   The dashboard run cost endpoint is out of scope (dashboard disabled since
   v0.1.1; Fix 6). If the dashboard is ever re-enabled, it reads through the
   same `internal/llmcost` engine — no separate cost formula is added.
3. Use exact text:

```text
Current LLM spend limit: $2.00
Committed metered LLM spend: $1.37
Unreconciled exposure: $0.00
$0.63 under the LLM spend limit
```

   Compute the under-limit amount as:

```text
limit - committed spend - active reservations - unreconciled exposure
```

   Never present unreconciled exposure as money still available.
   When amended, show both values and do not imply savings:

```text
Original LLM spend limit: $2.00
Current LLM spend limit: $3.00
Limit amended by an authorized operator before terminal exhaustion
```
4. When no hard limit exists on a legacy/non-routed run, omit “under limit.”
   A routed run without an explicit hard limit must have failed policy
   validation before execution.
5. When over:

```text
Metered LLM spend: $2.03
$0.03 over the $2.00 LLM spend limit
No further model calls were permitted
```

6. Include a visible note:

```text
Excludes local compute, infrastructure, storage, network, and external-tool cost.
```

7. Never use `saved`, `savings`, `optimized`, `cheapest overall`, or `total
   task cost` without a separate measured baseline.
8. Keep prompt/response content out of cost output.

### Tests to write first

- Exact under-limit arithmetic.
- Original/current/amendment-history rendering and redaction.
- Amendment raises headroom without changing committed spend/unreconciled
  exposure or rewriting historical “under limit” snapshots.
- No-limit output.
- Overage output.
- Unreconciled output.
- Mixed estimated/provider-reported output.
- Local/subscription basis.
- CLI/operator totals identical.
- Forbidden-claim grep.
- Existing legacy cost JSON fields remain.

### Exit gate

The public example can be reproduced from one ledger, and every surface shows
the same exact total.

## T06 — Handle local, subscription, stale, and unknown pricing

### Goal

Avoid false zero-cost selection and unsafe stale price assumptions.

### Required work

1. Billing mode behavior:
   - `metered`: enforce catalog/provider metered USD.
   - `subscription`: marginal metered spend may be zero; display subscription
     basis and unknown included-plan infrastructure/account cost.
   - `local`: marginal provider spend may be zero; display local basis and
     excluded compute/electricity/hardware cost.
2. A provider-reported charge overrides assumed zero marginal spend.
3. Quota/subscription exhaustion remains a B37 failure and may select an
   independently authorized target.
4. Unknown price:
   - Candidate is ineligible under a hard LLM spend limit.
   - It may not be treated as zero.
5. Price freshness:
   - Add explicit catalog validity/freshness metadata.
   - Generate a release calibration report comparing current primary provider
     documentation with catalog entries.
   - Propose a validity/maximum-age policy and offline update workflow.
   - Treat that policy as part of the B41 A1 roster approval, not as an
     unrecorded third approval gate.
   - Do not publish it before A1 approval.
   - Automated tests use an injected threshold and cover fresh, stale, and
     expired cases.
6. Permit a signed administrator price ceiling for a known cloud target only
   when it is at least as conservative as the embedded catalog. A lower
   override cannot reduce enforcement cost.
7. Mark administrator-asserted custom/local prices visibly.
8. Runtime selection never performs a mutable web lookup.

### Tests to write first

- Local/subscription zero-marginal basis.
- Provider charge on subscription target.
- Unknown price ineligible.
- Fresh/stale/expired injected threshold.
- Higher signed ceiling accepted; lower override cannot reduce cost.
- Quota failure leaves cost ledger intact.
- No “free” or “total task cost” language.

### Exit gate

Every eligible target has a bounded, labelled cost basis, and the final
freshness policy remains pending as an explicit part of B41 approval A1.

## T07 — Block gate and adversary review

### Required `make block38-gate`

Run:

```text
make block37-gate
go test ./internal/llmcost/... ./internal/modelcatalog/... -count=1 -race
go test ./internal/modelrouter/... ./internal/harness/... ./internal/routedrun/... ./internal/daemon/... -count=1 -race
go test ./internal/operator/... ./internal/cli/... -count=1 -race
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

Run Docker fake-provider cost/recovery scenarios with
`AGENTPAAS_DOCKER_TESTS=1`.

### Required adversary matrix

- Float rounding below the limit.
- Integer overflow/underflow.
- Concurrent double reservation.
- Duplicate release or commit.
- Worker-supplied cost/usage/provider-cost fields.
- Failed call omitted from spend.
- Timeout reservation incorrectly reused.
- Cache hit inferred without provider evidence.
- Cached/reasoning tokens double counted.
- Subscription/local target called free.
- Unknown/stale price treated as zero.
- Lower policy override reduces known price.
- Retry/worker attempt resets budget.
- Pipeline stage or child creation resets/splits the aggregate budget.
- Two sibling children reserve the same remaining workflow amount.
- Parent restart creates a second ledger or forgets child spend.
- Ordinary invoke token, worker, model output, checkpoint, or unconfirmed
  Hermes action raises the limit.
- Amendment decreases limit, resets spend, uses float/relative increments,
  reuses key with changed ceiling, or races terminal exhaustion inconsistently.
- Paused/needs-replan workflow actively reserves or spends, or freeze commits
  with an active/in-flight reservation; retained unreconciled exposure is
  allowed but cannot authorize work.
- Ledger truncation/tamper resets total.
- Cost output leaks prompt, response, key, or capability.
- “Under budget” changed to “saved.”

### Block success gate

B38 is complete only when:

1. `make block38-gate` passes.
2. Exact integer/decimal arithmetic drives all routed enforcement.
3. One durable workflow ledger spans physical calls, worker attempts, pipeline
   stages, parents, children, and continuation.
4. Failed and unreconciled calls are handled conservatively.
5. Cache and reasoning dimensions are provider-semantic and non-duplicative.
6. Every surface reports the same totals and basis.
7. Unknown price cannot enter a bounded lowest-metered-cost decision.
8. No savings or total-task-cost claim is made.
9. Terminal budget exhaustion cannot be amended. Authorized pre-terminal
   amendment mechanics are implemented and tested at the ledger level but
   the runtime path fails closed with `feature_not_enabled` in v0.5
   (deferred per Fix 5).

## Handoff record required after every task

Append:

- Task/date.
- Money/usage semantics.
- Files changed.
- Tests added first.
- Exact commands/PASS output.
- Reservation/commit examples.
- Provider cost fixtures.
- Adversary result.
- Compatibility impact.
- Open risks and freshness decision status.
- Next task unblocked.

## Pitfalls

- Do not use float64 for enforcement.
- Do not return an unknown timeout charge to available headroom; end the active
  reservation and convert it to inert unreconciled exposure.
- Do not reserve a fallback fund; all workflow nodes and attempts share one
  pool.
- Do not reset spend for a new worker attempt.
- Do not reset spend or reservations on pause, resume, amendment, or restart
  provenance; restart is a new workflow with a separately approved ledger.
- Do not allow relative “add money” enforcement updates; the CLI may calculate
  an absolute ceiling, but the stored/API amendment is absolute and
  idempotent.
- Do not infer cache use from prompt repetition.
- Do not call local/subscription execution free.
- Do not turn “under budget” into a savings claim.
