# Current State — Block 33 in progress

**Read this first** when starting work on AgentPaaS.

**Shipped release:** v0.3.0 (B1–B32)
**Development head:** B33 (v0.4 AgentPaaS-container MCP services)
**Merged:** B33-T01 baseline freeze; B33-T02 service package/SDK/runner
**In flight:** B33-T03 durable service registry/lifecycle

## B33 progress
| Task | Status |
|------|--------|
| T01 Characterize MCP gap | DONE — docs/owa-records/b33-t01-mcp-baseline.md |
| T02 Service package/SDK/runner | DONE — mcp_tool, service mode, LoadAgentYAML validation |
| T03 Durable service lifecycle | IN PROGRESS |
| T04 Service network + capability | pending |
| T05 Real MCP router e2e | pending |
| T06 Bounds/leases | pending |
| T07 Evidence/restart | pending |
| T08 Cross-container proof | pending |
| T09 block33-gate + adversary | pending |

## Suggested read order
1. This file
2. docs/execution/blocks/b33-summary.md
3. docs/owa-records/b33-t01-mcp-baseline.md
4. docs/owa-records/b33-t02.md
5. internal/mcpmanager/ + python/agentpaas_sdk/runner.py service mode
