# Current State — Block 34.5 CLOSED

**Shipped release tag:** v0.3.0 (B1–B32 product line)  
**Development head:** B34 + **B34.5 residual close**  
**Main tip:** see `git log -1 --oneline`  
**Date:** 2026-07-26

## B34.5 tracks (merged)

| Track | Status | Evidence |
|-------|--------|----------|
| A CancelWorkflow + MCP fence | DONE | `docs/owa-records/b345-cancel.md` |
| B Durable start (BUG-043) + gen CAS | DONE | `docs/owa-records/b345-043-durable.md` |
| C Pipeline admit→register + RuntimeStageLauncher | DONE | `docs/owa-records/b345-pipeline.md` |
| ClaimNextReady CAS no-steal | DONE | `docs/owa-records/b345-claim-cas.md` (12/10 flake gone) |

## Gates (disk-verified on tip)

- `make block34-gate` PASS  
- `make block345-gate` PASS  
- `make block34-docker-gate` PASS (AGENTPAAS_DOCKER_TESTS=1)  
- `make block345-docker-gate` PASS  
- `go test -race ./internal/workflow/pipeline/` PASS  
- `golangci-lint` daemon+pipeline **0 issues**  
- `make golden-fast` **23/23 PASS**  

## What is now proven

1. **CancelWorkflow** RPC real (not FEATURE_NOT_ENABLED); nodes cleared; MCP WorkflowTerminal fenced  
2. **InvokeDeployment ACCEPTED** → startDurableRun Create/Start (mock driver); replay no double-start; status CAS via GetRunGeneration  
3. **Pipeline admit** registers workflow; skips startDurableRun; daemon can use RuntimeStageLauncher; Docker e2e launches labeled stage container via ReconcileOnce  
4. Two controllers cannot double-claim after CAS fix  

## Honest remaining residuals (out of B34.5 scope)

- Full multi-image **packed** stage agents with real handoff schemas/LLM (default still alpine sleep via env)  
- Live operator durable multi-turn soak with real agent + daemon restart (mock path + G49–G52 library cover supervisor)  
- Pause/resume/restart RPCs still FEATURE_NOT_ENABLED (B35/control — intentional)  
- Pipeline default still off (`AGENTPAAS_PIPELINE_ENABLED=1`)  

## Next

**B35** parent/child fan-out after product GO. Spec ready; `agentpaas_child_spawn_not_enabled`.
