# AgentPaaS — Hermes Plugin

This plugin lets you build, deploy, and govern AI agents entirely through
Hermes. Every agent runs inside a locked-down container with default-deny
network policy, brokered credentials, and a tamper-evident audit trail.

## How It's Secure by Default

Every agent gets two containers on an isolated Docker network:

1. **Agent container** — no route to the internet. Non-root (UID 64000),
   read-only rootfs, no shell, all capabilities dropped, seccomp profile.
   The agent code cannot reach any network directly.

2. **Gateway sidecar** — the ONLY network path out. It enforces your
   egress policy (default-deny), and logs every allowed/denied call to
   the audit chain.

Even if the agent is prompt-injected or the agent code is malicious, it
can only call the exact endpoints you approved. Credentials are resolved
from macOS Keychain by the daemon at invoke time and injected into the
harness — the agent code never sees raw API keys. The SDK sends only
the credential ID (name); the harness injects the actual value into the
HTTP request at call time.

## Installation

### From inside Hermes (recommended for users)

Tell Hermes:

> Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas

Hermes installs the plugin, registers the toolset, and creates the skill
pointer automatically. Restart Hermes when prompted:

```
/quit
hermes
```

### Prerequisites

The AgentPaaS Go binary (daemon, CLI, harness) must be installed
separately from the plugin:

```bash
brew install agentpaas-ai/tap/agentpaas
agentpaas doctor
```

