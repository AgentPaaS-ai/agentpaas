# Current State — Block 33 in progress

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B33 (v0.4 AgentPaaS-container MCP services)

## B33 progress
| Task | Status |
|------|--------|
| Preflight–T07 | DONE |
| T08 Cross-container proof | DONE (c1–c5c + stretch S1/S2) |
| T09 block33-gate + adversary | NEXT |

## T08 complete evidence (local, 2026-07-24)
- B26 admission matrix includes mcp_client + mcp_service topologies PASS
- make mcp-container-e2e PASS (admission + cross-container + negatives + operator pack)
- TestE2E_OperatorPack_MCPFeedbackService PASS — pack service/client, real image digest, HTTP bridge, fixture marker, undeclared tool denied, zero orphans
- Port model: MCP bridge 0.0.0.0:8080 inside service container; harness admin 127.0.0.1:8090; no host port publish; per-workflow internal DNS
- CI is local-only (no GH runners)

## Suggested read order
1. This file
2. docs/owa-records/b33-t08.md
3. internal/mcpmanager/operator_pack_e2e_test.go
4. internal/harness/mcp_bridge.go
5. docs/execution/blocks/b33-summary.md → T09
