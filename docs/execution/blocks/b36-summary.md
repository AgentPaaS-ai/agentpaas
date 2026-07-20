# Block 36 — Model Catalog, Route Compiler, and Deterministic Selector

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Target release:** v0.5.0
**Depends on:** B35 complete and `make block35-gate` green
**Must complete before:** B37 and B38
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65

## Outcome

B36 turns a signed logical route into an immutable, explainable execution
plan. At block completion:

- One versioned catalog owns model capability, context, endpoint, effort, and
  price metadata.
- Existing OpenRouter, OpenAI, Anthropic, xAI, and Nous adapters participate
  behind one target abstraction.
- Explicit local/custom OpenAI-compatible endpoints can participate.
- Every model-using workflow member's signed policy is compiled into a
  node-scoped route snapshot with primary and recovery candidates; a
  standalone run is the one-node case.
- A pure deterministic selector filters candidates and chooses the least
  expensive eligible target.
- Every exclusion and tie-break is recorded.
- B29 runtime-profile requirements, including structured output, reasoning,
  streaming, tool-call, multimodal, and concurrency features, participate in
  deterministic eligibility. No unsupported feature is silently downgraded.
- Candidate credentials and model endpoints cannot be used through generic
  `agent.http()`/`agent.http_with_credential()` to bypass Routed Run controls.

B36 does not yet replay failed calls. B37 owns failure classification and
recovery execution.

## Locked selection behavior

For an initial call:

1. Load the node route and common catalog snapshot pinned at workflow start.
2. Consider only `role: primary`.
3. Apply policy, location, credential, context, capability, feature, effort,
   output, time, and price eligibility.
4. Select the lowest conservative metered cost.
5. Tie-break by stable candidate ID.

For an eligible recovery decision:

1. Exclude the failed target and every target/credential pair already marked
   unavailable.
2. Consider only `role: recovery`.
3. Apply the failure-derived requirements plus the original route minimum.
4. Select one target with the same deterministic rules.

There is no benchmark-weighted score, random choice, historical success rate,
prompt classifier, LLM judge, or unrestricted provider auto-router.

`TIME_INSUFFICIENT` is an envelope check. Before the whole-worker failure
continuation is consumed, a new call must fit inside the effective model-call
deadline, attempt lease, and `remaining workflow active time - recovery_margin`.
Its one B37 call-level recovery shares that same authorized call/attempt
envelope and does not reserve the margin again. Once B39 atomically starts the
single `FAILURE_CONTINUATION`, the margin is released for that final attempt
and no second margin is retained. v0.5 records actual latency but does not keep
a mutable latency score or rank candidates from a statistically unsupported
latency prediction.

## Versioned catalog contract

The catalog is implementation data behind an interface, not a third required
project file.

Each entry contains:

```text
schema_version
catalog_version
provider
model
upstream_provider
endpoint
location
capability_tier
context_tokens
max_output_tokens
features
supported_effort
pricing
source_url
effective_at
verified_at
valid_until
deprecated
accepted_response_models
```

Pricing contains:

```text
billing_mode: metered | subscription | local
currency: USD
input_per_million
cached_input_per_million
cache_write_per_million
output_per_million
reasoning_per_million
request_fee
reasoning_included_in_output
```

Optional dimensions are explicitly absent, not silently zero.

When `reasoning_effort` is supported, the entry must also state whether
reasoning tokens consume the advertised output cap or provide a conservative
maximum billable reasoning-token bound by effort. A target whose reasoning
cost cannot be bounded is ineligible under a hard LLM spend limit.

Direct known cloud targets match catalog metadata by provider/model.
Aggregator targets resolve every signed provider/model/upstream-provider
tuple. Signed policy may narrow capability or the aggregator upstream
allowlist but may not override cloud endpoint, context, capability, or price.
For a candidate with multiple approved upstreams, eligibility uses the
capability/feature intersection, the most restrictive context/output limit,
every applicable location/privacy constraint, and the highest conservative
price across that envelope. If those properties cannot be represented
truthfully, split the upstreams into separate candidates.

An explicitly approved local/custom OpenAI-compatible candidate may contain a
signed `custom_metadata` overlay with the same capability/pricing fields.
Such metadata is labelled `administrator_asserted` in route decisions and
cost output.

No runtime network lookup mutates a catalog snapshot.

`capability_tier` is an administrator-approved routing label for this tested
roster, not a universal intelligence score. Each assignment must cite the
release conformance tasks it passed. The selector may enforce a declared
minimum but may not infer tiers from marketing names, public benchmark rank,
or an LLM judgment.

## Model-route capability boundary

