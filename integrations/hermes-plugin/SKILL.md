---
name: agentpaas
description: >
  Deploy and govern AI agents with AgentPaaS — package agents as signed
  images, run them in sandboxed containers with network policy enforcement,
  brokered secrets, audit trails, and operator repair loops.
---

## Onboarding: When a user asks to install AgentPaaS

When a user asks to install, set up, or configure AgentPaaS:

1. Run `hermes plugins install https://github.com/AgentPaaS-ai/agentpaas --enable`
2. Run `hermes -p <profile> config set platform_toolsets.cli '["terminal", "file", "web", "skills", "todo", "code_execution", "agentpaas"]'`
   (append "agentpaas" to existing toolsets if the user has custom ones)
3. Tell the user to restart Hermes (plugin changes require process restart)

Do NOT just print these as instructions — actually run steps 1 and 2
via terminal. Only step 3 (restart) requires user action.

# AgentPaaS

AgentPaaS is a governance platform for AI agents. It packages agent code into
signed, immutable images and runs them inside sandboxed containers with
network egress policy enforcement, brokered secrets, and a tamper-evident
audit trail. You interact with it through Hermes tools (natural language) or
slash commands.

## When to Use

- When the user says "I want to use agentpaas" or mentions deploying, running,
  packaging, or governing AI agents.
- When you generate agent code and need to deploy it as a governed, sandboxed,
  audited workload.
- When the user asks about agent status, logs, audit, or policy.
- When a run fails and the user needs diagnosis or repair guidance.

## Plugin Installation (for new users)

When a user asks to install the AgentPaaS plugin, guide them through these
steps. All three are required — skipping step 2 means the agent can see slash
commands but cannot call `agentpaas_*` tools.

### Step 1: Install the plugin from GitHub

```bash
hermes -p <profile> plugins install https://github.com/AgentPaaS-ai/agentpaas --enable
```

The `--enable` flag enables the plugin automatically (skips a separate
`hermes plugins enable` call). The repo root has a plugin shim that redirects
to the real plugin in `integrations/hermes-plugin/`.

### Step 2: Add the agentpaas toolset to platform_toolsets

This is a separate config step from enabling the plugin. Without it, the agent
cannot see or call any `agentpaas_*` tools — slash commands will work but
natural-language tool calls will fail.

```bash
hermes -p <profile> config set platform_toolsets.cli '["terminal", "file", "web", "skills", "todo", "code_execution", "agentpaas"]'
```

If the profile already has custom toolsets, append `"agentpaas"` to the existing
list instead of replacing it.

### Step 3: Restart Hermes

```bash
# In the Hermes session:
/quit
# Then relaunch:
hermes -p <profile>
```

Plugins and toolsets load at process startup, not mid-session. A running session
will NOT pick up the new plugin even after `/new` or `/reset`.

### After restart, verify

```bash
hermes -p <profile> tools list | grep agentpaas    # should show ~30 tools
hermes -p <profile> skills list | grep -i agentpaas
```

## Agent Code Structure (REQUIRED for deployment)

AgentPaaS agents MUST use the AgentPaaS SDK pattern. A plain `main()` function
will NOT work — the harness invokes the agent via the SDK's `@agent.on_invoke`
handler. If the agent code does not register an invoke handler, the run fails
with: "agent must register an invoke handler with @agent.on_invoke"

### Correct Agent Structure (Python)

```python
from agentpaas_sdk import agent

@agent.on_invoke
def handle_invoke(payload):
    """Called when the agent is invoked. payload is a dict from the trigger."""
    # Your agent logic here
    result = do_something(payload)
    # Return a dict — this is serialized as the invoke response
    return {"status": "OK", "result": result}
```

### agent.yaml

```yaml
name: my-agent
version: "1.0"
runtime: python
description: What this agent does
```

### policy.yaml

Generated during the Build-Time Onboarding flow (see below). Each external
domain the agent accesses must be listed as an egress rule.

### requirements.txt

List any pip dependencies (besides agentpaas_sdk, which is bundled in the
image). If the agent uses requests, httpx, etc., list them here.

### File naming

The agent entrypoint must be `main.py` in the project root. The harness loads
`/app/main.py` inside the container.

## Onboarding Response

When a user expresses interest in AgentPaaS (e.g. "I want to use agentpaas",
"what is agentpaas", "how do I use agentpaas"), respond with:

1. A concise description of what AgentPaaS is and does.
2. Current daemon status (call `agentpaas_status` or `/agentpaas-status`).
3. Suggested first actions based on state:
   - If daemon is not ready: "Run `/agentpaas-doctor` to diagnose, or start
     the daemon with `agentpaas daemon start`."
   - If daemon is ready: "You're all set. Try these:"
     - `/agentpaas-init <path>` — create a new agent project
     - `/agentpaas-deploy <path>` — pack and run an agent end-to-end
     - `/agentpaas-status` — check active runs
     - `/agentpaas-doctor` — verify your setup is healthy

Example response:
"AgentPaaS packages AI agents into signed, sandboxed containers with network
policy enforcement, brokered secrets, and audit trails.

