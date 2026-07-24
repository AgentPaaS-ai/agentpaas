# B33-T08 Chunk 5c — Negative companion Docker e2e tests

Workdir: `/Users/pms88/projects/ap-b33-t08`
Branch: create/use `feat/b33-t08-mcp-cross-container` from current main (`63fcfab`)

LOCAL CI ONLY.

## Goal

Add Docker-gated negative tests companion to `TestE2E_CrossContainer_LookupFeedback`.
Each must **fail closed** with a clear error; no successful tool body; cleanup leaves zero orphans.

Spec negatives (b33-summary T08):
1. Undeclared tool
2. Undeclared service / binding
3. Generic HTTP bypass (no capability header → 401/deny)
4. Cross-workflow caller (client on wrong workflow network / wrong binding)
5. Service crash
6. Service timeout
7. Lease revoke / fence during tool (if practical in e2e)

## Implementation approach

File: `internal/mcpmanager/cross_container_neg_e2e_test.go`
Gate: `AGENTPAAS_DOCKER_TESTS=1`

Reuse helpers from positive e2e where possible — extract shared setup into `cross_container_e2e_helpers_test.go` if it reduces duplication:
- `setupDockerRegistry(t, workflowID, mockBinds) (*ServiceRegistry, *runtime.DockerRuntime, cleanup)`
- `startFeedbackService(t, reg, wf)` → instance
- `startClientAttached(t, dr, reg, wf)` → clientID
- `postMCP(t, dr, clientID, url, capHeader, tool, args)` → status, body

**Colima rule:** bind-mount only paths under `/Users/pms88/...` (repo testdata), never `/var/folders`.

### Test cases

#### 1. `TestE2E_Neg_UndeclaredTool`
- EnsureServices with AllowedTools=`lookup_feedback` only
- Client posts tools/call `evil_tool`
- Expect: non-200 OR JSON-RPC error OR ManagedServiceResolver error `not declared`
- Prefer testing **both**:
  - Resolver path: `NewManagedServiceResolver(reg,nil).ResolveToolCall(..., "evil_tool", ...)` must error
  - HTTP path to mock: mock returns tool not found — still OK but resolver is the production gate

#### 2. `TestE2E_Neg_UndeclaredBinding`
- Resolver.ResolveToolCall for binding `nope` that was never Ensure'd
- Expect error containing not found / not ready

#### 3. `TestE2E_Neg_HTTPBypassNoCapability`
- Direct HTTP POST from client **without** Capability header to service endpoint
- Expect HTTP 401 from mock (invalid capability)
- Proves capability is required on the wire

#### 4. `TestE2E_Neg_CrossWorkflowIsolation`
- Workflow A: EnsureServices feedback
- Workflow B: separate EnsureServices (or empty)
- Client attached **only** to B's network (or only to A)
- Attempt: client on B network calling A's DNS alias `svc-feedback` → must fail resolve or connection
- Client on A must not be able to use B's capability token against A (wrong cap → 401)

Minimal strong proof:
- Two workflows, two services
- Client attached only to wf-A
- nslookup/curl to wf-B service alias fails
- Wrong capability against wf-A service → 401

#### 5. `TestE2E_Neg_ServiceCrash`
- Start service with command that exits immediately after listen OR kill service container mid-flight
- Client call must error (connection refused / not ready)
- After crash, optional Reconcile marks unhealthy (if cheap)

Simplest: `SetServiceContainerDefaults(..., []string{"false"})` or `sleep 0` so container exits; EnsureServices may fail at readiness — if Start still reaches READY without probe, then HTTP fails. Prefer kill after READY:
```go
dr.Stop(ctx, inst.ContainerID, nil)
// then post → fail
```

#### 6. `TestE2E_Neg_Timeout`
- Mock: add support for tool `slow_tool` that sleeps 30s OR use a hanging endpoint
- Client/resolver context with 1s timeout
- Expect context deadline exceeded / timeout error code

Easiest path without mock change: ManagedServiceResolver with `http.Client{Timeout: 500ms}` against stopped service, OR extend mock:
```python
if tool_name == "slow_tool":
    time.sleep(30)
```
Declare slow_tool in AllowedTools for that test only.

#### 7. `TestE2E_Neg_FenceDuringCall` (best-effort)
- Start slow_tool call in goroutine
- Fence service
- Expect call cancelled / failed; late success not recorded if evidence store set

If too flaky, document skip and cover with existing unit tests for Fence+CancelTracker — but try once.

## Makefile
Extend `mcp-container-e2e` to run both positive and negatives:
```make
mcp-container-e2e:
	AGENTPAAS_DOCKER_TESTS=1 go test ./internal/mcpmanager/ -count=1 -race \
	  -run 'TestE2E_CrossContainer|TestE2E_Neg_' -timeout 20m
```

## Acceptance
```bash
cd /Users/pms88/projects/ap-b33-t08
go test ./internal/mcpmanager/ ./internal/daemon/ -count=1 -race
AGENTPAAS_DOCKER_TESTS=1 make mcp-container-e2e   # all E2E incl negatives PASS
# Prefer 2 consecutive full e2e runs
go build ./...
```

Commit: `test(mcp): cross-container negative companion e2e (B33-T08 c5c)`
Handoff: `docs/owa-records/b33-t08-chunk5c.md`
Update `docs/owa-records/b33-t08.md` remaining section.

Do not merge unless all pass. No GitHub Actions.
