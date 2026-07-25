# Current State — Block 34 in progress (T08 library done, T09 next)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01–T06 | DONE |
| T07 Failure/cancel/pause/idempotency | DONE (library cancel/fail/resume/races) |
| T08 Deploy invoke + operator inspection + enable flag + ref proof | DONE (inspect + enable + reference proof) |
| T09 block34-gate + adversary | NEXT |

## Evidence
- T07c1 CancelWorkflow + late success reject
- T07c2 ResumeWorkflow + control races
- T08 PipelineInspectSummary + BuildPipelineInspect
- T08 pipeline runtime enable (pipelineEnabled field + AGENTPAAS_PIPELINE_ENABLED=1)
- T08 ReferenceProof_ThreeStageLinear (Hermes-absent library proof)
- docs/owa-records/b34-t{07,08}.md

## Next
T09 adversary review + Docker e2e + Hermes pack live + full block34-gate
