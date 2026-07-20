# Block 37 — Model-Call Failure Classification and Recovery

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-18
**Target release:** v0.5.0
**Depends on:** B36 complete and `make block36-gate` green
**Must complete before:** B38 and B39
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D1–D65

## Outcome

B37 implements the call-level recovery boundary of Routed Run. At block
completion:

- `agent.llm()` on a routed project creates a normalized logical model call.
- The deterministic selector chooses an approved primary target.
- Provider and transport failures become stable B26 failure reasons.
- One eligible recovery target is selected and the same normalized call is
  replayed once.
- Uncommitted partial streaming output may be observed only under the B29
  release policy; it is never committed, checkpointed, or appended to a replay.
- The recovery target remains sticky for the rest of the worker attempt.
- Auth/quota failures can recover only through an independently usable
  credential/target.
- Explicit JSON response validation can trigger recovery without pretending to
  verify semantic quality.
- Every physical request and logical call has complete audit/ledger evidence.
- A deterministic fake-provider and fault-injection suite proves every path.

B37 does not implement whole-worker retry. If call-level recovery fails, the
worker receives a typed error; B39 later converts the attempt outcome into a
structured `NEEDS_REPLAN` or terminal result.

B37 defines and consumes a `CallBudgetAuthorizer` seam for physical requests.
Its deterministic tests use a strict fake. B38 supplies the durable production
reservation implementation, and B39 activates routed execution. Do not add a
temporary float/in-memory USD enforcer or expose a partially budgeted routed
path between these blocks.

## Locked logical-call contract

### Routed SDK input

The existing method remains:

```python
agent.llm(prompt, **kwargs)
```

Routed mode accepts only:

```text
max_output_tokens
response_format: text | json
effort: standard | high
min_context_tokens
min_capability_tier
```

These values may narrow the signed route. Unknown kwargs fail closed in routed
mode.

Normalize omitted values before selection and persist the normalized values in
the immutable call envelope:

- `response_format` -> `text`
- `effort` -> `standard`
- `max_output_tokens` -> the signed `max_tokens_per_request`

The last rule is deliberately conservative: provider-specific implicit output
defaults are not a safe basis for a hard spend reservation. The worker may
request a smaller positive cap, but never a value above signed policy or
target capability.

`model=` is rejected with `ROUTE_OVERRIDE_DENIED` in routed mode. It remains
compatible on the legacy single-model path.

### Normalized envelope

Every logical call contains:

```text
schema_version
workflow_id
node_id
run_id
attempt_id
lease_id
logical_call_id
route_id
route_snapshot_digest
prompt
max_output_tokens
response_format
effort
minimum_requirements
deadline
```

The immutable prompt/body is held only for executing the call. Audit and
ordinary ledgers store its digest, byte/token estimate, and redacted metadata,
not raw prompt text.

Buffered and streaming v0.5 calls share one logical-call contract. A buffered
request is committed as one complete response. A streaming request may emit
ordered uncommitted deltas under B29 policy, but only one validated terminal
result enters worker context. If a provider returns partial output and then
fails, no partial delta enters checkpoint/replay context and the recovery
request replays the exact normalized logical call. The event stream records
that previously observed deltas were abandoned.

### Physical attempts

A logical call has at most two physical requests:

```text
physical_request 1: selected primary
physical_request 2: one selected recovery target, only after eligible failure
```

No same-target HTTP retry is hidden inside AgentPaaS model routing. Model
candidate gateway retry is disabled for v0.5. If an upstream provider performs
internal retries, AgentPaaS records the provider-visible result but does not
describe that internal behavior as AgentPaaS recovery.

An OpenRouter physical request contains one exact model and the signed
upstream-provider allowlist from B36. Model fallback is disabled. Provider
routing inside that allowlist remains one AgentPaaS physical request and is
reported as aggregator behavior, not an AgentPaaS recovery. Routing outside
the allowlist, a missing/mismatched returned model identity, or a reported
upstream outside the allowlist is a terminal `MODEL_IDENTITY_MISMATCH`.
Unreported upstream identity is recorded honestly and relies on B36/B41
request-control conformance; it is never guessed.

### Sticky recovery

After a recovery request succeeds:

