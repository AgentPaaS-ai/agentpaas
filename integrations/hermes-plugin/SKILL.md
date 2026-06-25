# AgentPaaS Deploy Skill

## When to Use
When you generate agent code and need to deploy it as a governed, sandboxed, audited workload.

## How to Run
1. `/agentpaas deploy <path>` or call `agentpaas_pack` then `agentpaas_run`
2. Check status: `/agentpaas status` or `agentpaas_status`
3. View audit: `/agentpaas audit <run_id>` or `agentpaas_audit_query`

## Pitfalls
- Docker not running → `agentpaas_doctor` for diagnostics
- Policy denial → check audit events for `egress_denied` with destination/reason
- Agent not found → run `agentpaas_pack` first