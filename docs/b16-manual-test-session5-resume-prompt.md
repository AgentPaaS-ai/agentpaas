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

## STATE AT SESSION END (Session 5, continued)

### Code (all committed + pushed)
- HEAD: eff35f9
- Binaries: /usr/local/bin/agentpaas{,d} match repo (md5 7423aa1c...)
- All commits on origin/main:
  - 029505c  B16-T2: Fix daemon auto-start false-positive on stale socket
  - 96e7ece  B16-T3: Fix trigger_invoke payload schema + strengthen anti-fab
  - eff35f9  B16-T2: Fix daemon crash-loop on broken audit chain
  (plus docs commits c28408b, 9d2ab32)

### T1-T4 STATUS: ALL PASS

- T1: Plugin install — PASS (clean install, no homework)
- T2: Doctor 6/6 — PASS (daemon genuinely live)
- T3: Weather agent — PASS (build + invoke + exfil)
- T4: Immutable redeploy — PASS (new image digest, new code live)

### Bugs found but NOT yet fixed
- **bug-echo-payload-empty**: Echo agent invoke-response shows
  "Echo: " (empty message) instead of "Echo: hello world". The
  payload's "message" field is not reaching the agent code. Same
  class as t3-schema-payload-path but may be a different layer.
  Also: agent misreported the response (said "hello world" when
  actual was empty) — minor fabrication, anti-fab rule didn't catch
  it because the response structure looks similar.
  Status: PENDING — investigate after T4.
- **task-gateway-restart**: Gateway restart during plugin install
  takes forever. Need to investigate why and whether it's necessary.
  Status: PENDING.

### Profile (agentpaas-test)
- Plugin installed, echo-agent deployed
- Daemon running with fresh audit chain
- Ready for T5 (or bug investigation)

## WHAT TO DO WHEN SESSION RESUMES

1. Verify daemon is running: `agentpaas daemon status`

2. Investigate bug-echo-payload-empty (P0 — blocks T5+ invoke tests):
   - Echo agent returns "Echo: " (empty) instead of "Echo: hello world"
   - Payload "message" field not reaching agent code
   - Run: `agentpaas trigger invoke echo-agent --payload <file with {"message":"test"}>`
   - Check: `cat ~/.agentpaas/state/runs/<run_id>/invoke-response.json`
   - Trace payload through: CLI → REST → trigger server → invokeFunc →
     RunRequest.TriggerPayload → invokeAgent → buildInvokePayload → harness
   - The weather agent worked (Folsom data returned). Difference: weather
     agent reads "city" key, echo agent reads "message" key. Both should
     work the same way. Check if the issue is in how the harness delivers
     the payload to the Python SDK.

3. After bug fix, continue with T5 (B16-LC03: Trigger/API/event/cron
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
