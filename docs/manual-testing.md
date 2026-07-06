# Manual Testing Guide

This guide walks through the full AgentPaaS lifecycle as a first-time
user would experience it. Each test verifies a specific capability with
real tool output — not agent self-report.

## Prerequisites

1. **Hermes Agent** installed: `brew install nousresearch/tap/hermes-agent`
2. **Docker** running: `brew install colima && colima start`
3. **AgentPaaS** installed: `brew install agentpaas-ai/tap/agentpaas`
4. **Quarantine cleared** (brew cask is not notarized):
   ```bash
   xattr -cr /opt/homebrew/bin/agentpaas
   ```
5. **OpenRouter API key** (free tier works): https://openrouter.ai/keys

## Setup

Launch Hermes:
```bash
hermes
```

## T1: Plugin Install

**What it tests:** A first-time user can install the AgentPaaS plugin
from GitHub and have all tools available after restart.

**Prompt:**
> Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas

**What happens:** Hermes clones the repo, installs the plugin, registers
the toolset in config.yaml, and creates a skill pointer. You'll be told
to restart Hermes.

**After restart, verify:**
- Plugin installed: `ls ~/.hermes/profiles/<profile>/plugins/agentpaas/plugin.yaml`
- Toolset registered: `grep agentpaas ~/.hermes/profiles/<profile>/config.yaml`
- Skill pointer exists: `ls ~/.hermes/profiles/<profile>/skills/agentpaas/SKILL.md`
- Binary works: `agentpaas version`

**Pass criteria:** All 4 checks pass.

## T2: Doctor Health Check

**What it tests:** All system prerequisites are met (Docker, daemon,
keychain, harness binary, home directory).

**Prompt:**
> Run agentpaas doctor

**What happens:** 6 diagnostic checks run.

**Pass criteria:** All 6 checks show ✓:
- Version
- Docker CLI
- Docker daemon
- macOS Keychain
- Linux harness
- Home directory

## T3: Weather Agent E2E

**What it tests:** Full agent lifecycle — init, policy, LLM config,
pack, run, invoke. Verifies real data is returned (not LLM-fabricated).

**Prompt:**
> Build a weather agent that takes a city name as input, uses an LLM
> to look up the weather, and returns the current conditions.

**When asked about LLM, respond:**
> Yes, use openrouter deepseek flash, key is in openrouter-key

**Store the API key first (in a separate terminal):**
```bash
agentpaas secret add openrouter-key
# paste your OpenRouter key when prompted
```

**What happens:**
1. Hermes creates a project scaffold (main.py, agent.yaml, policy.yaml)
2. The agent fetches real weather data via `agent.http("GET", "https://wttr.in/{city}?format=j1")`
3. LLM summarizes the real data via `agent.llm()`
4. Egress policy allows only wttr.in:443 and openrouter.ai:443
5. Agent is packed into a signed Docker image and run

**Verify real data:**
```bash
# Cross-check the agent's weather output against wttr.in directly
curl -s "https://wttr.in/Folsom?format=j1" | python3 -c "
import json,sys
d=json.load(sys.stdin)
cur=d['current_condition'][0]
print(f'{cur[\"temp_F\"]}F, {cur[\"weatherDesc\"][0][\"value\"]}, {cur[\"humidity\"]}% humidity')
"
```
The agent's response should match the raw wttr.in data.

**Pass criteria:**
- Agent runs successfully
- Weather data matches wttr.in (not fabricated)
- Audit trail shows egress_allowed for wttr.in and openrouter.ai
- 0 policy denials

## T4: Immutable Redeploy

**What it tests:** Re-packing creates a new image with a different
digest (immutable builds). New code is live after repack.

**Prompt:**
> Change the weather agent's prompt to be more conversational and
> friendly, repack it, and show me that the image digest changed.

**What happens:**
1. Hermes updates the LLM prompt in main.py
2. Repacks the agent — new Docker image with different SHA256 digest
3. New code is deployed

**Verify:**
- Image digest changed (compare before/after)
- Invoke the agent — response style should be different (conversational)
- Real data still correct

**Pass criteria:** Different digest + new code is live + real data still matches.

## T5: Trigger/API/Cron Launch Surface

**What it tests:** Trigger invoke, cron scheduling (add, list, remove).

**Prompt:**
> Test the trigger invoke, then add a cron schedule for the weather
> agent that runs every 5 minutes with city=Folsom, list the cron
> schedules, and then remove it.

**What happens:**
1. Trigger invoke: agent runs and returns weather
2. Cron add: schedule created (*/5 * * * *)
3. Cron list: shows the schedule
4. Cron remove: schedule deleted, list is empty

**Pass criteria:**
- Trigger invoke succeeds
- Cron add → list shows it → remove → list is empty
- Audit trail shows the invoke run

