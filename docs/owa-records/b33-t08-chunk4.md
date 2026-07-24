# B33-T08 Chunk 4 — Handoff

Commit: `ec6417a feat(daemon): wire MCP EnsureServices + binding sidecar into both run paths (B33-T08 c4)`

Branch: `feat/b33-t08-mcp-cross-container`
PR: https://github.com/AgentPaaS-ai/agentpaas/pull/188

## What was implemented

Wired `EnsureWorkflowMCPServices` and MCP binding sidecar into both agent run paths
(legacy `Run()` and durable `startDurableRun()`).

### New file: `internal/daemon/mcp_run.go`

- `alwaysPromotedChecker` — always-promoted stub for PromotionChecker
- `alwaysReadyProbe` — always-ready stub for ReadinessProbe
- `ensureMCPRegistry()` — lazy-init `ServiceRegistry` using DockerRuntime as driver
- `loadWorkflowServices(deployedDir)` — parse workflow.yaml, return service bindings
- `prepareMCPBindingsForRun(ctx, runID, deployedDir, gatewayConfigDir)` — ensure services + write sidecar
- `cleanupMCPForRun(runID)` — best-effort `WorkflowTerminal` teardown

### Modified: `internal/daemon/control_handlers.go`

1. **`Run()` path** (~line 932): after delegation snapshot, call `prepareMCPBindingsForRun`.
   If services are declared but provisioning fails → cleanup gateway + networks + journal key → return `Internal` error.
   If services OK → bind-mount `mcp-bindings.json:ro` + set `AGENTPAAS_MCP_BINDING_SIDECAR_PATH`.

2. **`startDurableRun()` path** (~line 1562): same MCP block, with audit trail on failure.

3. **`finalizeRun()`** (~line 1758): call `cleanupMCPForRun(runID)` as best-effort step 1c.

### New file: `internal/daemon/mcp_run_test.go`

11 tests — all pass:
- `TestLoadWorkflowServices_NoWorkflowYAML` — no workflow.yaml → nil services
- `TestLoadWorkflowServices_EmptyServices` — workflow.yaml with no services → nil
- `TestLoadWorkflowServices_WithServices` — 2 services parsed correctly
- `TestLoadWorkflowServices_InvalidYAML` — parse error surfaced
- `TestPrepareMCPBindingsForRun_NoServices` — ok=false when no services declared
- `TestPrepareMCPBindingsForRun_WithServices` — sidecar written, READY bindings present
- `TestPrepareMCPBindingsForRun_EnsureFails` — error when mock driver fails create
- `TestPrepareMCPBindingsEnvAddition` — env var constant check
- `TestPrepareMCPBindings_RunPathIntegration` — bind mount + env var construction
- `TestCleanupMCPForRun_NilRegistry` — no-op when registry nil
- `TestCleanupMCPForRun_WithRegistry` — service transitions to STOPPED after cleanup

## Acceptance

```
cd /Users/pms88/projects/ap-b33-t08
go test ./internal/daemon/ -count=1 -race          # PASS (70s)
go test ./internal/mcpmanager/ -count=1 -race      # PASS (6.6s)
go test ./internal/harness/ -count=1 -race          # PASS (19s)
go build ./...                                       # OK
golangci-lint run --timeout 5m ./internal/daemon/... ./internal/mcpmanager/... ./internal/harness/...  # 0 issues
```

## Git log

```
ec6417a feat(daemon): wire MCP EnsureServices + binding sidecar into both run paths (B33-T08 c4)
83749ea feat(daemon): add MCP run helpers — EnsureServices, binding sidecar, cleanup (B33-T08 c4)
ca04fda docs: B33-T08 chunk3 handoff
```

## Design decisions

1. **runID as workflowID** — per-run service lifecycle isolation (brief requirement).
2. **Fail the run** if MCP services are declared but EnsureServices fails — MCP declared must work.
3. **always-promoted checker + always-ready probe** — promotion/readiness gates deferred to chunk 5.
4. **DockerRuntime as RuntimeDriver** — `DockerRuntime` already implements `RuntimeDriver`, no extra `Driver()` method needed.
5. **Cleanup in finalizeRun** — `sync.Once` guarantees best-effort cleanup exactly once per run.

## No: Docker e2e, GH Actions, schema validation