Routed model calls must not be bypassable through generic HTTP:

1. Candidate credentials are loaded into a model-only credential namespace.
2. `agent.http_with_credential()` cannot request a model-only credential.
3. Every compiled model backend requires an unguessable per-attempt
   model-route capability header.
4. The trusted harness receives that capability through protected startup
   state; the Python worker does not.
5. The gateway validates and strips the capability header before upstream
   transmission.
6. Generic proxy traffic without the capability cannot reach candidate model
   paths, including unauthenticated local endpoints.
7. If the pinned gateway cannot enforce and strip this header, stop the task
   and implement a narrowly scoped trusted model egress proxy with the same
   contract. Do not ship a documented bypass.

The capability is not a provider credential and cannot authorize anything
outside the exact compiled model routes.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Create the versioned model catalog | B26 | schema, canonicalization, source, and duplicate-price tests pass |
| 2 | T02 Refactor provider adapters behind target abstraction | T01 | existing provider tests plus custom local conformance pass |
| 3 | T03 Compile and protect signed model routes | T01, T02 | exact endpoint/path/capability and HTTP-bypass negatives pass |
| 4 | T04 Implement pure deterministic selector | T01 | exhaustive eligibility/tie-break property tests pass |
| 5 | T05 Create immutable workflow/node route snapshots | T03, T04 | daemon preflight persists every explainable plan before resources |
| 6 | T06 Catalog maintenance and model-roster approval packet | T01–T05 | synthetic catalog works; real defaults remain unapproved |
| 7 | T07 Block gate and adversary review | T01–T06 | `make block36-gate` passes |

## T01 — Create the versioned model catalog

### Goal

Replace scattered hard-coded metadata with one validated source usable by
selector, cost ledger, dashboard, CLI, and tests.

### Required work

1. Create a package such as `internal/modelcatalog`.
2. Define catalog and entry structs using B26’s shared integer nano-USD
   representation. Extend its checked primitives only in that
   dependency-neutral package; do not implement a temporary micro-dollar type
   that B38 must replace. Do not use binary float for enforcement.
3. Embed a release catalog artifact in the binary.
4. Add a `CatalogStore` interface and:
   - Embedded production implementation.
   - Deterministic in-memory test implementation.
   - Signed-policy custom overlay resolver for local/custom targets.
5. Validate:
   - Unique provider/model/upstream-provider identity.
   - HTTPS cloud endpoint.
   - Explicit private/local endpoint classification.
   - Positive context/output limits.
   - Known capability/feature/effort enums.
   - Non-negative price dimensions.
   - Source URL (or explicit `administrator_asserted` source for approved
     local custom metadata), effective/verification timestamps, and price
     validity.
   - A bounded reasoning-cost contract for every supported effort mode.
   - Bounded, non-conflicting accepted response-model aliases.
   - No conflicting duplicate entries.
6. Canonicalize and hash the catalog. Expose version and digest.
7. Migrate:
   - `internal/llm.EstimateCost`
   - `internal/dashboard/cost.go`
   to read through the catalog interface while preserving legacy output.
8. Mark legacy estimated fallback explicitly. B38 later determines whether an
   unpriced/stale target is eligible under a hard spend limit.
9. Do not populate new public defaults in this task. Use synthetic targets for
   all new selector tests.

### Likely files

- `internal/modelcatalog/**`
- embedded `internal/modelcatalog/catalog.json` or YAML
- `internal/llm/provider.go`
- `internal/dashboard/cost.go`
- cost/catalog tests

### Tests to write first

- Canonical catalog digest.
- Duplicate provider/model/upstream identity and conflicting response alias.
- Negative/missing price dimensions.
- Invalid endpoint/location combination.
- Unknown feature/tier/effort.
- Optional cache/request fields remain absent, not zero.
- Decimal arithmetic round trip.
- Dashboard and harness return the same price for one fixture.
- Legacy known/unknown cost fixtures remain compatible.
- Catalog mutation after snapshot cannot affect an existing run.

### Exit gate

Repository search finds no second production price table, and every cost
consumer can report the catalog version/digest it used.

## T02 — Refactor providers behind a candidate-target abstraction

### Goal

Make local, aggregator, and direct provider targets interchangeable to the
route engine without losing provider-specific request/usage behavior.

### Required work

1. Define:
   - `Target`
   - `NormalizedModelCall`
   - `NormalizedModelResult`
   - `ModelClient`
   - `ProviderAdapter`
2. A target carries resolved endpoint, provider/model, signed aggregator
   upstream allowlist when applicable, location, auth mode, credential
   reference, capabilities, context/output limits, and catalog identity.
