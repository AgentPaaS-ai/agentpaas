# Current State — As of Block 33 start

**Read this first** when starting work on AgentPaaS.

**Shipped release:** v0.3.0 (B1–B32)  
**Development head:** B33 in progress (v0.4 MCP services)  
**Preflight:** `make block32-gate` dependency restored green after README topology
truth + BUG-040 snapshot `CallerPackageDigest` fix (merge 5950bf0).  
**Golden:** 23/23 fast tier PASS (G01–G16, G44–G45, G47, G49–G52).

## What B32 added (shipped in v0.3.0)
- `internal/delegation` — task/message/result schemas, two-sided snapshot authz,
  gateway capability tokens, digest-bound artifact broker, TaskOutbox/TaskWaiter
- Harness RPC: `delegate_task`, `get_task`, `list_task_events`
- Python SDK: `Agent.delegate` / `TaskHandle`
- `workflow.yaml` `delegations:` + `pack.BuildCommunicationSnapshot`
- Live wiring fixes: BUG-036–043 (identity, install path, delegation trust state,
  durable container start, doctor, plugin CLI/skill)

## Binaries
Build with `make build` / `make build-all` so ldflags stamp **0.3.0-dev** until
v0.4 version bump. Prefer `~/projects/agentpaas/bin` ahead of Homebrew.

## Next
1. **B33** — AgentPaaS-container MCP services (real router, no synthetic success)
2. B34 — Runtime-native sequential pipelines
3. B35 — Bounded parent/child workflows

### Suggested read order
1. This file  
2. `docs/execution/blocks/b33-summary.md`  
3. `docs/execution/blocks/b32-summary.md` + `b32-review-notes.md`  
4. `docs/owa-records/b33-preflight-gate.md`  
5. `internal/mcpmanager/` + `internal/harness/rpc_server.go` handleMCP  
