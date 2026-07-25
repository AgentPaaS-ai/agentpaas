# Current State — Block 34 in progress (T06 done)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01–T04 | DONE |
| T05 Stage containers + authority | DONE (library+mock driver; labels/network/fence/authority) |
| T06 Artifact transfer + provenance | DONE (library + MemoryArtifactStore; 30 tests) |
| T07 Failure/cancel/pause/idempotency | pending |
| T08 Deploy invoke + operator inspection | pending |
| T09 block34-gate + adversary | pending |

## Evidence
- T04: controller, pause, reconcile, crash matrix
- T05: PipelineStageLabels, BuildStageContainerSpec, IntersectStageAuthority, RuntimeStageLauncher, FenceStage; 8 isolation tests
- T06: ArtifactStore (MemoryArtifactStore), WorkflowArtifactBudget, PromoteHandoffArtifacts, BuildROProjection, ProvenanceChain; 6 error codes; 30 tests (including path traversal, multi-megabyte, budget exhaustion, owner mismatch)
- docs/owa-records/b34-t05.md
- docs/owa-records/b34-t06.md
- Admission still gated; live Docker e2e optional residual

## Next
T07 Failure/cancel/pause/idempotency
