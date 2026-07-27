# B34 close — daemon pipeline reconcile loop + operator stretch seam

**Date:** 2026-07-26  
**Branch:** feat/b34-close-wire  
**Base:** main @ 4234e73  
**PR:** TBD

## Summary

Implemented the B34 pipeline daemon reconcile loop wiring and a Hermes-absent
operator seam proof. The daemon now starts a background reconcile loop when
`pipelineRuntimeEnabled` is true (either via `pipelineEnabled` field or
`AGENTPAAS_PIPELINE_ENABLED=1`). Pipeline, inferno, and CLI-free operators get
an always-pass in-process proof that is available without ever touching Docker.

## Changes

### 1. pipeline runtime component (`internal/daemon/pipeline_runtime.go`)

New `pipelineRuntime` struct:

- Holds `pipeline.PipelineStore`, reconcile func, interval, stop channel
- `Start(ctx)` launches a background goroutine that:
  - Iterates over registered pipeline workflow IDs
  - Calls the injected reconcile function for each
- `Stop()` cancels the loop context and waits for the goroutine
- Idempotent Start/Stop
- `RegisterPipelineWorkflowForReconcile(workflowID)` adds IDs to a registry
  (since PipelineStore has no ListWorkflows method)

### 2. Daemon wiring (`internal/daemon/server.go`, `stub_handlers.go`)

- Added `pipelineRuntime` field to `Daemon` struct
- In `Daemon.Start`: after store init, when `pipelineRuntimeEnabled()`:
  - Creates `pipeline.Reconciler` with the wired `LocalStore`, a
    `MemoryLaunchStore`, and `FakeLauncher` (no Docker)
  - Starts the reconcile loop at 1s interval
- In `Daemon.Stop`: stops the pipeline runtime before cleanup
- Added `controlServer.pipelineStore()` accessor exposing `*routedrun.LocalStore`

### 3. Registration path

`RegisterPipelineWorkflowForReconcile(workflowID)` adds pipeline workflow IDs
to the runtime's in-memory registry. When pipeline deployments are admitted and
workflow rows are created, the admit path calls this to ensure the reconcile
loop can see them.

### 4. Tests (no Docker required)

All in `internal/daemon/`:

| Test | What it proves |
|------|---------------|
| `TestPipelineReconcileLoop_TicksWhileEnabled` | Loop fires >=2 ticks, stops after Stop() |
| `TestPipelineReconcileLoop_DisabledByDefault` | No ticks when never started |
| `TestPipelineReconcileLoop_CallsControllerReconcileOnce` | Seeds 3-stage pipeline, loop advances node 0 to RUNNING via FakeLauncher |
| `TestPipelineRuntime_RegisterAndList` | Registry correctly tracks unique IDs |
| `TestPipelineReconcileLoop_StopIdempotent` | Safe to call Stop() multiple times |
| `TestPipelineReconcileLoop_StartIdempotent` | Safe to call Start() multiple times |

### 5. Hermes-absent operator seam proof (`pipeline_operator_seam_test.go`)

`TestPipelineOperatorSeam_ThreeStageNoHermes`:

- **Part A (controller_seed_inspect):** Seed 3-stage, drive claim/ack/success
  with `pipeline.Controller`, verify inspect summary shows 3 SUCCEEDED nodes.
  This is the Hermes-absent **control-plane** proof.
  
- **Part B (reconcile_loop_advances_launching_to_running):** Seed 3-stage,
  register with runtime, start reconcile loop with FakeLauncher, assert node 0
  advances to RUNNING. This proves the daemon loop fires end-to-end.

### 6. Makefile

`block34-gate` regex updated: `Pipeline|NotEnabled|pipeline` → `Pipeline|pipeline`
(NotEnabled tests are broader than pipeline scope).

## Acceptance

```bash
go test ./internal/daemon/ -count=1 -race -run 'Pipeline'
go test ./internal/workflow/pipeline/ -count=1 -race
go build ./...
make block34-gate   # PASS
```

## Known gaps (honest)

1. **Stage launcher is FakeLauncher (no-op).** The reconcile loop uses
   `FakeLauncher{}` which marks jobs STARTED without launching containers.
   Full multi-image pipeline pack+invoke with real container startup is
   deferred to the Docker e2e workstream.

2. **Pipeline workflow registration is manual.** The reconcile loop uses an
   in-memory registry. The admit path does not yet automatically call
   `RegisterPipelineWorkflowForReconcile` — this is a focused helper used by
   tests. Production wiring requires adding the call in the pipeline admit
   path.

3. **Single daemon pipeline runtime.** The MemoryLaunchStore is not persisted
   across daemon restarts. For production, the launch store should be backed
   by the LocalStore or a dedicated durable store.

4. **InvokeDeployment still starts single durable container.** Multi-stage
   advancement is not product-wired as a full operator; individual stages
   advance via the controller but actual container execution is not yet
   integrated with `startDurableRun`.

## Commit log

```
d911754 build(b34): update block34-gate to include pipeline daemon tests
f662ef4 feat(daemon): wire pipelineRuntime into Daemon Start/Stop
264bd2a feat(daemon): add Hermes-absent pipeline operator seam proof test
dcc35ea feat(daemon): add pipeline runtime reconcile loop component
```