Daemon status: [ready/not ready]

Quick start:
  /agentpaas-init ./my-agent     — scaffold a new agent project
  /agentpaas-deploy ./my-agent   — pack and run it
  /agentpaas-status              — check active runs
  /agentpaas-doctor              — verify setup health

You can also just ask me in natural language — e.g. 'create an agent that
calls the weather API' and I'll handle the rest."

## Build-Time Agent Onboarding (MANDATORY)

When building or deploying an agent (via `agentpaas_pack`, `agentpaas_run`,
`/agentpaas-deploy`, or natural language like "deploy this agent"), YOU MUST
proactively perform these three steps BEFORE packing. Do not skip them. Do not
wait for the user to ask. Do not print these as suggestions — execute them.

### Step 1: Confirm Egress Destinations (T4-17)

1. Read the agent's source code (main.py, app.py, agent.py, or equivalent).
2. Search for ALL external domains the agent accesses: URLs, API endpoints,
   `requests.get()`, `httpx.get()`, `urllib`, `openai.api_base`, webhook URLs,
   etc.
3. Extract the unique domains (e.g. `api.weather.gov`, `api.openai.com`).
4. Present them to the user:
   "This agent will access the following external domains:
     - api.weather.gov
     - api.openai.com
   Allow these in the egress policy?"
5. Generate a `policy.yaml` with ONLY the confirmed domains. Each domain gets
   its own egress rule with port 443 and appropriate methods.
6. NEVER use the `allow-http` template (wildcard `*:443`) unless the user
   explicitly requests wildcard access. Default to explicit domain allowlist.
7. Write the policy.yaml into the project directory.

If the agent accesses NO external domains, use `deny-all` and tell the user.

### Step 2: Procure API Keys / OAuth Credentials (T4-18)

1. Search the agent's source code for credential needs:
   - Environment variables: `os.environ["OPENAI_API_KEY"]`, `os.getenv("...")`
   - Auth headers: `Authorization: Bearer ...`, `X-API-Key: ...`
   - Config files referencing secrets
   - LLM client initialization (openai, anthropic, etc.)
2. For EACH credential need found:
   a. Tell the user: "This agent needs an API key for {service}. Do you have one?"
   b. If yes: ask for the key name (e.g. "openai-api-key") and value.
      Call `agentpaas_secret_add` with the name and value.
      Call `agentpaas_secret_test` to validate it works.
   c. If no: tell the user the agent cannot function without it. Do NOT proceed
      to pack until the credential is resolved or the user explicitly says to
      skip (and accept the agent will fail at runtime).
3. Reference stored credentials in agent.yaml or the credential section of
   policy.yaml as appropriate.

If the agent needs NO external credentials, skip this step silently.

### Step 3: Configure LLM Provider (T4-19)

1. Detect if the agent needs an LLM by checking for:
   - Imports: `openai`, `anthropic`, `xai`, `langchain`, `llm`
   - LLM client instantiation: `OpenAI()`, `Anthropic()`, `ChatOpenAI()`
   - agent.yaml `llm:` section
   - Natural language processing logic (chat, completion, embedding)
2. If an LLM is needed AND agent.yaml has no `llm:` section:
   a. Ask the user: "Which LLM provider? (openai / anthropic / xai)"
   b. Ask: "Which model? (e.g. gpt-4o, claude-sonnet-4, grok-beta)"
   c. Ask for the API key if not already stored in Step 2.
   d. Call `agentpaas_llm_configure` with project_dir, provider, model, credential.
   e. Add egress for the chosen provider domain to policy.yaml:
      - openai → api.openai.com:443
      - anthropic → api.anthropic.com:443
      - xai → api.x.ai:443

If the agent does NOT need an LLM, skip this step silently.

### Execution Order

Steps 1-3 must be done in order BEFORE calling `agentpaas_pack`. The flow is:

1. Analyze code → confirm egress (Step 1)
2. Detect credentials → procure keys (Step 2)
3. Detect LLM → configure provider (Step 3)
4. Write final policy.yaml (may be updated by steps 2-3 with credential/LLM rules)
5. Call `agentpaas_pack`
6. Call `agentpaas_run`

### Example: Weather Agent

User: "Deploy a weather agent that calls the NWS API"

You MUST:
1. Read the agent code. Find `requests.get("https://api.weather.gov/...")`.
   Present: "This agent accesses api.weather.gov. Allow?" → User confirms.
   Write policy.yaml with `egress: [{domain: api.weather.gov, ports: [443]}]`.
2. Check for credential needs. NWS API is free, no key needed. Skip silently.
3. Check for LLM needs. If the agent formats weather data using GPT:
   Ask: "Which LLM?" → User says "openai gpt-4o-mini".
   Ask for key → User provides it → `agentpaas_secret_add` → `agentpaas_secret_test`.
   Call `agentpaas_llm_configure` with openai/gpt-4o-mini.
   Add api.openai.com to policy.yaml egress.
4. Pack and run.

## Available Slash Commands