- The recovery target is stored on the worker attempt.
- Later logical calls use that target without reconsidering cheaper primary
  targets.
- Later calls still must satisfy policy, credential, context, effort, output,
  time, and budget requirements.
- If the sticky target cannot satisfy a later call, AgentPaaS does not perform
  a second recovery selection in that attempt. It returns a typed failure.

## Failure classification

The classifier consumes structured transport/provider errors, never error
message text alone when a structured field exists.

| Failure reason | Unavailability scope | Eligible recovery condition |
|---|---|---|
| `MODEL_TIMEOUT` | target/request | Another target fits remaining time |
| `MODEL_CONNECTION_FAILED` | endpoint | Another target is reachable/eligible |
| `MODEL_RATE_LIMITED` | target or credential | Another independently usable target fits time/budget |
| `MODEL_SERVICE_ERROR` | target/provider | Retryable 5xx or documented transient code |
| `MODEL_CONTEXT_LIMIT` | target | Recovery context is strictly larger than observed requirement |
| `MODEL_OUTPUT_LIMIT` | target | Recovery output limit and spend reservation fit |
| `MODEL_MALFORMED_JSON` | target | Caller explicitly requested JSON |
| `MODEL_AUTH_UNAVAILABLE` | credential | Recovery does not depend on the failed credential |
| `MODEL_QUOTA_EXHAUSTED` | account/credential/target | Independently authorized candidate remains |

These never trigger a model switch:

- Policy or cloud-transfer denial.
- Guardrail denial.
- LLM spend or token budget denial.
- Workflow active-time exhaustion or cancelled context.
- Invalid SDK arguments.
- Aggregator model identity or reported-upstream mismatch.
- Attempt lease expiry.
- Arbitrary low-quality or factually questionable text.
- Generic agent exception.

## Context boundary

B37 replays the exact failed request. It does not:

- Summarize or compact context.
- Drop prior content.
- transfer provider cache IDs.
- recover hidden reasoning.
- infer which earlier turns are important.

The worker owns explicit multi-turn context in its prompt and checkpoints.
Automatic context management remains deferred.

## Authoritative task order

| Order | Task | Depends on | Exit evidence |
|---|---|---|---|
| 1 | T01 Normalize routed `agent.llm()` calls | B36 | SDK/harness contract and route-override negatives pass |
| 2 | T02 Normalize provider and transport failures | T01 | provider conformance matrix maps stable reasons/scopes |
| 3 | T03 Implement one-selection recovery controller | T01, T02 | exact replay and sticky-target state tests pass |
| 4 | T04 Handle context/output/JSON/auth/quota constraints | T03 | targeted recovery and hard-stop tests pass |
| 5 | T05 Persist call and decision evidence | T03, T04 | logical/physical ledger correlation is complete |
| 6 | T06 Build deterministic fake-provider/fault-injection suite | T01–T05 | every failure path is repeatable without live services |
| 7 | T07 Block gate and adversary review | T01–T06 | `make block37-gate` passes |

## T01 — Normalize routed `agent.llm()` calls

### Goal

Give the router one transport-independent call object and preserve legacy SDK
behavior.

### Required work

1. Validate routed kwargs in Python for early feedback and again in Go.
2. Reject routed `model=` override before selection or network activity.
3. Generate a logical call ID in trusted runtime/harness state; do not accept
   one from worker input.
4. Merge call requirements with signed route minimum using the stricter value.
5. Bound:
   - Prompt bytes.
   - `max_output_tokens`.
   - Context requirement.
   - Enum values.
6. Apply the locked routed defaults before selection. Reject zero, negative,
   over-policy, or over-target output caps before network activity.
7. Derive a stable prompt digest and a conservative input-token estimate:
   - Use a pinned deterministic provider/model tokenizer only when its
     coverage is verified.
   - Otherwise use UTF-8 byte count plus bounded adapter framing overhead as
     the enforcement upper bound.
   - Never use a heuristic such as characters/4 for a hard context or spend
     decision.
8. Resolve the current workflow/node/run/attempt lease and the B36 node/package
   route snapshot. Reject any snapshot whose workflow catalog digest differs.
9. Select:
   - Existing sticky recovery target when present.
   - Otherwise one primary through B36 selector.
