# B16 Fix Round 3 — Resume Prompt (Session 2: Manual Testing + Hardcoded Guards)

**Date:** 2026-07-03
**Session:** Continuing from B16 fix round 3 session 1
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 515482a (all T4-14..T4-19 + T4-16b fixes pushed to origin/main)

## START HERE

Manual testing is next. All code fixes are done, built, installed, and pushed.
Binaries at /usr/local/bin/agentpaas{,d} are from 515482a.

## WHAT'S DONE (all merged + pushed)

### T4-16 (CRITICAL): Gateway config compiled from agent's own policy.yaml
- Run handler reads policy.yaml from deployed agent dir (not global gateway.yaml)
- policy.yaml stored as sidecar in `~/.agentpaas/state/agents/<name>/policy.yaml`
- AgentLock has PolicyYAML field (not in signed canonical map — sidecar only)
- Commit: 7fc51e4

### T4-16b: Gateway compiler fixed for agentgateway v1.3.0 CONNECT proxy
- Old format (static host backends + networkAuthorization with dns.domain CEL) NEVER WORKED
  — dns.domain is not populated for CONNECT requests (verified by testing)
- New format: hostname-based routing with dynamic (DFP) backends + connect:route mode
- One route per allowed domain with `hostnames: [domain]` + `dynamic: {}` backend
- Catch-all denied route returns 403 for unmatched domains
- `frontendPolicies.connect.mode: route` enables CONNECT handling
- Commit: 71c4d03

### T4-17/18/19: Plugin SKILL.md build-time onboarding
- "Build-Time Agent Onboarding (MANDATORY)" section with direct imperatives
- Step 1: Confirm egress destinations (analyze code, present domains, write policy.yaml)
- Step 2: Procure API keys (detect needs, ask user, store via secret_add, test)
- Step 3: Configure LLM (detect needs, ask provider/model, configure agent.yaml, add egress)
- "Agent Code Structure (REQUIRED)" section — @agent.on_invoke SDK pattern
- Commits: f6e5be6, 98d4829

### T4-14: Absolute paths in init --from-code
- Removed CWD containment check from validateInitProjectPath
- Security still handled by symlink rejection + null byte checks
- Commit: 7f449bb

### T4-15: recommend-patch parser improvements
- Fallback domain extraction from any position in input
- Handles port suffixes, alternative verbs (add, enable, allow without "egress to")
- Commit: 6851045

### E2E Verified
- Weather agent (weather-test) packed, run, invoked successfully
- Agent reached api.weather.gov through gateway proxy, returned real alert data
- Gateway config correctly uses hostname routing + dynamic backends + connect:route

## PENDING: Hardcoded Platform Guards (not yet implemented)

User wants these added so a "dumb LLM" can't skip safety steps. These are
platform-level checks that enforce the OUTPUT of the SKILL.md instructions.

### HG-1: Reject wildcard egress at pack time (MED)
- If policy.yaml has `domain: "*"` without `allow_wildcard: true`, pack should FAIL
- Currently: compiler blocks via isWildcardDomainBlocked() but pack succeeds silently
- File: `internal/daemon/control_handlers.go` Pack handler, or `internal/pack/lock.go` computePolicyDigest
- Add explicit error: "wildcard egress requires --allow-wildcard-egress flag"

### HG-2: Validate credential references before run (MED)
- If agent.yaml has `llm.credential: "openai-api-key"`, Run handler should check Keychain
- Currently: buildInvokePayload() silently returns empty payload if credential missing
- Better: fail at run time with "credential 'X' not found — run agentpaas secret add first"
- File: `internal/daemon/control_handlers.go` Run handler (before container start)

### HG-3: Require policy.yaml for pack (LOW)
- If no policy.yaml exists, pack should fail (or require --no-egress flag)
- Currently: pack succeeds with empty policy digest
- File: `internal/daemon/control_handlers.go` Pack handler (line ~98-104)

