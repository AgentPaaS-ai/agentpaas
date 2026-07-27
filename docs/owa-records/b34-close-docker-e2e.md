# B34 Close — Docker multi-container stage isolation e2e

**Date:** 2026-07-26
**Branch:** feat/b34-close-docker
**Base:** main @ 4234e73

## Summary

Live Docker e2e tests prove multi-container stage isolation via
`RuntimeStageLauncher` + `runtime.NewDockerRuntime()`.

## Tests Added

File: `internal/workflow/pipeline/stage_docker_e2e_test.go`

1. **TestDockerE2E_TwoStageIsolation_SeparateContainersAndNetworks**
   - Creates two distinct internal networks (net0, net1)
   - Launches stage0 on net0, asserts running + labels present (no secrets)
   - FenceStage stage0, confirms container removed
   - Launches stage1 on net1, asserts running + on correct network
   - Confirms distinct ContainerIDs and NetworkIDs
   - Cleanup leaves zero orphans

2. **TestDockerE2E_FenceStage_IdempotentAndNoOrphans**
   - Launch one stage, Fence twice
   - List by workflow label → empty after fence

3. **TestDockerE2E_EnsureLaunch_Idempotent**
   - Same job key twice → exactly one container

## Production Changes

- `internal/workflow/pipeline/stage_spec.go`: Added `Command []string` to `StageLaunchRequest`, wired through `BuildStageContainerSpec`
- `internal/workflow/pipeline/launch.go`: Added `Command []string` to `StageLaunchJob`
- `internal/workflow/pipeline/stage_launcher.go`: Wired `job.Command` into the `StageLaunchRequest`

## Makefile

- `block34-gate`: updated echo to mention separate docker gate
- `block34-docker-gate`: new target running `DockerE2E` tests with `AGENTPAAS_DOCKER_TESTS=1`

## Test Execution

### Run 1 (first):
```
=== RUN   TestDockerE2E_TwoStageIsolation_SeparateContainersAndNetworks
    stage_docker_e2e_test.go:324: SUCCESS: two-stage Docker isolation with distinct containers and networks
--- PASS: TestDockerE2E_TwoStageIsolation_SeparateContainersAndNetworks (26.68s)
=== RUN   TestDockerE2E_FenceStage_IdempotentAndNoOrphans
    stage_docker_e2e_test.go:434: SUCCESS: FenceStage idempotent, zero orphans
--- PASS: TestDockerE2E_FenceStage_IdempotentAndNoOrphans (13.32s)
=== RUN   TestDockerE2E_EnsureLaunch_Idempotent
    stage_docker_e2e_test.go:560: SUCCESS: EnsureLaunch idempotent — same key, one container
--- PASS: TestDockerE2E_EnsureLaunch_Idempotent (13.32s)
PASS
ok  	github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline	53.599s
```

### Run 2 (stability):
```
ok  	github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline	52.547s
```

### Run 3 (make block34-docker-gate):
```
ok  	github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline	54.754s
```

### Orphan check (after all runs):
```
Orphans: 0 containers, 0 networks
```

## Image Used

- `alpine:3.20` with `Command: ["sleep", "120"]`

## Label Keys Observed

- `agentpaas.managed-by` = `agentpaas`
- `agentpaas.resource-type` = `agent`
- `agentpaas.run-id`
- `agentpaas.workflow_id`
- `agentpaas.node_id`
- `agentpaas.attempt_id`
- `agentpaas.pipeline_stage` = `true`
- `agentpaas.stage_order`

## Commands

```bash
# Non-docker tests (always green):
go test ./internal/workflow/pipeline/ -count=1 -race

# Docker e2e tests:
AGENTPAAS_DOCKER_TESTS=1 go test ./internal/workflow/pipeline/ -count=1 -race -run 'DockerE2E' -timeout 10m

# Via Makefile:
make block34-docker-gate

# Orphan check:
docker ps -a --filter label=agentpaas.workflow_id
docker network ls --filter label=agentpaas.workflow_id
```
