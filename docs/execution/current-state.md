# Current State — Block 34 in progress (T04 chunk3 done)

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines

## B34 progress
| Task | Status |
|------|--------|
| T01 Freeze workflow/handoff conformance fixtures | DONE |
| T02 Strict pipeline compilation | DONE |
| T03 SDK input/handoff ops | DONE |
| T04 Durable linear scheduler | IN PROGRESS (c1+c2+c3 done; c4 crash matrix next) |
| T05 Stage containers + authority | pending |
| T06 Artifact transfer + provenance | pending |
| T07 Failure/cancel/pause/idempotency (full matrix) | pending (pause seam in c2) |
| T08 Deploy invoke + operator inspection + ref proof | pending |
| T09 block34-gate + adversary | pending |

## T04 evidence
- c1: Controller Claim/Ack/CommitSuccess/Failure (MemoryStore)
- c2: PAUSE_REQUESTED blocks claim; success→PAUSED without next READY
- c3: LaunchIdempotencyKey `wf|node|gen`, StageLaunchJob+FakeLauncher, ReconcileOnce, harness PipelineStageContextFromParams, thin daemon PipelineReconcileHook
- docs/owa-records/b34-t04-chunk{1,2,3}.md
- Orch re-ran: `go test -race ./internal/workflow/pipeline/`, harness FromParams, daemon Tick — PASS
- Admission still `agentpaas_pipeline_not_enabled`; no Docker e2e

## Gaps deferred (c4 / later)
- Wire PipelineReconcileHook into Daemon.Start loop
- Full envelope JSON in CollectStageContextParams (currently ContextJSON only)
- Live harness RPC test SetPipelineContext+workflow_input (struct path covered)
- Concurrent reconcile when PutIfAbsent loses race still uses local job copy for EnsureLaunch
- LAUNCHING recovery defaults launch gen=1 (first-claim only)
- Crash inject matrix at every CAS boundary (T04 item 402–407)

## Next session
T04 chunk4: crash/idempotency expansion at CAS boundaries; optional daemon loop wire (still fake launch; Docker e2e stays T05)