10. Build a provider-specific request only after target selection.
11. Preserve legacy `handleLLM` behind an explicit compatibility branch.
12. Return the existing `text`, `content`, `tokens`, and `model` fields, plus
    additive routed metadata:
    - `target_id`
    - `logical_call_id`
    - `recovered`

### Likely files

- `python/agentpaas_sdk/agent.py`
- `internal/harness/rpc_server.go`
- new `internal/modelrouter/call.go`
- adapter/client integration
- SDK and harness tests

### Tests to write first

- Valid routed text/JSON/effort requirements.
- Unknown kwarg.
- Exact model override denied in routed mode.
- Legacy override remains functional.
- Worker-supplied route/target/call/lease control fields ignored or rejected.
- Requirement narrowing and attempted broadening.
- Omitted routed kwargs normalize identically across candidates; explicit
  output cap cannot exceed signed policy.
- Prompt bounds and digest stability.
- ASCII, multilingual, emoji, and adversarial token-estimate upper bounds.
- No raw prompt in route decision/audit fixture.

### Exit gate

One normalized object crosses selection and provider boundaries, and legacy
calls remain unchanged.

## T02 — Normalize provider and transport failures

### Goal

Classify observable failures consistently without matching brittle prose when
structured evidence is available.

### Required work

1. Define a structured `ModelError` containing:
   - Stable reason.
   - Retry/recovery eligibility.
   - Unavailability scope (`request`, `target`, `endpoint`, `credential`,
     `account`, or `provider`).
   - HTTP status.
   - Sanitized provider code/type.
   - `Retry-After` when present.
   - Context/output limits when reported.
   - Whether usage/cost may have been incurred.
2. Update every provider adapter to parse its documented error envelope.
3. Map transport conditions:
   - Dial/DNS/TLS failure.
   - Context cancellation.
   - Per-call deadline.
   - Response read truncation.
4. Distinguish user/run cancellation from model timeout.
5. Treat:
   - 401 as credential unavailable.
   - 402 or documented account quota as quota exhausted.
   - 429 as rate-limited unless provider code proves quota exhaustion.
   - Retryable 5xx as service error.
   - 400/413 context/output only with provider code/evidence; otherwise
     terminal invalid request.
6. Never include raw provider bodies, credentials, or prompts in errors.
7. Preserve a redacted bounded excerpt only for operator evidence.

### Likely files

- provider adapter files
- new `internal/modelrouter/errors.go`
- harness error mapping
- provider conformance fixtures

### Tests to write first

- Every table row for every supported provider fixture.
- Ambiguous 400/403 remains non-recoverable.
- Cancellation versus timeout.
- Retry-After parsing and bounds.
- Malformed/oversized error body.
- Secret echoed by provider is redacted.
- Unknown provider code maps to safe generic terminal/service category.
- Fuzz provider error parsing.

### Exit gate

The recovery controller receives typed evidence and never needs to scrape
human error strings.

## T03 — Implement the bounded recovery controller

### Goal

Replay one failed logical call on one eligible recovery target and make that
target sticky.

### Required work

1. Execute the selected primary physical request with the route’s model-call
   timeout.
2. On success:
   - Parse complete result.
   - Validate declared response format.
   - Commit usage/result.
3. On failure:
   - Persist physical request failure before selecting recovery.
   - Ask the classifier whether model recovery is eligible.
   - Apply failure-derived requirements.
   - Atomically confirm/mark that this attempt has not already consumed its
     one recovery selection.
   - Invoke B36 selector in recovery phase.
4. If no recovery target:
   - Return the original typed failure plus `NO_ELIGIBLE_TARGET` decision
     evidence.
5. If one target is selected:
   - Rebuild provider request from the immutable normalized envelope.
   - Do not reuse partial body/decoder/provider continuation state.
   - Execute exactly once.
6. On recovery success:
   - Mark logical call `recovered`.
   - Store sticky target on the attempt with compare-and-swap.
7. On recovery failure:
   - Record it.
   - Return its typed reason with the original failure chain.
   - Do not select a third target.
   The recovery allowance remains consumed whether the selected recovery
   request succeeds or fails.
