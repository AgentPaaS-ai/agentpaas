# Block 9 — OWA Records

## Table of Contents

- [B9-T01 OWA Record: Trigger API Serving and Auth](#b9-t01)
- [B9-T02 OWA Record: API Key Lifecycle](#b9-t02)
- [B9-T03 OWA Record: Durable Idempotency and Payload Limits](#b9-t03)
- [B9-T04 OWA Record: SSE and Event Bus](#b9-t04)
- [B9-T05 OWA Record: Webhook Delivery](#b9-t05)
- [B9-T06 OWA Record: Cron Triggers](#b9-t06)
- [B9-T07 OWA Record: Local Handoff Triggers](#b9-t07)
- [B9-T08 OWA Record: CancelRun Semantics](#b9-t08)
- [B9-T09 OWA Record: Control API REST/JSON Fuzzing](#b9-t09)
- [Block 9 Block-End Verifier Report](#verification) — verification record

---

# B9-T01 OWA Record: Trigger API Serving and Auth

**Block:** 9 — Trigger API, Events, Webhooks, Cron
**Subtask:** T01 — Trigger API Serving and Auth
**Date:** 2026-06-22

## Worker

- Model: GPT-5.5 (Codex CLI)
- Branch: feat/b9-t01-trigger-api-auth
- Commit: 1ed3e1a → merged as b004610
- Files created: internal/trigger/{doc.go, auth.go, server.go, server_test.go}

## Scope

- gRPC server on :7718 + grpc-gateway REST on :7717 (loopback by default)
- API key Bearer token auth (gRPC interceptor + REST middleware)
- CORS deny-by-default middleware (unlisted origins → 403 on preflight)
- Max payload 1 MiB enforcement
- --expose refuses without authenticator
- Stub TriggerService (Invoke/InvokeStream/GetRun/ListRuns/CancelRun)

## Gate Results

| Gate | Command | Result |
|------|---------|--------|
| BUILD | `go build ./...` | PASS |
| TEST | `go test -race -count=1 ./internal/trigger/...` | PASS (1.48s) |
| LINT | `golangci-lint run ./internal/trigger/...` | PASS (0 issues) |
| ADVERSARY | `go test -tags=adversary -race -count=1 ./internal/trigger/...` | PASS (1.58s) |

## Adversary

- Model: grok-4.3 (agentpaas-adversary profile)
- Tests written: 15 (TestAdversaryB9T01_*)
- Breaks found: 1
  - **InvokeStream without auth returned nil** — stream interceptor not enforcing auth on server-streaming RPC. Fix worker dispatched: added eager first-response buffering in stream interceptor to enforce auth before handler runs. Commit b004610.
- All 15 adversary tests pass post-fix.

## Fix Worker

- Break: InvokeStream auth bypass
- Fix: Stream interceptor now eagerly evaluates auth before allowing the stream to proceed
- Commit: b004610

## Acceptance Criteria

- [x] Server starts gRPC on :7718 and REST on :7717 (loopback)
- [x] API key auth required even on loopback (unauthenticated → 401)
- [x] --expose refuses without authenticator
- [x] CORS deny-by-default (unlisted origin → 403)
- [x] Max payload 1 MiB enforced
- [x] All tests pass: `go test -race -count=1 ./internal/trigger/...`

## Verifier

See docs/owa-records/b9-block-end.md

## Non-goals (deferred to later subtasks)

- API key store (T02)
- Idempotency table (T03)
- Real event bus/SSE (T04)
- Webhook delivery (T05)
- Cron triggers (T06)
- Local handoff (T07)
- CancelRun semantics (T08)
- Fuzzing (T09)

---

# B9-T02 OWA Record: API Key Lifecycle

**Block:** 9 — Trigger API, Events, Webhooks, Cron
**Subtask:** T02 — API Key Lifecycle
**Date:** 2026-06-22

## Worker

- Model: GPT-5.5 (Codex CLI)
- Branch: feat/b9-t02-apikey-lifecycle
- Commit: 157aa8d
- Files: internal/trigger/{apikey.go, apikey_test.go}

## Scope

- API key store with create/validate/revoke/rotate lifecycle
- Raw keys never stored (SHA-256 hash only), shown once on create
- Scoped by agent/action with wildcard support (agent-a:* matches agent-a:invoke)
- File-based persistence with atomic save (temp+rename)
- Audit events: api_key_created, api_key_revoked, auth_failed
- Implements Authenticator interface for integration with T01 server
- Constant-time key comparison (crypto/subtle)

## Gate Results

| Gate | Command | Result |
|------|---------|--------|
| BUILD | `go build ./...` | PASS |
| TEST | `go test -race -count=1 ./internal/trigger/...` | PASS (1.50s) |
| LINT | `golangci-lint run ./internal/trigger/...` | PASS (0 issues) |
| ADVERSARY | `go test -tags=adversary -race -count=1 ./internal/trigger/...` | PASS (1.60s) |

## Adversary

- Model: grok-4.3 (agentpaas-adversary profile)
- Tests written: 11
- Breaks found: 0
- All claims verified: raw key not in file, revoked key fails, scopes enforced, constant-time comparison, file permissions 0600, concurrent access safe

## Acceptance Criteria

- [x] Raw keys never stored or re-shown (SHA-256 hash only)
- [x] Revoked key fails validation
- [x] Rotation revokes old + creates new with same scopes
- [x] Scopes enforced (wildcard support)
- [x] Audit events emitted for create/revoke/auth_failed
- [x] Persistence survives restart
- [x] All tests pass

## Verifier

See docs/owa-records/b9-block-end.md

---

# B9-T03 OWA Record: Durable Idempotency and Payload Limits

**Block:** 9 — Trigger API, Events, Webhooks, Cron
**Subtask:** T03 — Durable Idempotency and Payload Limits
**Date:** 2026-06-22

## Worker

- Model: GPT-5.5 (Codex CLI)
- Branch: feat/b9-t03-idempotency
- Commits: 69bef9b, a4d62d9 (test fix), a6c04ab (lint fix)
- Files: internal/trigger/{idempotency.go, idempotency_test.go, adversary_b9_t03_test.go}, server.go (modified)

## Scope

- Idempotency store with 24h replay window
- Canonical request hash over caller/agent/lock_digest/payload/content_type/api_version
- Durable file-based persistence (survives daemon restart)
- Idempotency: replay (same key+payload → same run_id), conflict (409), new
- Audit events: idempotency_replayed, idempotency_conflict
- Wired into TriggerService.Invoke
- Max payload 1 MiB enforced

## Gate Results

| Gate | Command | Result |
|------|---------|--------|
| BUILD | `go build ./...` | PASS |
| TEST | `go test -race -count=1 ./internal/trigger/...` | PASS (1.46s) |
| LINT | `golangci-lint run --build-tags=adversary ./internal/trigger/...` | PASS (0 issues) |
| ADVERSARY | `go test -tags=adversary -race -count=1 ./internal/trigger/...` | PASS (1.67s) |

## Adversary

- Tests written: 11
- Breaks found: 1 (test bug — adversary test called Invoke without auth, got Unauthenticated before payload check)
- Fix: adversary test updated to include valid Bearer token
- All 11 tests pass post-fix

## Acceptance Criteria

- [x] Same key + same payload returns same run_id (no re-execution)
- [x] Same key + different payload returns 409 (ALREADY_EXISTS)
- [x] Empty key = idempotency disabled (always new run)
- [x] Expired entry (>24h) treated as new
- [x] Daemon restart preserves entries (durable)
- [x] Canonical hash is deterministic + differs on any field change
- [x] Audit events for replay + conflict
- [x] Max payload 1 MiB enforced
- [x] All tests pass

## Verifier

See docs/owa-records/b9-block-end.md

---

# B9-T04 OWA Record: SSE and Event Bus

**Block:** 9 — Trigger API, Events, Webhooks, Cron
**Subtask:** T04 — SSE and Event Bus
**Date:** 2026-06-22

## Worker

- Model: GPT-5.5 (Codex CLI)
- Branch: feat/b9-t04-sse-eventbus
- Commit: 60f32b4 → merged as 2ee849e
- Files: internal/trigger/{eventbus.go, sse.go, server.go (modified), eventbus_test.go, server_test.go (modified)}

## Scope

- In-memory event bus with per-run buffers, ordered monotonic event IDs
- Subscriber management: Subscribe with fromEventID replay, non-blocking publish
- Terminal event handling: closes channels, no duplicate terminal events
- SSE handler with heartbeats (15s), Last-Event-ID reconnect
- REST SSE endpoint at /v1/trigger/events?run_id=...
- InvokeStream streams run lifecycle events over gRPC
- EventBus wired into ServerConfig and TriggerService

## Gate Results

| Gate | Command | Result |
|------|---------|--------|
| BUILD | `go build ./...` | PASS |
| TEST | `go test -race -count=1 ./internal/trigger/...` | PASS (1.524s) |
| LINT | `golangci-lint run ./internal/trigger/...` | PASS (0 issues) |
| ADVERSARY | `go test -tags=adversary -race -count=1 ./internal/trigger/...` | PASS (1.662s) |

## Adversary

- Model: grok-4.3 (agentpaas-adversary profile)
- Tests written: 15 (TestAdversaryB9T04_*)
- Breaks found: 0
- All vectors confirmed safe: event ordering, terminal handling, slow subscriber non-blocking, Last-Event-ID edge cases (negative/zero/large/non-numeric), heartbeat, InvokeStream context cancellation, race conditions, no cross-run leaks

## Acceptance Criteria

- [x] Ordered event IDs (monotonically increasing per run)
- [x] Heartbeats sent on idle SSE connections
- [x] Terminal event closes stream (no duplicates)
- [x] Last-Event-ID reconnect replays missed events
- [x] SSE over REST works at /v1/trigger/events?run_id=...
- [x] InvokeStream streams lifecycle events over gRPC
- [x] All tests pass: `go test -race -count=1 ./internal/trigger/...`

## Verifier

See docs/owa-records/b9-block-end.md

---

# B9-T05 OWA Record: Webhook Delivery

**Block:** 9 — Trigger API, Events, Webhooks, Cron
**Subtask:** T05 — Webhook Delivery
**Date:** 2026-06-22

## Worker

- Model: GPT-5.5 (Codex CLI)
- Branch: feat/b9-t05-webhook
- Commits: a7f9377, 6ff6de8 (SSRF fix) → merged as ef8cbf9
- Files: internal/trigger/{webhook.go, webhook_test.go, adversary_b9_t05_test.go}

## Scope

- HMAC-SHA256 webhook delivery with timestamp+body signing
- Receiver-side verification: HMAC signature + timestamp replay window (5 min)
- 3 retries with exponential backoff (1s, 2s, 4s)
- Dead-letter to audit after all retries fail
- Egress policy check blocks non-allow-listed domains
- Audit events: webhook_delivered, webhook_dead_lettered
- HTTP client prevents redirect following (SSRF fix)

## Gate Results

| Gate | Command | Result |
|------|---------|--------|
| BUILD | `go build ./...` | PASS |
| TEST | `go test -race -count=1 ./internal/trigger/...` | PASS (1.526s) |
| LINT | `golangci-lint run ./internal/trigger/...` | PASS (0 issues) |
| ADVERSARY | `go test -tags=adversary -race -count=1 ./internal/trigger/...` | PASS (43.583s) |

## Adversary

- Model: grok-4.3 (agentpaas-adversary profile)
- Tests written: 20 (TestAdversaryB9T05_*)
- Breaks found: 1 critical
  - **HTTP redirect following (SSRF bypass)** — default http.Client follows redirects, allowing an allowed external host to redirect to an internal/private host. Fix: set CheckRedirect to return http.ErrUseLastResponse. Commit 6ff6de8.
- After fix: all 20 adversary tests pass.

## Fix Worker

- Break: HTTP redirect SSRF bypass
- Fix: Set CheckRedirect on http.Client to http.ErrUseLastResponse (prevent all redirects)
- Commit: 6ff6de8

## Acceptance Criteria

- [x] HMAC-SHA256 signing with timestamp+body
- [x] 3 retries with exponential backoff (1s, 2s, 4s)
- [x] Dead-letter to audit after all retries fail
- [x] Non-allow-listed domain blocked
- [x] Bad HMAC rejected by receiver fixture
- [x] Replay (expired timestamp) rejected by receiver fixture
- [x] Audit events: webhook_delivered, webhook_dead_lettered
- [x] All tests pass

## Verifier

See docs/owa-records/b9-block-end.md

---

# B9-T06 OWA Record: Cron Triggers

## Subtask
B9-T06: Cron Triggers

## Scope
- Cron schedule parsing (5-field expressions: minute hour day-of-month month day-of-week)
- Step values (*/N), ranges (1-5), lists (1,3,5), wildcards (*)
- Timezone support via time.LoadLocation
- DST handling: skip nonexistent times (spring forward), deduplicate repeated times (fall back)
- Missed run policy: "skip" (default) or "catchup"
- Concurrency policy: "forbid" (default) prevents overlapping runs
- Idempotency key generation per schedule + time
- Audit events: cron_fired, cron_skipped, cron_missed
- Integration with TriggerService.Invoke

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Dispatch: existing worktree reused (Op Pattern #22)
- Files: internal/trigger/cron.go (465 lines), internal/trigger/cron_test.go (374 lines)
- Status: complete

## Gate (local)
- build: PASS
- test: PASS (0.496s)
- race: PASS (1.519s)
- lint: PASS (0 issues, fresh cache)
- adversary: PASS (20/20 tests, -count=5)

## Adversary
- Profile: agentpaas-adversary (grok-4.3)
- Tests: 20 total covering: large/zero/negative step, reverse range, non-numeric,
  empty field, tabs/whitespace, invalid timezone, caller ID propagation, active
  runs concurrent access, concurrency forbid race, start/stop race, tick/fire
  concurrent, missed audit, lastFire map growth, DST nonexistent skip, DST
  repeated once
- Breaks: 1 (TestAdversaryB9T06_DSTRepeatedOnce)
  - Severity: test bug (not production)
  - Description: test expected ZERO invocations during DST fall-back, but correct
    behavior is exactly ONE (first occurrence fires, second deduped by
    sameWallMinute). Additionally, fire() calls invoke in a goroutine, making
    the channel select non-deterministic (race).
  - Fix: changed assertion to expect 1 invocation + added time.Sleep(50ms) for
    goroutine completion. Commit 01d9228.
  - Verification: -count=5 PASS (5/5 runs stable)
  - Lesson: adversary DST tests must verify deduplication (fire once) not
    suppression (fire zero). See Op Pattern #29 (adversary test bugs vs
    production bugs).

## Verifier
See docs/owa-records/b9-block-end.md (block-end verification)

## Merge
- Commit: 7fb7ad9 (merge --no-ff)
- Branch: feat/b9-t06-cron → main
- Files: cron.go, cron_test.go, adversary_b9_t06_test.go (1241 insertions)

---

# B9-T07 OWA Record: Local Handoff Triggers

## Subtask
B9-T07: Local Handoff Triggers

## Scope
- Static approved handoffs from source agent to target agent via Trigger API
- Handoff caller ID: system:handoff:<source_agent>
- A2A-compatible envelope: source/target agent-card refs, parent run, correlation,
  message role, parts, artifact refs, metadata map
- Payload modes: empty, summary_ref, artifact_ref, fixed_json
- Cycle/depth guard (MaxDepth default 5, correlation ID tracking)
- Idempotency key: prefix + ":" + correlation ID
- Audit events: handoff_invoked, handoff_skipped, handoff_denied

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Files: handoff.go (412 lines), handoff_test.go (432 lines)
- Status: complete, all acceptance criteria met

## Gate (local)
- build: PASS
- test: PASS (0.477s)
- race: PASS (1.514s)
- lint: PASS (0 issues, fresh cache)
- adversary: PASS (9/9 tests, -count=5)

## Adversary
- Profile: agentpaas-adversary (grok-4.3)
- Tests: 9 total covering: unapproved source denial, max depth enforcement,
  caller ID injection, payload mode validation, idempotency key format,
  envelope integrity, concurrency, audit completeness, cycle guard
- Breaks: 2 (both test bugs, not production)
  1. CallerIDInjection: test used unconfigured source name → invoke never called.
     Fix: added configs for injection source names. Commit 27e8f39.
  2. CycleGuard: pre-set depth=2 with MaxDepth=3, but 2>=3 is false (not denied).
     Fix: changed depth to 3 (boundary). Commit 27e8f39.
  - Verification: -count=5 PASS (45/45 runs stable)
  - Lesson: adversary tests must verify the correct boundary condition and
    set up the precondition that exercises the code path being tested.

## Verifier
See docs/owa-records/b9-block-end.md (block-end verification)

## Merge
- Commit: merge --no-ff feat/b9-t07-handoff
- Files: handoff.go, handoff_test.go, adversary_b9_t07_test.go (1223 insertions)

---

# B9-T08 OWA Record: CancelRun Semantics

## Subtask
B9-T08: CancelRun Semantics

## Scope
- CancelRun RPC implementation (replacing T01 stub)
- RunStore: in-memory run tracking (Register, Get, MarkStarted, MarkFinished)
- Cancel paths: pending (immediate), running (graceful 30s → forced)
- Audit events: cancel_requested, cancel_graceful, cancel_forced
- EventBus integration: publishes EventRunCancelled
- Idempotent cancel (double cancel returns same Run)
- Terminal run rejection (FailedPrecondition)
- GetRun updated to use RunStore
- run_id validation: empty, whitespace, length > 256 → InvalidArgument

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Files: cancel.go (304 lines), cancel_test.go (215 lines), server.go (83 lines modified)
- Status: complete, all acceptance criteria met

## Gate (local)
- build: PASS
- test: PASS
- race: PASS
- lint: PASS (0 issues, fresh cache)
- adversary: PASS (12/12 tests, -count=5)

## Adversary
- Profile: agentpaas-adversary (grok-4.3)
- Tests: 12 total covering: idempotent cancel (double/triple), terminal rejection
  (succeeded/failed/budget_exceeded), pending immediate cancel, graceful period,
  forced cancel, empty/whitespace/long run_id, unknown run_id, audit completeness,
  EventBus publish, GetRun after cancel, concurrent cancel race, context cancellation
- Breaks: 1 (production bug)
  - EmptyWhitespaceLongRunID: CancelRun only checked req.GetRunId() == "" but
    accepted whitespace-only ("   ") and 1000-char run_ids. These should be
    InvalidArgument.
  - Fix: added strings.TrimSpace + length check (max 256 chars). Used trimmed
    runID throughout CancelRun. Commit fix(trigger): B9-T08 validate run_id.
  - Verification: -count=5 PASS (60/60 runs stable)
  - Lesson: input validation should always trim whitespace and enforce length
    limits, not just check for empty string.

## Verifier
See docs/owa-records/b9-block-end.md (block-end verification)

## Merge
- Commit: merge --no-ff feat/b9-t08-cancel
- Files: cancel.go, cancel_test.go, adversary_b9_t08_test.go, server.go (851 insertions)

---

# B9-T09 OWA Record: Control API REST/JSON Fuzzing

## Subtask
B9-T09: Control API REST/JSON Fuzzing

## Scope
- Fuzz the Control API REST JSON ingestion (POST /v1/trigger/invoke)
- 100k random JSON executions with 0 crashes
- Malformed JSON returns 400 with line/column information
- Large payloads rejected without crash
- Special characters don't cause panics
- Empty body returns 400 InvalidArgument
- Null bytes in JSON string values return 400 InvalidArgument
- JSON validation middleware on REST gateway

## Worker
- Model: GPT-5.5 via Codex CLI (local mode)
- Files: fuzz_test.go (260+ lines), server.go (128 lines modified)
- Status: complete, all acceptance criteria met

## Gate (local)
- build: PASS
- test: PASS (6.649s)
- race: PASS
- lint: PASS (0 issues, fresh cache)
- adversary: PASS (9/9 tests, -count=5)

## Adversary
- Profile: agentpaas-adversary (grok-4.3)
- Tests: 9 total covering: malformed JSON all cases (22 variants), empty body,
  null byte string, large payload, content-type manipulation, HTTP method
  validation, path traversal, concurrent requests, non-object top-level
- Breaks: 2 (both production bugs)
  1. MalformedJSONAllCases case 9: JSON with null byte in string value
     (" test") returned 200 instead of 400. The protobuf JSON decoder
     accepts null bytes in string values.
  2. EmptyBody: POST with empty body returned 200 with pending run instead
     of 400 InvalidArgument.
  - Fix: added jsonValidationMiddleware that checks for empty body and null
     bytes (raw 0x00 and escaped  ) before passing to the REST gateway.
     Returns 400 with line/column info. Commit fix(trigger): B9-T09.
  - Verification: -count=5 PASS (45/45 runs stable)
  - Lesson: REST gateways need pre-decode validation for empty bodies and
     null bytes — the protobuf JSON decoder is lenient by default.

## Verifier
See docs/owa-records/b9-block-end.md (block-end verification)

## Merge
- Commit: merge --no-ff feat/b9-t09-fuzz
- Files: fuzz_test.go, server.go, adversary_b9_t09_test.go

---

# Block 9 Block-End Verifier Report

**Block:** 9 — Trigger API, Events, Webhooks, Cron
**Date:** 2026-06-22
**Verifier model:** z-ai/glm-5.2 (agentpaas-verifier profile, fresh session)
**Repo:** /Users/pms88/projects/agentpaas
**Branch:** main (all 9 subtasks + verifier fixes merged)

## Verifier Scope

Full Block 9 verification: gate execution, cross-subtask integration review,
adversary test stability (count=5), security review, and code quality audit.
9 subtasks (T01-T09) covering `internal/trigger/` — Trigger API (gRPC :7718 +
REST :7717), event bus, webhook delivery, cron triggers, local handoff,
CancelRun, and REST/JSON fuzzing.

## Gate Results (post-fix)

| Gate | Command | Result |
|------|---------|--------|
| BUILD | `go build ./...` | PASS |
| TEST | `go test ./internal/trigger/ -count=1` | PASS (92 tests) |
| RACE | `go test -race ./internal/trigger/ -count=1` | PASS (0 races, 47s) |
| VET | `go vet ./internal/trigger/...` | PASS |
| LINT | `golangci-lint run ./internal/trigger/...` (fresh cache) | PASS (0 issues) |
| OSV | `make osv` | PASS (7 daemon-side CVEs filtered, accurate reasoning) |
| ADVERSARY | `go test -tags=adversary -run TestAdversaryB9 -count=5` | PASS (96 tests × 5 = 480 execs, 0 flaky, 217s) |
| BLOCK GATE | `make block9-gate` | PASS (build/test/race/lint/osv + trigger tests + adversary) |
| GOFMT | `gofmt -l internal/trigger/` | PASS (0 files need formatting) |

## Cross-Subtask Integration Findings

| Integration | Status | Evidence |
|-------------|--------|----------|
| T01→T02: Bearer auth with API key store | PASS | APIKeyStore.Authenticate implements Authenticator, returns "api_key:<id>" |
| T01→T03: caller ID flows to idempotency hash | PASS | server.go CallerFromContext feeds CanonicalRequestHash |
| T01→T04: SSE handler enforces auth | PASS (fixed) | ServeSSE checks CallerFromContext; server.go SSE route extracts Bearer token |
| T01→T05: webhook delivery caller ID | PASS | webhook.go audit actor "system:webhook" |
| T01→T06: cron caller ID "system:cron:" | PASS | cron.go:195 |
| T01→T07: handoff caller ID "system:handoff:" | PASS | handoff.go:239 |
| T01→T08: CancelRun respects auth | PASS | cancel.go:130, AuthInterceptor covers unary RPCs |
| T03→T06: cron uses idempotency key prefix | PASS | cronIdempotencyKey() appends :YYYYMMDDHHMM |
| T03→T07: handoff uses idempotency key prefix | PASS | idempotencyKey() appends :correlationID |
| T04→T08: CancelRun publishes EventRunCancelled | PASS | cancel.go:158,191 |
| T04→T05: events trigger webhooks | PASS | EventBus.Publish → webhook subscriber |
| T09→T01: fuzz tests exercise REST gateway | PASS | fuzz_test.go tests jsonValidationMiddleware |

## Adversary Test Stability

All 96 adversary tests across 7 files passed 5 consecutive runs (-count=5):
- adversary_b9_t01_test.go: 15 tests (InvokeStream auth bypass)
- adversary_b9_t03_test.go: 11 tests (payload limits, idempotency)
- adversary_b9_t05_test.go: 20 tests (SSRF redirect, egress policy, HMAC)
- adversary_b9_t06_test.go: 20 tests (DST handling, cron caller ID)
- adversary_b9_t07_test.go: 9 tests (handoff caller ID, cycle guard)
- adversary_b9_t08_test.go: 12 tests (cancel validation, context cancellation)
- adversary_b9_t09_test.go: 9 tests (empty body, null bytes, REST fuzz)

No flaky tests. Total: 480 executions, 0 failures.

## Security Review

| Check | Status |
|-------|--------|
| Auth interceptor covers unary AND streaming RPCs | PASS |
| Webhook delivery disables HTTP redirect (SSRF) | PASS (CheckRedirect: http.ErrUseLastResponse) |
| CancelRun validates run_id (whitespace + length) | PASS |
| REST gateway rejects empty body + null bytes | PASS |
| Cron DST: skip nonexistent, deduplicate repeated | PASS |
| Handoff caller ID always "system:handoff:" prefixed | PASS |
| Idempotency: same key+hash → replay; same key+diff hash → conflict | PASS |
| SSE endpoint enforces auth (verifier fix) | PASS |
| API key store: constant-time comparison | PASS |
| API key store: symlink path rejection | PASS |
| Idempotency store: symlink/system path rejection | PASS |
| Webhook HMAC: constant-time comparison | PASS |
| Webhook URL redaction in audit logs | PASS |

## Verifier Findings and Resolutions

### verifier-1a: `make block9-gate` target not implemented (BLOCKER)
**Finding:** Makefile block9-gate target was still "not implemented" placeholder.
**Fix:** Implemented gate target (build/test/race/lint/osv + trigger tests + adversary).
Updated help text to "ACTIVE".
**Commit:** e591d70 (fix worker), merged in 668e53b.
**Verification:** `make block9-gate` exits 0.

### verifier-2a: gofmt formatting issues in 2 test files (LOW)
**Finding:** adversary_b9_t08_test.go and adversary_b9_t09_test.go had trailing
newline / whitespace alignment issues.
**Fix:** `gofmt -w` applied to both files.
**Commit:** e591d70 (fix worker).
**Verification:** `gofmt -l internal/trigger/` returns empty.

### verifier-3a: SSE endpoint does not enforce authentication (LOW)
**Finding:** ServeSSE handler did not check for valid caller ID. Unauthenticated
clients could connect to SSE stream if they knew a run_id.
**Fix:** Added auth check at start of ServeSSE (returns 401 if no caller in
context). Added Bearer token extraction in server.go SSE route handler. Updated
all existing SSE tests to include auth. Added new test
TestSSEHandlerRejectsUnauthenticatedRequest and
TestSSEEndpointRequiresAndAcceptsAuth.
**Commit:** c6186ff (fix worker).
**Verification:** All SSE tests pass, new auth tests pass, adversary tests pass.

## Code Quality

- No TODO/FIXME in production code: PASS
- All error returns checked (errcheck): PASS (0 issues)
- No unused variables/imports: PASS
- gofmt clean: PASS

## Final Verdict

**VERIFY PASS**

All gate components green (build/test/race/lint/osv/adversary/gate/gofmt).
All cross-subtask integrations verified. All security checks pass. All 3 verifier
findings (1 BLOCKER, 2 LOW) resolved by fix worker and verified on merged main.

Block 9 is complete and ready for checkpoint.
