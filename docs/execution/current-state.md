# Current State — Block 34 in progress (T07 library done)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01–T06 | DONE |
| T07 Failure/cancel/pause/idempotency | DONE (library cancel/fail/resume/races) |
| T08 Deploy invoke + operator inspection | NEXT |
| T09 block34-gate + adversary | pending |

## Evidence
- T07c1 CancelWorkflow + late success reject
- T07c2 ResumeWorkflow + control races
- docs/owa-records/b34-t07-chunk{1,2}.md
- Admission still gated

## Next
T08 operator inspection + pipeline admission enable + ref proof scaffolding
