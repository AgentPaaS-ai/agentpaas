Continue AgentPaaS post-Block-14 — volunteer gate + release.

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read checkpoint: docs/b14b-checkpoint-1.md
- Read risk analyses: docs/b14a-risk-analysis.md, docs/b14b-risk-analysis.md, docs/b14c-risk-analysis.md

STATE:
- Repo: ~/projects/agentpaas, on main, last commit includes all Block 14 work
- BLOCK 14 COMPLETE: all gates pass (make block14-gate green)
- 14A0: 5 tasks, 14A: 8 tasks, 14B: 5 tasks, 14C: 3 tasks = 21 tasks total
- All on local main, not yet pushed to GitHub

BLOCK 14 SUMMARY:
- 14A0: B13 correctness fixes (run status, orphan reconciliation, invoke/Stop sync, e2e, rename)
- 14A: Security remediation (8 gaps: path allow-list, binary verify, output cap, confirmation state,
  hash chain, daemon socket check, sanitizer, cosign integration)
- 14B: Gateway topology (dual-homed agentgateway v1.3.0, HTTP_PROXY policy enforcement,
  real-time egress via audit tailer, DockerRuntime.Stats, trigger server)
- 14C: Release infra (goreleaser, homebrew formula, README, 6 docs)

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Worker: grok-composer-2.5-fast via Grok CLI ($0)
- Adversary: grok-4.3 via agentpaas-adversary profile
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only

REMAINING MANUAL GATES (cannot be automated):
1. Volunteer clean-machine test: 2 users follow README on their own macOS machines,
   reach running governed agent in <15 min. Every deviation = docs bug.
2. v0.1.0 tag + goreleaser release (after volunteer test)
3. cosign verify-blob + agent verify-release on real artifacts
4. Offline bundle creation + verification

P1 BACKLOG (accepted adversary findings — user must review accept vs fix):
From 14A: 10 items (see docs/b14a-risk-analysis.md)
From 14B: 9 items (see docs/b14b-risk-analysis.md)
From 14C: 5 items (see docs/b14c-risk-analysis.md)

POST-BLOCK-14 ACTION ITEM (user request):
After Block 14 is released, go through entire AgentPaaS project — review each MEDIUM
or LOW adversary callout so user can decide accept vs fix.

PODMAN vs DOCKER DECISION (user asked):
Research completed. Recommendation: STAY on Docker for P1.
- Podman: daemonless/rootless is better security model, ~95% Docker API compat
  via podman.socket + DOCKER_HOST. But migration cost + macOS dev friction.
- containerd: no Docker-compatible API server. Would require rewriting
  internal/runtime/docker.go entirely. K8s-native, not worth it for P1.
- Docker rootless mode on Linux prod closes most of the security gap.
- Pragmatic path: keep Docker API in code, eval Podman socket in prod with
  compat test suite, consider containerd only when moving to K8s.

Start at: Push Block 14 to GitHub, close issues #159-#163, then coordinate
volunteer clean-machine test with 2 macOS users.