3. Preserve existing direct adapters and tests while adapting them to the new
   interface.
4. Add an `openai-compatible` adapter that accepts an explicitly resolved
   endpoint and:
   - Supports local `auth: none`.
   - Supports an optional bearer/header credential.
   - Uses the same normalized result fields as cloud OpenAI-compatible
     providers.
5. Keep Anthropic-specific request/response format isolated in its adapter.
6. Support normalized optional:
   - `max_output_tokens`
   - `response_format: text|json`
   - `effort: standard|high`
   only when target metadata says the feature is supported.
7. Reject unsupported requirements before network activity.
8. Preserve the B29 normalized buffered/streaming envelope. Target selection is
   completed before the physical stream starts, every delta retains the same
   logical call/target identity, and strict buffered guardrails remain honest.
9. Never include credential value in `Target`, decisions, errors, or result.
   Resolve it only at trusted request construction.
10. Preserve one exact provider model in every request. For OpenRouter:
    - Never send `models`, `fallbacks`, or an auto-router model alias.
    - Constrain provider routing to signed `upstream_providers` using the
      execution-time documented allowlist controls.
    - Disable provider fallback outside that envelope.
    - Require support for every request parameter rather than permitting an
      upstream to ignore an effort/output/format control.
    - Apply every request-side price ceiling the adapter can faithfully derive
      from the pinned catalog; this is defense in depth and does not replace
      B38’s reservation.
    - Record the returned upstream endpoint when the response exposes it.
11. Validate response model identity against the exact requested model and
    catalog-approved response aliases. For an aggregator response, missing or
    mismatched model identity, or a reported upstream outside the signed
    allowlist, is `MODEL_IDENTITY_MISMATCH`; discard the response and stop
    terminally. If the provider does not return upstream identity, record
    `not_reported` rather than inventing it; A1 conformance must prove the
    request-side allowlist control before that adapter can enter the public
    roster.

### Likely files

- `internal/llm/provider.go`
- provider adapter files
- new target/client files
- `internal/harness/rpc_server.go` integration seam
- provider tests

### Tests to write first

- Every existing provider’s request/parse golden.
- Custom OpenAI-compatible endpoint and auth-none request.
- Custom bearer/header credential.
- Unsupported effort/JSON/output requirement fails pre-network.
- Endpoint cannot be overridden by agent call parameters.
- OpenRouter request contains one model, the signed upstream allowlist, and no
  model-fallback/auto-router fields.
- OpenRouter parameter-support and supported price-ceiling controls are
  present and derived only from the trusted target snapshot.
- Missing/mismatched aggregator model identity or a reported upstream outside
  the allowlist is discarded with `MODEL_IDENTITY_MISMATCH`.
- Absent upstream response metadata is recorded as `not_reported` and never
  fabricated.
- Credential sentinel absent from errors and normalized objects.
- Redirects remain denied.

### Exit gate

All providers satisfy one conformance matrix, and no selector code switches on
provider-specific request JSON.

## T03 — Compile and protect signed model routes

### Goal

Turn policy candidates into gateway/runtime routes that enforce the same
authority the selector sees.

### Required work

1. Resolve every policy candidate against catalog or approved custom metadata.
   Resolve each aggregator upstream separately and compile the conservative
   candidate envelope defined above.
2. Validate endpoint hostname, port, scheme, and exact API path.
   Reject URL userinfo, fragments, wildcard hosts, ambiguous encodings,
   non-canonical ports/paths, and policy-unapproved query parameters.
3. Cross-check:
   - Candidate endpoint against signed egress/private-network authority.
   - Candidate credential declaration.
   - Candidate location against cloud transfer.
   - Pattern/role constraints.
4. Compile exact candidate routes into gateway configuration. For an
   aggregator, compile the model and signed upstream envelope into trusted
   adapter state; neither is accepted from worker call input.
5. Compile capability-protected route templates. Generate a random
   per-attempt model-route capability only when B30/B39 creates the attempt;
   never persist or share one workflow-wide capability.
6. Require the capability on model backend routes and strip it upstream.
7. Load model candidate credentials into a separate model-only map in the
   harness.
8. Reject model-only credential IDs in generic credentialed HTTP RPC.
9. Ensure generic `agent.http()`, raw Python HTTP with proxy variables, and
   `agent.http_with_credential()` cannot reach model routes successfully.
10. Keep direct non-model egress behavior unchanged.
11. Record sanitized compile evidence: route ID, candidate IDs, endpoint
    host/path, credential ID, location, catalog version, and policy digest.

### Likely files

