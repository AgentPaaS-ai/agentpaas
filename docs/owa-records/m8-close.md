# M8 — Run-record persistence CLI (rescoped) CLOSE

**Status:** CLOSED eng 2026-08-03 (post-M7.6)
**Depends on:** M7.6 green

## Scope residual after M7.5 moved output loop

| Claim | Status | Evidence |
|-------|--------|----------|
| Full run-record until explicit delete | PASS | no TTL cleanup in cloud src/; runs persist |
| cloud status / logs / result | PASS | OSS CLI + nuclear E2E |
| DELETE /v1/runs/:id kills artifacts + signed URLs | PASS | test/m8-run-delete.test.ts (delete → URL 404; cross-tenant 404) |
| Signed-URL tenant scoping regression | PASS | same suite + M7.5 T01 artifacts ADV |

## Not in M8

- CLI `cloud runs delete` verb — API exists; optional polish → M8.6 JSON surface can add
- M8.6 machine JSON layer
- M8.5 registry/MCP

## Gate

Standard TDD: m8-run-delete tests in cloud 553 suite. Live delete optional (cost).
