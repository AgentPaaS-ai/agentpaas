Continue AgentPaaS Block 14A0 — B13 Correctness Fixes.

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read risk analysis: docs/b13-risk-analysis.md (the 4 issues we identified)
- Read execution plan: agentpaas-execution-plan-v1.md search "14A0" (full spec for each task)
- Read B13 decisions: docs/b13-decisions-and-learnings.md (architecture context)

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: dad9607 (risk analysis doc)
- Block 13 is COMPLETE (make block13-gate passes). All B13 work committed.
- Block 14A0 has 4 tasks — they are correctness fixes for runtime bugs found
  during the B13 in-depth review. No security work yet (that's 14A).

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API. Plans, dispatches, reviews, merges. Does NOT edit code.
- Worker: grok-composer-2.5-fast via Grok CLI ($0). Dispatch on specific tasks.
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only.

CRITICAL CONTEXT FROM B13:
1. The daemon is `stubControlServer` in internal/daemon/control_handlers.go — this is the PRODUCTION server, not a stub. The name is misleading but functional.
2. trackedRun struct is at control_handlers.go line ~565. It has Container, Network, AuditDir fields.
3. The auto-invoke goroutine is at line ~251. It uses context.WithTimeout(context.Background(), 2*time.Minute) — detached from run lifecycle.
4. invokeAgent method is at line ~559. It discards stdout (_ = stdout).
5. Stop handler is at line ~263. It always publishes EventRunSucceeded.
6. Event types are in internal/trigger/events.go. EventRunFailed may not exist — add it if needed.
7. Docker tests use AGENTPAAS_DOCKER_TESTS=1 env var gate (see Block 5 tests for pattern).
8. Colima mounts ONLY /Users — e2e AGENTPAAS_HOME must be under ~/, NOT /tmp.

TASKS (in dependency order — each is a micro-chunk, merge after each):

1. 14A0-T01: Run status tracking (2 micro-chunks)
   - Add `status` field to trackedRun: "running" | "succeeded" | "failed"
   - Set status in auto-invoke goroutine based on invokeAgent result
   - Check container exit code in Stop before removing; set failed if non-zero
   - Publish EventRunFailed (add to events.go if missing) when status is failed
   - Record status in run_stop audit payload
   - Test: TestRun_FailedInvoke_SetsFailedStatus

2. 14A0-T03: Invoke/Stop synchronization (1-2 micro-chunks) — DO WITH T01
   - Add cancelInvoke context.CancelFunc to trackedRun
   - Store cancel in trackRun; call it in Stop BEFORE removing container
   - Use a sync.WaitGroup or done channel to wait ~2s for goroutine exit
   - Test: TestStop_RaceWithInvoke_GoroutineExitsCleanly

3. 14A0-T02: Orphan container reconciliation (2-3 micro-chunks)
   - Add reconcileOrphanedContainers() to daemon Start()
   - ListContainers with label agentpaas/resource-type=agent
   - Stop + remove orphans not in s.runs map
   - Reference Block 5 TestE2E_CrashReconciliation for pattern
   - Test: TestReconcileOrphans_StopsOrphanedContainers

4. 14A0-T04: Docker e2e test in block13-gate (2-3 micro-chunks)
   - TestE2E_PackRunInvokeStopAudit gated behind AGENTPAAS_DOCKER_TESTS=1
   - Full flow: pack → run → wait for invoke → stop → audit query → assert egress_denied
   - AGENTPAAS_HOME under ~/ (colima mount limit)
   - Wire into Makefile block14a0-gate (conditional on AGENTPAAS_DOCKER_TESTS)
   - Verify with real Docker (colima)

GATE: make block14a0-gate must pass before starting 14A.

If you reach turn 35 before finishing all 4 tasks, checkpoint and write exit prompt for the remainder.

Start at: 14A0-T01 — add status field to trackedRun in internal/daemon/control_handlers.go.
