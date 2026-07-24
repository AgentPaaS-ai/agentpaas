# B33-T08 Chunk 5b — Docker cross-container e2e (positive)

Workdir: `/Users/pms88/projects/ap-b33-t08`
Branch: `feat/b33-t08-mcp-cross-container` (5a already landed: endpoint, aliases, AttachClientContainer)

LOCAL ONLY.

## Goal

Add `TestE2E_CrossContainer_LookupFeedback` + `make mcp-container-e2e` that proves:
service container + client container on internal MCP network; client reaches service DNS alias with capability header; distinctive tool result; cleanup leaves zero orphans.

## Implement

### 1. Mock MCP server script
`internal/mcpmanager/testdata/mcp_mock_server.py`:
- Listen 0.0.0.0:8080
- POST JSON-RPC tools/call
- Require header equal to env `AGENTPAAS_MCP_CAPABILITY` (daemon/registry injects capability into service env — **if not already**, set on createServiceContainer: `AGENTPAAS_MCP_CAPABILITY` from instance after generate — order issue: capability generated AFTER create).

**Capability timing:** currently capability generated after container create. For mock auth either:
- (A) Mock does not check capability (resolver still sends header; prove network path) + separate unit test for capability rejection, OR
- (B) Restart/recreate container after capability known, OR  
- (C) Mock accepts any non-empty X-AgentPaaS capability header matching CapabilityHeader constant.

Use **(C)**: mock requires header name from env `MCP_CAP_HEADER` defaulting to the real header name; value must be non-empty 64-hex. Log rejection.

Find CapabilityHeader string value and hardcode in mock.

### 2. Inject capability into service container env (small fix)
After GenerateCapability, if container already started, we can't easily set env. So either:
- Generate capability **before** createServiceContainer and put in env + instance field, OR
- Mock only checks header present.

Prefer **generate capability before create** and pass in Env. Move GenerateCapability earlier in Start().

### 3. E2E test
`internal/mcpmanager/cross_container_e2e_test.go`:

```go
//go:build // no build tag — skip via env
func TestE2E_CrossContainer_LookupFeedback(t *testing.T) {
  if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" { t.Skip(...) }
  ...
}
```

Steps:
1. rt, err := runtime.NewDockerRuntime()
2. Copy mock script to temp dir
3. reg := NewServiceRegistry(rt, alwaysPromoted, alwaysReady)
4. reg.SetServiceContainerDefaults("python:3-alpine", []string{"python","/mock/mcp_mock_server.py"})
5. Need bind-mount mock into service container — **extend createServiceContainer** to support optional binds via SetServiceContainerDefaults or SetServiceBinds([]string).

Add:
```go
func (r *ServiceRegistry) SetServiceBinds(binds []string)
```
E2E: bind tempdir with script to `/mock:ro`.

6. EnsureServices(ctx, "e2e-wf", []pack.ServiceBinding{{ServiceID:"feedback", BundleDigest:"sha256:e2e", AllowedTools:[]string{"lookup_feedback"}, PackageName:"feedback-tools", PackageVersion:"1.0.0"}})
7. Get instance — Endpoint should be http://svc-feedback:8080 (check actual DNS alias helper)
8. Create client container: Image python:3-alpine or curlimages/curl, Command sleep infinity
9. reg.AttachClientContainer(ctx, "e2e-wf", clientID)
10. docker exec client: python/curl POST to Endpoint with Capability header and JSON-RPC tools/call lookup_feedback
11. Assert body contains `b33-t08-docker-e2e` and items
12. Also: ManagedServiceResolver.ResolveToolCall from **host may fail** on internal net — primary proof is exec path; optionally try resolver and skip if dial fails
13. WorkflowTerminal + remove client; ListNetworks/ListContainers labels → zero orphans for workflow
14. t.Cleanup always tears down

### 4. Makefile
```make
.PHONY: mcp-container-e2e
mcp-container-e2e:
	AGENTPAAS_DOCKER_TESTS=1 go test ./internal/mcpmanager/ -count=1 -race -run 'TestE2E_CrossContainer' -timeout 15m
```

### 5. Handoff docs/owa-records/b33-t08-chunk5a.md + chunk5b.md

## Acceptance
```bash
go test ./internal/mcpmanager/ ./internal/daemon/ ./internal/runtime/ -count=1 -race
go build ./...
AGENTPAAS_DOCKER_TESTS=1 make mcp-container-e2e   # or go test ... PASS
# Run e2e twice more for stability if first passes
```

Commit message: `test(mcp): Docker cross-container lookup_feedback e2e (B33-T08 c5b)`
