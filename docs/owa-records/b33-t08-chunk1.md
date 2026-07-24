# B33-T08 Chunk 1 — Handoff

Branch: `feat/b33-t08-mcp-cross-container`
Base: `main` @ `6b2a61a`

## What changed

### `internal/daemon/stub_handlers.go`
- Added `mcpRegistry *mcpmanager.ServiceRegistry` field to `controlServer` (nil = MCP blocked).
- Added `SetMCPServiceRegistry(reg *mcpmanager.ServiceRegistry)` setter.
- Imported `internal/mcpmanager`.

### `internal/daemon/routed_handlers.go`
- `failClosedRoutedRun`: MCP service gate now passes when `mcpRegistry != nil`. When nil, block ref updated B29 → B33.
- `workflowKindNotEnabled`: mcp_service mapping updated B29 → B33.

### `internal/daemon/routed_handlers_test.go`
- `TestWorkflowKindNotEnabledCodes`: added assertion that mcp_service block is B33.
- `TestFailClosedRoutedRun_MCPService_DeniedWithoutRegistry` (new): no registry → FailedPrecondition, code `agentpaas_mcp_service_not_enabled`, mentions B33 not B29, no leaked runs.
- `TestFailClosedRoutedRun_MCPService_AllowedWithRegistry` (new): inject `mcpmanager.NewServiceRegistry(nil, nil, nil)` → err == nil, no leaked runs.
- Imported `internal/mcpmanager`.

## Test results

```
=== RUN   TestWorkflowKindNotEnabledCodes
--- PASS: TestWorkflowKindNotEnabledCodes (0.00s)
=== RUN   TestFailClosedRoutedRun_MCPService_DeniedWithoutRegistry
--- PASS: TestFailClosedRoutedRun_MCPService_DeniedWithoutRegistry (0.03s)
=== RUN   TestFailClosedRoutedRun_MCPService_AllowedWithRegistry
--- PASS: TestFailClosedRoutedRun_MCPService_AllowedWithRegistry (0.02s)
PASS
ok  	github.com/AgentPaaS-ai/agentpaas/internal/daemon	1.534s
```

Full daemon test suite also passing (`go test ./internal/daemon/ -count=1 -race`).

## Next chunk: T08-2

Daemon starts service containers declared in agent config when MCP registry is set. Wire `mcpmanager` service lifecycle, network attach, capability injection. Still no harness sidecar or Docker e2e.
