# B34-T04 Chunk 4 — Crash/idempotency matrix + residual fixes

**IMPLEMENT B34-T04 CHUNK 4 NOW. Do not ask questions. Do not claim complete until RED GATE passes.**

**Worktree:** `/Users/pms88/projects/ap-b34-t04`
**Branch:** `feat/b34-t04-scheduler`
**Base HEAD:** run `git rev-parse HEAD | tee /tmp/b34-t04-c4-pre.sha` first
**Spec:** docs/execution/blocks/b34-summary.md § T04 tests 401–411 (library crash/idempotency; no Docker)
**Priors:** c1–c3 on main

## Hard boundaries

IN SCOPE:
1. Fix PutIfAbsent `!created` path to EnsureLaunch(**existing**) not the local job copy
2. AcknowledgeRunning idempotent if node already RUNNING (recovery double-ack)
3. LAUNCHING recovery: use existing job.Generation/Key when found; do not hardcode gen=1 if job exists
4. Crash-inject / fault matrix tests at CAS boundaries (library only)
5. Sixteen-stage deterministic happy path via ReconcileOnce + CommitStageSuccess
6. Concurrent double ReconcileOnce (goroutines) → one attempt, one launch key per stage
7. Live harness RPC: SetInvoke + SetPipelineContext(FromParams) + workflow_input for stage0 and mid-stage
8. CollectStageContextParams: marshal **full handoff envelope** to IncomingHandoffJSON (json.Marshal of HandoffEnvelope), not ContextJSON alone
9. Optional thin: document OR implement daemon tick registration behind not-enabled gate — **prefer library matrix green first**. If wiring Daemon.Start, must remain behind feature flag / not flip admission.

OUT OF SCOPE:
- Real Docker/container launch (T05)
- Removing `agentpaas_pipeline_not_enabled` admission gate
- T06 artifacts mounts, T07 full cancel/MCP fence, T08 CLI, T09 gate Makefile unless tiny stub
- Python SDK changes

## Required code fixes

### reconciler.go PutIfAbsent race
```go
existing, created, err := r.Launches.PutIfAbsent(ctx, job)
...
toLaunch := job
if !created {
  toLaunch = existing
}
if err := r.Launcher.EnsureLaunch(ctx, toLaunch); err != nil { ... }
// update status on toLaunch then Update store
```

### AcknowledgeRunning idempotent
If node.Status == RUNNING already, treat as success (optionally still ensure run/attempt RUNNING). Do not ErrInvalidTransition when already RUNNING for same claim.

### LAUNCHING recovery
When ListByWorkflow finds job for node, use `job.Generation` and `job.Key` for claim fields and launchGen.

### stage_context.go
```go
if node.IncomingHandoffID != nil {
  handoff, err := store.GetHandoff(...)
  b, err := json.Marshal(handoff) // full envelope
  incoming = b
}
```

## Tests to ADD (must PASS with -race)

File: `internal/workflow/pipeline/crash_test.go` and/or extend `reconciler_test.go`

1. `TestCrashAfterClaimBeforeLaunchPut` — claim via Ctrl only (or inject launcher that fails before put by testing sequence): after ClaimNextReady, kill path = new Reconciler same stores → ReconcileOnce must PutIfAbsent + Ensure + Ack without second attempt; still one launch key
2. `TestCrashAfterLaunchBeforeAck` — PutIfAbsent+EnsureLaunch manually with node LAUNCHING, new Reconciler → Ack only, one job
3. `TestDoubleCommitStageSuccessIdempotent` — already exists; keep green
4. `TestConcurrentReconcileOnce` — two goroutines ReconcileOnce same wf after seed; end state: at most one LAUNCHING/RUNNING stage0; exactly one launch job for stage0; at most one attempt for stage0 run
5. `TestSixteenStageDeterministic` — seed 16, loop reconcile+commit with handoffs until workflow SUCCEEDED; 16 launch keys
6. `TestRestartMidLaunchingSharedStores` — after claim+put before ack, new Controller+Reconciler same MemoryStore+LaunchStore → Ack, no duplicate attempt/job
7. `TestCollectStageContextParamsFullEnvelope` — mid-stage IncomingHandoffJSON unmarshals to object with handoff_id / workflow_id fields (not bare context only)

Harness:
8. `TestWorkflowInputRPC_FromPipelineParams` in harness — use existing test helpers (setPipelineContext / SetInvoke patterns from workflow_handoff_rpc_test.go): stage0 available=false; mid-stage with full envelope JSON available=true and handoff present

## Do NOT
- Import docker/moby in pipeline
- One-shot T05–T09
- Vacuous tests that only check string constants

## Docs
- `docs/owa-records/b34-t04-chunk4.md` handoff with SHAs + test names + residual list
- Update `docs/execution/current-state.md` only when red gate green (or leave to orch)

## RED GATE

```bash
cd /Users/pms88/projects/ap-b34-t04
PRE=$(cat /tmp/b34-t04-c4-pre.sha 2>/dev/null || echo 2adceb2)

rg -n "toLaunch|already RUNNING|json.Marshal\\(handoff" internal/workflow/pipeline/ || true
# Prefer: EnsureLaunch uses existing on !created — verify by reading reconciler.go

go test ./internal/workflow/pipeline/ -count=1 -run 'Crash|Concurrent|Sixteen|RestartMid|FullEnvelope|Reconcile' -v
go test ./internal/workflow/pipeline/ -race -count=1
go test ./internal/harness/ -count=1 -run 'WorkflowInputRPC_FromPipeline|PipelineStageContext|workflow_input|WorkflowInput' -v
test -f docs/owa-records/b34-t04-chunk4.md
git log --oneline ${PRE}..HEAD
# must show new commits after PRE
```

Conventional commits `feat(b34-t04):` / `test(b34-t04):` / `fix(b34-t04):`

START NOW. Chunk 4 only. Print WORKER_DONE only after RED GATE green.
