# Block 16 — Manual Testing & Bug Fixes

**Status:** COMPLETE — all fixes merged to main, v0.1.0 tag moved
**Date:** 2026-07-03 to 2026-07-04
**Block scope:** End-to-end manual testing of the full AgentPaaS lifecycle, bug discovery and fixes

## Scope

Block 16 was the manual testing phase. A tester (using the agentpaas-test profile with a fresh
plugin install from GitHub) exercised the full lifecycle: plugin install → onboarding → policy
authoring → pack → run → invoke → audit → timeline. Bugs were found, triaged, fixed, and
verified across multiple test rounds.

## Test Rounds Summary

### Round 1 — Initial Manual Test (T1-T3)
Found 6 bug clusters:
| Fix | Priority | Description |
|-----|----------|-------------|
| T3f | P0 | Bundle Python SDK in container image + bridge app() entry point |
| T3a | BLOCKER | Sanitize log messages to valid UTF-8 before proto marshal |
| T3b | BLOCKER | Add `agentpaas status <run-id>` CLI subcommand |
| T3c | BLOCKER | Trigger invoke inline JSON payload handling (plugin-side) |
| T3d | — | Surface failure reason in run_stop timeline event |
| T3e | — | Fix daemon auto-start false-positive on stale socket |

**All 6 fixes complete, merged, pushed.** Binaries rebuilt with `go build -a`.

### Round 2 — Full Lifecycle Test
| Test | Result | Notes |
|------|--------|-------|
| T1a: Plugin install from GitHub | PASS | Tools work, doctor returns 6/6 healthy |
| T2: Policy schema (CLI) | PASS (with bugs) | All 4 templates scaffold+validate. policy show broken. |
| T3: E2E lifecycle | PASS | init→policy→validate→pack→run→invoke→timeline all work |
| T3: Cron add/list/remove | PASS | Works via CLI |
| T3: Secret add/list/remove | PASS | Works via CLI |
| T3: LLM configure | PASS (cosmetic) | Config written but leaves duplicate commented-out lines |
| T3: Audit query/export | PASS | Works via CLI |

New bugs found: deps locked but never installed in container (CRITICAL), pack CLI timeout too
short (30s→5min), Docker build errors swallowed, resolveSDKDir() for binary installs, ExecWithStdin
corrupting payload with multiplexed frame header.

### Round 3 — All 30 Plugin Tools
| Test | Result | Notes |
|------|--------|-------|
| All 30 plugin tools | 28 PASS, 2 BUG | See bugs below |
| Full lifecycle (stdlib agent) | PASS | init→policy→validate→pack→run→invoke→timeline |
| Full lifecycle (deps agent) | PARTIAL | Pack succeeds, run fails: egress proxy denies |
| Doctor via agent | PASS | Returns structured JSON |
| E2E weather agent via agent | PASS | Full lifecycle with weather demo |

Bugs fixed: daemon crash-loop on broken audit chain, trigger_invoke empty payload, slow
gateway restart, Docker resource leak, agent fabrication guardrail, cron payload support,
auto-ensure toolset in register(), proactive LLM detection + pre-pack gate, xAI provider
name mismatch (xiai→xai), Nous provider added, actionable LLM error messages, LLM HTTP
timeout 5s→120s, SDK embedded in daemon binary, OpenRouter provider, egress policy fix,
agent name auto-derived from dir, secret whitespace trimming.

## Final State

All fixes merged to main. Last commit: 1bd016b "B16-T6: add OpenRouter provider, fix egress
policy, fix agent name, trim secret whitespace". Verified end-to-end: init → policy (allow-llm)
→ pack → run → invoke with OpenRouter deepseek/deepseek-v4-flash. Agent returned correct LLM
response ('Paris' for 'What is the capital of France?').

## Key Fixes by Impact

**P0 Blockers (nothing works without these):**
- Python SDK not bundled in container (ModuleNotFoundError) → SDK embedded via go:embed
- Docker build errors swallowed → surface errors instead of JSON decode
- Pack CLI timeout 30s → 5min
- ExecWithStdin corrupting payload → fix multiplexed frame header handling

**Security/Correctness:**
- xAI provider name mismatch (adapter was 'xiai' not 'xai')
- Egress policy blanket-allowed all LLM providers → only allow configured provider
- Secret values had trailing newline → trim stdin whitespace
- Agent fabrication guardrail strengthened

**UX:**
- Agent name auto-derived from project dir basename (not always "agent")
- Actionable LLM error messages (auth/rate-limit/model failures)
- LLM HTTP timeout 5s → 120s
- OpenRouter added as recommended provider (keys don't expire)

## Commits (B16, latest 10)

- 1bd016b: OpenRouter provider, egress policy, agent name, secret whitespace
- e8087e8: Embed Python SDK in daemon binary for brew-only installs
- 5878e7f: Open-source: rename Go module, remove PII
- 0637ea0: Open-source cleanup (269 files removed)
- 0f1620c: Proactive LLM detection + pre-pack gate
- 2ec08b8: xai provider name mismatch fix
- 62b8e4c: Add Nous Research provider
- 096a8a8: LLM HTTP timeout 5s→120s
- a39cfc1: Actionable LLM error messages
- d22c6b9: trigger_invoke empty payload + gateway restart

## Full Details

- [Fix Round 1 Summary](../archive/session-history/b16-session-history.md) — All 6 fix details
- [Round 2 Findings](../archive/session-history/b16-session-history.md) — 16KB of test results
- [Round 3 Findings](../archive/session-history/b16-session-history.md) — 8KB of test results
- [Session History](../archive/session-history/b16-session-history.md) — All resume prompts from sessions 1-5
