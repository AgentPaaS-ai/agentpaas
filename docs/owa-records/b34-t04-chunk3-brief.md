# B34-T04 Chunk 3 — Daemon reconcile + fake launch job (NO Docker e2e)

**IMPLEMENT B34-T04 CHUNK 3 NOW. Do not ask questions. Do not claim complete until RED GATE commands pass.**

**Worktree:** `/Users/pms88/projects/ap-b34-t04`
**Branch:** `feat/b34-t04-scheduler`
**Spec:** `docs/execution/blocks/b34-summary.md` § T04 items 2–4, 9 (library + daemon hook; fake launch only)
**Priors:** chunk1 controller CAS DONE; chunk2 pause seam DONE
**HEAD at dispatch:** run `git rev-parse HEAD` and record it; WORKER_DONE requires a **new** commit after that SHA.

## Hard boundaries

IN SCOPE:
1. Stable launch idempotency key = `workflowID|nodeID|generation`
2. Stage launch job record + in-memory launch store (idempotent PutIfAbsent)
3. Fake launcher (no Docker, no moby, no runtime driver)
4. Pipeline Reconciler: claim → ensure launch job → ack running; reconcile LAUNCHING without duplicate launch
5. Build harness `PipelineStageContext` from a claimed/acked stage (helper; call SetPipelineContext in test)
6. Thin daemon hook that can run one reconcile tick (unit-tested; does NOT flip `agentpaas_pipeline_not_enabled` admission)
7. Crash-at-boundary tests for: after claim before launch put; after launch put before ack; double reconcile tick

OUT OF SCOPE (do NOT implement):
- Real container start / T05 isolation
- Enabling pipeline admission (`agentpaas_pipeline_not_enabled` stays)
- T06 artifacts, T07 full fault matrix, T08 CLI/operator
- Full pause/resume control API (pause seam already in controller)
- Python SDK changes
- One-shot entire remaining T04

## Package layout (preferred)

```
internal/workflow/pipeline/
  launch.go           // LaunchIdempotencyKey, StageLaunchJob, LaunchStore, MemoryLaunchStore
  fake_launcher.go    // FakeLauncher implementing StageLauncher
  reconciler.go       // Reconciler
  reconciler_test.go  // RED GATE tests
  stage_context.go    // plain StageContextParams (no harness import)

internal/harness/
  pipeline_stage_context.go  // BuildPipelineStageContext(params) PipelineStageContext
                             // OR extend workflow_handoff.go — avoid import cycles
                             // pipeline must NOT import harness (harness already imports pipeline)

internal/daemon/
  pipeline_reconcile.go      // thin wrapper type PipelineReconcileHook{...}
  pipeline_reconcile_test.go // unit test: hook.Tick calls reconciler once (fake deps)
```

If a file name collides, adapt — keep responsibilities.

## API contract (implement exactly enough to pass tests)

```go
// launch.go
func LaunchIdempotencyKey(workflowID routedrun.WorkflowID, nodeID routedrun.NodeID, generation int64) string
// MUST format: fmt.Sprintf("%s|%s|%d", workflowID, nodeID, generation)
// generation is the node CAS generation used at claim (the expectedGeneration passed to UpdateNode READY→LAUNCHING)

type LaunchStatus string
const (
  LaunchStatusPending   LaunchStatus = "PENDING"
  LaunchStatusStarted   LaunchStatus = "STARTED"
  LaunchStatusCompleted LaunchStatus = "COMPLETED"
  LaunchStatusFailed    LaunchStatus = "FAILED"
)

type StageLaunchJob struct {
  Key          string // LaunchIdempotencyKey
  WorkflowID   routedrun.WorkflowID
  NodeID       routedrun.NodeID
  RunID        routedrun.RunID
  AttemptID    routedrun.AttemptID
  Generation   int64
  Status       LaunchStatus
  CreatedAt    time.Time
  UpdatedAt    time.Time
}

type LaunchStore interface {
  // PutIfAbsent returns existing job if key present (created=false), else stores clone (created=true).
  PutIfAbsent(ctx context.Context, job *StageLaunchJob) (existing *StageLaunchJob, created bool, err error)
  Get(ctx context.Context, key string) (*StageLaunchJob, error)
  Update(ctx context.Context, job *StageLaunchJob) error
}

type StageLauncher interface {
  // EnsureLaunch is idempotent for job.Key. Fake: mark STARTED, no Docker.
  EnsureLaunch(ctx context.Context, job *StageLaunchJob) error
}

// Claim should expose LaunchGeneration used for the key.
// Extend Claim:
type Claim struct {
  ...
  LaunchGeneration int64  // node gen used at claim
  LaunchKey        string // LaunchIdempotencyKey(...)
}
```