| Command | Description |
|---------|-------------|
| `/agentpaas-init <path>` | Create a new agent project scaffold |
| `/agentpaas-pack <path>` | Build a signed agent image |
| `/agentpaas-run <image\|project>` | Start a governed agent run |
| `/agentpaas-deploy <path>` | Pack + run in one step |
| `/agentpaas-status` | Show daemon status and active runs |
| `/agentpaas-stop <run_id>` | Stop a running agent |
| `/agentpaas-logs <run_id>` | Tail logs for a run |
| `/agentpaas-timeline <run_id>` | Show timeline events for a run |
| `/agentpaas-summarize <run_id>` | Summarize a completed or failed run |
| `/agentpaas-explain-failure <run_id>` | Diagnose a failed run |
| `/agentpaas-audit [run_id]` | Show audit events |
| `/agentpaas-doctor` | Run system diagnostics |
| `/agentpaas-policy-show [dir\|run_id]` | Show active policy |
| `/agentpaas-secret-list` | List stored credentials |
| `/agentpaas-cron-list` | List cron schedules |
| `/agentpaas-trigger <agent_name>` | Invoke an agent via trigger API |

All commands are also available as natural language requests — just ask.

## Available Tools (for programmatic use)

- `agentpaas_init_project` — scaffold a new agent project
- `agentpaas_reconcile_project` — generate agent.yaml from existing code
- `agentpaas_validate_project` — validate a project before packing
- `agentpaas_pack` — build a signed agent image
- `agentpaas_run` — start a governed agent run
- `agentpaas_stop` — stop a running agent
- `agentpaas_status` — daemon or run status
- `agentpaas_logs` — tail logs
- `agentpaas_get_run_timeline` — chronological timeline
- `agentpaas_summarize_run` — structured run summary
- `agentpaas_explain_failure` — root cause analysis for failed runs
- `agentpaas_next_action` — recommended next operator action
- `agentpaas_doctor` — system diagnostics
- `agentpaas_policy_show` — show active policy
- `agentpaas_policy_init` — scaffold policy.yaml from a template
- `agentpaas_explain_policy_denial` — explain why a destination was denied
- `agentpaas_recommend_policy_patch` — suggest a policy fix
- `agentpaas_audit_query` — query audit log
- `agentpaas_export_audit` — export audit bundle
- `agentpaas_secret_add` — store a credential in Keychain
- `agentpaas_secret_list` — list stored credentials
- `agentpaas_secret_remove` — remove a credential
- `agentpaas_secret_rotate` — replace a credential (atomic)
- `agentpaas_secret_test` — validate a credential against its provider
- `agentpaas_llm_configure` — write LLM provider config into agent.yaml
- `agentpaas_trigger_invoke` — invoke an agent via trigger REST API
- `agentpaas_cron_add` — schedule automatic agent invocation
- `agentpaas_cron_list` — list cron schedules
- `agentpaas_cron_remove` — remove a cron schedule

## Installation

Run `make install-plugin` from the AgentPaaS repo root to register this plugin
with your Hermes profile. See README.md → "Hermes Plugin (Developer Setup)".

## Pitfalls

- **Daemon won't start (checkpoint key corrupted)** → After binary upgrades
  or killing the daemon, it may fail with "decrypt checkpoint key: cipher:
  message authentication failed". Fix by deleting the corrupted key:
  `rm -f ~/.agentpaas/state/audit-checkpoint-key.der` then
  `agentpaas daemon start`. The daemon will generate a fresh key.
- **Daemon socket not found** → The daemon is not running. Start it:
  `agentpaas daemon start`. If it fails, see the checkpoint key fix above.
- Docker not running → `/agentpaas-doctor` for diagnostics
- Policy denial → `agentpaas_explain_policy_denial` for root cause, or
  `agentpaas_recommend_policy_patch` for a suggested fix
- Agent not found → run `agentpaas_pack` first
- Slash commands not resolving → **run `/quit` to restart Hermes** (plugins and
  toolsets load at startup; a running session will not pick them up). After
  relaunching, the slash commands and `agentpaas_*` tools will be available.
- No `agentpaas_*` tools visible even after restart → the plugin is enabled
  but the `agentpaas` toolset is missing from `platform_toolsets.cli`. Run:
  `hermes -p <profile> config set platform_toolsets.cli '["terminal", "file", "web", "skills", "todo", "code_execution", "agentpaas"]'`
  then `/quit` and relaunch. (If you installed via `make install-plugin`, it
  already did both steps.)
- **Run by path fails with "not deployed"** → `agentpaas run` accepts a
  project path, image digest, OR agent name. If you pass a path, it reads
  agent.yaml to resolve the agent name. Make sure you `agentpaas pack` the
  project first — the agent must be deployed before running.
- **Agent code uses plain app() or main()** → The harness requires the
  `@agent.on_invoke` SDK pattern. A plain function will fail with
  "agent must register an invoke handler with @agent.on_invoke". Always
  use:
  ```python
  from agentpaas_sdk import agent

  @agent.on_invoke
  def handle_invoke(payload):
      ...
      return {"status": "OK"}
  ```
