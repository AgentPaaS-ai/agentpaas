# B34 Multi-Agent Docker e2e — Three-Stage Hermes-Absent

**Date:** 2026-07-26
**Branch:** feat/b34-multiagent-e2e
**Base:** main @ 29872bc

## Summary

Proves the full multi-agent pipeline pattern on real Docker, Hermes-absent:

1 parent workflow, 3 sequential stage agents (separate containers + networks),
handoffs between stages, workflow SUCCEEDED, inspectable evidence, cleanup with
zero orphans.

This closes the B34 T08/T09 residual that the library reference_proof and
single-stage B345 Docker did not close.

## Test

File: `internal/workflow/pipeline/multiagent_docker_e2e_test.go`

`TestB34MultiAgentE2E_ThreeStageHermesAbsent` — gated on `AGENTPAAS_DOCKER_TESTS=1`.

Flow:
1. `runtime.NewDockerRuntime()` → real Docker
2. MemoryStore + Controller + MemoryLaunchStore + `NewRuntimeStageLauncher(dr)` +
   Reconciler with NetworkDriver=dr
3. Seed 3-stage pipeline via `SeedPipelineWorkflow`
4. For stage 0..2:
   - `ReconcileOnce` → claims node, creates network, launches container, acks
   - Assert: claim non-nil, node RUNNING, container ID set, labels
     (workflow_id, node_id, run_id, attempt_id, pipeline_stage, stage_order)
   - Assert: distinct ContainerID and NetworkID from prior stages
   - If stage < 2: `CommitStageSuccess` with handoff envelope
     (ContextJSON: `{"stage":N,"marker":"agent-N"}`)
   - If stage == 2: `CommitStageSuccess` without handoff
   - `FenceStage` after commit (container stopped+removed)
5. Workflow Status == SUCCEEDED
6. `BuildPipelineInspect` → 3 nodes all SUCCEEDED, ordered by stage
7. `ListHandoffs` → exactly 2
8. No duplicate launch keys across stages
9. Defer cleanup: remove networks, assert zero orphan containers/networks
10. Log banner: `B34 MULTI-AGENT E2E PASS hermes-absent containers=3 handoffs=2`

### Image / Command

`alpine:3.20` with `["sleep", "30"]` (default from reconciler `fillStageJobDefaults`)

### Label Keys Observed

- `agentpaas.workflow_id`
- `agentpaas.node_id`
- `agentpaas.run-id`
- `agentpaas.attempt_id`
- `agentpaas.pipeline_stage` = `true`
- `agentpaas.stage_order`

## Makefile

- `block34-multiagent-gate`: runs 3 consecutive passes with `-race`
- `block34-gate` echo updated to mention multiagent gate as separate required-for-close
- `gates` help updated

## Test Execution

### Single run:
```
=== RUN   TestB34MultiAgentE2E_ThreeStageHermesAbsent
    multiagent_docker_e2e_test.go:43: Docker Engine version: 29.5.2
    multiagent_docker_e2e_test.go:66: Workflow: wf-o2oswnczrrxj5skuu265ywevdq  Nodes: [...]
    multiagent_docker_e2e_test.go:105: --- Stage 0 ---
    ...
    multiagent_docker_e2e_test.go:105: --- Stage 1 ---
    ...
    multiagent_docker_e2e_test.go:105: --- Stage 2 ---
    ...
    multiagent_docker_e2e_test.go:257: Workflow status: SUCCEEDED
    multiagent_docker_e2e_test.go:276:   inspect node[0]: id=... stage=0 status=SUCCEEDED outcome=SUCCEEDED
    multiagent_docker_e2e_test.go:276:   inspect node[1]: id=... stage=1 status=SUCCEEDED outcome=SUCCEEDED
    multiagent_docker_e2e_test.go:276:   inspect node[2]: id=... stage=2 status=SUCCEEDED outcome=SUCCEEDED
    multiagent_docker_e2e_test.go:289:   handoff: source=... target=... id=...
    multiagent_docker_e2e_test.go:289:   handoff: source=... target=... id=...
    multiagent_docker_e2e_test.go:294:   launch keys: 3 (no duplicates)
    multiagent_docker_e2e_test.go:317: B34 MULTI-AGENT E2E PASS hermes-absent containers=3 handoffs=2
--- PASS: TestB34MultiAgentE2E_ThreeStageHermesAbsent (36.01s)
```

### 3 consecutive runs (make block34-multiagent-gate):
```
--- multiagent run 1/3 ---
ok   github.com/.../internal/workflow/pipeline  37.512s
--- multiagent run 2/3 ---
ok   github.com/.../internal/workflow/pipeline  37.257s
--- multiagent run 3/3 ---
ok   github.com/.../internal/workflow/pipeline  37.312s
Block 34 multi-agent gate: PASS
```

### Non-Docker tests:
```
go test ./internal/workflow/pipeline/ -count=1 -race
ok   github.com/.../internal/workflow/pipeline  1.457s
```

### Gate regression checks:
```
make block34-gate    → PASS
make block345-gate   → PASS
```

## What Is Proven

- Multi-container multi-agent pipeline runtime with durable handoffs
- 3 sequential stage agents launched in separate containers
- Per-stage network isolation (distinct NetworkIDs)
- Handoff envelopes carrying ContextJSON between stages
- All nodes transition to SUCCEEDED
- BuildPipelineInspect returns correct ordered summary
- Zero orphan containers and networks after cleanup
- Hermes-absent: no Hermes daemon, plugin, or operator required

## What Is NOT Proven (Residual)

- Packed LLM agents (full product pack path)
- Weather/LLM agent functionality
- B35 parent/child spawn
- Pause RPCs in Docker context
- Hermes plugin work
- Live agent execution with real workloads

## Commands

```bash
# Non-docker tests (always green):
go test ./internal/workflow/pipeline/ -count=1 -race

# Docker e2e single run:
AGENTPAAS_DOCKER_TESTS=1 go test ./internal/workflow/pipeline/ -count=1 -race \
  -run 'TestB34MultiAgentE2E_ThreeStageHermesAbsent' -timeout 15m -v

# 3 consecutive runs:
make block34-multiagent-gate

# All B34 gates:
make block34-gate              # library + adversary
make block34-docker-gate       # 2-stage isolation + idempotency
make block34-multiagent-gate   # 3-stage multi-agent e2e (3x)

# All B34.5 gates:
make block345-gate
make block345-docker-gate

# Orphan check:
docker ps -a --filter label=agentpaas.workflow_id
docker network ls --filter label=agentpaas.workflow_id
```