- `internal/policy/compiler.go`
- `internal/daemon/control_handlers.go`
- `internal/harness/rpc_server.go`
- gateway fixtures/conformance tests
- credential payload/load code

### Tests to write first

- Exact path match for each candidate.
- Generic HTTP to candidate path without capability denied.
- Guessed/wrong/replayed capability denied.
- Capability header stripped before fake upstream.
- Model credential denied through `http_with_credential`.
- Model RPC succeeds with protected capability.
- Candidate endpoint not in signed authority fails preflight.
- Aggregator request cannot omit, add, or replace a signed upstream.
- Local private endpoint requires explicit private access.
- DNS rebinding/private-resolution and encoded-path bypass fixtures.
- Two attempts, including attempts in the same workflow, cannot use each
  other’s capability.
- Capability absent from agent env, logs, audit, checkpoint, and artifacts.

### Exit gate

Routed model calls have a protected path and cannot be converted into
unbudgeted generic HTTP by worker code.

## T04 — Implement the deterministic selector

### Goal

Produce a pure, exhaustively tested function that selects one candidate and
explains the decision.

### Required work

1. Define selector input:
   - Route snapshot.
   - `primary` or `recovery` phase.
   - Original and per-call requirements.
   - Failure-derived constraints.
   - Remaining B38 workflow metered LLM spend and applicable node sublimit.
   - Remaining workflow active/attempt time.
   - Credential/target availability.
2. Define typed exclusion reasons:
   - `ROLE_MISMATCH`
   - `POLICY_DENIED`
   - `CLOUD_TRANSFER_DENIED`
   - `CREDENTIAL_UNAVAILABLE`
   - `TARGET_UNAVAILABLE`
   - `CAPABILITY_TOO_LOW`
   - `CONTEXT_TOO_SMALL`
   - `OUTPUT_TOO_SMALL`
   - `FEATURE_UNSUPPORTED`
   - `EFFORT_UNSUPPORTED`
   - `PRICE_UNBOUNDED`
   - `BUDGET_INSUFFICIENT`
   - `TIME_INSUFFICIENT`
   - `FAILED_TARGET`
3. Merge requirements by taking the stricter value. A call cannot reduce the
   signed minimum.
4. Compute conservative candidate call cost through a catalog cost-estimator
   interface. B38 replaces provisional reservation with enforcement.
5. Sort eligible targets by:
   - Conservative metered cost.
   - Stable candidate ID when cost is equal.

   Capability above the declared minimum does not participate in the
   tie-break. This preserves the approved selector contract and avoids
   treating excess strength as an unmeasured quality score.
6. Return one `RouteDecision` containing considered/excluded candidates,
   reasons, selected target, selector version, catalog version, and policy
   digest.
7. Return typed `NO_ELIGIBLE_TARGET` with the same evidence when empty.
8. Make the selector stateless and replaceable behind an interface.

### Tests to write first

- Table test for every exclusion reason.
- Primary cannot choose recovery role.
- Recovery cannot choose primary role.
- Per-call requirement only narrows.
- Context failure imposes larger context.
- Capability-up imposes next tier.
- Cloud denial.
- Independent credential availability.
- Insufficient budget/time.
- Stable tie-break under map/list reordering.
- Property test: selected target is always in input and satisfies every
  requirement.
- Property test: adding an ineligible candidate never changes selection.
- Property test: same snapshot/input always gives byte-identical decision.

### Exit gate

Selection is pure, deterministic, fully explainable, and independent of live
provider behavior.

## T05 — Create immutable workflow/node route snapshots

### Goal

Pin the exact policy/catalog/candidate state used by every model-capable
workflow member so price or model metadata changes cannot rewrite history or
cause sibling nodes to use different catalogs mid-workflow.

### Required work

1. During routed workflow preflight, before any worker/service resources:
   - Enumerate the standalone node or every declared pipeline stage, parent,
     allowed leaf-child package, and model-using MCP service.
   - Load each signed package and policy.
   - Load catalog.
   - Resolve candidates.
   - Validate credentials by label/availability without exposing values.
   - Compile protected routes.
2. Persist one common catalog snapshot/digest on the B26 workflow plus a
   canonical route snapshot on each model-using node/package containing:
   - Route and pattern.
   - Resolved candidates and metadata source.
   - Minimum requirements.
   - Runtime limits.
   - Policy, image/lock, and catalog digests.
   - Selector version.
3. Persist a static preflight eligibility report against route-level
   requirements. Do not manufacture an “initial selection” from a synthetic
   call: B37 makes the first real selection only after it has the normalized
   prompt and output cap.
4. Refuse to start if the configured route has no eligible primary or no
   recovery target while recovery is enabled.
