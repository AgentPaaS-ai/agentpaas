# B33-T08 Chunk 4 — Wire EnsureServices + MCP sidecar into client run path

Workdir: `/Users/pms88/projects/ap-b33-t08`
Branch: `feat/b33-t08-mcp-cross-container` @ `ca04fda`

## Goal

On agent container start (both legacy Run and durable InvokeDeployment paths),
if the deployed package has `workflow.yaml` services:

1. Ensure `s.mcpRegistry` is non-nil (lazy-init with real RuntimeDriver from getOrCreateRuntime, always-promoted checker, nil readiness OK for now or always-true probe).
2. `EnsureWorkflowMCPServices(ctx, workflowID, services)`.
3. Write MCP binding sidecar via `mcpRegistry.WriteBindingSidecar`.
4. Bind-mount `:ro` + set `AGENTPAAS_MCP_BINDING_SIDECAR_PATH=/agentpaas/mcp-bindings.json` (mirror delegation pattern).

If no services, no-op (backward compat).

## Implementation notes

### Lazy registry init
```go
func (s *controlServer) ensureMCPRegistry() error {
  if s.mcpRegistry != nil { return nil }
  rt, err := s.getOrCreateRuntime()
  if err != nil { return err }
  // Use rt.Driver() or whatever exposes RuntimeDriver — check DockerRuntime API.
  s.mcpRegistry = mcpmanager.NewServiceRegistry(driver, alwaysPromoted{}, alwaysReady{})
  return nil
}
```
If DockerRuntime doesn't expose driver, use `runtime.NewDockerRuntimeWithDriver` pattern from tests OR store driver on controlServer when creating runtime.

### Load services from deployed dir
```go
func loadWorkflowServices(deployedDir string) (workflowID string, services []pack.ServiceBinding, err error)
```
- Read workflow.yaml if present
- workflowID: use agent name + run id or lock workflow id if present; stable string required. Prefer `runID` as workflowID for isolation per run, OR lock-provided id. **Use runID as workflowID** for per-run service lifecycle isolation.
- services from wf.Services

### writeMCPBindingSidecarForRun(runID, gatewayConfigDir string) (path string, ok bool)
- ensureMCPRegistry
- load services; if empty return "", false
- EnsureWorkflowMCPServices(ctx, runID, services) — if error, log and return "", false OR fail the run? **Fail the run** with Internal if Ensure fails (MCP declared must work).
- WriteBindingSidecar to gatewayConfigDir/mcp-bindings.json
- return path, true

### Wire both paths
After delegation snapshot block (~line 926 and ~1536), add MCP block:
```go
mcpPath, mcpOK := s.prepareMCPBindingsForRun(ctx, runID, deployedDir, gatewayConfigDir)
if mcpOK {
  agentBinds = append(..., mcpPath+":/agentpaas/mcp-bindings.json:ro")
  proxyEnv = append(..., "AGENTPAAS_MCP_BINDING_SIDECAR_PATH=/agentpaas/mcp-bindings.json")
}
```

If services present and prepare fails → return error to caller (don't start agent without services).

### Cleanup
On run finalize/stop, call mcpRegistry.WorkflowTerminal or CleanupServiceResources for runID workflow — best effort in finalizeRun if easy; else TODO comment for chunk 5. Prefer add best-effort `s.cleanupMCPForRun(runID)` in finalizeRun.

## Tests

1. Unit: `loadWorkflowServices` empty / with services
2. Unit: `prepareMCPBindingsForRun` with mock driver registry → file exists, JSON has READY binding
3. Unit: run-path env includes AGENTPAAS_MCP_BINDING_SIDECAR_PATH when services present — if hard to test full startRun, test prepare helper only + a small extracted function that returns binds/env additions
4. Existing daemon tests still pass

## Acceptance (LOCAL ONLY — no GitHub Actions)
```bash
cd /Users/pms88/projects/ap-b33-t08
go test ./internal/daemon/ -count=1 -race -run 'MCP|Ensure|FailClosed|prepareMCP'
go test ./internal/daemon/ -count=1 -race
go test ./internal/mcpmanager/ ./internal/harness/ -count=1 -race
go build ./...
golangci-lint run --timeout 5m ./internal/daemon/... ./internal/mcpmanager/... ./internal/harness/...
```

Commit: `feat(daemon): wire MCP EnsureServices + binding sidecar into run path (B33-T08 c4)`
Handoff: `docs/owa-records/b33-t08-chunk4.md`

No Docker e2e yet (chunk 5). No GH Actions.
