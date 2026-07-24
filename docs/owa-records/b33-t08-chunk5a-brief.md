# B33-T08 Chunk 5a — Real endpoint + client network attach + Docker connectivity e2e

Workdir: `/Users/pms88/projects/ap-b33-t08`
Branch: `feat/b33-t08-mcp-cross-container`

LOCAL CI ONLY. No GitHub Actions.

## Problems blocking live cross-container MCP

1. `service_registry.go:315` sets `Endpoint = "internal://" + containerID` — not HTTP-reachable.
2. `AttachNetwork` does not set DNS aliases — service has no stable name on the network.
3. Client agent container is never attached to the MCP service network.
4. No Docker e2e proving container-to-container MCP call.

## Required work

### 1. AttachNetwork aliases (runtime)

Extend API carefully (prefer additive overload):

```go
// Option A — new method (preferred if AttachNetwork widely used):
AttachNetworkWithAliases(ctx, containerID, networkID NetworkID, aliases []string) error

// Option B — change AttachNetwork to accept optional aliases via a small options struct.
```

Docker implementation: `NetworkConnect` with `network.EndpointSettings{Aliases: aliases}`.

Fake drivers in mcpmanager + daemon tests: record aliases.

Update `AttachToServiceNetwork` to take `aliases []string` and pass through.

### 2. Per-service DNS alias + HTTP endpoint

When Start completes network attach for the **service** container:
- DNS alias: prefer stable `svc-<serviceBindingID>` sanitized to `[a-z0-9-]` (or keep random alias but store it on instance — already have NetworkAlias on network state; **per-instance alias is better**).
- Set `inst.NetworkAlias = serviceDNSAlias` (instance field, not only network-level).
- Set `inst.Endpoint = fmt.Sprintf("http://%s:%d", serviceDNSAlias, DefaultMCPServicePort)` 
- `const DefaultMCPServicePort = 8080`

Update any test that asserts `internal://` prefix.

### 3. Attach client container to service network

```go
// ServiceRegistry.AttachClientContainer attaches the caller agent container
// to the workflow service network so it can resolve service DNS aliases.
// No service capability is granted by attachment alone.
func (r *ServiceRegistry) AttachClientContainer(ctx context.Context, workflowID string, clientContainerID runtime.ContainerID) error
```

Daemon (`control_handlers.go`) after agent container Create+Start succeeds AND mcp bindings were prepared:
```go
if mcpOK && s.mcpRegistry != nil {
  _ = s.mcpRegistry.AttachClientContainer(ctx, runID, containerID) // or log+fail?
  // Prefer fail run if attach fails when services were declared.
}
```
Do both Run() and startDurableRun() paths. workflowID must match prepareMCPBindingsForRun (runID).

### 4. Optional: injectable service command for tests

```go
func (r *ServiceRegistry) SetServiceContainerDefaults(image string, command []string)
```
Default remains `agentpaas-mcp-service:latest` / `sleep infinity`. E2E sets image to something available (e.g. python:3.12-slim) and command to run a tiny MCP JSON-RPC server.

### 5. Tiny in-tree MCP mock for Docker e2e

Add `internal/mcpmanager/testdata/mcp_mock_server.py` or embed a Go `mcp mock` binary via `go test` helper that:
- Listens `:8080`
- Requires header `X-AgentPaaS-MCP-Capability` (or whatever `CapabilityHeader` is)
- Handles tools/call for `lookup_feedback` returning `{"items":[{"id":"f1"}],"source":"b33-t08-docker-e2e"}`

### 6. Docker e2e test

`internal/mcpmanager/cross_container_e2e_test.go` gated by `AGENTPAAS_DOCKER_TESTS=1`:

Positive path:
1. NewDockerRuntime
2. ServiceRegistry with always-promoted + always-ready (or readiness TCP)
3. SetServiceContainerDefaults to run mock server
4. EnsureServices with one binding `feedback` / tool `lookup_feedback`
5. Create a second client container on same host network stack... OR just call ManagedServiceResolver **from the test process** after attaching a "client" isn't required if resolver runs on host — **BUT** host cannot reach internal Docker network easily on Colima.

**Critical Colima note:** host often cannot dial internal Docker networks. E2E must either:
- (A) Run CallTool **inside** a third container attached to the service network (true cross-container), or
- (B) Attach service network as non-internal for test only (weaker), or  
- (C) Use Docker exec into a client container that curls the service.

Prefer **(C)**: after EnsureServices, create client container (`curlimages/curl` or alpine), AttachClientContainer, `docker exec curl -H capability http://alias:8080` with JSON-RPC body, assert distinctive body. Also unit-level: ManagedServiceResolver against endpoint if we publish a test-only host port — skip if Colima blocks.

Also assert:
- Service network has Internal=true (inspect)
- No host port published on service container
- After WorkflowTerminal, network/containers gone (orphan check)

### 7. Makefile

```make
mcp-container-e2e:
	AGENTPAAS_DOCKER_TESTS=1 go test ./internal/mcpmanager/ -count=1 -race -run 'TestE2E_CrossContainer' -timeout 10m
```

## Tests without Docker
- Endpoint format unit test after Start with fake driver
- AttachClientContainer idempotent
- AttachNetwork aliases recorded on fake driver

## Acceptance (local)
```bash
cd /Users/pms88/projects/ap-b33-t08
go test ./internal/mcpmanager/ ./internal/daemon/ ./internal/harness/ ./internal/runtime/ -count=1 -race
go build ./...
golangci-lint run --timeout 5m ./internal/mcpmanager/... ./internal/daemon/... ./internal/runtime/...
AGENTPAAS_DOCKER_TESTS=1 go test ./internal/mcpmanager/ -count=1 -run 'TestE2E_CrossContainer' -timeout 10m
```

Commit: `feat(mcp): real service endpoint, client net attach, cross-container e2e (B33-T08 c5a)`
Handoff: `docs/owa-records/b33-t08-chunk5a.md`

## Out of scope
- Full pack/CLI weather-style e2e with signed packages (later 5b)
- All negative companions (5c)
- B26 admission matrix
