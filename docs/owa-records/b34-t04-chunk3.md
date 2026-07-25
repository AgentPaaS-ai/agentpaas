# B34-T04 Chunk 3 — Handoff Record

**Date:** 2026-07-24
**Author:** OWA Worker (ap-worker)
**Branch:** feat/b34-t04-scheduler
**Base commit:** 357947a

## What was implemented

### Files created/modified

**`internal/workflow/pipeline/controller.go`** (modified)
- Extended `Claim` with `LaunchGeneration` (int64) and `LaunchKey` (string)
- `ClaimNextReady` populates both from `LaunchIdempotencyKey(workflowID, nodeID, nodeGen)`
- Added CAS conflict retry logic for restart scenarios (stale gen maps)

**`internal/workflow/pipeline/launch.go`** (new)
- `LaunchIdempotencyKey(workflowID, nodeID, generation)` — `fmt.Sprintf("%s|%s|%d", ...)`
- `LaunchStatus` type with PENDING, STARTED, COMPLETED, FAILED
- `StageLaunchJob` struct with Key, WorkflowID, NodeID, RunID, AttemptID, Generation, Status
- `LaunchStore` interface: PutIfAbsent, Get, Update, ListByWorkflow
- `MemoryLaunchStore` in-memory implementation

**`internal/workflow/pipeline/fake_launcher.go`** (new)
- `StageLauncher` interface with `EnsureLaunch(ctx, *StageLaunchJob) error`
- `FakeLauncher` marks jobs STARTED, no Docker/moby imports

**`internal/workflow/pipeline/reconciler.go`** (new)
- `Reconciler` struct: Ctrl, Launches, Launcher
- `ReconcileOnce` advances at most one stage:
  1. Recovery path for LAUNCHING nodes (crash between claim and ack)
  2. ClaimNextReady for ready nodes
  3. PutIfAbsent launch job (idempotent)
  4. EnsureLaunch → STARTED
  5. AcknowledgeRunning

**`internal/workflow/pipeline/stage_context.go`** (new)
- `StageContextParams` struct: WorkflowKind, NodeID, StageOrder, IsFinalStage, IncomingHandoffJSON, etc.
- `CollectStageContextParams` loads node list and incoming handoff from store

**`internal/workflow/pipeline/reconciler_test.go`** (new)
- 7 RED GATE tests, all passing with `-race`

**`internal/harness/pipeline_stage_context.go`** (new)
- `PipelineStageContextFromParams` converts params → harness PipelineStageContext
- No import cycle (pipeline doesn't import harness, harness imports pipeline)

**`internal/harness/pipeline_stage_context_test.go`** (new)
- TestPipelineStageContextFromParams: stage0 available=false, mid-stage available=true

**`internal/daemon/pipeline_reconcile.go`** (new)
- `PipelineReconcileHook` with injected Reconcile func
- `Tick` calls reconcile once

**`internal/daemon/pipeline_reconcile_test.go`** (new)
- TestPipelineReconcileHookTick: proves hook delegates correctly

### Commits

```
9dee40a docs: b34-t04 chunk3 handoff record
955aa2b feat(b34-t04): add thin daemon pipeline reconcile hook
cc4ea3c feat(b34-t04): add PipelineStageContextFromParams + test in harness
760b0b5 test(b34-t04): add RED GATE reconciler tests (7 tests)
907ef97 feat(b34-t04): add pipeline reconciler with ReconcileOnce
579cf44 feat(b34-t04): add launch idempotency, stage launcher, and stage context params
```

### Orch re-verification (2026-07-24)

```
$ go test ./internal/workflow/pipeline/ -count=1 -run 'LaunchIdempotency|LaunchStore|Reconcile|CollectStage' -v
PASS (7 tests)

$ go test ./internal/workflow/pipeline/ -race -count=1
ok  pipeline 1.394s

$ go test ./internal/harness/ -count=1 -run 'TestPipelineStageContextFromParams'
ok  harness

$ go test ./internal/daemon/ -count=1 -run 'TestPipelineReconcileHookTick'
ok  daemon
```

### Gaps deferred (chunk4+)
- Daemon.Start does not call PipelineReconcileHook yet (test seam only)
- CollectStageContextParams uses handoff.ContextJSON only, not full envelope
- Harness test is struct-level; no live SetPipelineContext+workflow_input RPC
- PutIfAbsent race path still EnsureLaunch(local job) not existing copy
- LAUNCHING recovery hardcodes launchGen=1
- Crash inject at every CAS write (T04 §Tests)

### Out of scope (not implemented)
- Real container start / T05 isolation
- Pipeline admission (agentpaas_pipeline_not_enabled stays)
- T06 artifacts, T07 full fault matrix, T08 CLI/operator
- Full pause/resume control API
- Python SDK changes
