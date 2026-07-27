# B34 Block End — Runtime-Native Sequential Agent Pipelines

**Date:** 2026-07-26 (updated from 2026-07-24 library close)
**Status:** CLOSED (library + stretch gates)
**Main tip:** see `git log -1 --oneline`

## Outcome delivered

### Library (prior T01–T09)
- Linear durable scheduler (claim/ack/commit, pause, resume, cancel)
- Fake + Runtime stage launch plans, labels, authority intersection, fence helper
- Artifact promote/project/provenance with symlink and budget guards
- Inspect summary; pipeline enable flag (default off)
- Reference proof three-stage library path
- Adversary suite green after nested reserved-key + commit validation + symlink fixes

### Close-out stretch (2026-07-26)
- **CancelWorkflow** clears ALL non-terminal nodes (READY/PENDING/LAUNCHING/RUNNING) — RES-1 closed
- **child_spawn** not-enabled block label corrected **B31 → B35**
- **Live Docker e2e** multi-container stage isolation (`TestDockerE2E_*`, `make block34-docker-gate`)
- **Daemon.Start** enable-gated pipeline reconcile loop (`pipelineRuntime`)
- **Hermes-absent** control-plane seam test (3-stage in-process, no Hermes)
- FenceStage stop timeout (no fire-and-forget Stop/Remove race)

## Gates
- `make block34-gate` PASS
- `make block34-docker-gate` PASS (AGENTPAAS_DOCKER_TESTS=1, 2× consecutive)
- `go test -race ./internal/workflow/pipeline/` PASS including Adversary_B34
- `go test -race ./internal/daemon/ -run Pipeline` PASS

## Handoff records
- `docs/owa-records/b34-t0{1..9}*.md` (task history)
- `docs/owa-records/b34-close-library.md`
- `docs/owa-records/b34-close-docker-e2e.md`
- `docs/owa-records/b34-close-wire-reconcile.md`
- `docs/owa-records/b34-close-bugs-spotcheck.md`

## Explicit residuals (not blocking B34 close; honest product gaps)
1. Multi-image pipeline **operator** pack+invoke (per-stage packed agents advanced by product path) — still FakeLauncher in daemon loop
2. Live CLI cancel of pipeline+MCP family end-to-end (MCP fence proven at mcpmanager layer)
3. Fresh manual durable multi-turn soak beyond G49–G52/unit wiring for BUG-043

## Next
B35 fan-out/fan-in after product GO. Do not start without GO.


## B34.5 residual close (2026-07-26)

Merged tracks: cancel+MCP fence, durable start tests+gen CAS, pipeline
register+RuntimeStageLauncher, ClaimNextReady CAS no-steal.

Gates: block34-gate, block345-gate, docker gates, golden-fast 23/23, lint 0.

See docs/execution/current-state.md and docs/owa-records/b345-*.md.


## Multi-agent three-stage Docker e2e (2026-07-26)

- `TestB34MultiAgentE2E_ThreeStageHermesAbsent` + `make block34-multiagent-gate` (3× PASS)
- StageOrder propagated on ReconcileOnce claim path (`3b93647`)
- Closes B34 multi-container multi-agent runtime residual
