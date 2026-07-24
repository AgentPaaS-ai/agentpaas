# OWA Record — B33-T01 MCP Baseline Freeze

**Date:** 2026-07-23
**Commit:** b36101f (branch: feat/b33-t01-mcp-gap-freeze)
**Block:** B33 (AgentPaaS-Container MCP Services)
**Next task unblocked:** T02 (Implement MCP service package and SDK runner)

## Component reuse/replace decisions

| Component | Decision | Rationale |
|---|---|---|
| `policy.MCPServer` | **REUSE** | Struct with Name, Transport, Command, Args, Endpoint, AllowedTools, HostAffecting is correct for v0.4 service packages. T02 adds the `kind: mcp_service` validation bridge. |
| `mcpmanager.Manager` | **REUSE** | In-memory allowlist/confirmation store is the right shape for the runtime directory. T03 adds registry-backed generation tracking. |
| `mcpmanager.Lifecycle` | **EVOLVE** | Stdio child-process management is sound. HTTP sidecar mode exists but is not production-wired to a Docker runtime driver. T03 adds durable service run/container/lease model. T06 adds lease-fencing cancellation. |
| `mcpmanager.Router` | **EVOLVE** | JSON-RPC dispatch (stdio/HTTP) and audit helpers work correctly. T05 installs the router in production harness setup. T06 replaces the fixed 5s stdio timeout with the B30 operation deadline. |
| `harness.handleMCP` (rpc_server.go:941) | **EVOLVE** | Three-branch structure (router→call, allowlist→deny, no-router→fail-closed-or-fake) is correct. T05 adds real router installation + lease metadata. T06 adds call deadline from B30. |
| `agent.mcp(server_id, tool, input)` (Python SDK) | **REUSE** | Signature and RPC wire format are correct. T02 adds declarative `mcp_bindings` validation. No SDK changes required for this block beyond binding mapping. |
| `mcpmanager/audit.go` helpers | **REUSE** | AuditToolDenied/AuditToolCall provide bounded redacted metadata without raw bodies. T07 ensures one audit record per call path. |
| `daemon routed_handlers.go` not-enabled path | **REUSE** | Returns typed `agentpaas_mcp_service_not_enabled` error (B26-reserved). T05 removes this guard for managed services once the router is installed. |
| `internal/daemon/` production package | **KEEP ABSENT** | No daemon file imports mcpmanager (verified by TestCharacterization_MCPManagerExistsButDaemonDoesNotUseIt). T05 adds the controlled production import + router installation. |

## Production wiring gap evidence

The production daemon (`internal/daemon/`) does **not** import `mcpmanager` and does **not** call `SetRouter`. Evidence:

- `TestCharacterization_MCPManagerExistsButDaemonDoesNotUseIt` (test/compat/v0.2.3/v023_test.go:779) — source-scans all `.go` files in `internal/daemon/` and fails if any import `"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"`. Currently passes.
- `TestCharacterization_MCPRouterNotInstalled` (v023_test.go:506) — documents that `harnessRPCServer.router` field (rpc_server.go:38) defaults to nil, and `SetRouter` (rpc_server.go:318) is only called from test files.
- `SetRouter` call sites: only in `internal/harness/rpc_server_mcp_test.go:229` (test code).
- `NewRouter` is never called from production packages; only used in test helpers and the mcpmanager package's own tests.

## Synthetic path confinement evidence

The synthetic `{ok: true}` result in `handleMCP` (rpc_server.go:987–999) is **strictly gated** behind `os.Getenv("AGENTPAAS_TEST_FAKE_MCP") == "1"`. Evidence:

- `TestMCP_SyntheticPayloadStringAbsentInProduction` (rpc_server_mcp_test.go:267) — source-scans rpc_server.go and asserts the `map[string]any{"ok": true}` construction appears **after** the `AGENTPAAS_TEST_FAKE_MCP` check, not before it.
- `TestMCP_NoRouter_FailsClosedInProduction` (rpc_server_mcp_test.go:52) — with flag unset, call fails with `agentpaas_mcp_service_not_enabled` code, result is nil, audit records `mcp_denied`.
- `TestMCP_NoRouter_FakeModeReturnsSynthetic` (rpc_server_mcp_test.go:113) — with flag set, call returns synthetic `{ok: true}`, audit records `mcp_call`.
- `TestMCP_NoRouter_UndeclaredToolStillDenied` (rpc_server_mcp_test.go:168) — even in fake mode, undeclared server/tool is denied with `mcp_denied` before reaching the synthetic path.

