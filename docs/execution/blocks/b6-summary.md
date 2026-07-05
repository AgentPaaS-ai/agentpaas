# Block 6 — Agent Harness & Python SDK

**Status:** COMPLETE
**Gate:** make block6-gate
**Date:** Unknown

## Scope
- Full block gate: `make block6-gate` (build + test + race + lint + osv) - All adversary tests on merged main: `go test -race -run TestAdversary_B6 ./...` - Cross-subtask integration review across subtask boundaries: - T01 harness lifecycle (server/python_worker) + T02 budget enforcer + T03 PID 1 reaper + T04 SDK RPC back-channel + T05 failure context/redaction all interact through server.go invoke path - Verify budget-triggered kill (T02) → process reaper cleanup (T03) → failure context categor

## Subtasks Completed
| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| b6-t01 | B6-T01: Harness HTTP Lifecycle | COMPLETE | 11 tests | 3 adversary breaks found | 2 HIGH | 1 MEDIUM | - Endpoints: /healthz (always 200), /readyz (200 ready / 503 + ErrorResponse not-ready), /invoke (se |
| b6-t02 | B6-T02: Budget Enforcement | COMPLETE | 4 tests | 2 adversary breaks found | 1 HIGH | 1 MEDIUM | - Implementation: configurable per-invoke wall-clock/iteration/token budgets; BudgetEnforcer hook fo |
| b6-t03 | B6-T03: PID 1 Process Duties | COMPLETE | 6 tests | 0 adversary breaks | - Implementation: Linux child reaper with tracked worker PID coordination (WNOHANG loop to ECHILD, o |
| b6-t04 | B6-T04: Python SDK Core | COMPLETE | 11 tests | 0 adversary breaks |
| b6-t05 | B6-T05: Structured Failure Context and Redaction | COMPLETE | 6 tests | 4 adversary breaks found | 3 HIGH | 1 MEDIUM | - Implementation: enumerated failure categories (task_failed, tool_failed, saas_failed, mcp_failed,  |

## Block-End Verification
- ### Findings - **Verifier finding (verifier 7e)**: `mcp_denied` failure category was not propagated through the full invoke path. When the SDK (T04) called `mcp()` with a denied server, the harness logged the denial but the structured `FailureContext.category` was not set to `mcp_denied` in the end-
- VERIFY PASS (after fix 7e): - build PASS, test PASS, race PASS - golangci-lint 0 issues (fresh cache) - osv: No issues found (5 daemon-side CVEs suppressed with accurate reasoning) - All T01-T05 adversary tests pass on merged main - Cross-subtask integration: budget → reaper → failure-context chain 
- - No per-subtask verifier was run (Block 6+ local-first design — adversary catches per-subtask breaks, block-end verifier handles integration). - Adversary totals for the block: T01=3 breaks, T02=2 breaks, T03=0, T04=0, T05=4 breaks. All 9 breaks resolved by fix workers + verified. - T03 has 5 Linux

## Risk Analysis Summary
- - No per-subtask verifier was run (Block 6+ local-first design — adversary catches per-subtask breaks, block-end verifier handles integration). - Adversary totals for the block: T01=3 breaks, T02=2 breaks, T03=0, T04=0, T05=4 breaks. All 9 breaks resolved by fix workers + verified. - T03 has 5 Linux

## Commits
`1a95a7d`, `3faaba4`, `461f72b`, `48503e1`, `5ba0908`, `69308a2`, `970dab7`, `c5b52b1`, `cae0a6a`, `cc9446e`, `e4b11e8`, `f2b7ed8`
