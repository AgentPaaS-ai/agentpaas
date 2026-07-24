# B33-T08 Chunk 5b: Colima Bind-Mount Fix for Cross-Container E2E

## Date
2026-07-24

## Root Cause
Colima VM cannot see macOS `/var/folders` temporary directories. The e2e test
copied `mcp_mock_server.py` to `os.MkdirTemp` and bind-mounted from there,
causing container failures (`python: cant open /mock/mcp_mock_server.py`).

## Changes

### 1. Bind-mount repo testdata directly (`cross_container_e2e_test.go`)
- Use `filepath.Abs("testdata")` to resolve the repo's testdata directory
- Bind-mount: `abs + ":/mock:ro"` — Colima always sees repo directories
- Removed `os.MkdirTemp` + `exec.Command("cp")` workaround

### 2. Container liveness assertion after EnsureServices
- `docker inspect -f {{.State.Running}}` after EnsureServices
- On failure, grab container logs for diagnostics

### 3. Reduced debug noise
- Removed verbose `docker inspect` network dumps
- Removed `nslookup` DNS check
- Removed per-network logging loops
- Kept essential: IP discovery, HTTP response, orphan checks

### 4. Deleted probe-only test files
- `internal/mcpmanager/ensure_docker_probe_test.go` — removed
- `internal/runtime/attach_alias_probe_test.go` — removed
- These were scratch probes from debugging, not permanent tests

### 5. Orphan network reconciliation
- Added `ReconcileOrphanServiceNetworks` after `WorkflowTerminal`
- Ensures networks are removed even when client containers were removed
  outside the registry's tracking

## E2E Test Evidence (3 consecutive PASS runs)

```sh
AGENTPAAS_DOCKER_TESTS=1 go test ./internal/mcpmanager/ -count=1 \
  -run TestE2E_CrossContainer_LookupFeedback -v -timeout 15m
```

### Run 1
```
=== RUN   TestE2E_CrossContainer_LookupFeedback
    Docker Engine version: 29.5.2
    bind-mount testdata from: /Users/pms88/projects/ap-b33-t08/internal/mcpmanager/testdata
    Service endpoint: http://svc-feedback:8080
    Service container ... is running ✓
    Client attached to service network
    Using direct IP: http://172.18.0.2:8080
    Python HTTP response stdout: {"jsonrpc": "2.0", "result": {"content": [{"type": "text", "text": "{\"marker\": \"b33-t08-docker-e2e\", ...}"}]}, "id": 1}
        HTTP_STATUS:200
    ReconcileOrphanServiceNetworks removed 1 orphan network(s)
    Zero MCP orphan containers ✓
    Zero service network orphans ✓
--- PASS: TestE2E_CrossContainer_LookupFeedback (26.82s)
```

### Run 2
```
--- PASS: TestE2E_CrossContainer_LookupFeedback (26.87s)
```

### Run 3
```
--- PASS: TestE2E_CrossContainer_LookupFeedback (26.77s)
```

## Unit Test Evidence (with -race)

```sh
go test ./internal/mcpmanager/ ./internal/daemon/ ./internal/runtime/ -count=1 -race
```

```
ok  github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager 6.736s
ok  github.com/AgentPaaS-ai/agentpaas/internal/daemon     70.493s
ok  github.com/AgentPaaS-ai/agentpaas/internal/runtime     2.221s
```

All packages pass with race detector enabled. Zero data races.

## Capability assertion fix
The inner JSON text field in the mock response contains escaped quotes
(`\"capability_provided\"`). Changed assertion from exact quote match
to substring match on `capability_provided`.