## MANUAL TESTING CHECKLIST

The test profile is `agentpaas-test`. The plugin + toolset should already be
installed there from prior sessions.

### Test 1: Full weather agent build from scratch
Use the agentpaas-test profile (or a fresh Hermes session):
1. Tell Hermes: "Build a weather agent that calls the NWS API"
2. Hermes should (per SKILL.md):
   - Analyze the code and identify api.weather.gov as the egress domain
   - Ask: "This agent will access api.weather.gov. Allow?"
   - Generate policy.yaml with api.weather.gov egress rule
   - Check if NWS API needs a key (it doesn't — free API)
   - Check if agent needs an LLM (depends on code)
   - Pack and run the agent
3. The agent must successfully reach api.weather.gov and return real data

### Test 2: Agent with LLM (if user has an API key)
1. Tell Hermes: "Build an agent that summarizes weather data using GPT"
2. Hermes should:
   - Identify api.weather.gov + api.openai.com as egress
   - Ask for OpenAI API key
   - Store via secret_add, test it
   - Configure llm: section in agent.yaml
   - Add both domains to policy.yaml
3. Agent must reach both APIs and return summarized weather data

### Test 3: Egress denial
1. Deploy an agent with policy.yaml allowing only api.weather.gov
2. Try to make the agent call a different domain (e.g. evil.example.com)
3. Gateway should return 403 "egress denied: domain not in allowlist"

### Test 4: Absolute path init (T4-14)
```
agentpaas init /tmp/test-abs --from-code --noninteractive
```
Should succeed (previously failed with "project path must be under current directory")

### Test 5: recommend-patch parser (T4-15)
```
agentpaas recommend-patch "allow api.example.com"
agentpaas recommend-patch "add api.example.com to egress"
agentpaas recommend-patch "allow egress to api.example.com:443"
```
All should produce valid patches (previously only "allow egress to <domain>" worked)

## BINARIES

Current binaries at /usr/local/bin/agentpaas{,d} are from 515482a.
After any new fix: rebuild with `go build -a`, install, restart daemon.

```bash
cd ~/projects/agentpaas
go build -a -o bin/agentpaas ./cmd/agentpaas
go build -a -o bin/agentpaasd ./cmd/agentpaasd
sudo cp -f bin/agentpaas bin/agentpaasd /usr/local/bin/
agentpaas daemon stop; sleep 2; rm -f ~/.agentpaas/daemon.sock
rm -f ~/.agentpaas/state/audit-checkpoint-key.der  # if checkpoint key error
agentpaas daemon start
```

## CONTEXT FROM PRIOR SESSIONS

- See `docs/b16-manual-test-round2-findings.md` for T4-01..T4-13 bugs (ALL FIXED)
- See `docs/b16-manual-test-round3-findings.md` for T4-14..T4-19 bugs (ALL FIXED)
- See `docs/b16-fix-round1-complete.md` for RC1-RC6 fixes (ALL MERGED)
- agentpaas-test profile has plugin + toolset installed
- Test weather agent at /tmp/weather-test-agent/ (uses @agent.on_invoke SDK pattern)
- Daemon is running, binaries at 515482a
- KEY INSIGHT: agentgateway v1.3.0 needs connect:route + hostname routing + dynamic backends
  (see T4-16b commit 71c4d03 for the working config format)

## KEY PITFALLS

- `make build` can produce stale binaries. Always `go build -a`.
- Verify binary installation with `md5 bin/agentpaasd /usr/local/bin/agentpaasd`
- After daemon restart, check `agentpaas status` shows "ready"
- If daemon fails to start: `rm ~/.agentpaas/state/audit-checkpoint-key.der`
- Concurrent run limit is 3. Stop old runs before starting new ones.
- Agent code MUST use `@agent.on_invoke` — plain main() fails with
  "agent must register an invoke handler"
- Gateway config uses agentgateway v1.3.0 syntax: connect:route + hostname routing
