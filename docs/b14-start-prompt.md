Start AgentPaaS Block 14 — post-B13 build. Begin with 14A0 (correctness fixes).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read execution plan: agentpaas-execution-plan-v1.md search "BLOCK 14" then "14A0"
- Read risk analysis: docs/b13-risk-analysis.md (why these fixes exist)
- Read B13 decisions: docs/b13-decisions-and-learnings.md (architecture context)
- GitHub issues: #154–#158 (14A0), #159–#163 (14B)

STATE:
- Repo: ~/projects/agentpaas, on main, clean tree, all pushed
- Block 13 is COMPLETE (make block13-gate passes)
- Block 14 has 4 sub-segments: 14A0 (correctness) → 14A (security) → 14B (gateway+policy) → 14C (release)

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API. Plans, dispatches, reviews, merges. Does NOT edit code.
- Worker: grok-composer-2.5-fast via Grok CLI ($0). Dispatch on specific tasks.
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only.

CRITICAL CONTEXT FROM B13:
1. The daemon is `stubControlServer` in internal/daemon/control_handlers.go — this is the PRODUCTION server, not a stub. (14A0-T05 renames it.)
2. trackedRun struct is at control_handlers.go line ~565. Fields: Container, Network, AuditDir.
3. Auto-invoke goroutine at line ~251. Uses context.WithTimeout(context.Background(), 2*time.Minute) — detached from run lifecycle.
4. invokeAgent method at line ~559. Discards stdout (_ = stdout).
5. Stop handler at line ~263. Always publishes EventRunSucceeded.
6. Event types in internal/trigger/events.go. EventRunFailed may not exist — add it if needed.
7. Docker tests use AGENTPAAS_DOCKER_TESTS=1 env var gate.
8. Colima mounts ONLY /Users — e2e AGENTPAAS_HOME must be under ~/, NOT /tmp.

14A0 TASKS (in dependency order — each is a micro-chunk, merge after each):

1. Issue #154 — 14A0-T01: Run status tracking
   - Add status field to trackedRun, set correctly in invoke goroutine + Stop
   - Publish EventRunFailed when agent crashed/invoke failed
   - 2 micro-chunks. Start here.

2. Issue #156 — 14A0-T03: Invoke/Stop synchronization — DO WITH T01
   - Store cancel func in trackedRun; call in Stop before container removal
   - 1-2 micro-chunks.

3. Issue #155 — 14A0-T02: Orphan container reconciliation
   - reconcileOrphanedContainers() on daemon Start()
   - Depends on T01's status field
   - 2-3 micro-chunks.

4. Issue #157 — 14A0-T04: Docker e2e test in block14a0-gate
   - Real pack→run→invoke→stop→audit flow, gated behind AGENTPAAS_DOCKER_TESTS=1
   - AGENTPAAS_HOME under ~/ (colima mount limit)
   - 2-3 micro-chunks.

5. Issue #158 — 14A0-T05: Code hygiene rename + stale docs
   - Rename stubControlServer → controlServer, fix CLI doc.go
   - 1 micro-chunk (mechanical).

GATE: make block14a0-gate must pass before starting 14A.

AFTER 14A0: Block 14B is the CRITICAL sub-segment. Issue #159 (14B-T01) adds the
gateway container — the single most important piece of work in Block 14. B13's
egress "denial" is just network isolation (DNS fails), not actual policy enforcement.
14B-T01 creates a dual-homed gateway container and wires policy.yaml to runtime
enforcement. This is the core product value prop.

Start at: Issue #154 — 14A0-T01. Add status field to trackedRun in internal/daemon/control_handlers.go.
