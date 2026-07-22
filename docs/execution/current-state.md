# Current State — As of Block 32

**Read this first** when starting work on AgentPaaS.

**Shipped release:** v0.2.3 (B1–B25)  
**Development head:** B26–B32 implemented; **next** B33  
**Gates:** `make block32-gate` PASS; golden-fast 19/19; version hygiene **0.3.0-dev**  
**Stable tag:** not cut — requires manual testing + founder approval for v0.3.0

## What B32 added
- `internal/delegation` — task/message/result schemas, two-sided snapshot authz, gateway capability tokens, digest-bound artifact broker, TaskOutbox/TaskWaiter
- Harness RPC: `delegate_task`, `get_task`, `list_task_events`
- Python SDK: `Agent.delegate` / `TaskHandle`
- `workflow.yaml` `delegations:` + `pack.BuildCommunicationSnapshot`
- Packaging: CLI/daemon default `0.3.0-dev`, Makefile ldflags, Formula template v0.3.0, `scripts/check-release-versions.sh`

## Binaries
Build with `make build` / `make build-all` so ldflags stamp **0.3.0-dev**.  
Prefer `~/projects/agentpaas/bin` ahead of Homebrew (cask may still be 0.2.3 until release).

## Next
1. **Manual testing** (ap-testing / human) before v0.3.0 tag  
2. R32 publish only after founder approval  
3. B33 — MCP router on same logical identity model  

### Suggested read order
1. This file  
2. `docs/execution/blocks/b32-summary.md` + `b32-review-notes.md`  
3. `docs/b32-risk-analysis.md`  
4. `internal/delegation/doc.go`  