**Controller change (required):**
- In `ClaimNextReady`, after successful node CAS READY→LAUNCHING, set:
  - `claim.LaunchGeneration = nodeGen` (the expectedGeneration used in UpdateNode)
  - `claim.LaunchKey = LaunchIdempotencyKey(workflowID, nodeID, nodeGen)`
- Do not create Docker resources.

```go
// reconciler.go
type Reconciler struct {
  Ctrl     *Controller
  Launches LaunchStore
  Launcher StageLauncher
}

// ReconcileOnce advances at most one stage claim/launch/ack for the workflow.
// Behavior:
// 1. List nodes. If any node is LAUNCHING:
//    a. Find attempt for that node's run (latest).
//    b. Build key = LaunchIdempotencyKey(wf, node, launchGen).
//       Launch gen recovery: if claim map lost, use controller.nodeGen[node]-1 if >0, else 1.
//       Prefer storing LaunchKey on a side table via Launches: scan is OK for memory store OR
//       store key on Claim only and require Launches lookup by scanning jobs for matching node LAUNCHING.
//       SIMPLER REQUIRED APPROACH: MemoryLaunchStore.FindByNode(workflowID, nodeID) optional method
//       OR encode key only via Put at claim time in ReconcileOnce path below.
//    c. PutIfAbsent launch job if missing; EnsureLaunch; if node still LAUNCHING → AcknowledgeRunning.
//    d. Return claim-like result without double-creating attempts.
// 2. Else call ClaimNextReady. If nil claim, return nil,nil.
// 3. PutIfAbsent StageLaunchJob{Key: claim.LaunchKey, ..., Status: PENDING}
// 4. Launcher.EnsureLaunch → STARTED
// 5. Ctrl.AcknowledgeRunning(claim)
// 6. Return claim
//
// MUST be safe to call twice concurrently-ish: second tick must not create second attempt
// for same stage (double Claim already covered; launch PutIfAbsent covers launch dup).
func (r *Reconciler) ReconcileOnce(ctx context.Context, workflowID routedrun.WorkflowID) (*Claim, error)

// DriveToRunning is optional test helper: ReconcileOnce until claim non-nil or nothing ready.
```

**LAUNCHING recovery without lost in-memory gen maps (required for restart test):**

Controller loses `nodeGen` maps on process restart. MemoryStore still has node status LAUNCHING.

Add to Controller OR Reconciler:
```go
// RehydrateGenerations walks store nodes/runs/attempts and sets controller gen maps
// to the store's current generation if available; else if MemoryStore doesn't expose gen,
// set nodeGen[id]=2 after successful claim pattern: for LAUNCHING nodes set nodeGen=2
// (seed was 1, claim bumped to 2). Document the convention matching existing controller_test.
func (c *Controller) RehydrateFromStore(ctx, workflowID) error
```
Look at how MemoryStore tracks generations — if GetNode doesn't return generation, keep the same convention as existing `TestRestartSimulation` and document it. Launch key for a LAUNCHING node must still be deterministic: use generation **1** as the claim generation for first launch (the expectedGeneration at first READY→LAUNCHING). On seed, node gen starts at 1; claim uses expectedGen=1. So LaunchKey always uses that claim expectedGen. Store the key on StageLaunchJob at first Put; on restart Find job by listing all launches for workflow+node.

Add:
```go
func (s *MemoryLaunchStore) ListByWorkflow(ctx, wfID) ([]*StageLaunchJob, error)
```

## Stage context for harness (no import cycle)

```go
// pipeline/stage_context.go
type StageContextParams struct {
  WorkflowKind        string // "pipeline"
  NodeID              string
  StageOrder          int
  IsFinalStage        bool
  IncomingHandoffJSON json.RawMessage // nil/empty for stage 0
  LeaseExpiresAt      time.Time
  LeaseGeneration     int64
  Classification      string
  ToNodeID            string
}

// CollectStageContextParams loads node list + incoming handoff from store for a claim.
func CollectStageContextParams(ctx context.Context, store PipelineStore, claim *Claim) (StageContextParams, error)
```

```go
// harness — convert params → PipelineStageContext + SetPipelineContext
func PipelineStageContextFromParams(p pipeline.StageContextParams) PipelineStageContext
```

Test in harness: SetInvoke + SetPipelineContext(FromParams(...)) + workflow_input RPC returns available=false for stage 0.

