# B16 Manual Testing — Resume Prompt (Session 5)

**Date:** 2026-07-04
**Session:** Continue T3 manual testing from user perspective, then T4-T10
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 99dee37 (all fixes pushed to origin/main)
**Profile:** agentpaas-test

## START HERE

The user is manually testing AgentPaaS Block 16 (T1-T10). We are mid-T3.
All code fixes are done, binaries rebuilt, plugin updated. The user needs to
restart their Hermes session to pick up the latest plugin (99dee37) and then
continue the manual test flow.

## STATE AT SESSION END (Session 5, final)

### Code (all committed + pushed)
- HEAD: d22c6b9
- Binaries: /usr/local/bin/agentpaas{,d} match repo (md5 42b09737...)
- All commits on origin/main:
  - 029505c  B16-T2: Fix daemon auto-start false-positive on stale socket
  - 96e7ece  B16-T3: Fix trigger_invoke payload schema + strengthen anti-fab
  - eff35f9  B16-T2: Fix daemon crash-loop on broken audit chain
  - d22c6b9  B16-T4: Fix trigger_invoke empty-payload + slow gateway restart

### T1-T4 STATUS: ALL PASS

- T1: Plugin install — PASS (clean install, no homework)
- T2: Doctor 6/6 — PASS (daemon genuinely live)
- T3: Weather agent — PASS (build + invoke + exfil)
- T4: Immutable redeploy — PASS (new image digest, new code live)

### ALL BUGS FIXED THIS SESSION (5 total)

1. t2-stale-socket-autostart (029505c): _ensure_daemon_running() used
   os.path.exists() — stale socket fooled it. Fixed: connects to socket.
2. t3-schema-payload-path (96e7ece): schema said "path to file" — agent
   didn't pass inline JSON. Fixed: schema says "inline JSON or file path".
3. t3-fab-guardrail-weak (96e7ece): anti-fab rule in SKILL.md only.
   Fixed: rule now in SOUL.md snippet (always present).
4. t2-audit-chain-recovery (eff35f9): daemon crash-loops on broken audit
   chain. Fixed: recoverFromCorruption() truncates at break, re-replays.
5. t4-empty-payload-warning (d22c6b9): trigger_invoke without payload
   returned empty response, agent fabricated data. Fixed: warning field
   added when no payload provided. Also: after-install.md now uses
   ensure-toolset.py instead of hermes config set (avoids slow gateway
   restart).

### Profile (agentpaas-test)
- FULLY CLEAN — reset to baseline, ready for fresh T1 install
- No plugin, no skill pointer, no SOUL.md, no sessions, no agents, no runs
- No leftover project dirs
- Daemon stopped, all state removed
- Binaries updated with all fixes

## WHAT TO DO WHEN SESSION RESUMES

1. Verify daemon is running: `agentpaas daemon status`

2. Have the user run T1 (plugin install) from their agentpaas-test
   terminal:
   ```
   Install AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas
   ```
   This verifies the full clean-install path. The agent should now use
   ensure-toolset.py (no slow gateway restart) instead of hermes config set.

3. Then T2: `Run agentpaas_doctor to check if my AgentPaaS setup is healthy`
   - Verify 6/6 checks pass
   - Verify daemon auto-started (register() fires on session load)
   - Verify daemon socket is genuinely live (not stale)

4. Then T3: Build weather agent + invoke with Folsom + exfil probe.
   All 3 T3 sub-steps should pass cleanly with the payload schema fix
   and anti-fabrication guardrail.

5. Then T4: Build echo agent, change prompt, repack, verify new digest.
   The empty-payload warning should prevent fabrication if the agent
   forgets to pass the payload.

6. After T1-T4 pass, continue with T5 (B16-LC03: Trigger/API/event/cron
   launch surface).

5. I verify from my side:
   - run state, invoke-response.json content
   - gateway-config for the run (hostname routing)
   - explain-denial output

## T3 PASS CRITERIA (all pass)

- [x] Agent loads deploy skill (SOUL.md trigger)
- [x] Agent asks to confirm egress (no wildcard)
- [x] Pack + run succeed
- [x] Invoke with payload returns REAL weather data (P0 fix + schema fix)
- [x] No Docker exhaustion (leak fix — check containers/networks after)
- [x] Agent reports failure honestly if anything fails (no fabrication)
- [x] Exfil probe → 403 Forbidden
- [x] explain-denial → default_deny rule