## Timeout inventory

| Location | Constant/Value | Notes |
|---|---|---|
| `internal/mcpmanager/router.go:25` | `stdioResponseTimeout = 5 * time.Second` | Fixed 5s — T05 replaces with B30 operation deadline. Frozen by `TestCharacterization_MCPTimeoutInventory`. |
| `internal/mcpmanager/lifecycle.go:333` | `time.After(5 * time.Second)` | CheckReadiness poll interval — not a total timeout but limits readiness check frequency. T03 adds full readiness model. |
| `internal/mcpmanager/router.go:24` | `maxBodySize = 1 << 20` (1 MiB) | Response body size limit. T06 enforces request + response bounds. |
| HTTP client | `http.DefaultClient` (no timeout) | `NewRouter` receives an `HTTPDoer`; test code passes `http.DefaultClient` which has no transport timeout. T05/T06 must add explicit deadline propagation. |
| Daemon invoke timeout | 2 minutes | In `internal/daemon/control_handlers.go:794` — not MCP-specific but caps any single invoke including potential future MCP calls. Documented in `TestCharacterization_TimeoutConflict`. |

## Baseline test results (2026-07-23)

All tests PASS on macOS 26.5.1, Go 1.26.5.

### Go: mcpmanager + policy + harness
```
$ go test ./internal/mcpmanager/... ./internal/policy/... ./internal/harness/ -count=1
ok  	github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager	1.956s
ok  	github.com/AgentPaaS-ai/agentpaas/internal/policy	0.397s
ok  	github.com/AgentPaaS-ai/agentpaas/internal/harness	19.641s
```

### Python SDK
```
$ PYTHONPATH=python python3 -m unittest discover -s python/agentpaas_sdk/tests -v
Ran 115 tests in 0.004s
OK
```

### Compatibility characterization
```
$ go test ./test/compat/v0.2.3/... -count=1
ok  	github.com/AgentPaaS-ai/agentpaas/test/compat/v0.2.3	0.380s
```

Key characterization tests:
- `TestCharacterization_MCPRouterNotInstalled` — PASS
- `TestCharacterization_MCPManagerExistsButDaemonDoesNotUseIt` — PASS
- `TestCharacterization_MCPTimeoutInventory` — PASS (new in this task)
- `TestMCP_NoRouter_FailsClosedInProduction` — PASS
- `TestMCP_NoRouter_FakeModeReturnsSynthetic` — PASS
- `TestMCP_NoRouter_UndeclaredToolStillDenied` — PASS
- `TestMCP_WithRouter_FailsClosedDoesNotAffectRealRouter` — PASS
- `TestMCP_SyntheticPayloadStringAbsentInProduction` — PASS
- `TestMCP_NoRouter_AuditRecordIsDeniedNotCall` — PASS

## Files changed in this task

| File | Change |
|---|---|
| `test/compat/v0.2.3/v023_test.go` | Updated stale SetRouter line numbers (34→38, 193→318). Added `TestCharacterization_MCPTimeoutInventory`. |
| `docs/owa-records/b33-t01-mcp-baseline.md` | This handoff record (new). |

## Open risks for T02–T05

| Risk | Impact | Mitigation |
|---|---|---|
| R1: Fixed 5s stdio timeout survives into production | T05 must explicitly replace with B30 deadline; timeout inventory test will fail if it's silently left. | `TestCharacterization_MCPTimeoutInventory` fails if constant changes before T05. T05 must update the test. |
| R2: HTTP client has no timeout | Production MCP HTTP calls could hang indefinitely. | T05/T06 must add explicit `http.Client` timeout propagating from B30 deadline. |
| R3: Synthetic path accessible via env var | A misconfigured production deployment with `AGENTPAAS_TEST_FAKE_MCP=1` would return fake success. | T05 removes or build-time-restricts the synthetic path. Adversary review (T09) tests for this. |
| R4: Lifecycle HTTP mode untested against real Docker | `Lifecycle.StartHTTP` exists but has no end-to-end test with a real Docker runtime driver. | T03 adds durable service container lifecycle with real Docker/fake parity. |
| R5: Router audit helpers may double-count with harness audit | If T05 wires router audit + harness audit on the same call path. | T05 spec: "Audit one call once; avoid duplicate router+harness events." |
| R6: maxBodySize applies only to responses | Request size is unchecked at the router level. | T06 must enforce both request and response size bounds. |

## Build verification
```
$ go build ./...
BUILD: OK
```
