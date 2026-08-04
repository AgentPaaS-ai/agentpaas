# M9 — Audit + metrics APIs

**Status:** ENG GREEN + LIVE 2026-08-03
**Cloud main:** c3e7718 (src/audit.ts + routes) — 564 tests PASS
**OSS main:** FF feat/m9-audit-cli
**Worker:** redeployed c1a03982… with rebind

## APIs
- GET /v1/runs/:id/events
- GET /v1/audit?since&until&limit
- GET /v1/runs/:id/audit/export (hash chain, genesis)
- GET /v1/metrics

## CLI / Hermes
- cloud events | audit | audit export | metrics (+ --json)
- agentpaas_cloud_events/audit/audit_export/metrics

## Live smoke
- cloud metrics --json and cloud audit --json via CLI (not raw urllib; CF 1010)

## Residuals
- Dashboard usage-page stub (M10 WI)
- Founder manual gate: Hermes NL “what ran this week…” not run this session
- Hash chain is export-time over stored events (not continuous signed checkpoints)

## Next
M10 skinny dashboard + 30-min onboarding (R4 DNS still pending founder)

## Payload wire fix
Audit event `payload` is string|null on API; CLI initially expected object. Fixed.
`cloud audit --json` and metrics verified live.
