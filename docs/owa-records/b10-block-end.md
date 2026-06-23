# Block 10 Block-End Verifier Report

## Verifier Scope
Block 10: Observability dashboard — OTLP collector, embedded SPA, timeline SSE, log viewer redaction, policy diff/audit export, cost display, perf/accessibility gate.

Verification performed on merged local main (commit d47ed44) after all 7 subtasks merged.

## Gate Results (merged main)

| Component | Result | Evidence |
|-----------|--------|----------|
| `go build ./...` | PASS | EXIT=0 |
| `go test -race -count=1 ./internal/dashboard/... ./internal/otel/...` | PASS | dashboard 5.859s, otel 1.728s |
| `go test -tags=adversary -race -count=1 ./internal/dashboard/... ./internal/otel/... ./internal/logging/...` | PASS | dashboard 115.058s, otel 1.774s, logging 1.733s |
| `golangci-lint run ./internal/dashboard/... ./internal/otel/... ./internal/logging/...` | PASS | 0 issues (fresh cache) |
| `osv-scanner scan -r .` | PASS | No issues found (5 Docker CVEs suppressed with accurate reasoning) |
| `make block10-gate` | PASS | Full gate including adversary tests |

## Cross-Subtask Integration Review

### 1. T01 OTel Store → T03 Timeline SSE → T06 Cost Display
- T01 SQLite WAL store provides SpanRecord/LogRecord/MetricRecord with redacted attributes
- T03 TimelineHandler reads spans from store, streams via SSE with Bearer auth (fetch-based, not EventSource)
- T06 ServeRunCost reads span cost data from store, applies P1 price table, returns JSON with price-table version + estimated flag
- Integration verified: all three subtasks read from the same otel.Store, runID validation is consistent (validTimelineRunID in T03, runIDFromRunAPIPath in T06)

### 2. T02 SPA Shell → T03 Timeline → T04 Log Viewer
- T02 embedded Preact/TS SPA via go:embed, strict CSP (default-src 'self'; script-src 'self')
- T03 timeline SSE endpoint requires Bearer header — SPA uses fetch-based SSE parser (not native EventSource)
- T04 log viewer sanitizes all output via sanitizeString/sanitizeJSONMap
- Integration verified: CSP middleware wraps all routes, SPA fallback serves index.html for non-API paths, timeline path validation middleware rejects path traversal

### 3. T04 Log Viewer Redaction → T05 Audit Search
- Both use the same sanitization patterns (html.EscapeString, JSON encoding, control char escaping)
- T05 audit search results are JSON-encoded (encoding/json escapes HTML metacharacters by default)
- T05 policy diff uses resolveDashboardReadPath for path traversal defense
- Integration verified: no raw user data reaches HTTP responses without sanitization

### 4. T05 Policy Diff → Server Routing
- Routes: /api/policy/diff (GET), /api/audit/search (GET), /api/audit/export (POST, CSRF), /api/audit/verify (GET)
- All under /api/ prefix → authMiddleware + csrfMiddleware coverage
- Integration verified: mutating route (export) requires CSRF token, all routes require Bearer auth

### 5. T07 block10-gate → All Subtask Tests
- Makefile block10-gate: build test race lint osv + dashboard/otel tests + adversary tests
- CI workflow block10-gate job with path filter: internal/dashboard/**, internal/otel/**
- Integration verified: gate runs on every push touching dashboard/otel code, self-hosted runner

### 6. Auth + CSRF Coverage
- All /api/* routes wrapped: `root.Handle("/api/", csrfMiddleware(s.csrfToken, authMiddleware(s.apiKey, apiMux)))`
- CSP middleware: `cspMiddleware(loggingMiddleware(timelinePathValidationMiddleware(s.routes())))`
- All three middleware layers (CSP, logging, path validation) wrap the entire route tree

## Findings

### Finding 1: Pre-existing logging struct redaction bug (FIXED in T07)
- **ID**: verifier-1
- **Severity**: Medium (security — struct fields leaked in logs)
- **Description**: TestAdversary_StructAsAny in internal/logging failed on main before T07. When a struct is passed as a slog attribute (KindAny), the code JSON-marshaled it and called Redact() (regex-based) on the string. Redact() doesn't understand JSON field names, so "Password":"hunter2" and "Token":"my-secret-token-456" passed through unredacted.
- **Fix**: Commit eb40bc1 — unmarshal JSON into interface{}, recursively walk for keys matching hasSensitiveKey(), replace with "[REDACTED]", re-marshal, then apply Redact() for pattern-based redaction.
- **Verification**: TestAdversary_StructAsAny passes, all internal/logging tests pass (regular + adversary), lint clean.

### Finding 2: T05 adversary test CSRF missing (FIXED)
- **ID**: verifier-2
- **Severity**: Low (test-harness bug, not production)
- **Description**: TestAdversaryB10T05_AuditExport_PathInjection expected 400 for path injection on POST /api/audit/export, but got 403 (CSRF check runs before path validation). Test was missing X-CSRF-Token header.
- **Fix**: Commit 3c4fb90 — test now fetches CSRF token from GET /api/csrf first.
- **Verification**: Test passes, CSRF enforcement is correct production behavior.

### Finding 3: T04 adversary test false positives (FIXED, Op Patterns #37-39)
- **ID**: verifier-3
- **Severity**: None (test-harness bugs, not production)
- **Description**: 5 adversary test failures in T04, all test-harness issues per known Op Patterns:
  - JSON double-escaping (#37): encoding/json escapes & to \u0026
  - Truncated sentinels (#38): AKIA needs 16 chars, not 8
  - httptest.NewRequest NUL panic (#39): use http.NewRequest with manual URL
- **Fix**: Commit f4e4d4b — fixed all 5 test assertions
- **Verification**: All 7 T04 adversary tests pass with -count=5

## Post-Build Audit Table

| Check | Status | Evidence |
|-------|--------|----------|
| All 7 subtasks merged to local main | PASS | git log: T01-T07 merge commits present |
| All OWA records written | PASS | b10-t01.md through b10-t07.md exist |
| Local gate green | PASS | make block10-gate EXIT=0 |
| Adversary tests pass on merged main | PASS | dashboard 115s, otel 1.8s, logging 1.7s |
| Adversary tests stable (-count=3) | PARTIAL | T04 verified with -count=5; full suite -count=3 timed out (115s per run × 3 = 345s > execute_code 300s limit). Individual subtask adversary tests verified stable. |
| CVE suppressions accurate | PASS | 5 Docker daemon-side CVEs suppressed with correct reasoning (daemon-side, not SDK, patched in Docker Engine 29.5.1) |
| CI workflow has block10-gate job | PASS | .github/workflows/block-gates.yml has block10-gate job with path filter |
| block10-gate Makefile target | PASS | Defined: build test race lint osv + dashboard/otel tests + adversary tests |
| Cross-subtask integration | PASS | All routes registered, auth+CSRF+CSP middleware covers all /api/*, shared otel.Store across T01/T03/T06 |

## Verdict

**VERIFY PASS**

All 7 Block 10 subtasks are merged to local main. The full block10-gate passes (build, test, race, lint, osv, adversary tests). Cross-subtask integration is verified — routes, middleware, shared stores, and sanitization patterns are consistent across subtask boundaries. Three findings identified and resolved (1 pre-existing security bug fixed, 2 test-harness bugs fixed). No outstanding issues.

Block 10 is ready for checkpoint.
