# Current State — Block 34 CLOSED (library + stretch)

**Shipped release tag:** v0.3.0 (B1–B32 product line)
**Development head:** B34 Runtime-Native Sequential Agent Pipelines — **closed**
**Main tip:** see `git log -1 --oneline` after b34-close merges
**Date:** 2026-07-26

## B34 status

| Area | Status |
|------|--------|
| T01–T09 library + adversary | DONE (prior) |
| Cancel READY/PENDING residual (RES-1) | **FIXED** (`feat/b34-close-library`) |
| child_spawn gate label B31→B35 | **FIXED** |
| Live Docker multi-container stage isolation | **DONE** (`make block34-docker-gate`) |
| Daemon pipeline reconcile loop (enable-gated) | **DONE** (FakeLauncher in daemon Start) |
| Hermes-absent control-plane seam | **DONE** (in-process 3-stage proof) |
| B26 admission-conformance (store topologies) | **DONE** (`go test ./internal/routedrun/ -run Admission`) |

## Gates (disk-verified 2026-07-26)

- `make block34-gate` → PASS on main
- `AGENTPAAS_DOCKER_TESTS=1 make block34-docker-gate` → PASS (2 consecutive)
- `go test ./internal/daemon/ -race -run Pipeline` → PASS
- `go test ./internal/workflow/pipeline/ -race` → PASS (incl. Adversary)

## Explicit remaining gaps (do not overclaim)

1. **Product multi-image pipeline pack+invoke** — daemon reconcile uses `FakeLauncher` by default; `InvokeDeployment`/`startDurableRun` still starts a **single** durable container path. Multi-stage advancement with real per-stage packed agent images is **not** operator-complete.
2. **MCP service fence on pipeline cancel (live product path)** — MCP fence/WorkflowTerminal proven in mcpmanager unit + cross-container e2e; not yet a single CLI cancel of a live pipeline+MCP family.
3. **BUG-043 live operator durable multi-turn** — code wired (`startDurableRun`); unit/admission green; full multi-turn soak still operator-validated via G49–G52 library graders, not a fresh manual durable soak this session.

## BUG-036…043

Code fixes are on main. Records updated to FIXED/CLOSED with spotcheck evidence in each `docs/owa-records/BUG-0*.md`. See `docs/owa-records/b34-close-bugs-spotcheck.md`.

## Next

**B35** parent/child fan-out/fan-in — after product GO. Spec: `docs/execution/blocks/b35-summary.md` (execution-ready). No B35 tasks started.
