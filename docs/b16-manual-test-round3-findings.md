# B16 Manual Test Round 3 — Bug Findings

**Date:** 2026-07-03
**Tester:** Hermes (GLM-5.2, agentpaas profile)
**Profile:** agentpaas-test (reset, plugin installed from GitHub, toolset added)
**Binaries:** Fresh build from commit 4c85edb (all T4-01 through T4-13 fixes)
**Context:** All 30 plugin tools tested. Full lifecycle verified. New bugs below.

## Test Results Summary

| Test | Result | Notes |
|------|--------|-------|
| All 30 plugin tools | 28 PASS, 2 BUG | See bugs below |
| Full lifecycle (stdlib agent) | PASS | init→policy→validate→pack→run→invoke→timeline all work |
| Full lifecycle (deps agent) | PARTIAL | Pack succeeds (requests installed). Run fails: egress proxy denies |
| Doctor via agent | PASS | Returns structured JSON, agent displays it |
| E2E weather agent via agent | FAIL | Egress proxy rejects — gateway allowlist doesn't match policy |

## New Bugs Found

### BUG-T4-14 (MED): `agentpaas_reconcile_project` fails with absolute paths

**Command:** `agentpaas init /tmp/happy-path-test --from-code --noninteractive`
**Error:** `Error: project path must be under current directory`
**Root cause:** The `init --from-code` command rejects absolute paths outside
the current working directory. The plugin tool
`agentpaas_reconcile_project` passes absolute paths, so it always fails.
**Workaround:** `cd` to the project dir first, then use `init . --from-code`.
**Fix:** Allow absolute paths in `init --from-code`, or have the plugin tool
`cd` to the project dir before calling the CLI.

---

### BUG-T4-15 (LOW): `recommend-patch` natural language parsing is fragile

**Command:** `agentpaas recommend-patch "allow example.com on port 443"`
**Result:** `proposed_patch: ""`, `rationale: "unable to parse desired behavior"`
**Works:** `agentpaas recommend-patch "allow egress to api.openai.com"` →
returns valid patch.
**Root cause:** The natural language parser only recognizes the specific
pattern "allow egress to <domain>". Other phrasings ("allow X on port Y",
"permit https traffic to X") return empty patches.
**Fix:** Improve the parser to handle more phrasings, or accept structured
input (domain + port flags).

---

### BUG-T4-16 (CRITICAL): Egress proxy rejects all traffic — gateway allowlist not synced to agent policy

**Symptom:** Weather agent using `requests` to call `api.open-meteo.com`
fails with `ConnectionError: Connection reset by peer`. DNS resolution
also fails. The agent code works (deps installed, app() executes), but
no outbound network traffic succeeds.

**Root cause (confirmed):**
1. The agent container is on an internal-only Docker network with no
   direct internet access and no DNS resolver.
2. The agentgateway (172.20.0.2:7799) is configured as an HTTP proxy
   (HTTP_PROXY/HTTPS_PROXY env vars set in the container).
3. The gateway IS receiving proxy requests from the agent container,
   but REJECTS them with: `proxy error: network authorization denied:
   authorization failed`.
4. The gateway's config.yaml has a hardcoded allowlist that doesn't
   match the agent's policy.yaml. Example gateway config:
   ```yaml
   routes:
     - name: egress
       backends:
         - host: api.weather.gov:443   # <-- hardcoded, doesn't match policy
   frontendPolicies:
     networkAuthorization:
       - allow: dns.domain == "api.weather.gov"  # <-- only allows this domain
   ```
5. The agent's policy.yaml allows `*:443` (allow-http template) or
   `api.open-meteo.com:443`, but the gateway doesn't read or enforce
   the agent's policy. The gateway config is generated independently.

**Confirmed in gateway logs:**
```
proxy error: network authorization denied: authorization failed
connection.id=1 src.addr=172.20.0.3:34420
```

**Impact:** ANY agent that makes outbound HTTP/HTTPS calls will fail.
The egress proxy denies everything unless the domain happens to match
the hardcoded gateway allowlist. Policy enforcement at the gateway level
is completely disconnected from the agent's policy.yaml.

