# Current State — Block 34 in progress (T03 done)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01 Freeze workflow/handoff conformance fixtures | DONE |
| T02 Strict pipeline compilation | DONE |
| T03 SDK input/handoff ops | DONE |
| T04 Durable linear scheduler | NEXT |
| T05 Stage containers + authority | pending |
| T06 Artifact transfer + provenance | pending |
| T07 Failure/cancel/pause/idempotency | pending |
| T08 Deploy invoke + operator inspection + ref proof | pending |
| T09 block34-gate + adversary | pending |

## T03 evidence (local, 2026-07-24)
- harness RPC workflow_input + commit_handoff (candidate staging)
- Python SDK methods + 10 unittest
- docs/owa-records/b34-t03.md
- pipeline still not enabled at daemon admission

## Suggested read order
1. This file
2. docs/owa-records/b34-t03.md
3. docs/execution/blocks/b34-summary.md § T04
