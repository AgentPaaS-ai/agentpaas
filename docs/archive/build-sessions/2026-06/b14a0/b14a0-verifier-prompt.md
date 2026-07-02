Run block-end verification for AgentPaaS Block 14A0 (B13 correctness fixes).

Repo: ~/projects/agentpaas, on main, all 5 subtasks merged.

14A0 subtasks completed:
- T01: Run status tracking (trackedRun.Status field, EventRunFailed) — commit 036d9e5
- T02: Orphan container reconciliation (reconcileOrphanedContainers on daemon Start) — commit 9c64111 + 0f9bdab (adversary hardening)
- T03: Invoke/Stop synchronization (CancelInvoke + InvokeDone channel) — commit 036d9e5
- T04: Docker e2e test (TestE2E_PackRunInvokeStopAudit) — commit 240f8f6
- T05: Code hygiene rename (stubControlServer → controlServer + doc.go fix) — commit 8b41770

Gate already passed: `make block14a0-gate` (with AGENTPAAS_DOCKER_TESTS=1) — all green.

Verify:
1. Full block gate: build/test/race/lint + e2e with Docker
2. All adversary tests pass on merged main
3. Cross-subtask integration review: T01 status field + T03 InvokeDone channel + T02 reconciliation all interact correctly
4. Check for any remaining stubControlServer references
5. Check for any remaining "not yet implemented" in doc.go for implemented commands
