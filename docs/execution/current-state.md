# Current State — Block 34 in progress (T04 complete)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01 Freeze workflow/handoff conformance fixtures | DONE |
| T02 Strict pipeline compilation | DONE |
| T03 SDK input/handoff ops | DONE |
| T04 Durable linear scheduler | DONE (c1–c4 library+fake launch; admission still gated) |
| T05 Stage containers + authority | NEXT |
| T06 Artifact transfer + provenance | pending |
| T07 Failure/cancel/pause/idempotency (full matrix) | pending |
| T08 Deploy invoke + operator inspection + ref proof | pending |
| T09 block34-gate + adversary | pending |

## T04 evidence
- c1 controller CAS; c2 pause seam; c3 reconcile+fake launch; c4 crash/idempotency matrix
- docs/owa-records/b34-t04-chunk{1,2,3,4}.md
- go test -race ./internal/workflow/pipeline/ PASS
- Admission still agentpaas_pipeline_not_enabled; no Docker e2e

## Residuals into T05+
- Daemon.Start does not tick PipelineReconcileHook
- Real container StageLauncher (T05)
- Full cancel/fence matrix (T07)

## Next
T05: separate stage containers + authority isolation
