# B34.5-C — Pipeline Product Path Thin Vertical

**Date:** 2026-07-26  
**Branch:** `feat/b345-pipeline` (based on `main`)  
**Scope Lock:** B34.5-C (pipeline product path thin vertical)

## Summary

Connected existing pieces to make admit/invoke register pipeline workflows for
reconciliation, run reconcile with a real launcher when Docker is available,
and fill stage launch metadata (image, command, network).

## Changes

### 1. Pipeline Admission Routing (`routed_handlers.go`)

After `InvokeDeployment` returns ACCEPTED:
- If workflow kind is `pipeline` and `pipelineRuntime` is wired:
  → calls `registerPipelineWorkflow` (registers for reconcile loop)
  → does NOT call `startDurableRun`
- If workflow kind is `standalone` (or pipelineRuntime is nil):
  → calls `startDurableRun` as before (unless `disableContainerLaunch`)
- Pipeline registration runs even when `disableContainerLaunch` is true
  (no Docker required for registration)

New methods on `controlServer`:
- `shouldUsePipelineReconcile(ctx, receipt) bool`
- `registerPipelineWorkflow(ctx, receipt)`

### 2. Daemon Start Launcher Wiring (`server.go`)

When pipeline runtime is enabled:
- If Docker runtime available → `pipeline.NewRuntimeStageLauncher(driver)`
- Otherwise → `FakeLauncher` (unit test fallback)
- `controlServer.pipelineRuntime` set for `InvokeDeployment` callback

### 3. Stage Job Metadata (`reconciler.go`)

New `Reconciler.NetworkDriver` optional field. When non-nil:
- Creates per-stage internal Docker network via driver
- Sets `NetworkID` on `StageLaunchJob`

New `fillStageJobDefaults` method:
- Image: `AGENTPAAS_PIPELINE_STAGE_IMAGE` env var → `alpine:3.20` default
- Command: `["sleep", "30"]` default
- Does NOT overwrite already-set fields (recovery safe)

### 4. Tests

**Unit (daemon):**
- `TestB345_PipelineAdmit_RegistersForReconcile` — admit → register → stage0 RUNNING + launch job exists
- `TestB345_StandaloneAdmit_NotRegisteredForReconcile` — standalone skips pipeline register
- `TestB345_PipelineRuntime_DoubleRegisterIdempotent` — double register safe
- `TestB345_PipelineRuntime_DisabledWithoutEnv` — nil pipelineRuntime path

**Unit (pipeline):**
- `TestB345_FillStageJobDefaults_FillsImageAndCommand` — defaults filled
- `TestB345_FillStageJobDefaults_PreservesExistingImage` — no overwrite
- `TestB345_FillStageJobDefaults_StoresStageOrder` — order preserved
- `TestB345_ReconcileOnce_FillsDefaultsOnJob` — end-to-end fill → launch

**Docker e2e:**
- `TestB345DockerE2E_ReconcileLaunchesStageContainer` — real container with labels, network, Fence

### 5. Makefile

New targets:
- `block345-gate` — extends `block34-gate` with daemon pipeline admission + reconcile tests
- `block345-docker-gate` — Docker e2e with `AGENTPAAS_DOCKER_TESTS=1`

## Acceptance

```bash
# Unit gate:
make block345-gate
# → go test ./internal/daemon/ -run 'Pipeline|B345|Reconcile'
# → go test ./internal/workflow/pipeline/ -run 'B345'

# Build:
go build ./...

# Docker gate (requires Docker):
AGENTPAAS_DOCKER_TESTS=1 make block345-docker-gate
```

## Residuals (documented, NOT blocking)

1. **Default image only:** `alpine:3.20` + `["sleep","30"]` is a dev/test default.
   Production pack path must override via `AGENTPAAS_PIPELINE_STAGE_IMAGE` or
   set real image/command on the `StageLaunchJob` before `ReconcileOnce`.
   Full lock→image digest resolution from registry is not wired.

2. **Network lifecycle:** Per-stage networks are created but not torn down
   automatically when the stage completes. Fence handles cleanup for now.

3. **Multi-agent pack product UX:** Not built. This is the control-plane
   thin vertical. Full three-agent packed images + schema handoff from
   real LLMs is out of scope for B34.5.

## Out of Scope (confirmed)

- Hermes skills / operator UX docs rewrite
- Full three packed agent images + schema handoff
- B35 parent/child
- Pause/resume RPCs
- MCP cancel (other branch)
- Enabling pipeline by default (still env-flagged)