See the [main README](https://github.com/AgentPaaS-ai/agentpaas#install)
for full install instructions including Hermes and Docker.

## Slash Commands

All commands are also available as natural language — just ask Hermes.

### Building & Running

| Command | Description |
|---------|-------------|
| `/agentpaas-init <path>` | Create a new agent project scaffold |
| `/agentpaas-pack <path>` | Build a signed agent image |
| `/agentpaas-run <name>` | Start a governed agent run |
| `/agentpaas-deploy <path>` | Pack + run in one step |
| `/agentpaas-trigger <agent_name>` | Invoke an agent via trigger API |

### Monitoring & Debugging

| Command | Description |
|---------|-------------|
| `/agentpaas-status` | Show daemon status and active runs |
| `/agentpaas-list` | List runs, split by running/recent |
| `/agentpaas-logs <run_id>` | Tail logs for a run |
| `/agentpaas-timeline <run_id>` | Show chronological events for a run |
| `/agentpaas-summarize <run_id>` | Summarize a completed or failed run |
| `/agentpaas-explain-failure <run_id>` | Diagnose a failed run |
| `/agentpaas-stop <run_id>` | Stop a running agent |

### Policy & Audit

| Command | Description |
|---------|-------------|
| `/agentpaas-doctor` | Run system diagnostics (6 checks) |
| `/agentpaas-policy-show [dir\|run_id]` | Show active policy |
| `/agentpaas-audit [run_id]` | Show audit events |
| `/agentpaas-secret-list` | List stored credentials (by label, never value) |
| `/agentpaas-cron-list` | List scheduled agent invocations |

### Sharing

| Command | Description |
|---------|-------------|
| `/agentpaas-export <path>` | Export agent as shareable bundle |
| `/agentpaas-inspect <file>` | Inspect a bundle before installing |
| `/agentpaas-install <file>` | Print terminal install instructions |
| `/agentpaas-installed` | List installed agents |
| `/agentpaas-fork <ref> <dir>` | Fork an installed agent |
| `/agentpaas-provenance <ref>` | Show provenance chain |
| `/agentpaas-trust` | List trusted publishers |
| `/agentpaas-identity` | Show publisher identity |

## Agent Code Structure (Required)

AgentPaaS agents MUST use the SDK pattern. The harness loads
`/app/main.py` and calls the registered `@agent.on_invoke` handler.

```python
from agentpaas_sdk import agent

@agent.on_invoke
def handle_invoke(payload):
    """Called when the agent is invoked. payload is a dict from the trigger."""
    question = payload.get("question", "")
    if not question:
        return {"status": "ERROR", "error": "No question provided"}

    # agent.llm() routes through the gateway — credential injected at runtime
    result = agent.llm(prompt=f"Answer concisely: {question}")
    return {"status": "OK", "answer": result.get("text", "")}
```

### When to Fetch Real Data vs Ask the LLM

If the agent needs real-time, factual, or external data (weather, stock
prices, news, API responses), it MUST use `agent.http()` to fetch the
data first, then optionally use `agent.llm()` to summarize or reason
about it. Never ask the LLM to "look up" or "provide" real-time data —
LLMs fabricate plausible-looking but false values.

Correct pattern (weather agent):
```python
# 1. Fetch REAL data via HTTP
resp = agent.http("GET", f"https://wttr.in/{city}?format=j1")
# Response keys: status (int), status_code (same int), headers, body (str)
if resp.get("status", resp.get("status_code")) != 200:
    return {"status": "ERROR", "error": f"fetch failed: {resp.get('status')}"}
weather_data = resp.get("body", "")

# 2. Use LLM to SUMMARIZE the real data
result = agent.llm(prompt=f"Summarize this weather data: {weather_data}")
return {"status": "OK", "answer": result.get("text", "")}
```

Incorrect pattern (fabricated data):
```python
# WRONG — LLM will make up weather values
result = agent.llm(prompt=f"What's the weather in {city}?")
```

The SDK also provides:
- `agent.http(method, url, **kwargs)` — non-credentialed HTTP through the gateway.
  Returns `{"status": <int>, "status_code": <int>, "headers": {...}, "body": "..."}`.
  Prefer `resp["status"]` (canonical). `status_code` is an alias for the same value.
- `agent.http_with_credential(credential_id, method, url, **kwargs)` — brokered credentialed HTTP (same response shape)
- `agent.llm(prompt=...)` — LLM call (provider/model/credential from agent.yaml)
- `agent.mcp(server, tool, args)` — MCP tool call

**CRITICAL:** `agent.http` and `agent.http_with_credential` take `method` as the
2nd (or 1st for http) positional arg — NOT the URL. Common mistake: passing the
URL where `method` should go.

```python
# CORRECT — method is "GET", url is the full URL
resp = agent.http("GET", "https://wttr.in/Folsom?format=j1")

# CORRECT — credential_id, method, url
resp = agent.http_with_credential("my-api-key", "GET", "https://api.example.com/data")

# WRONG — missing method arg, URL passed as method
resp = agent.http_with_credential("my-api-key", "https://api.example.com/data")
```

A plain `main()` function will fail with: "agent must register an invoke
handler with @agent.on_invoke".

## Build-Time Onboarding (Mandatory)

When building an agent, complete these steps BEFORE packing. Ask the user
what you need, then act. Do not dump plans, secure-pattern lectures, or
multi-item checklists into the chat.

### User-facing tone (critical)

- Be terse. One short question at a time.
- NEVER show ports (`:443`, `ports: [443]`) to the user. Users confirm
  hostnames only (e.g. `wttr.in`, `openrouter.ai`). Ports are an
  implementation detail you add only when writing `policy.yaml`.
- NEVER ask the user to "confirm domains with ports" or to author policy
  YAML. You invent the policy from confirmed hostnames + provider.
- Do not paste long "Next steps" / "I will not proceed until" walls.
  Ask, wait, proceed.

### Step order (default for new agents)

If the agent needs an LLM (intent words: answer, summarize, look up with
LLM, chat, classify, generate, analyze, translate, weather with LLM, etc.):
start with **LLM + secret**, then confirm hostnames, then write code.

If the agent has no LLM: confirm hostnames (and any API keys), then code.

### Step 0: Publisher Identity (REQUIRED before packing)

Every agent MUST be signed with a publisher identity. Pack will fail
with "no publisher identity" if this hasn't been done. Check first:

```
agentpaas identity show
```

If it returns "no publisher identity", tell the user to run in their
terminal:

```
agentpaas identity init
```

They'll be prompted for a publisher name (GitHub-style slug, 1-39 chars).
After they confirm, verify with `agentpaas identity show`. This is a
one-time setup — subsequent packs reuse the same identity.

This step MUST happen before `agentpaas_pack`. Do NOT skip it.

### Step 1: Configure LLM Provider (when needed)

1. Ask only: "Which LLM provider? (openrouter / openai / anthropic / xai / nous)"
2. Ask only: "Which model?"
3. Tell the user to store the API key in a separate terminal (key never
   enters this conversation). Suggest a name, e.g. for OpenRouter:
   ```
   agentpaas secret add openrouter-key
   ```
   Then: "Paste your OpenRouter API key when prompted, then tell me when done."
4. After the user confirms, verify via `agentpaas_secret_list` (labels only)
   and `agentpaas_secret_test`.
5. Later call `agentpaas_llm_configure` with provider, model, credential name.
6. Agent code uses `agent.llm()` — never reads the key from env.

Provider → hostname map (for YOU when writing policy; do not show as
`host:port` to the user):
- openrouter → openrouter.ai
- openai → api.openai.com
- anthropic → api.anthropic.com
- xai → api.x.ai
- nous → inference-api.nousresearch.com

Default port in policy.yaml for all of the above: 443.

### Step 2: Confirm Egress Hostnames

1. From intent and/or source code, list external hostnames only.
2. Present briefly, no ports:
   "This agent will access: wttr.in, openrouter.ai. Allow these?"
3. Generate `policy.yaml` with ONLY confirmed hostnames. Write ports
   yourself (default 443). Never use wildcard `*:443` unless the user
   explicitly requests it.

### Step 3: Other Credentials (non-LLM APIs)

1. For each non-LLM API key needed:
   - Tell the user to run in their terminal:
     `agentpaas secret add <suggested-name>`
   - User pastes via stdin; key never enters the Hermes conversation
   - Verify with `agentpaas_secret_list` + `agentpaas_secret_test`
2. Declare each credential in policy.yaml:

   ```yaml
   credentials:
     - id: my-api-key
       type: header
       header: Authorization  # or X-API-Key, etc.
   ```

   - `id` must match the Keychain secret name
   - `type` must be `header`
   - `header` defaults to `Authorization` if omitted

### Example: Weather Agent (user-facing turns)

User: "Build a weather agent that uses an LLM…"

You (turn 1): "Which LLM provider? (openrouter / openai / anthropic / xai / nous)"
You (turn 2): "Which model?"
You (turn 3): "In your terminal run: `agentpaas secret add openrouter-key`
then paste your OpenRouter API key. Tell me when done."
You (turn 4): "This agent will access wttr.in and openrouter.ai. Allow these?"
Then: scaffold project, write main.py (http fetch + llm summarize), write
policy.yaml with hostnames + port 443, configure LLM, pack, run.

### Pre-Pack Gate (silent checks — do not dump this list to the user)

Before `agentpaas_pack`, verify:
1. Publisher identity exists (`agentpaas identity show` — must not error).
2. Egress policy lists every external hostname the agent will access.
3. Every credential is in Keychain (`agentpaas_secret_list` — never
   `agentpaas_secret_add` with the key value as a tool parameter).
4. Every credential used by `agent.http_with_credential()` is declared in
   policy.yaml `credentials:`.
5. If LLM: agent.yaml has `llm:` pointing at the credential.
6. The LLM provider hostname is in the egress policy.

If ANY are missing, do NOT pack — ask only for the missing piece.

### Anti-Fabrication (Critical — user-facing results)

Never claim an invoke succeeded unless you verified it from tool output:
1. After invoke, call `agentpaas_status` with the run_id.
2. Read the real invoke response (status, conditions/answer, error).
3. Confirm harness audit has `egress_allowed` for every expected domain
   (e.g. wttr.in AND openrouter.ai for a weather+LLM agent).
4. If `result.status` is ERROR, or there is no LLM egress when LLM was
   required, report FAILURE with the real error — do NOT scrape weather
   numbers out of an error body and call it success.

### Security: Secret Ingestion (Critical)

API keys MUST NEVER enter the Hermes conversation context. The Hermes agent
MUST NOT call `agentpaas_secret_add` with the key value as a tool parameter.

The correct flow:
1. Hermes tells the user: "Please run this command in your terminal:
   `agentpaas secret add <name>`
   Then paste your API key when prompted."
2. The user runs the command in a SEPARATE terminal — the key goes directly
   into macOS Keychain via stdin.
3. The user tells Hermes they're done.
4. Hermes verifies via `agentpaas_secret_list` (returns labels only, never values).

Why: If Hermes calls `agentpaas_secret_add` with the value as a tool parameter,
the key value is part of the tool-call arguments sent to the LLM provider as
part of the conversation. This leaks the key to the LLM provider. The terminal
flow keeps the key out of the conversation entirely.

## LLM Provider Guide

### Recommended: OpenRouter

OpenRouter is the recommended provider because it uses standard API keys
that don't expire. Get a key at [openrouter.ai](https://openrouter.ai).

**To add your OpenRouter key, tell Hermes:**

> I have an OpenRouter API key in the file /tmp/openrouter-key.txt.
> Pipe it into AgentPaaS: cat /tmp/openrouter-key.txt | agentpaas secret add openrouter-key

Or if you need to create the file first:

> Write my OpenRouter key to a temp file, then pipe it into agentpaas
> secret add. The key is: sk-or-v1-xxxxx

**Important:** API keys that match JWT/Bearer patterns get redacted by
Hermes when displayed in terminal output. Always pipe keys directly into
`agentpaas secret add` via stdin — never use command substitution
(`$(cat file)`) which shows the agent a redacted preview that gets stored
instead of the real key.

### Known Limitations: xAI and Nous OAuth tokens

xAI and Nous Research use OAuth tokens that expire:
- **xAI OAuth tokens** expire after ~6 hours. If multiple Hermes profiles
  share the same OAuth client, refreshing in one profile revokes the
  token in another.
- **Nous agent_key** expires after ~15 minutes — too short for reliable
  production use.

For these reasons, **OpenRouter is strongly recommended**. If you must
use xAI or Nous, extract a fresh token immediately before storing it.

## Sharing Agents (Export)

When a user wants to share an agent with someone else:

1. **Verify identity exists** — call `agentpaas_identity_show`. If no identity,
   tell the USER to run in their terminal: `agentpaas identity init` and follow
   the prompts. Do NOT create the identity yourself.

2. **Export the bundle** — call `agentpaas_export` with the project directory.
   The tool returns the bundle path, digest, and publisher fingerprint.

3. **Relay the fingerprint** — tell the user: "Read your fingerprint
   <fingerprint> to the receiver over another channel (phone, Signal, etc.)
   so they can verify the bundle is genuinely from you."

4. **Share the file** — tell the user where the .agentpaas bundle file is
   located. They can send it via any file-sharing method.

The bundle contains signed code, policy, and credential declarations (IDs only
— no secret values). The receiver will need their own API keys.

## Receiving Agents (Install)

When a user receives a .agentpaas bundle and wants to install it:

1. **Inspect before trust** — call `agentpaas_bundle_inspect` with the bundle
   path. Summarize for the user:
   - What the agent CAN ACCESS (list every egress domain)
   - What credentials it needs (list credential IDs)
   - Publisher name and fingerprint
   - Provenance chain (who created it, who forked it)
   - Any policy lints or warnings

2. **Verify fingerprint** — tell the user: "The publisher's fingerprint is
   <fingerprint>. Verify this matches what the sender told you over a
   separate channel." Do NOT skip this step or assume trust.

3. **Check credentials** — call `agentpaas_secret_list` to see which of the
   required credentials the user already has. For missing ones, guide the
   user through `agentpaas secret add <name>` in their terminal.

4. **Hand off to terminal** — tell the user: "Run in your terminal:
   `agentpaas install <bundle-path>` and follow the prompts. You'll confirm
   the fingerprint, approve the policy, and map credentials." Do NOT attempt
   to complete the install yourself — trust approval and policy acceptance
   ALWAYS happen in the user's terminal.

5. **Verify install** — after the user confirms, call `agentpaas_installed_list`
   to verify the agent appears.

6. **Offer a test run** — suggest running the installed agent to verify it
   works with the user's credentials.

### D3 Language Rules (Critical)

- NEVER describe a bundle as "safe" or "trusted". Always say "verified" or
  "the fingerprint matches."
- ALWAYS summarize what the agent CAN ACCESS (egress domains, credentials).
- NEVER say "the agent cannot access anything" — list what it CAN do.
- NEVER auto-approve or skip consent steps. The user must decide.

## Contributing

### Request a Feature

Open an issue on
[GitHub](https://github.com/AgentPaaS-ai/agentpaas/issues) describing
what you want and why. See [docs/known-limitations.md](../../docs/known-limitations.md)
for current development status and upcoming features.

### Build Your Own and Merge

1. Fork the repo
2. Build your feature following the existing patterns
3. Test: `make test && make redteam-smoke`
4. Open a PR describing what changed and why

For LLM provider additions specifically, see the
[known limitations](../../docs/known-limitations.md) document for current provider
support and the [open issues](https://github.com/AgentPaaS-ai/agentpaas/issues)
for planned additions.

## Pitfalls

- **NEVER fabricate output.** If a tool fails, report the error honestly.
  Do not invent plausible-looking output to mask failures.
- **Always verify run status.** After `agentpaas_run` or
  `agentpaas_trigger_invoke`, check `agentpaas_status`. "Run started"
  means the container launched, not that it succeeded.
- **Daemon won't start (checkpoint key corrupted)** → After binary
  upgrades: `rm -f ~/.agentpaas/state/audit-checkpoint-key.der` then
  `agentpaas daemon start`.
- **No `agentpaas_*` tools visible after restart** → The `agentpaas`
  toolset is missing from `platform_toolsets.cli`. The plugin's
  `register()` runs `ensure-toolset.py` automatically on session load.
  If it didn't, run it manually or reinstall the plugin.
- **Slash commands not resolving** → Run `/quit` and relaunch Hermes.
  Plugins load at startup, not mid-session.
- **Agent code uses plain app() or main()** → The harness requires
  `@agent.on_invoke`. See "Agent Code Structure" above.
