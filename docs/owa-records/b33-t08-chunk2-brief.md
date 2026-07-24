# B33-T08 Chunk 2 — EnsureWorkflowMCPServices (Declare+Start)

Workdir: `/Users/pms88/projects/ap-b33-t08`
Branch: `feat/b33-t08-mcp-cross-container` @ `8396ecc`

## Goal (this chunk ONLY)

Add a daemon method that, given a workflow ID and `[]pack.ServiceBinding`,
**Declare + Start** each binding via the injected `mcpRegistry`. Unit-test with
mock RuntimeDriver. Do **not** wire into InvokeDeployment yet. No harness
sidecar. No real Docker e2e.

## API

```go
// EnsureWorkflowMCPServices declares and starts every service binding for a
// workflow. Requires s.mcpRegistry != nil. Idempotent Start on already-READY
// services. On first failure, best-effort Stop/cleanup already-started
// services in this call and return error.
func (s *controlServer) EnsureWorkflowMCPServices(ctx context.Context, workflowID string, services []pack.ServiceBinding) error
```

Behavior:
1. If `s.mcpRegistry == nil` → error wrapping `agentpaas_mcp_service_not_enabled` (or fmt.Errorf clear).
2. If `services` empty → nil.
3. For each binding in order:
   - `packageDigest := binding.BundleDigest` (required non-empty in tests)
   - tools := binding.AllowedTools (may be empty slice)
   - `Declare(workflowID, binding, packageDigest, tools)` — ignore already-declared if Get succeeds? Prefer: if Get works and READY, skip; if DECLARED, Start; if missing, Declare then Start.
   - `Start(ctx, workflowID, binding.ServiceID)`
4. Collect started service IDs; on error, Stop each started (best effort) then return error.
5. Do not leak capability/endpoint into returned error strings (use sanitize if needed).

Optional helper types for promotion/readiness if registry was constructed with nils:
- Tests construct registry as:
  ```go
  mock := defaultMockRuntimeDriver() // or whatever daemon tests use
  // ServiceRegistry needs runtime.RuntimeDriver — mock must implement full interface.
  // Prefer reusing mcpmanager test fake if exported; else adapt daemon mockRuntimeDriver
  // OR construct registry with driver from mcpmanager tests via exporting nothing new:
  // simplest path: put pure function in mcpmanager:
  ```

### Prefer putting orchestration in mcpmanager if cleaner

```go
// EnsureServices declares+starts bindings. Exported for daemon use.
func (r *ServiceRegistry) EnsureServices(ctx context.Context, workflowID string, services []pack.ServiceBinding) error
```

Then daemon method is thin:
```go
func (s *controlServer) EnsureWorkflowMCPServices(...) error {
  if s.mcpRegistry == nil { return ... }
  return s.mcpRegistry.EnsureServices(ctx, workflowID, services)
}
```

**Prefer mcpmanager.EnsureServices** so daemon stays thin and unit tests can live in mcpmanager with existing fakeRuntimeDriver.

## Tests (mcpmanager preferred)

`internal/mcpmanager/ensure_services_test.go`:

1. `TestEnsureServices_NilRegistryReceiver` N/A
2. `TestEnsureServices_Empty` → nil
3. `TestEnsureServices_DeclareAndStartOne` — one binding, fake driver, readiness always true → READY, Capability non-empty, network exists
4. `TestEnsureServices_IdempotentSecondCall` — second Ensure does not create second container
5. `TestEnsureServices_StartFailureCleansPrior` — two bindings; second Start fails; first stopped/cleaned
6. `TestEnsureServices_Unpromoted` — promotion checker false → error, no containers

Daemon tests:

1. `TestEnsureWorkflowMCPServices_NoRegistry` → error
2. `TestEnsureWorkflowMCPServices_DelegatesToRegistry` — set registry with fake driver via mcpmanager.NewServiceRegistry(...), call daemon method, assert READY

PromotionChecker: use existing fake from service_registry_test.go (unexported) — either export `AlwaysPromotedChecker` test helper in test_helpers.go or define local test fakes.

## Acceptance
```bash
cd /Users/pms88/projects/ap-b33-t08
go test ./internal/mcpmanager/ -count=1 -race -run 'EnsureServices'
go test ./internal/mcpmanager/ -count=1 -race
go test ./internal/daemon/ -count=1 -race -run 'EnsureWorkflow|FailClosed|MCPService'
go test ./internal/daemon/ -count=1 -race
go build ./...
golangci-lint run --timeout 5m ./internal/mcpmanager/... ./internal/daemon/...
```

Commit: `feat(mcpmanager): EnsureServices declare+start for workflow bindings (B33-T08 c2)`
Handoff: `docs/owa-records/b33-t08-chunk2.md`
Do not merge.

## Out of scope
- InvokeDeployment wiring
- Harness managed resolver sidecar
- Real image pull / real Docker
- Evidence file store
