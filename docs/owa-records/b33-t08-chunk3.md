# B33-T08 Chunk 3 — MCP binding sidecar loads managed resolver into harness

SHA: `b3f7d08`
Branch: `feat/b33-t08-mcp-cross-container`
Repo: `AgentPaaS-ai/agentpaas`

## Summary

Trusted path implemented: daemon writes a JSON sidecar of READY MCP bindings
(endpoint + capability + allowed tools). Harness loads it at startup (before
Python), builds an in-memory ServiceRegistry snapshot, wires
ManagedServiceResolver onto the existing Router, and registers each binding
on the Manager as `transport=agentpaas-service`. Capability never reaches the
Python environment.

## Files

| File | Status | Purpose |
|------|--------|---------|
| `internal/mcpmanager/binding_sidecar.go` | new | Types, read/write/install helpers |
| `internal/mcpmanager/binding_sidecar_test.go` | new | Roundtrip, registry, InstallSidecar tests |
| `internal/harness/server.go` | modified | `SetRouter(router, manager)`, `InstallMCPBindingSidecar` |
| `internal/harness/rpc_server.go` | modified | `harnessRPCServer.SetRouter(router, manager)` |
| `internal/harness/mcp_binding_sidecar_test.go` | new | Harness-level sidecar load tests |
| `internal/harness/rpc_server_mcp_test.go` | modified | Updated all SetRouter call sites |
| `cmd/harness/main.go` | modified | `AGENTPAAS_MCP_BINDING_SIDECAR_PATH` env support |

## Key changes

1. **SetRouter signature changed**: `Server.SetRouter(router, manager)` and
   `harnessRPCServer.SetRouter(router, manager)` — both now accept a Manager
   alongside the Router. The Manager is stored on Server so
   InstallMCPBindingSidecar can register bindings later.

2. **InstallMCPBindingSidecar**: Server method reads the sidecar file, wires
   bindings into Router and Manager via `InstallSidecarOnRouter`, then deletes
   the file (like credentials sidecar pattern).

3. **main.go**: After `server.SetRouter(router, manager)`, checks
   `AGENTPAAS_MCP_BINDING_SIDECAR_PATH` and calls InstallMCPBindingSidecar if set.

## Test results

```
go test ./internal/harness/ -count=1 -race     → ok (19.218s)
go test ./internal/mcpmanager/ -count=1 -race   → ok (6.772s)
go build ./...                                  → ok
```

## Not done (next chunks)

- Daemon-side `writeMCPBindingSidecarForWorkflow` helper (chunk 4)
- InvokeDeployment bind-mount path (chunk 4)
- Docker e2e (chunk 5)
