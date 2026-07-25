# Current State — Block 34 in progress (T02 done)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01 Freeze workflow/handoff conformance fixtures | DONE |
| T02 Strict pipeline compilation | DONE |
| T03 SDK input/handoff ops | NEXT |
| T04 Durable linear scheduler | pending |
| T05 Stage containers + authority | pending |
| T06 Artifact transfer + provenance | pending |
| T07 Failure/cancel/pause/idempotency | pending |
| T08 Deploy invoke + operator inspection + ref proof | pending |
| T09 block34-gate + adversary | pending |

## T02 evidence (local, 2026-07-24)
- CompilePipeline + CompiledPipelineSnapshot (deterministic digest)
- pack min-2 stages; real PIPELINE_UNDECLARED_MCP
- pipeline tests race PASS; daemon still pipeline_not_enabled
- docs/owa-records/b34-t02.md

## Suggested read order
1. This file
2. docs/owa-records/b34-t02.md
3. docs/execution/blocks/b34-summary.md § T03
