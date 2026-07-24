# B33-T08 Chunk 5c — Negative companion Docker e2e

**SHA:** 4cbe919 (plus prior c5c commits)
**Branch:** feat/b33-t08-mcp-cross-container

## Tests (AGENTPAAS_DOCKER_TESTS=1)

| Test | Proof |
|------|--------|
| TestE2E_Neg_UndeclaredTool | Resolver rejects `not declared`; HTTP mock error |
| TestE2E_Neg_UndeclaredBinding | Resolver service not found |
| TestE2E_Neg_HTTPBypassNoCapability | HTTP 401 without capability header |
| TestE2E_Neg_CrossWorkflowIsolation | Client on wf-A cannot reach wf-B service; same-wf OK |
| TestE2E_Neg_ServiceCrash | Post-stop call fails |
| TestE2E_Neg_Timeout | Real `context deadline exceeded` against slow_tool via service IP |
| TestE2E_Neg_FenceDuringCall | In-flight cancel + post-fence not ready |

Timeout/fence patch Endpoint to container IP so host-side resolver hits the real service (not vacuous DNS failure).

## Local gates
```
make mcp-container-e2e  PASS (×2)
go test mcpmanager/daemon/runtime/harness -race PASS
```