## Daemon hook (thin)

```go
// internal/daemon/pipeline_reconcile.go
// Do NOT enable admission. Do NOT start containers.

type PipelineReconcileHook struct {
  Reconcile func(ctx context.Context, workflowID string) error // injected
}

// Tick is a single reconcile invocation for tests / future daemon loop.
func (h *PipelineReconcileHook) Tick(ctx context.Context, workflowID string) error
```

Unit test injects a counter/func — prove Tick calls it. Optional: integration-style test in pipeline package is enough if daemon test is trivial.

## Tests (RED GATE — must exist and PASS)

### pipeline package

1. `TestLaunchIdempotencyKeyFormat` — exact `wf|node|gen`
2. `TestMemoryLaunchStorePutIfAbsent` — second put returns existing, created=false
3. `TestReconcileOnceTwoStageFake` — seed 2-stage → ReconcileOnce → stage0 RUNNING + launch STARTED → CommitStageSuccess+handoff → ReconcileOnce → stage1 RUNNING → final success → workflow SUCCEEDED; **exactly 2 launch keys** ever created
4. `TestReconcileOnceIdempotentWhileLaunching` — after first ReconcileOnce leaves RUNNING, second ReconcileOnce returns nil (nothing READY) and launch count unchanged; separately: interrupt after PutIfAbsent before Ack by using a launcher that records but test double-Reconcile on LAUNCHING node creates **one** job only
5. `TestReconcileRestartNoDuplicateLaunch` — complete stage0; new Controller+Reconciler sharing same MemoryStore + MemoryLaunchStore; ReconcileOnce must claim stage1 only, not re-launch stage0; launch store still has stage0 key once
6. `TestReconcileRespectsPause` — set DesiredState pause before claim; ReconcileOnce returns nil; no launch jobs
7. `TestCollectStageContextParamsStage0AndMid` — stage0 empty handoff; after stage0 success, stage1 params include handoff JSON and StageOrder=1

### harness package

8. `TestSetPipelineContextFromReconcileParams` — stage0 context → workflow_input available=false; mid-stage with handoff JSON → available=true

### daemon package

9. `TestPipelineReconcileHookTick` — Tick invokes injected reconcile once

Race: `go test -race ./internal/workflow/pipeline/... ./internal/harness/... -count=1` (daemon test file only is OK without full daemon race if slow — at least `go test ./internal/daemon/ -run PipelineReconcile -count=1`)

## Implementation notes

- Read existing: `controller.go`, `controller_test.go`, `internal/harness/workflow_handoff.go`, `routedrun.MemoryStore`
- Reuse `SeedPipelineWorkflow`, handoff builders from controller_test
- FakeLauncher must not import docker/moby
- Conventional commits: `feat(b34-t04): ...`
- Update `docs/owa-records/b34-t04-chunk3.md` handoff with commit SHA + test names
- Update `docs/execution/current-state.md` only if you finish fully (orch may do this)
- Keep `make lint` clean on touched packages if feasible

## RED GATE (orch will re-run; do not claim complete if any fail)

```bash
cd /Users/pms88/projects/ap-b34-t04
PRE=$(cat /tmp/b34-t04-c3-pre.sha)  # orch writes this; or: git rev-parse HEAD before you started

# symbols exist
rg -n "LaunchIdempotencyKey|ReconcileOnce|StageLaunchJob|CollectStageContextParams" internal/workflow/pipeline/
rg -n "PipelineStageContextFromParams|SetPipelineContext" internal/harness/
rg -n "PipelineReconcileHook" internal/daemon/

# tests
go test ./internal/workflow/pipeline/ -count=1 -run 'LaunchIdempotency|LaunchStore|Reconcile|CollectStage'
go test ./internal/workflow/pipeline/ -race -count=1
go test ./internal/harness/ -count=1 -run 'PipelineContext|workflow_input|WorkflowInput|SetPipeline'
go test ./internal/daemon/ -count=1 -run 'PipelineReconcile'

# new commits
git rev-parse HEAD
git log --oneline ${PRE}..HEAD
test -f docs/owa-records/b34-t04-chunk3.md
```

If PRE unknown: `git log --oneline -5` must show new feat commits after `357947a`.

## Done definition

- All RED GATE commands exit 0
- No Docker usage
- Admission still not enabled (do not edit routed_handlers pipeline_not_enabled to allow)
- Handoff record written
- Print WORKER_DONE only after the above

START NOW. Chunk 3 only.
