# B33-T08 Stretch — Pack/CLI path + B26 MCP admission (finish T08 before T09)

Workdir: `/Users/pms88/projects/ap-b33-t08-stretch`
Branch: `feat/b33-t08-stretch` @ `f1fd795`

LOCAL CI ONLY. Small commits. Do not start T09.

## Stretch 2 first (smaller) — B26 MCP-client admission matrix

File: `internal/routedrun/admission_conformance_test.go`

Current `admissionTopologies` has `mcp_service` as standalone+meta. Spec wants **MCP-client** topology explicitly.

1. Add topology case:
```go
{
  name: "mcp_client",
  kind: "standalone", // admission shape still one READY node for client deployment
  meta: map[string]string{
    "mcp:client": "true",
    "mcp:binding:feedback": "feedback-tools@1.0.0",
    "mcp:allowed_tools:feedback": "lookup_feedback",
  },
  wantRuns: 1, wantNodes: 1,
}
```
Keep existing `mcp_service` entry too (service package deployment).

2. Ensure `TestAdmissionConformance_Matrix` still passes for both backends × all topologies including `mcp_client`.

3. Makefile:
```make
mcp-admission-conformance:
	go test ./internal/routedrun/ -count=1 -race -run 'TestAdmissionConformance_Matrix' -timeout 5m
```
Add this to `mcp-container-e2e` target **before** docker tests (fast unit gate).

4. Commit: `test(routedrun): explicit mcp_client topology in B26 admission matrix`

---

## Stretch 1 — Operator path (pack → invoke, Hermes absent)

### Gap
- Managed resolver speaks **HTTP JSON-RPC** + capability header.
- Service runner speaks **stdin/stdout** `mcp_tools_call`.
- `createServiceContainer` still defaults to `agentpaas-mcp-service:latest` / sleep.

### S1a — HTTP bridge for mcp_service (harness)

When `Config.AgentKind == "mcp_service"` (or env `AGENTPAAS_AGENT_KIND=mcp_service`):

1. Start Python service runner as today (stdin protocol).
2. **Also** start HTTP server on `0.0.0.0:8080` (or `AGENTPAAS_MCP_HTTP_ADDR`) that:
   - Requires header `X-AgentPaaS-MCP-Capability` == env `AGENTPAAS_MCP_CAPABILITY` (constant-time compare); else 401
   - Accepts POST JSON-RPC `tools/call` / `tools/list` (same shape as `buildMCPRequest` / mock server)
   - Translates to stdin `mcp_tools_call` / `mcp_tools_list` and returns JSON-RPC result
   - Never exposes capability in responses/logs

Unit tests in harness (no docker): httptest against bridge with fake stdin worker or real python short script.

Commit: `feat(harness): HTTP MCP bridge for mcp_service containers`

### S1b — Resolve service image from package digest

`ServiceRegistry.createServiceContainer`:
- If `inst.BundleDigest` non-empty, image = `sha256:<digest>` bare digest (daemon local store) OR `pack.LocalImageRef(packageName, digest)` — match how agent containers resolve installed images in control_handlers.
- Prefer bare `sha256:` digest like installed agent path (see control_handlers isInstalled).
- Env already has capability + declared tools.
- Command: default harness entry from image (don't force sleep infinity when image from digest).
- Keep SetServiceContainerDefaults override for tests.

When using packed image, ContainerSpec should use image entrypoint from Dockerfile (harness). Set env:
- AGENTPAAS_AGENT_KIND=mcp_service
- AGENTPAAS_MCP_DECLARED_TOOLS
- AGENTPAAS_MCP_CAPABILITY
- AGENTPAAS_AGENT_PATH=/agent/main.py (or pack convention)

Commit: `feat(mcpmanager): start service containers from package image digest`

### S1c — Fixtures

```
test/e2e/fixtures/mcp-feedback-service/
  agent.yaml   # kind: mcp_service, tools: [lookup_feedback], transport: streamable_http
  policy.yaml  # deny-all egress (or minimal)
  main.py      # @agent.mcp_tool lookup_feedback returns distinctive marker
  requirements.txt if needed

test/e2e/fixtures/mcp-feedback-client/
  agent.yaml   # worker
  workflow.yaml # services: feedback → package feedback-tools digest filled at test time OR name/version
  policy.yaml
  main.py      # on_invoke: result = agent.mcp("feedback","lookup_feedback",{}); return result
```

Pack must validate these (unit test without docker: pack.Detect/Validate).

Note: workflow ServiceBinding needs BundleDigest at EnsureServices time — e2e fills after pack.

### S1d — Docker operator e2e (Hermes absent)

`internal/daemon/mcp_operator_e2e_test.go` or `test/e2e/mcp_operator_e2e_test.go` gated by AGENTPAAS_DOCKER_TESTS=1:

1. Build linux harness binary
2. Pack service fixture → image digest Ds
3. Pack client fixture (rewrite workflow.yaml with Ds) → digest Dc  
4. Start controlServer with home temp + docker RT + SetMCPServiceRegistry
5. Install/promote packages as existing e2e does for weather
6. Invoke client deployment via gRPC InvokeDeployment or Run — **no Hermes**
7. Assert invoke success body contains distinctive marker from service tool
8. Assert evidence/sidecar path used; cleanup zero orphans
9. At least one negative: invoke with undeclared tool in client code fails

If full InvokeDeployment is too heavy, acceptable intermediate:
- Pack both images
- EnsureServices with BundleDigest=Ds and image resolution
- Sidecar + client container from Dc image with MCP binding env
- Invoke client via docker exec/http to harness /invoke
- Still proves pack artifacts + Hermes-absent runtime

Prefer maximal real path; fall back to intermediate if pack+InvokeDeployment blocked, document gap.

### S1e — Makefile

```make
mcp-container-e2e:
	go test ./internal/routedrun/ -count=1 -race -run 'TestAdmissionConformance_Matrix' -timeout 5m
	AGENTPAAS_DOCKER_TESTS=1 go test ./internal/mcpmanager/ -count=1 -race -run 'TestE2E_CrossContainer|TestE2E_Neg_' -timeout 20m
	AGENTPAAS_DOCKER_TESTS=1 go test ./internal/daemon/ -count=1 -race -run 'TestE2E_MCPOperator|TestE2E_MCP' -timeout 25m
```

## Acceptance (all local)

```bash
cd /Users/pms88/projects/ap-b33-t08-stretch
go test ./internal/routedrun/ -count=1 -race -run TestAdmissionConformance_Matrix
go test ./internal/harness/ ./internal/mcpmanager/ ./internal/daemon/ -count=1 -race
AGENTPAAS_DOCKER_TESTS=1 make mcp-container-e2e
go build ./...
```

## Docs
- Update `docs/owa-records/b33-t08.md` stretch section DONE with evidence
- Handoffs per chunk: `b33-t08-stretch-s2.md`, `b33-t08-stretch-s1.md`
- Do **not** merge to main until acceptance green; then orch merges

## Order of work
1. Stretch 2 (admission) — one commit
2. S1a HTTP bridge + tests
3. S1b image digest
4. S1c fixtures
5. S1d operator e2e
6. Makefile + docs

Ping orchestrator only when full acceptance green or hard blocked with evidence.