## BUGS FIXED THIS BLOCK

| Bug | ID | Status | Commit |
|-----|----|--------|--------|
| trigger invoke --payload DROPPED | t3-payload-dropped | FIXED | 251e797 |
| Docker resource leak (networks/containers) | t3-docker-leak | FIXED | 99dee37 |
| Agent fabricates data on failure | t3-agent-fabricates | MITIGATED→STRENGTHENED | 99dee37 → 96e7ece |
| Daemon auto-start on session load | t2-daemon-autostart | FIXED | 251e797+edd164d |
| Daemon auto-start false-positive on stale socket | t2-stale-socket-autostart | FIXED | 029505c |
| trigger_invoke payload schema misleading | t3-schema-payload-path | FIXED | 96e7ece |
| Anti-fabrication guardrail in SKILL only (not SOUL) | t3-fab-guardrail-weak | FIXED | 96e7ece |
| SOUL.md injection wrong profile | BUG 8 regression | FIXED | d498d8d |
| Skill pointer name collision | deploy/build | FIXED | bef0970 |
| /agentpaas-list missing | — | ADDED | bef0970 |
| Slash command hints missing | — | FIXED | c7adb0d |
| /agentpaas-list format (running/recent) | t2-list-format | FIXED | 251e797 |

### Session 5 bug details

**t2-stale-socket-autostart (029505c):** `_ensure_daemon_running()` used
`os.path.exists(socket_path)` as liveness check. A stale socket file left
by `pkill -9` or unclean shutdown passed this check, so the function
returned early and the daemon stayed down. Doctor reported 6/6 healthy
while daemon was unreachable. Fix: now connects to the socket (0.5s
timeout) to verify liveness. If connection fails, socket is stale —
remove and restart.

**t3-schema-payload-path (96e7ece):** The `agentpaas_trigger_invoke`
schema said payload = "Optional path to a payload file". The agent
interpreted this as requiring a file path and didn't pass inline JSON
like `{"city": "Folsom"}`. The tool code already handles inline JSON
(checks if payload starts with `{`, writes to temp file), but the schema
didn't tell the agent. Fix: schema now says "inline JSON or file path"
with explicit example `{"city": "Folsom"}`.

**t3-fab-guardrail-weak (96e7ece):** The anti-fabrication rule was in
SKILL.md Pitfalls (line 317-324). The agent loaded the skill during the
build phase but didn't re-read it for the invoke step. After 10 failed
invokes (empty payload due to schema bug), the agent fabricated
realistic weather data (82°F, Sunny) instead of reporting the error.
Fix: added "AgentPaaS Anti-Fabrication Rule" to the SOUL.md snippet
injected by register(). SOUL.md is loaded fresh every turn, so the rule
is always present. The rule also covers "always verify run status."

## STILL OPEN (non-blocking for T3)

- **t3-checkpoint-loop**: daemon crashes on restart with corrupted checkpoint
  key. Auto-start works around it (removes stale key first), but the real fix
  belongs in the daemon binary (regenerate key if decrypt fails, not crash).
- **t2-invoke-spawns-new-run**: trigger invoke starts a NEW container instead
  of invoking the already-running agent. Design question: should invoke reuse
  the running container, or should run+invoke semantics be redefined?
- **post-b16-rich-input**: large files, Drive/cloud links, online URLs,
  videos, images. Payload capped at 1MB. Deferred to post-B16.

## AFTER T3

Continue with T4 (B16-LC05/UC04: Prompt-change immutable redeploy) —
already done as T2's second half, may just need confirmation. Then T5-T10.

T-card reference (from block16-manual-testing-setup.md):
- T4: B16-LC05/UC04 — Prompt/code change immutable redeploy
- T5: B16-LC03 — Trigger/API/event/cron launch surface
- T6: B16-UC02 — Secret-brokered SaaS/API action
- T7: B16-UC03 — Agentic repair loop
- T8: B16-UC05 — Long-running mixed-egress agent
- T9: B16-UC06 — Clean-machine install under 15 minutes
- T10: B16-UC07 — Policy authoring from scratch

## KEY SKILLS TO LOAD

- agentpaas-acceptance-testing (test flow, pitfalls, verification)
- agentpaas-build-rhythm (build discipline)
- systematic-debugging (if new bugs surface)
- cost-aware-model-selection (every session start)
