# AgentPaaS Checkpoint

Date/time: 2026-06-11 22:53:09 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 9 Trigger API, events/webhooks, and cron
planning review after the Block 8 packaging checkpoint. The work remained in
planning/spec review mode; no implementation code has been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_22-53-09_PDT.md`

Latest committed checkpoint entering this review:
- `b36f211 docs: clarify block 8 packaging pipeline`

## Block 9 Review Outcome

Block 9 is now scoped as the P1 callable/unattended execution surface. It
turns a packed local agent into something Codex, Hermes, Claude Code, the
AgentPaaS CLI, local apps, CI jobs, or cron can invoke without bypassing the
identity, policy, gateway, budget, secrets, audit, and observability controls
from earlier blocks.

Decisions locked:
- Trigger API serves gRPC on `:7718` and grpc-gateway REST on `:7717`,
  loopback by default.
- Trigger API requires AgentPaaS API-key or mTLS auth even on loopback.
  Loopback reduces network exposure but is not the authorization boundary.
- `--expose` refuses to start without an API key.
- AgentPaaS API keys are Trigger API credentials used to access an agent
  being tested or run locally. They are shown once, stored hashed, scoped by
  agent/action, revocable/rotatable, and audited by key id.
- REST CORS is deny-by-default. Browser-originated local requests receive no
  ambient trust from being on localhost.
- Stable caller ids are `api_key:<id>`, `spiffe:<subject>`,
  `system:cron:<agent>`, and `local_user:<uid>`.
- Rate limiting is token-bucket per caller.
- Idempotency is durable, survives daemon restart, and uses a 24-hour replay
  window to protect client retries from causing duplicate external effects.
- The canonical idempotency hash covers caller id, agent name, `agent.lock`
  digest, payload bytes, content type, and API version.
- Same idempotency key with the same canonical request returns the original
  `run_id`; same key with a different request returns 409. Expired keys return
  an explicit expired-key error.
- Invoke payloads are capped at 1 MiB by default. Larger inputs should be
  stored externally and passed by reference or future managed blob handle.
- `InvokeStream` exists for live progress in CLI, dashboard, and coding-tool
  integrations. REST uses SSE.
- P1 event types include `run_queued`, `run_started`, `run_log`, `run_span`,
  `egress_allowed`, `egress_denied`, `secret_injected`, `budget_warning`,
  `budget_exceeded`, `cancel_requested`, `run_canceled`, `run_failed`, and
  `run_succeeded`.
- SSE supports ordered event ids, heartbeat, and `Last-Event-ID` reconnect
  without duplicating terminal events.
- P1 supports URL webhooks only; local command hooks are deferred.
- Webhook deliveries are HMAC-signed with timestamp/replay-window protection,
  retried 3x with exponential backoff, and dead-lettered to audit.
- Webhook destinations are policy-checked egress.
- P1 cron uses 5-field syntax only, local timezone by default, and optional
  explicit timezone.
- DST behavior is explicit: nonexistent local time is skipped; repeated local
  time runs once.
- Missed-run policy defaults to `skip`; `catchup: 1` is explicit opt-in.
- Cron concurrency defaults to `forbid`; a tick is skipped and audited if the
  prior run is still active.
- `CancelRun` records `cancel_requested`, asks the harness/gateway path to
  stop gracefully, waits 30s, then force-stops the container if needed.
- Dashboard read-only loopback SSE may remain unauthenticated in P1, but
  exposed dashboard routes require API key, and mutating Trigger API calls
  require auth even on loopback.

## Tests / Gates Added To Plan

Block 9 now requires:
- API conformance suite generated from proto
- API-key lifecycle e2e: create, shown once, hashed storage, list, revoke,
  rotate, auth failure audit
- CORS/preflight tests proving random browser-originated localhost requests
  without API key are denied
- idempotency replay, conflict, expiry, and daemon-restart durability tests
- rate-limit tests with `Retry-After`
- malformed JSON and 1 MiB payload-limit tests
- SSE reconnect tests with `Last-Event-ID`, heartbeat, ordered event ids, and
  no duplicate terminal events
- webhook delivery, retry, HMAC, replay rejection, bad-signature rejection,
  policy-denied destination, and dead-letter audit tests
- cron tests for missed-run behavior, `catchup: 1`, DST skip/repeated-time
  behavior, and `concurrency_policy: forbid`
- cancelation e2e proving graceful then forced behavior and final audit
  outcome
- fuzz on REST JSON ingestion: 100k executions, 0 crashes
- tests proving cron and webhooks use the same policy/audit path as manual
  Invoke

Required Trigger/API audit events now include:
- `api_key_created`
- `api_key_revoked`
- `auth_failed`
- `invoke_accepted`
- `invoke_rejected`
- `idempotency_replayed`
- `idempotency_conflict`
- `rate_limited`
- `webhook_delivered`
- `webhook_dead_lettered`
- `cron_missed`
- `cron_skipped_concurrency`
- `cancel_requested`
- `cancel_graceful`
- `cancel_forced`

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 9
- `agentpaas-prd-v4-master.md` §2.7 API surfaces
- `agentpaas-prd-v4-master.md` §2.7.1 Trigger semantics
- `agentpaas-prd-v4-master.md` §2.10 Dashboard

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 10, the OTel pipeline and
dashboard. Focus on local data retention, SQLite/WAL behavior, dashboard auth
boundaries, SSE reconnects from Block 9, XSS/log escaping, run timeline
shape, and audit export/verify UX.

Other open items still relevant:
1. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
2. Policy schema reference: required/optional fields, defaults, unknown
   fields, wildcard behavior, CIDR/private-network behavior, credential
   binding behavior, MCP declarations.
3. Telemetry/privacy: no telemetry without opt-in; define opt-in UX and
   payload.
4. P1 must-ship vs can-slip: plan is large; decide what can slip without
   weakening the wedge.
5. Security review packet: threat model, policy, SBOM, signed audit export,
   enforcement proof, limitations.
6. Dashboard specifics: `docs/status.md` generation and GitHub API/CLI
   dependency.
7. GitHub Project setup once private repo exists.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-11 22:53:09 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `b36f211`. Next, review Block 10: OTel pipeline and
Dashboard, especially retention, SQLite/WAL behavior, dashboard auth
boundaries, SSE reconnects, XSS/log escaping, run timeline shape, and audit
export/verify UX."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.
