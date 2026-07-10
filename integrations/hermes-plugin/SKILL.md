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
- `agent.http(method, url, **kwargs)` — non-credentialed HTTP through the gateway
- `agent.http_with_credential(credential_id, method, url, **kwargs)` — brokered credentialed HTTP
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
1. Egress policy lists every external hostname the agent will access.
2. Every credential is in Keychain (`agentpaas_secret_list` — never
   `agentpaas_secret_add` with the key value as a tool parameter).
3. Every credential used by `agent.http_with_credential()` is declared in
   policy.yaml `credentials:`.
4. If LLM: agent.yaml has `llm:` pointing at the credential.
5. The LLM provider hostname is in the egress policy.

If ANY are missing, do NOT pack — ask only for the missing piece.

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