**Fix needed:** The daemon must generate the gateway config.yaml FROM
the agent's policy.yaml egress rules. When `agentpaas run` starts a
run, it should:
1. Read the agent's policy.yaml
2. Generate a gateway config with `networkAuthorization` rules matching
   the policy's egress allowlist
3. Write that config to the gateway container before starting it

Currently the gateway config appears to use a fixed/default allowlist
that doesn't reflect the agent's actual policy.

---

### BUG-T4-17 (HIGH): Agent doesn't confirm egress destinations with user during build

**User expectation:** When building an agent that will access external
websites/APIs, Hermes should ask the user:
- "I'm going to use api.open-meteo.com for weather data. Is that OK?"
- "I'm going to use api.openai.com for LLM calls. Allow this egress?"
Then open those egress paths in the policy.

**Current behavior:** The agent writes policy.yaml (from a template)
without asking the user which specific domains the agent needs. The
allow-http template allows `*:443` (all HTTPS), which is overly broad.
Or the agent picks a template without understanding what the agent code
actually needs.

**Fix needed:** The SKILL.md onboarding/build instructions should tell
the agent to:
1. Analyze the agent code to identify external domains it will access
2. Present those domains to the user for confirmation
3. Generate a policy.yaml with ONLY those confirmed domains (not wildcard)
4. Use the `allow-llm` template + confirmed domains if LLM is needed

---

### BUG-T4-18 (HIGH): Agent doesn't procure API keys/OAuth from user

**User expectation:** When an agent needs external API keys (e.g. OpenAI,
weather API tokens), Hermes should:
1. Detect that the agent code needs credentials
2. Ask the user: "This agent needs an OpenAI API key. Do you have one?"
3. Store the key via `agentpaas_secret_add`
4. Reference it in the agent.yaml llm config or as a credential

**Current behavior:** The agent doesn't ask for credentials. If the
agent code needs an API key, it will fail at runtime with an auth error.
The user has to manually figure out what credentials are needed and
add them.

**Fix needed:** The SKILL.md should instruct the agent to:
1. Analyze the agent code for credential usage (API key env vars, auth
   headers, etc.)
2. Ask the user for each required credential
3. Store via `agentpaas_secret_add` and reference in agent.yaml

---

### BUG-T4-19 (HIGH): Agent doesn't ask user which LLM to use

**User expectation:** When deploying an agent that needs an LLM (e.g.
a chatbot, summarization agent), Hermes should:
1. Recognize the agent needs LLM capabilities
2. Ask: "Which LLM provider do you want? (OpenAI/Anthropic/xAI)"
3. Ask: "Which model? (gpt-4o, claude-sonnet-4, grok-beta)"
4. Ask for the API key if not already stored
5. Configure agent.yaml with the llm: section
6. Open the egress path for the chosen LLM provider

**Current behavior:** The agent deploys without LLM config. If the
agent code calls an LLM, it fails at runtime.

**Fix needed:** The SKILL.md onboarding flow should include LLM
configuration as an explicit step when the agent needs intelligence.

---

## Architecture Observations

1. **Network topology is correct:** agent → internal network → gateway
   (HTTP proxy on 7799) → external internet. The gateway is the intended
   egress enforcement point. The problem is config sync (T4-16).

2. **Deps installation works:** The T4-12 multi-stage Dockerfile fix is
   confirmed working. `import requests` succeeds in the container.
   Deps are baked into the image at build time.

3. **`secret list` / `cron list` return bare JSON arrays** — unusual
   but not broken. Plugin tools handle it via json.loads. Cosmetic only.

4. **`secret test` returns exit 1 on auth failure** — expected behavior.
   The plugin tool surfaces the error correctly via _parse_cli_result.

## Fix Priority

1. **BUG-T4-16 (CRITICAL):** Gateway allowlist must be synced from agent
   policy.yaml. Without this, no agent can make external calls.
2. **BUG-T4-17 (HIGH):** Agent must confirm egress with user.
3. **BUG-T4-18 (HIGH):** Agent must procure API keys from user.
4. **BUG-T4-19 (HIGH):** Agent must ask which LLM to use.
5. **BUG-T4-14 (MED):** reconcile_project fails with absolute paths.
6. **BUG-T4-15 (LOW):** recommend-patch parser improvements.