5. Every later stage/child/service and attempt continues to use the pinned
   workflow catalog and its node/package route snapshot after a catalog
   update. A dynamic child package not present at preflight is forbidden.
6. Inspect/summarize commands can render the snapshot without credentials.
7. B26’s routed-run not-enabled gate remains until B37/B38 are integrated and
   B39 activates routed execution.

### Tests to write first

- Snapshot canonicalization and digest.
- Catalog changes do not affect existing snapshot.
- Credential removed before start fails pre-resource.
- Policy/lock mismatch fails.
- No primary/recovery fails.
- Snapshot contains no secret/capability value.
- Inspect output is deterministic and bounded.
- Multi-node workflow pins one catalog digest and all declared node/package
  route snapshots before the first container starts.
- Catalog change between stage/child launches does not affect selection.

### Exit gate

Every future model call can be traced to one immutable workflow catalog and
node/package route snapshot.

## T06 — Catalog maintenance and model-roster approval packet

### Goal

Make model metadata maintainable without prematurely approving public defaults.

### Required work

1. Add an offline catalog validation command or script that:
   - Validates schema.
   - Detects duplicates/stale verification dates.
   - Recomputes digest.
   - Prints changed price/capability fields.
2. Add a documented update procedure requiring:
   - Primary provider documentation URL.
   - Verification date.
   - Effective and valid-until date for metered price data.
   - Adapter conformance result.
   - Context/output/effort test evidence.
   - Price dimension review.
3. Generate a candidate-roster report template for B41 containing:
   - Local/cloud role.
   - Credential independence.
   - Capabilities and limits.
   - Pricing source/effective date.
   - Live smoke result.
   - Known deprecation/availability risk.
4. Use synthetic catalog entries in all automated Routed Run tests.
5. Preserve existing legacy entries only as compatibility data; do not label
   them recommended by implication.
6. Record that founder approval is still required for public model defaults.
   The B41 A1 approval also covers the public price-validity/freshness policy.

### Tests to write first

- Invalid catalog update fails.
- Diff is deterministic.
- Missing source/verification date fails.
- Generated report contains every required field.
- No network fetch occurs during runtime selection.

### Exit gate

The catalog can be safely updated and reviewed, but no unapproved model roster
is published.

## T07 — Block gate and adversary review

### Required `make block36-gate`

Run:

```text
make block35-gate
go test ./internal/modelcatalog/... ./internal/llm/... -count=1 -race
go test ./internal/policy/... ./internal/harness/... ./internal/daemon/... -count=1 -race
go test ./internal/dashboard/... -count=1 -race
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

Run Docker gateway conformance tests with fake local/cloud upstreams and
`AGENTPAAS_DOCKER_TESTS=1`.

### Required adversary matrix

- Policy candidate overrides known cloud endpoint/price/capability.
- Custom metadata used for a cloud target.
- Cloud endpoint over HTTP.
- Private/local endpoint without explicit authority.
- Generic HTTP bypass of model route.
- Model credential through generic credentialed HTTP.
- Capability theft, cross-run reuse, logging, or upstream leakage.
- Catalog reorder/tamper/duplicate/stale metadata.
- Float rounding or negative price.
- Requirement broadening from worker input.
- Candidate order manipulation.
- OpenRouter auto-router/model fallback or provider routing outside the signed
  upstream allowlist.
- Provider response model differs from requested model.
- Secret sentinel in decision/snapshot/errors.

### Block success gate

B36 is complete only when:

1. `make block36-gate` passes.
2. One catalog supplies all production price metadata.
3. Existing and custom provider adapters pass one conformance matrix.
4. Model routes are exact, protected, and inaccessible through generic HTTP.
5. Selector decisions are deterministic and property-tested.
6. Workflow/node route snapshots are immutable and secret-free.
7. No public default roster or routing optimization claim is introduced.

## Handoff record required after every task

Append:

- Task ID/date.
- Catalog/adapter/schema decisions.
- Files changed.
- Tests added first.
- Exact commands and PASS output.
- Gateway/HTTP-bypass evidence.
- Catalog sources used.
- Compatibility impact.
- Open risks.
- Next task unblocked.

## Pitfalls

- An aggregator’s model list is not an approved model pool.
- Do not fetch mutable pricing during a run.
- Do not use float64 for budget enforcement.
- Do not let signed policy override known cloud price/capability metadata.
- Do not make local `auth: none` reachable through generic HTTP.
- Do not expose candidate credentials to `http_with_credential`.
- Do not treat higher capability as automatically better when the declared
  minimum is satisfied.
- Do not activate recovery in this block; B37 owns it.
