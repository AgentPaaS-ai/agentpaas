# Current State — Block 34 in progress (T01 done)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01 Freeze workflow/handoff conformance fixtures | DONE |
| T02 Strict pipeline compilation | NEXT |
| T03 SDK input/handoff ops | pending |
| T04 Durable linear scheduler | pending |
| T05 Stage containers + authority | pending |
| T06 Artifact transfer + provenance | pending |
| T07 Failure/cancel/pause/idempotency | pending |
| T08 Deploy invoke + operator inspection + ref proof | pending |
| T09 block34-gate + adversary | pending |

## T01 evidence (local, 2026-07-24)
- Package `internal/workflow/pipeline/` validators + fixtures
- `go test ./internal/workflow/pipeline/...` PASS (race PASS)
- pack/daemon pipeline-not-enabled still green
- Handoff: docs/owa-records/b34-t01.md
- Residuals for T02: pack min-2 single source of truth; real UNDECLARED_MCP

## Operator notes
- After `make build`/`build-all`: `make restart-local-daemon`
- CI is local-only (no GH runners)
- MCP: per-workflow internal net; bridge :8080; harness admin 127.0.0.1:8090

## Suggested read order
1. This file
2. docs/owa-records/b34-t01.md
3. docs/execution/blocks/b34-summary.md § T02
