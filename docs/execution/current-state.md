# Current State — Block 34 CLOSED (multi-agent proof)

**Shipped release tag:** v0.3.0 (B1–B32 product line)  
**Development head:** **B34 CLOSED** including multi-agent three-stage Docker e2e  
**Main tip:** see `git log -1 --oneline`  
**Date:** 2026-07-26

## B34 close criteria

| Item | Status |
|------|--------|
| T01–T09 library + adversary | DONE |
| B34.5 residuals (cancel, durable tests, pipeline register, CAS) | DONE |
| **Multi-agent three-stage Docker e2e (Hermes-absent)** | **DONE** `make block34-multiagent-gate` ×3 |
| StageOrder on launch jobs | FIXED |

## Docker policy (standing)

- `make ensure-docker` — start/install Colima+Docker or **fail** (no skip)
- B34 pipeline Docker/multi-agent tests use `requireDockerE2E` — **never `t.Skip`**
- `make block34-gate` ends with `block34-docker-gate` (multi-agent ×3 + isolation)

## Gates (disk-verified)

- `make block34-gate` PASS  
- `make block345-gate` PASS  
- `make block34-docker-gate` PASS  
- `make block34-multiagent-gate` PASS (3 consecutive)
- **Standing:** `block34-docker-gate` and `block345-docker-gate` **include** multi-agent; use `make block34-full-gate` for library+docker close path  
- `make block345-docker-gate` PASS  
- golden-fast (run at close)  
- golangci-lint pipeline 0 issues  

## Multi-agent proof (what it is)

`TestB34MultiAgentE2E_ThreeStageHermesAbsent`:
- 3 sequential stages, **3 distinct real Docker containers + networks**
- ReconcileOnce + RuntimeStageLauncher (no Hermes)
- Durable handoffs (2) + final CommitStageSuccess → workflow SUCCEEDED
- BuildPipelineInspect all SUCCEEDED; labels stage_order 0/1/2
- Fence cleanup zero orphans

Evidence: `docs/owa-records/b34-multiagent-e2e.md`

## Honest residual (not blocking B34 close)

- Packed product stage **agent images** with Python SDK handoff RPCs end-to-end (test uses alpine stage containers + controller CommitStageSuccess as the durable handoff plane — same contract agents will use)
- Live CLI pack of three signed packages as operator UX (runtime multi-agent proven)

## Next

**B35** parent/child fan-out after product GO.
