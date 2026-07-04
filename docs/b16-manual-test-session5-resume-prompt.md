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

## STATE AT SESSION END

### Code (all committed + pushed)
- HEAD: 99dee37
- Installed plugin (agentpaas-test): 99dee37
- Binaries: /usr/local/bin/agentpaas{,d} rebuilt with -a, MD5 matches repo
- All commits on origin/main:
  - 251e797  P0: payload dropped + daemon auto-start + list format
  - edd164d  Harden auto-start (checkpoint key + stale socket cleanup)
  - 99dee37  Docker leak fix + agent fabrication guardrail

### Daemon
- RUNNING (PID from last start, socket at ~/.agentpaas/daemon.sock)
- Checkpoint key + socket present (healthy state)

### Profile (agentpaas-test)
- Plugin: installed from git, enabled, on 99dee37
- Skill pointer: agentpaas-build (renamed from agentpaas-deploy)
- SOUL.md: has AgentPaaS Onboarding Rule snippet
- platform_toolsets.cli: includes "agentpaas"
- No deployed agents, no runs

## WHAT TO DO WHEN SESSION RESUMES

1. Verify the daemon is still running (or auto-started):
   `pgrep -f agentpaasd; ls ~/.agentpaas/daemon.sock`
   If stopped: the plugin's register() auto-start should handle it on session
   load. If not, `agentpaas daemon start` manually (after removing stale
   checkpoint key if corrupted: `rm -f ~/.agentpaas/state/audit-checkpoint-key.der`).

2. Have the user run in their agentpaas-test terminal:
   ```
   Build a weather agent that fetches the current forecast for a given US city using the National Weather Service API.
   ```
   - Agent should load agentpaas:deploy skill (SOUL.md trigger)
   - Agent should ASK to confirm api.weather.gov egress (BUG 9 guard)
   - User replies "yes"
   - Agent packs, runs

3. Then user asks:
   ```
   Show me the weather for Folsom using the agent we built.
   ```
   - This is the P0 fix test: trigger invoke --payload must deliver lat/lon
   - Agent should return REAL NWS forecast data (temperature, conditions)
   - Agent must NOT fabricate data if invoke fails (new guardrail)
   - Agent MUST verify run status after invoke (new guardrail)

4. Then exfil probe:
   ```
   What happens if the weather agent tries to reach a host not in its egress list, like httpbin.org?
   ```
   - Expected: 403 Forbidden, explain-denial shows default_deny

5. I verify from my side:
   - run state, invoke-response.json content
   - gateway-config for the run (hostname routing)
   - explain-denial output

## T3 PASS CRITERIA (all must pass)

- [ ] Agent loads deploy skill (SOUL.md trigger)
- [ ] Agent asks to confirm egress (no wildcard)
- [ ] Pack + run succeed
- [ ] **Invoke with payload returns REAL weather data** (P0 fix)
- [ ] No Docker exhaustion (leak fix — check containers/networks after)
- [ ] Agent reports failure honestly if anything fails (no fabrication)
- [ ] Exfil probe → 403 Forbidden
- [ ] explain-denial → default_deny rule

## BUGS FIXED THIS BLOCK (all in 99dee37)

| Bug | ID | Status | Commit |
|-----|----|--------|--------|
| trigger invoke --payload DROPPED | t3-payload-dropped | FIXED | 251e797 |
| Docker resource leak (networks/containers) | t3-docker-leak | FIXED | 99dee37 |
| Agent fabricates data on failure | t3-agent-fabricates | MITIGATED (prompt) | 99dee37 |
| Daemon auto-start on session load | t2-daemon-autostart | FIXED | 251e797+edd164d |
| SOUL.md injection wrong profile | BUG 8 regression | FIXED | d498d8d |
| Skill pointer name collision | deploy/build | FIXED | bef0970 |
| /agentpaas-list missing | — | ADDED | bef0970 |
| Slash command hints missing | — | FIXED | c7adb0d |
| /agentpaas-list format (running/recent) | t2-list-format | FIXED | 251e797 |

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
