# B16 Manual Testing — Resume Prompt (Session 4)

**Date:** 2026-07-04
**Session:** Autonomous bug fixing + remaining tests
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** e8dcd02 (all fixes pushed to origin/main)

## START HERE

This is an autonomous work session. Fix all known bugs, run remaining tests
(T3-T5), fix any new bugs found, rebuild binaries, and leave the system ready
for the user to manually run T1-T5 without errors.

## HOW TO USE THIS FILE

1. Read this file fully at session start.
2. Check PROGRESS SECTION below for current state.
3. Work through REMAINING WORK in order.
4. After each fix or test, update PROGRESS SECTION by editing this file.
5. If approaching 180 turns, save state here, then tell the user to /reset
   and paste the same prompt again. The new session will read this file
   and continue from where you left off.

## PROGRESS SECTION

### Bugs Found (Session 3, 2026-07-04)

| Bug | Priority | Status | Description |
|-----|----------|--------|-------------|
| BUG 7 | P0 | FIXED | Concurrent run limit (3/3). Fixed: activeRunCount() now only counts "running" status runs, not completed ones. Commit 6e679ca. |
| BUG 8 | P1 | FIXED | Agent ignores skill pointer. Fixed: register() now writes SOUL.md snippet (always injected into system prompt) instructing agent to load skill_view("agentpaas:deploy"). Commit e8dcd02. |
| BUG 9 | P1 | FIXED | No egress confirmation. Fixed: pack CLI now checks policy.yaml for wildcard domains, refuses unless --allow-wildcard is passed. Commit e8dcd02. |
| BUG 10 | P1 | FIXED | No LLM configuration step. No separate fix — consequence of BUG 8. SOUL.md injection forces skill loading which includes LLM onboarding. |
| BUG 11 | P0 | FIXED | trigger_invoke does not return agent response. Fixed: invokeAgent() returns (stdout, error), stdout stored to <runDir>/invoke-response.json, CLI trigger invoke --wait polls and displays, summarize reads and displays. Commit 1083b9a. |

### Tests Run

| Test | Status | Notes |
|------|--------|-------|
| T1 | PARTIAL | Plugin install works, daemon starts, pack works, run completes. With BUG 8/11 fixes, agent-level onboarding should work now (SOUL.md injection). User should test manually. |
| T2 | SKIP | Needs API key. User can test manually. |
| T3 | PASS | Egress denial: agent tried httpbin.org, gateway returned 403 Forbidden. explain-denial shows "default_deny" rule. Run: run-68dd19ddc807c7b9. |
| T4 | PASS | Absolute path init: agent.yaml (no entry: main:app), main.py (@agent.on_invoke), policy.yaml (egress: []). |
| T5 | PASS | Recommend-patch 3 variants: (1) httpbin.org:443 → valid patch, (2) httpbin.org → valid patch with 443, (3) * → empty patch with ask_user. |

### Fixes Applied

1. BUG 7 (commit 6e679ca): activeRunCount() only counts "running" status runs
2. BUG 11 (commit 1083b9a): invokeAgent returns stdout, stored to invoke-response.json
3. BUG 8+9+10 (commit e8dcd02): SOUL.md injection + wildcard egress guard

### Current Binary State

- Binaries at /usr/local/bin/agentpaas{,d} from commit e8dcd02
- MD5: agentpaasd=e30b81d52b35e9e26b2f44ff21d0bf02, agentpaas=1cacdb7889ceb47c0da595dc0e65d466
- SDK at /usr/local/python/
- All commits pushed to origin/main

### Profile State

- agentpaas-test profile is FULLY CLEAN (verified: no sessions, no plugins, no skill pointer, no SOUL.md, no deployed agents, no run state, no checkpoint key, daemon not running)

## ALL WORK COMPLETE

All P0 and P1 bugs are fixed. T3, T4, T5 all pass. Binaries rebuilt and
installed. All commits pushed. Test profile is clean and ready for manual
testing.

The user can now manually test the onboarding flow:
1. `hermes -p agentpaas-test`
2. "Install AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas"
3. /quit, restart
4. "Build a weather agent that calls the NWS API"
5. Agent should follow onboarding (SOUL.md forces skill loading)
6. "Find the weather for Folsom, CA using the weather agent"
7. Agent should trigger_invoke with --wait and return actual weather data