8. Respect cancellation, lease, active-time/operation deadline, and the injected
   `CallBudgetAuthorizer` before both physical requests. In B37 production
   wiring, fail closed as not-yet-enabled until B38 provides the durable
   authorizer.
9. Serialize model calls within one worker attempt initially. Parallel routed
   calls are out of v0.5 unless a later approved change defines budget and
   sticky-target races.

### Likely files

- `internal/modelrouter/controller.go`
- harness RPC integration
- `internal/routedrun` attempt update methods
- tests

### Tests to write first

- Primary success: one request.
- Primary eligible failure/recovery success: two requests.
- Primary terminal failure: one request.
- Recovery failure: exactly two requests.
- No eligible recovery: one request plus decision.
- Exact normalized envelope equivalence across provider transformations.
- Partial response discarded.
- Sticky target on later call.
- Sticky target cannot satisfy later call: no second selection.
- Concurrent call attempt rejected/serialized deterministically.
- Cancellation between primary failure and recovery prevents second request.

### Exit gate

No test path can produce a third physical request or return partial primary
output.

## T04 — Implement failure-derived eligibility

### Goal

Handle the user’s concrete failure cases without semantic judging.

### Required work

1. Context limit:
   - Derive required context from conservative input estimate, requested
     output cap, adapter framing overhead, and any stricter provider evidence.
   - Recovery candidate context must be strictly larger.
2. Output limit:
   - Preserve requested output cap.
   - Recovery candidate must support it.
   - The `CallBudgetAuthorizer` must approve the maximum cost. B37 uses a
     deterministic fake for contract tests; B38 implements the production
     reservation.
3. JSON response:
   - Only when `response_format=json`.
   - Parse complete UTF-8 JSON.
   - Reject trailing non-whitespace and excessive nesting/size.
   - One malformed result is an eligible target failure.
   - Do not inspect factual content or schema semantics unless the worker
     supplied a deterministic JSON schema in a future approved version.
4. Authentication:
   - Mark the credential unavailable in the workflow availability registry.
   - Exclude every later stage/parent/child candidate using the same approved
     credential identity.
5. Quota/subscription:
   - Use provider scope evidence where available.
   - Mark target, credential, or provider unavailable at the narrowest safe
     workflow scope.
6. Rate limit:
   - Select another target when available.
   - Do not sleep through the attempt lease merely because Retry-After exists.
7. Service/connection/timeout:
   - Exclude failed target for this recovery selection.
8. A cheaper recovery candidate below minimum capability is never eligible.

### Tests to write first

- Context candidate exact boundary and strictly larger rule.
- Output cap and budget hook.
- Valid/invalid/oversized/deep JSON.
- Plain text never gets JSON quality judgment.
- 401 excludes all same-credential candidates.
- A later workflow node cannot retry a credential/account marked unavailable.
- Independent credential recovers.
- Same account/provider quota scope.
- Rate limit with/without eligible target.
- Lower-capability cheap candidate excluded.

### Exit gate

Each approved failure pattern has deterministic positive and negative tests,
and no general verifier is introduced.

## T05 — Persist complete call and decision evidence

### Goal

Make recovery explainable and prepare the cost ledger B38 will enforce.

### Required work

For each logical call record:

- Workflow, node, run, attempt, logical call, and lease IDs.
- Route ID and selector/catalog/policy versions.
- Prompt digest and bounded input estimate.
- Requirements.
- Primary decision.
- Each physical request target, start/end, status, failure reason/scope.
- Recovery decision and exclusion reasons.
- Sticky-target transition.
- Usage/cost placeholder or normalized value.
- Terminal logical-call result.

Requirements:

1. Append operational records to B26 `ledger.jsonl`.
   Every record carries the B26 state schema version.
2. Mirror sanitized evidence to audit/timeline.
3. Never store raw prompt, response text, credential, capability token, or
   provider raw body in ordinary ledger/audit.
4. Correlate every physical request to one logical call.
5. Flush the failure record before issuing recovery so a crash cannot erase
   why a second target was called.
6. Bound record size and candidate exclusion list.
7. Operator rendering must say:
   - Primary target.
   - Observable failure.
   - Recovery target.
   - Whether recovery succeeded.
   - Why other candidates were excluded.

