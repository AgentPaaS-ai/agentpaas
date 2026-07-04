# B16 Manual Testing — Resume Prompt (Session 3)

**Date:** 2026-07-03
**Session:** Continuing B16 fix round 3 manual testing
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 96eb9bc (all fixes pushed to origin/main)

## START HERE

Continue manual testing T1-T5. The agentpaas-test profile is clean and ready.
All bugs found in prior sessions are fixed and pushed. Binaries are installed.

## WHAT'S FIXED (all merged + pushed)

- BUG 1 (T4-20): Scaffolded main.py now uses @agent.on_invoke SDK pattern
- BUG 2 (T4-20): Removed meaningless `entry: main:app` from scaffolded agent.yaml
- BUG 3 (T4-20): Init now scaffolds default-deny policy.yaml
- BUG 4 (T4-25): Plugin register() deterministically creates local skill pointer
  in the active profile so it appears in <available_skills>. No reliance on
  after-install.md. Uses hermes_cli.profiles.get_active_profile_name().
- BUG 5 (T4-20): Reset script cleans leftover agent dirs + deployed state +
  run state + checkpoint key
- BUG 6 (T4-21): `agentpaas run` accepts project path, image digest, OR agent
  name via resolveRunTarget() in control.go
- T4-22: Skill pointer description triggers on "build/create agent" not just "AgentPaaS"
- T4-23: SKILL.md pitfalls section has daemon checkpoint key fix, run-by-path,
  SDK pattern, concurrent run limit
- T4-24: Reset script cleans ~/.agentpaas/state/runs/ + audit-checkpoint-key.der

## STILL OPEN

- BUG 7: trigger_invoke creates a new container per invoke instead of reusing
  running container. Completed runs stay tracked in daemon memory, consuming
  the 3-slot limit. Not yet fixed — for now, agent should stop old runs before
  invoking, or restart daemon between test cycles.

## MANUAL TESTING CHECKLIST

The test profile is `agentpaas-test`. It is currently fully clean:
- No sessions, no plugins, no skill pointer, no SOUL.md
- No daemon running, no weather agents, no deployed state
- Binaries at /usr/local/bin/agentpaas{,d} from 96eb9bc

### FULL CLEAN SLATE (do this before EVERY test run, without being asked)

```bash
pkill -f agentpaasd 2>/dev/null
rm -rf ~/weather-agent /tmp/weather-test-agent /tmp/e2e-final-test /tmp/deps-test \
  /tmp/scaffold-test /tmp/egress-denial-test /tmp/test-abs /tmp/scaffold-verify \
  /tmp/run-path-test 2>/dev/null
rm -rf ~/.agentpaas/state/agents/* ~/.agentpaas/state/runs/* \
  ~/.agentpaas/state/audit-checkpoint-key.der 2>/dev/null
cd ~/projects/agentpaas && bash scripts/reset-agentpaas-test.sh
# Delete any remaining sessions
hermes -p agentpaas-test sessions list | grep -oE '20260703_[0-9]{6}_[a-f0-9]{6}' | \
  while read sid; do hermes -p agentpaas-test sessions delete "$sid" --yes; done
rm -f ~/.hermes/profiles/agentpaas-test/SOUL.md
rm -rf ~/.hermes/profiles/agentpaas-test/checkpoints/* \
  ~/.hermes/profiles/agentpaas-test/cron/* \
  ~/.hermes/profiles/agentpaas-test/skills/agentpaas 2>/dev/null
```

### T1: Full weather agent build from scratch (onboarding flow E2E)

1. Start: `hermes -p agentpaas-test`
2. Prompt: "Install AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas"
3. Agent should: install plugin, add toolset, tell user to restart
4. Restart: `/quit` then `hermes -p agentpaas-test`
5. Verify: `/skills list` should show `agentpaas-deploy`
6. Prompt: "Build a weather agent that calls the NWS API"
7. Agent should (per SKILL.md loaded via skill pointer):
   - Load skill_view("agentpaas:deploy")
   - Start the daemon (or fix checkpoint key if needed)
   - Use agentpaas_init to scaffold the project
   - Write @agent.on_invoke SDK pattern code
   - Confirm egress: "This agent accesses api.weather.gov. Allow?"
   - Write policy.yaml with api.weather.gov only (NOT wildcard)
   - Check for credentials (NWS is free, skip)
   - Check for LLM (none needed, skip)
   - Pack and run the agent
   - Check run status with agentpaas_status
   - Report actual run result (not local python3 test)
8. Then: "Find the weather for Folsom, CA using the weather agent"
   - Agent should trigger_invoke the deployed agent
   - May hit BUG 7 (concurrent run limit) — if so, note it and stop old runs

### T2: Agent with LLM (if user has an API key)
### T3: Egress denial test
### T4: Absolute path init: `agentpaas init /tmp/test-abs --from-code --noninteractive`
### T5: recommend-patch parser (3 variants)

## KEY PITFALLS

- Daemon checkpoint key corruption after kill/upgrade: `rm -f ~/.agentpaas/state/audit-checkpoint-key.der`
- Plugin skills don't appear in available_skills — the register() fix creates
  a local pointer skill that does appear
- Concurrent run limit is 3 — completed runs stay tracked, stop old runs or
  restart daemon
- `go build -a` (not `make build`) for runtime fixes
- Verify binary install: `md5 bin/agentpaasd /usr/local/bin/agentpaasd`
- Agent code MUST use @agent.on_invoke — plain app()/main() fails
- after-install.md only shows if installed from repo root URL, not subdir URL
  (this is why the register() fix is critical)
