# AgentPaaS Checkpoint

Date/time: 2026-06-11 23:02:26 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 10 OTel pipeline and dashboard planning
review after the Block 9 Trigger API checkpoint. This is a major checkpoint:
Blocks 1-10 have now been reviewed and tightened before implementation. The
work remained in planning/spec review mode; no implementation code has been
built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_23-02-26_PDT.md`

Latest committed checkpoint entering this review:
- `315b75a docs: clarify block 9 trigger semantics`

## Block 10 Review Outcome

Block 10 is now scoped as the local observability and review surface. It
turns runtime events, spans, logs, policy state, and audit records into a
dashboard that developers and security reviewers can use to understand what
ran, what was allowed or denied, what it cost, and how to export signed proof.

Core distinction locked:
- OTel/SQLite is for visibility, correlation, and dashboard speed.
- Canonical audit JSONL is the authoritative security/compliance record.
- SQLite audit indexes are derived/rebuildable and must not be presented as
  the source of truth.

Decisions locked:
- P1 uses an in-process OTLP collector writing to SQLite in WAL mode.
- Dashboard telemetry retention defaults to 7 days and is configurable.
- OTel retention applies only to dashboard traces/logs/metrics.
- Canonical audit JSONL is not pruned by dashboard retention; it is retained
  until an explicit future user retention/purge command.
- Agent, harness, and gateway logs are ingested as OTel log records for
  dashboard correlation.
- Daemon operational logs remain bounded structured JSON files under
  `~/.agentpaas/logs/` with rotation and redaction.
- Dashboard logs and daemon operational logs are not canonical audit records.
- Log and trace rendering treats agent-controlled text as hostile.
- HTML, binary/control characters, huge attributes, and sentinel secrets are
  escaped, truncated, and/or redacted before display.
- Cost estimates record provider, model, price-table version, token counts,
  and `estimated=true`.
- P1 ships a built-in price table; P2 allows user or tenant-modified price
  tables.
- Policy view shows both human git-file diff and normalized effective policy
  digest used for enforcement/audit.
- Audit search is labeled as an indexed view over canonical audit records.
- One-click signed audit export UX shows trust-anchor fingerprint, included
  sequence range, verification command, and verification result.
- Dashboard read-only loopback SSE may be unauthenticated in P1.
- Exposed dashboard routes require API key/session.
- Mutating Trigger API calls require auth even on loopback.
- Dashboard SSE reuses Block 9 ordered event id, heartbeat, and
  `Last-Event-ID` reconnect semantics.
- Dashboard security requires strict CSP, no inline JS, CSRF tokens on
  mutating routes, no runtime CDN, no API keys stored in browser localStorage,
  and no sampling/removal of security-relevant canonical audit events even
  when OTel telemetry is pruned.

## Tests / Gates Added To Plan

Block 10 now requires:
- Playwright e2e: launch agent, watch live run, see DENIED egress row, export
  audit, verify export
- Lighthouse performance score >= 90 local
- 10k-span run rendering with virtualized lists
- SSE reconnect behavior using `Last-Event-ID`
- SQLite WAL/read-pool behavior under concurrent writes
- SQLite migration, WAL checkpoint, vacuum/prune, and corruption recovery
- XSS escape test with planted `<script>` in agent output
- sentinel-secret redaction tests across logs, spans, trace attributes, and
  errors
- binary/control-character and huge-log/attribute truncation tests
- empty-state rendering for zero agents and zero runs
- policy diff tests for both git-file diff and normalized effective digest
- signed audit export/verify UX test
- accessibility and keyboard smoke test

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 10
- `agentpaas-prd-v4-master.md` §2.10 Dashboard

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 11, the red-team suite. Focus on
whether the attacker library exercises the real pack/run path, whether every
prior security promise has a malicious fixture, and whether the suite is
stable enough to become a permanent CI release gate.

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

"Continue from the AgentPaaS checkpoint dated 2026-06-11 23:02:26 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `315b75a`. Next, review Block 11: Red-team suite,
especially whether every attacker runs through the real pack/run path,
whether the attack library covers prior security promises, and whether the
suite is suitable as a permanent CI release gate."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.
