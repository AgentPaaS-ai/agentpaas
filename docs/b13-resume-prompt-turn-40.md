Continue AgentPaaS Block 13. 40 turns used. Pick up at BUG 7d Step 3.

CONTEXT (load these first):
- Load skills: agentpaas-40-turn-rhythm, agentpaas-owa-build-orchestration
- Read checkpoint: docs/b13-checkpoint-turn-40.md
- Read architecture trace: docs/b13-deploy-e2e-checkpoint-v5.md (§"FULL ARCHITECTURE TRACE" and §"PLANNED FIX Steps 3-5")
- Read the B13 block spec in agentpaas-execution-plan-v1.md (search "BLOCK 13")

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: 3236c1a fix(b13): wire harness audit appender + egress audit events + ContainerSpec.Binds
- Plugin tests: 109 passing. Daemon tests: all passing. Build + lint clean.
- Concurrent run limit (maxConcurrentRuns=3) enforced in Run handler — DONE this session.
- Execution plan restructured to v1.1: Block 14 = consolidated (14A/14B/14C), Block 15 = manual use-case assessment.
- Session limit: 40 turns per session. Micro-chunks only. Merge after each. Checkpoint every 2-3 chunks. Exit prompt mandatory at turn 38.

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API. Plans, dispatches, reviews, merges. Does NOT edit code.
- Worker: grok-composer-2.5-fast via Grok CLI ($0). Dispatch on git worktree via tmux.
- Adversary: grok-4.3 via agentpaas-adversary profile ($0).
- Verifier: GLM-5.2 via agentpaas-verifier profile, ONCE at block-end.

IMMEDIATE NEXT ACTION:
Implement BUG 7d Step 3 — mount audit volume in Run handler.
File: internal/daemon/control_handlers.go, Run() handler (~line 184, ContainerSpec.Create).
- Create host dir: {homePaths.State}/runs/{runID}/harness-audit/
- Add to ContainerSpec.Binds: "{hostAuditDir}:/audit"
- Add to ContainerSpec.Env: "AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl"
This is a 3-4 turn micro-chunk: edit, build, test, merge.

THEN (in order, each is a micro-chunk):
1. BUG 7d Step 4: Ingest harness audit JSONL into daemon chain (Stop handler) — 4-5 turns
2. BUG 7d Step 5: Update test agent to call agent.http() — 2-3 turns
3. BUG 7d e2e verification: build binaries, pack, run, stop, query audit, verify egress_denied — 4-5 turns
4. B13-T06: prompt-change immutable redeploy — 4-5 turns
5. B13-T07: demo matrix fixtures — 3-4 turns
6. B13-T08: /agentpaas slash commands — 4-5 turns
7. B13-T09: bundled SKILL.md — 3-4 turns
8. make block13-gate implementation — 2-3 turns
9. Block-end verifier + b13-block-end.md — 3-4 turns

CRITICAL FROM THIS SESSION:
1. Execution plan is now v1.1. Block 14 is consolidated (14A security + 14B egress timeline + 14C release). Block 15 is your manual use-case assessment (10 UCs). Old calendar/sequencing block removed as gate.
2. 40-turn rhythm skill created (agentpaas-40-turn-rhythm). Follow it: micro-chunks, merge often, checkpoint every 2-3 chunks, exit prompt at turn 38.
3. maxConcurrentRuns=3 enforced in daemon Run handler. Returns gRPC ResourceExhausted. Tested.
4. B13 security audit identified 8 gaps + 6 shortcuts. Remediation is Block 14A (9 tasks). Do NOT start 14A until B13 is fully done.
5. Harness FileAuditAppender already writes egress audit events (Steps 1-2 done). ContainerSpec.Binds field exists. Just need to wire the volume mount + daemon ingestion.

REMAINING MICRO-CHUNKS (in order, ~30-35 turns total):
1. BUG 7d Step 3: mount audit volume — est 3-4 turns
2. BUG 7d Step 4: ingest harness audit — est 4-5 turns
3. BUG 7d Step 5: update test agent — est 2-3 turns
4. BUG 7d e2e verify — est 4-5 turns
5. B13-T06: immutable redeploy — est 4-5 turns
6. B13-T07: demo fixtures — est 3-4 turns
7. B13-T08: slash commands — est 4-5 turns
8. B13-T09: SKILL.md — est 3-4 turns

If you reach turn 35 before finishing all, checkpoint and write exit prompt for the remainder.

Start at: BUG 7d Step 3 — edit internal/daemon/control_handlers.go Run() handler to mount audit volume.
