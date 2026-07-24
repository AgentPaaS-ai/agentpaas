# B33-T08 Chunk 2 — EnsureWorkflowMCPServices (Handoff)

Branch: `feat/b33-t08-mcp-cross-container`
Base: `8396ecc`

## What was done

Added `mcpmanager.ServiceRegistry.EnsureServices()` — idempotent Declare+Start for
workflow service bindings — and a thin daemon `EnsureWorkflowMCPServices` method
that delegates to it.

### Files changed

| File | Change |
|------|--------|
| `internal/mcpmanager/service_registry.go` | `EnsureServices(ctx, workflowID, services)` method: nil-check, empty-early-return, per-binding Declare+Start with rollback on failure |
| `internal/mcpmanager/ensure_services_test.go` | 6 test cases: empty, one binding, two bindings, idempotent, start-failure-clears-prior, unpromoted |
| `internal/mcpmanager/lifecycle_test.go` | Minor additions for EnsureServices coverage |
| `internal/daemon/stub_handlers.go` | `EnsureWorkflowMCPServices(ctx, workflowID, services)` thin delegation to `s.mcpRegistry.EnsureServices()` with nil-registry guard |
| `internal/daemon/ensure_mcp_services_test.go` | Daemon-level tests: nil registry → error, delegates to registry, empty services |
| `internal/daemon/control_handlers_test.go` | Updated for EnsureWorkflowMCPServices presence |

### Behavior

- `EnsureServices` walks bindings in order, calling Declare + Start for each
- Idempotent: if service is already READY, skips (no duplicate containers)
- On failure, best-effort Stops already-started services from this call
- Daemon method returns error if `mcpRegistry` is nil
- RuntimeDriver fakes drive tests — no real Docker

### Verification (all PASS)

```
go test ./internal/mcpmanager/ -count=1 -race
go test ./internal/daemon/ -count=1 -race
golangci-lint run --timeout 5m ./internal/mcpmanager/... ./internal/daemon/...
go build ./...
```

0 lint issues, 0 race issues.

## Next: T08-3 — Harness managed resolver sidecar

Out of scope for chunk 2:
- InvokeDeployment wiring
- Harness managed resolver sidecar
- Real image pull / real Docker
- Evidence file store
