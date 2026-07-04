# B16 Manual Testing — Resume Prompt (Session 4)

**Date:** 2026-07-04
**Session:** Autonomous bug fixing + remaining tests
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 96eb9bc (all prior fixes pushed to origin/main)

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
| BUG 7 | P0 | OPEN | Concurrent run limit (3/3). Completed runs stay tracked in daemon memory. trigger_invoke fails with HTTP 500 ResourceExhausted. Fix: untrack runs when they complete (in completion goroutine, not just Stop). |
| BUG 8 | P1 | OPEN | Agent ignores skill pointer. Pointer is in available_skills but agent (deepseek-v4-flash) doesn't call skill_view("agentpaas:deploy"). Root cause of BUG 9 and 10. Investigate: stronger pointer, SOUL.md injection, or CLI-level guard. |
| BUG 9 | P1 | OPEN | No egress confirmation. Agent uses wildcard *:443 policy instead of asking user and writing specific domain. Consequence of BUG 8 but also worth adding CLI guard (pack or init should warn on wildcard). |
| BUG 10 | P1 | OPEN | No LLM configuration step. Agent builds pure Python without checking if LLM needed. Consequence of BUG 8. |
| BUG 11 | P0 | OPEN | trigger_invoke does not return agent response payload. CLI only shows run_id + status. Actual weather data is invisible. harness-audit.jsonl is 0 bytes. Fix: CLI should display invoke response, or store it in run dir. |

### Tests Run

| Test | Status | Notes |
|------|--------|-------|
| T1 | PARTIAL | Plugin install works, daemon starts, pack works, run completes. BUT: agent skipped onboarding (BUG 8/9/10), trigger_invoke hit BUG 7 then BUG 11. |
| T2 | SKIP | Needs API key. User can test manually. |
| T3 | NOT RUN | Egress denial test |
| T4 | NOT RUN | Absolute path init |
| T5 | NOT RUN | Recommend-patch parser (3 variants) |

### Fixes Applied

(none yet this session)

### Current Binary State

- Binaries at /usr/local/bin/agentpaas{,d} from commit 96eb9bc
- MD5 matches repo: ca6f0570946fe11eb9d095363ae93d84
- SDK at /usr/local/python/

## REMAINING WORK (in order)

### Phase 1: Fix P0 Bugs

#### BUG 7: Concurrent run limit — completed runs not untracked

**Problem:** `trigger_invoke` calls `Run()` internally, which creates a NEW
container per invoke. Completed/succeeded runs stay tracked in `s.runs` in
the daemon's memory — `untrackRun` is only called from `Stop`, not from the
completion goroutine. After 3 runs (even completed ones), the next invoke
fails with "concurrent run limit reached (3/3 active)".

**Fix:** In the daemon's run completion code (likely in
`internal/daemon/server.go` or wherever the run goroutine completes),
call `s.untrackRun(runID)` when the run reaches a terminal state
(completed/failed/errored). This frees the slot for the next run.

**Verify:**
```bash
# Start daemon
agentpaas daemon start
# Create a test agent and pack it
agentpaas init /tmp/bug7-test
agentpaas pack /tmp/bug7-test
# Run 4 times sequentially without stopping
for i in 1 2 3 4; do
  echo '{"test": '$i'}' > /tmp/bug7-payload.json
  agentpaas --json trigger invoke agent --payload /tmp/bug7-payload.json
  sleep 3
  agentpaas --json run list
done
# All 4 should succeed (each completes and frees its slot)
```

#### BUG 11: trigger_invoke does not return agent response payload

**Problem:** `agentpaas trigger invoke <name> --payload <file>` returns only
`{"run_id": "...", "status": "RUN_STATUS_RUNNING"}`. The actual agent response
(the invoke result) is not displayed. The harness-audit.jsonl in the run dir
is 0 bytes. There is no way to see what the agent actually returned.

**Investigation needed:**
1. Check the trigger invoke HTTP handler — does the response body contain the
   agent's output? The trigger API likely calls the harness, gets a response,
   but the CLI may discard it.
2. Check `internal/cli/control.go` or wherever `trigger invoke` is handled.
3. The fix: either (a) CLI displays the invoke response, or (b) the daemon
   stores the invoke response in the run directory (e.g.,
   `~/.agentpaas/state/runs/<run_id>/invoke-response.json`), or both.

**Verify:**
```bash
echo '{"lat": 38.6754, "lon": -121.1767}' > /tmp/folsom.json
agentpaas --json trigger invoke weather-agent --payload /tmp/folsom.json
# Should show the weather data in the response, not just run_id + status
```

### Phase 2: Fix P1 Bugs (Onboarding Flow)

#### BUG 8: Agent ignores skill pointer

