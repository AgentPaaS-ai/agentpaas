# B33-T08 Chunk 1 — Enable MCP service path (daemon gate + registry hook)

Workdir: `/Users/pms88/projects/ap-b33-t08`
Branch: `feat/b33-t08-mcp-cross-container` (already checked out from main@6b2a61a)

## Goal (this chunk ONLY)

Unblock MCP service workflows at the daemon fail-closed gate and install a
daemon-owned `ServiceRegistry` hook. Do **not** start containers, do **not**
wire harness sidecar, do **not** write Docker e2e yet.

## Spec refs

- `docs/execution/blocks/b33-summary.md` T08
- T05 R1: daemon does not inject live ServiceRegistry (this chunk starts that)
- `internal/daemon/routed_handlers.go` `failClosedRoutedRun` lines ~954-956 currently:
  ```
  case sig != nil && sig.HasMCPService:
      return notEnabledFailedPrecondition("mcp_service", "B29", "agentpaas_mcp_service_not_enabled")
  ```

## Required implementation

### 1. controlServer field + setter
- Add `mcpRegistry *mcpmanager.ServiceRegistry` (or interface) on `controlServer` in daemon.
- `SetMCPServiceRegistry(reg *mcpmanager.ServiceRegistry)` method (thread-safe if server has mu; match existing patterns).
- Optionally construct a default empty registry at server init if a RuntimeDriver is already available — only if trivial; otherwise tests inject via setter.

### 2. failClosedRoutedRun behavior change
- When `sig.HasMCPService`:
  - If `s.mcpRegistry != nil` → **allow** (return nil for this case; fall through past MCP case). Feature is enabled for B33.
  - If `s.mcpRegistry == nil` → still fail closed with code `agentpaas_mcp_service_not_enabled` but block ref **B33** (not B29).
- Do not change pipeline/child_spawn/routed_run fail-closed behavior.

### 3. workflowKindNotEnabled
- Update mcp_service mapping if it still says B29 → B33 for consistency.
- Tests in `TestWorkflowKindNotEnabledCodes` that assert B29 may need update to B33.

### 4. Tests first / with implementation
Add to `internal/daemon/routed_handlers_test.go` (or new `mcp_service_enable_test.go`):

- `TestFailClosedRoutedRun_MCPService_DeniedWithoutRegistry`
  - HasMCPService=true, no registry → FailedPrecondition, code contains `agentpaas_mcp_service_not_enabled`, message mentions B33 not B29.

- `TestFailClosedRoutedRun_MCPService_AllowedWithRegistry`
  - Inject `mcpmanager.NewServiceRegistry(nil, nil, nil)` via setter
  - HasMCPService=true, HasRoute=false, standalone → err == nil (or only non-MCP failures absent)
  - Ensure no Docker resources created.

- Update any existing test that hard-codes B29 for mcp_service.

### 5. Characterization / compat
If `test/compat` asserts mcp_service not enabled at B29, update to B33 or "enabled when registry set". Grep for `mcp_service_not_enabled` and `B29` near MCP.

## Out of scope (later chunks)
- Declaring/starting service containers
- Network attach / capability injection into harness
- CLI e2e / Docker
- Makefile mcp-container-e2e

## Acceptance
```bash
cd /Users/pms88/projects/ap-b33-t08
go test ./internal/daemon/ -count=1 -race -run 'FailClosed|WorkflowKind|MCPService'
go test ./internal/daemon/ -count=1 -race
go test ./internal/mcpmanager/ -count=1 -race
go build ./...
golangci-lint run --timeout 5m ./internal/daemon/...
```
All PASS. Commit with conventional message on branch. Do not merge.

Write short handoff note: `docs/owa-records/b33-t08-chunk1.md`

Return: commit SHA, files changed, PASS lines.