### Tests to write first

- One-request and two-request correlation.
- Failure persisted before recovery.
- Crash fixture between failure and recovery.
- Deterministic ordering.
- Raw prompt/response/credential/capability sentinels absent.
- Oversized candidate pool truncates honestly with count/digest.
- Timeline renders typed evidence, not provider prose as control fields.

### Exit gate

An operator can reconstruct every AgentPaaS routing action without seeing
private prompt or credential content.

## T06 — Build deterministic fake providers and fault injection

### Goal

Prove recovery without relying on flaky live services or intentionally
exhausting real accounts.

### Required work

1. Add local fake OpenAI-compatible and Anthropic-style servers.
2. Support scripted outcomes by physical request number:
   - Success with normalized usage.
   - Delay past call timeout.
   - Connection close.
   - 401, 402, 429, and 5xx.
   - Context/output error code.
   - Malformed JSON model output.
   - Partial/truncated body.
3. Record received request digest, target, headers, and count, while redacting
   auth.
4. Add a fake clock where possible; use very short bounded real delays only
   for transport deadline integration.
5. Fault injection configuration is test-only:
   - It cannot be enabled from trigger payload or agent source.
   - It is visibly labelled in audit/demo output.
   - Production builds do not expose an unauthenticated control endpoint.
6. Create scenario fixtures for every T02–T04 path.

### Likely files

- `internal/modelrouter/testprovider/**`
- test fixtures under `test/routedrun/**`
- Docker test support

### Tests to write first

- Fault script determinism.
- Agent input cannot select fault.
- Header/auth redaction.
- Exact request counts.
- Server cleanup/port isolation under parallel tests.

### Exit gate

All recovery behavior runs offline and repeatably in CI.

## T07 — Block gate and adversary review

### Required `make block37-gate`

Run:

```text
make block36-gate
go test ./internal/modelrouter/... ./internal/llm/... ./internal/harness/... -count=1 -race
go test ./internal/routedrun/... ./internal/daemon/... -count=1 -race
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
go vet ./...
golangci-lint run --timeout 5m
govulncheck ./...
make golden-fast
```

Run Docker protected-route recovery fixtures with
`AGENTPAAS_DOCKER_TESTS=1`.

### Required adversary matrix

- Routed exact-model/endpoint/route override.
- Third physical request through nested retry.
- Gateway retry accidentally enabled.
- OpenRouter model fallback or provider routing outside the signed upstream
  allowlist.
- Partial primary output leaked into recovery/result.
- Raw prompt/response/provider body in ledger.
- Ambiguous provider error misclassified as recoverable.
- 401 recovery using the same credential.
- Lower-capability downgrade.
- JSON validator used on plain text or as semantic judge.
- Cancellation/lease/deadline race before recovery.
- Sticky target reset by agent input or later cheap candidate.
- Fault injection enabled by untrusted input.
- Provider error echoes secret.

### Block success gate

B37 is complete only when:

1. `make block37-gate` passes.
2. Routed calls reject exact model override.
3. Every logical call has one or at most two physical requests.
4. Recovery uses one deterministic eligible target and exact request replay.
5. Recovery target is sticky for the attempt.
6. Auth/quota handling requires independent authorization.
7. No semantic verifier or automatic context compaction appears.
8. Fault fixtures prove all paths without live providers.
9. The daemon still cannot expose routed execution without B38’s durable
   authorizer and B39’s supervisor.

## Handoff record required after every task

Append:

- Task/date.
- Normalized call or failure-schema decisions.
- Files changed.
- Tests added first.
- Exact commands/PASS output.
- Physical request counts.
- Fault/adversary result.
- Compatibility impact.
- Open risks.
- Next task unblocked.

## Pitfalls

- Do not confuse an upstream provider’s hidden retry with AgentPaaS recovery.
- Do not replay partial output or provider continuation state.
- Do not classify arbitrary 400/403 responses as recoverable.
- Do not reuse an expired credential on another model.
- Do not let a recovery target reset on the next call.
- Do not validate “quality” under the name of structured output.
- Do not create a second streaming protocol; reuse B29 envelopes, sequence,
  backpressure, guardrail, usage, and cancellation semantics.