**Problem:** After plugin install + restart, the skill pointer
`~/.hermes/profiles/agentpaas-test/skills/agentpaas/SKILL.md` appears in
available_skills with description "Build, deploy, package, run, and govern AI
agents. Use when the user asks to build, create, deploy, pack, or run any
agent. You MUST load the full skill with skill_view(name=\"agentpaas:deploy\")."

But the agent (deepseek-v4-flash) does NOT call skill_view. It builds the
agent directly, skipping all onboarding steps (egress confirmation, LLM
config, credential check).

**This is the #1 blocker for the onboarding flow.**

**Investigation:**
1. Is this a model capability issue? deepseek-v4-flash may not be strong
   enough to follow "load this skill first" instructions. Test with a
   stronger model?
2. Is the pointer description not prominent enough? Could make it more
   aggressive, or add a SOUL.md instruction in the plugin's after-install
   that creates a SOUL.md entry telling the agent to always load the skill.
3. Alternative: the register() function could inject the skill content
   directly into the agent's context (like a forced skill load) instead of
   relying on the agent to call skill_view.

**Possible fixes (evaluate each):**
- Option A: Make register() write a SOUL.md snippet that says "When building
  agents, ALWAYS load skill_view('agentpaas:deploy') first" — SOUL.md is
  always injected into the system prompt.
- Option B: Have register() call a function that directly injects the
  SKILL.md content into the system prompt (if the Hermes plugin API
  supports it).
- Option C: Accept this as a model limitation and just make the CLI tools
  enforce the onboarding steps (e.g., pack refuses wildcard policies,
  init asks about egress interactively).
- Option D: Strengthen the skill pointer description to be even more
  imperative, and add a second pointer or a SOUL.md.

**The most robust fix is Option A (SOUL.md injection) because it doesn't
depend on the model following available_skills hints.**

#### BUG 9: No egress confirmation (wildcard policy)

**Problem:** Agent writes `domain: "*"` with `allow_wildcard: true` instead
of asking the user and writing a specific domain (e.g., api.weather.gov).

**Fix approach:** Even if BUG 8 is fixed, add a CLI-level guard:
- `agentpaas pack` should warn (or refuse) when policy.yaml contains a
  wildcard domain. Print: "WARNING: policy contains wildcard egress
  (domain: '*'). This allows the agent to access ANY HTTPS domain. Specify
  exact domains for production agents. Use --allow-wildcard to suppress
  this warning."
- Or `agentpaas validate` checks for wildcard and warns.

**Verify:** Pack an agent with wildcard policy, should see warning.

#### BUG 10: No LLM configuration step

**Problem:** Agent builds without checking if LLM is needed.

**Fix:** This is a consequence of BUG 8. If the skill loads correctly,
the onboarding flow includes LLM detection. No separate fix needed if
BUG 8 is resolved. But for the weather agent specifically, no LLM IS
needed (it just calls an API), so this is expected behavior IF the agent
followed the onboarding flow and explicitly decided "no LLM needed."

### Phase 3: Run Remaining Tests

After all P0/P1 bugs are fixed and binaries are rebuilt + installed:

#### T3: Egress denial test

```bash
# Full clean slate
pkill -f agentpaasd; sleep 1
rm -rf ~/.agentpaas/state/agents/* ~/.agentpaas/state/runs/* \
  ~/.agentpaas/state/audit-checkpoint-key.der 2>/dev/null
rm -rf /tmp/egress-denial-test 2>/dev/null
cd ~/projects/agentpaas && bash scripts/reset-agentpaas-test.sh

# Create an agent that tries to reach a domain NOT in its policy
agentpaas daemon start
agentpaas init /tmp/egress-denial-test
# Write agent code that calls httpbin.org (or example.com)
cat > /tmp/egress-denial-test/main.py << 'EOF'
from agentpaas_sdk import agent
import requests

@agent.on_invoke
def handle_invoke(payload):
    try:
        resp = requests.get("https://httpbin.org/get", timeout=10)
        return {"status": "OK", "code": resp.status_code}
    except Exception as e:
        return {"status": "ERROR", "message": str(e)}
EOF
echo "requests>=2.31.0" > /tmp/egress-denial-test/requirements.txt

# Write policy that only allows api.weather.gov (NOT httpbin.org)
cat > /tmp/egress-denial-test/policy.yaml << 'EOF'
version: "1"
agent:
  name: "egress-test"
  description: "Test egress denial"
egress:
  - domain: "api.weather.gov"
    ports:
      - 443
credentials: []
mcp_servers: []
hooks: []
ingress: []
EOF

agentpaas validate /tmp/egress-denial-test
agentpaas pack /tmp/egress-denial-test
agentpaas run egress-test

# Wait for completion
sleep 8
agentpaas status <run_id>

# The agent should have been DENIED access to httpbin.org
# Check with explain-denial
agentpaas explain-denial --run-id <run_id> --destination httpbin.org:443

# Verify gateway config has hostname routing (not just wildcard)
cat ~/.agentpaas/state/runs/<run_id>/gateway-config/config.yaml
# Should show api.weather.gov route + catch-all denied route
```

**Expected:** Agent's HTTP call to httpbin.org is blocked by the gateway.
The run may complete (exit 0) but the agent returns an error about
connection refused or timeout. The explain-denial tool shows the denial.

#### T4: Absolute path init

```bash
rm -rf /tmp/test-abs
agentpaas init /tmp/test-abs --from-code --noninteractive
# Verify:
ls /tmp/test-abs/agent.yaml
ls /tmp/test-abs/main.py
ls /tmp/test-abs/policy.yaml
cat /tmp/test-abs/main.py  # Should have @agent.on_invoke
cat /tmp/test-abs/policy.yaml  # Should have default-deny egress: []
cat /tmp/test-abs/agent.yaml  # Should NOT have entry: main:app
```

**Expected:** All files scaffolded correctly with SDK pattern and default-deny policy.

#### T5: Recommend-patch parser (3 variants)

```bash
# Need a run_id that had a denial. Use the T3 run.

# Variant 1: Specific domain
agentpaas recommend-patch --run-id <run_id> --destination httpbin.org:443
# Should suggest adding httpbin.org:443 to egress list

# Variant 2: Domain without port
agentpaas recommend-patch --run-id <run_id> --destination httpbin.org
# Should suggest adding httpbin.org:443

# Variant 3: Wildcard
agentpaas recommend-patch --run-id <run_id> --destination "*"
# Should suggest adding wildcard or explain why wildcard is dangerous
```

**Expected:** Each variant returns a valid policy patch suggestion.

### Phase 4: Rebuild + Install + Final Verification

After all fixes:

```bash
cd ~/projects/agentpaas

# Rebuild with -a (force, no cache)
go build -a -o bin/agentpaas ./cmd/agentpaas
go build -a -o bin/agentpaasd ./cmd/agentpaasd

# Install
sudo cp -f bin/agentpaas bin/agentpaasd /usr/local/bin/

# Verify binaries match
md5 bin/agentpaasd /usr/local/bin/agentpaasd  # MUST match
md5 bin/agentpaas /usr/local/bin/agentpaas    # MUST match

# Verify SDK
ls /usr/local/python/agentpaas_sdk/

# Commit + push all fixes
git add -A
git commit -m "B16: Fix BUG 7,8,9,10,11 + pass T3,T4,T5"
git push origin main
```

### Phase 5: Leave Clean Profile for User

```bash
pkill -f agentpaasd; sleep 1
rm -rf ~/.agentpaas/state/agents/* ~/.agentpaas/state/runs/* \
  ~/.agentpaas/state/audit-checkpoint-key.der 2>/dev/null
rm -rf ~/weather-agent /tmp/weather-test-agent /tmp/egress-denial-test \
  /tmp/test-abs /tmp/bug7-test /tmp/folsom-payload.json 2>/dev/null
cd ~/projects/agentpaas && bash scripts/reset-agentpaas-test.sh
# Delete any sessions created by hermes commands
hermes -p agentpaas-test sessions list | grep -oE '20260703_[0-9]{6}_[a-f0-9]{6}' | \
  while read sid; do hermes -p agentpaas-test sessions delete "$sid" --yes; done
rm -f ~/.hermes/profiles/agentpaas-test/SOUL.md
```

## KEY CONTEXT

### What the user will do manually when they return

1. `hermes -p agentpaas-test` (fresh session, clean profile)
2. "Install AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas"
3. /quit, restart
4. "Build a weather agent that calls the NWS API"
5. Agent should follow onboarding: egress confirmation, specific policy, pack, run
6. "Find the weather for Folsom, CA using the weather agent"
7. Agent should trigger_invoke and return actual weather data

### What "passing" means

- T1: Full E2E through the agent. Plugin install, skill loads, egress
  confirmation (specific domain, not wildcard), pack, run, trigger_invoke
  with visible response payload.
- T3: Agent with denied domain gets blocked by gateway. explain-denial works.
- T4: init with absolute path scaffolds correct files.
- T5: recommend-patch returns valid suggestions for 3 variants.

### Build rules (CRITICAL)

- Use `go build -a` (not `make build`) for runtime fixes — Go cache produces
  stale binaries
- Always verify: `md5 bin/agentpaasd /usr/local/bin/agentpaasd` must match
- After binary upgrade: `rm -f ~/.agentpaas/state/audit-checkpoint-key.der`
  before restarting daemon
- SDK must be at /usr/local/python/ — verify after install

### Testing methodology

- CLI-level tests (direct agentpaas commands) prove the binary works
- Agent-level tests (through hermes -p agentpaas-test) prove the onboarding works
- Both must pass. The user tests at agent level manually.
- When testing through the agent, ALWAYS verify run status after deploy
- NEVER test with local python3 — everything must go through AgentPaaS

### Files to investigate for each bug

- BUG 7: `internal/daemon/server.go` (run tracking, completion goroutine)
  Search for `untrackRun`, `trackRun`, `s.runs`
- BUG 11: `internal/cli/control.go` (trigger invoke CLI command),
  `internal/daemon/` (trigger HTTP handler, invoke response)
  Search for `trigger`, `invoke`, `TriggerInvoke`
- BUG 8: `integrations/hermes-plugin/` (register function, after-install.md)
  The register() function creates the skill pointer. Need stronger mechanism.
- BUG 9: `internal/cli/control.go` or `internal/pack/` (pack validation),
  `internal/cli/init.go` (init scaffolding)
