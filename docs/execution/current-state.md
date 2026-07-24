# Current State — Block 33 in progress

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B33 (v0.4 AgentPaaS-container MCP services)

## B33 progress
| Task | Status |
|------|--------|
| Preflight (block32-gate regressions) | DONE — README topology truth + snapshot ImageDigest |
| T01 Characterize MCP gap | DONE |
| T02 Service package/SDK/runner | DONE |
| T03 Durable service lifecycle | DONE — ServiceRegistry + state machine |
| T04 Service network + capability | NEXT |
| T05 Real MCP router e2e | pending |
| T06 Bounds/leases | pending |
| T07 Evidence/restart | pending |
| T08 Cross-container proof | pending |
| T09 block33-gate + adversary | pending |

## Suggested read order
1. This file
2. docs/execution/blocks/b33-summary.md
3. docs/owa-records/b33-t0{1,2,3}.md
4. internal/mcpmanager/service_registry.go
5. python/agentpaas_sdk/runner.py (service mode)