## T6: Secret-Brokered SaaS/API Action

**What it tests:** Credentials stored in Keychain are brokered to the
agent at runtime — agent code never sees the raw key value.

**Store a test key first:**
```bash
agentpaas secret add test-api-key
# paste any test value (e.g. test-secret-value-12345)
```

**Prompt:**
> Build an agent called auth-agent that calls https://httpbin.org/headers
> using the credential 'test-api-key' already stored in Keychain.

**What happens:**
1. Hermes creates an agent using `agent.http_with_credential("test-api-key", "GET", "https://httpbin.org/headers")`
2. Policy declares the credential: `credentials: [{id: test-api-key, type: header, header: Authorization}]`
3. At runtime, the daemon resolves the credential from Keychain
4. The harness injects the Authorization header — agent code only sends the credential ID
5. httpbin.org/headers echoes back all request headers, including the injected Authorization

**Verify:**
- Response includes `"Authorization": "test-secret-value-12345"` (or whatever you stored)
- Agent code in main.py does NOT contain the key value — only the ID "test-api-key"
- Audit trail shows egress_allowed for httpbin.org

**Pass criteria:** Credential injected, agent code clean, audit trail present.

## T7: Agentic Repair Loop

**What it tests:** Agent retries an operation on failure with a
maximum attempt limit.

**Prompt:**
> Build an agent that fetches data from https://httpbin.org/status/500
> and retries up to 3 times if the response is not successful, then
> returns the final result.

**What happens:**
1. The /status/500 endpoint always returns HTTP 500
2. Agent loops 3 times, each getting a 500
3. After max retries, returns the final failed result

**Verify:**
- Audit trail shows 3 egress_allowed events (one per attempt)
- Agent returns `attempts: 3` with the 500 status
- No infinite loop — terminates at max retries

**Pass criteria:** Exactly 3 attempts, proper termination, audit trail shows all 3.

## T8: Long-Running Mixed-Egress Agent

**What it tests:** An agent with both HTTP and LLM egress in the same run.

**Note:** This is already covered by T3 (weather agent uses HTTP to
wttr.in + LLM to openrouter.ai). You can skip T8 or run it as
confirmation.

**Pass criteria:** (Same as T3 — both egress types in one run.)

## T9: Clean-Machine Install Under 15 Minutes

**What it tests:** The full install → build → invoke flow completes in
under 15 minutes on a clean profile.

**This is validated by completing T1 through T7 in a single session.**

**Pass criteria:** All tests T1-T7 pass within one session.

## T10: Policy Authoring From Scratch

**What it tests:** Policy CLI tools — init, validate, show,
recommend-patch.

**Prompt:**
> Create a policy from scratch. Initialize a deny-all policy, then add
> an egress rule for api.example.com on port 443, validate it, show it,
> and recommend a patch for a denied destination.

**What happens:**
1. `policy init --template deny-all` creates an empty egress policy
2. Egress rule added for api.example.com:443
3. `validate` passes (ready: true)
4. `show` displays the policy
5. `recommend-patch` proposes adding a denied destination with risk assessment

**Pass criteria:** All 5 steps complete with valid output.

## Troubleshooting

### `brew install` fails with 404

The release may still be in draft mode. Check:
```bash
gh release view v0.1.1 --json isDraft
```
If `isDraft: true`, publish it:
```bash
gh release edit v0.1.1 --draft=false
```

### `agentpaas` binary won't run (quarantine)

Brew cask binaries are not notarized. Clear the quarantine attribute:
```bash
xattr -cr /opt/homebrew/bin/agentpaas
```

### Daemon won't start (checkpoint key corrupted)

After binary upgrades or clean state resets:
```bash
rm -f ~/.agentpaas/state/audit-checkpoint-key.der
agentpaas daemon start
```

### No `agentpaas_*` tools visible in Hermes

The toolset isn't registered. Run the ensure-toolset script:
```bash
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/ensure-toolset.py <profile>
```
Then restart Hermes: `/quit` then `hermes -p <profile>`

### Agent fails with "credential is not declared"

The credential must be declared in policy.yaml, not just stored in
Keychain. Add it to the `credentials:` section:
```yaml
credentials:
  - id: my-api-key
    type: header
    header: Authorization
```

### Agent fails with "agentpaas fake llm response"

The LLM is not configured. Either uncomment the `llm:` section in
agent.yaml or tell Hermes to configure it:
> Yes, use openrouter deepseek flash, key is in openrouter-key

### Pack fails with "agentpaas-sdk was not found"

The SDK is bundled automatically — do NOT list `agentpaas-sdk` in
requirements.txt. The requirements.txt should only list your agent's
own dependencies.
