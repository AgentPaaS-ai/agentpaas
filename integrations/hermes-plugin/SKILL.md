# AgentPaaS — Hermes Plugin

This plugin lets you build, deploy, and govern AI agents and workflows
entirely through Hermes. Every agent runs inside a locked-down container
with default-deny network policy, brokered credentials, and a
tamper-evident audit trail. Multi-agent jobs are a signed workflow
envelope that AgentPaaS runs after you hang up.

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

> Install from https://github.com/AgentPaaS-ai/agentpaas

Hermes installs the plugin, but cold installs require the filesystem
completer before setup is complete:

```bash
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/complete-install.py <profile>
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/verify-installed-state.py <profile>
```

Filesystem state completes on disk without a restart. Slash commands and
`agentpaas_*` tools load only when Hermes starts, so reopen the session once:

```
/quit
hermes -p <profile>
```

Do not reinstall and do not ask for OpenRouter or other LLM/API keys during
setup. Ask for keys only when building an agent that needs them.

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

Workflows have no slash command. When the user says "build a workflow"
or names stages/branches/specialists, follow **Build a Workflow** below.

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

Cloud observability is available after `agentpaas cloud login` through the
CLI or the matching Hermes tools:

| Command/tool | Description |
|---|---|
| `agentpaas cloud events <run_id>` / `agentpaas_cloud_events` | Show events for one cloud run |
| `agentpaas cloud audit [--since --until --limit]` / `agentpaas_cloud_audit` | Query tenant cloud audit events |
| `agentpaas cloud audit export <run_id>` / `agentpaas_cloud_audit_export` | Fetch a run audit export |
| `agentpaas cloud metrics` / `agentpaas_cloud_metrics` | Show aggregate cloud run and audit metrics |

All four cloud commands support `--json`. Hermes passes filters and run IDs to
the CLI and never accepts cloud API tokens as tool parameters.

## Cloud webhooks (API after login; no CLI verb)

There is no `agentpaas cloud webhook` command. Do not invent one.
`agentpaas cloud --help` will not list webhooks. After the user has run
`agentpaas cloud login` in their terminal, configure webhooks with the
logged-in cloud API. Never print HMAC secrets or tenant tokens in chat.

Base URL: `$AGENTPAAS_CLOUD_API_URL` (staging or prod). Auth for PUT/GET:
the same tenant token the CLI already has (Keychain). Inject via env in
Python/subprocess. Never argv. Never paste `apc_` into chat.

### Ingress (doorbell)

Someone else POSTs to AgentPaaS and a run starts.

```
PUT /v1/deployments/<dep_id>/webhook
{"provider":"generic_hmac","secret":"<generate; do not print>"}
```

Response `{configured:true, provider, deployment_id}` — no secret.

Fire (no tenant token):

```
POST /v1/deployments/<dep_id>/hooks/generic_hmac
Header: X-Agentpaas-Signature: t=<unix_seconds>,v1=<64 hex>
Body: raw JSON bytes you HMAC
```

`v1` is HMAC-SHA256(secret, `{t}.{raw_body}`) as lowercase hex.
Signed POST admits a `run_`. Missing/bad/stale (>300s) signature: 401
and no run. Stripe: `"provider":"stripe"` and `Stripe-Signature` with
the same `t=,v1=` shape; path `/hooks/stripe`.

### Completion (receipt) and delivery (the answer)

Public HTTPS destinations only (webhook.site is fine for a test).

```
PUT /v1/deployments/<dep_id>/completion-webhook
{"url":"https://..."}
PUT /v1/deployments/<dep_id>/delivery-webhook
{"url":"https://..."}
```

Use two different URLs so the two POSTs are distinguishable.
Completion is run id + terminal state. Delivery is declared
`final_output` only (no logs, secrets, artifacts). Hermes still polls.
Do not POST `/v1/runs/:id/terminal` to fake a delivery.

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

### HARD GATE: choose the project before any filesystem side effect

Before creating **ANY** project directory or writing **ANY** file, ask the
user to choose one of these options:

1. Use the bundled demo (for a matching request, such as
   `demo/weather-agent`), or
2. Name a **NEW** project directory.

Never scaffold into a path the user did not name. Never write agent files
before this choice is confirmed. If the request matches an existing demo,
prefer pointing at that demo and ask before scaffolding a new project.

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

**NEVER run `agentpaas identity init` via the terminal tool. ALWAYS tell the user to run it in their own terminal. Creating the identity yourself bypasses user consent and key sovereignty.**

Every agent MUST be signed with a publisher identity. Pack will fail
with "no publisher identity" if this hasn't been done. Check first:

```
agentpaas identity show
```

If it returns "no publisher identity", tell the user to run in their
own terminal (NEVER via your terminal tool):

```
agentpaas identity init --name <your-name>
```

They'll be prompted for a publisher name (GitHub-style slug, 1-39 chars).
After they confirm they've done it, verify with `agentpaas identity show`.
This is a one-time setup — subsequent packs reuse the same identity.

This step MUST happen before `agentpaas_pack`. Do NOT skip it.

### Cloud login (MANDATORY pattern)

When the user is not logged in to cloud (`agentpaas_cloud_whoami` fails):
1. Do **NOT** call `agentpaas_cloud_login` expecting it to finish auth (it only
   returns coaching text).
2. Tell the user to run in **their** terminal:
   `agentpaas cloud login`
3. They open the printed URL in the **same browser as their claim link**, approve,
   then say done.
4. Verify with `agentpaas_cloud_whoami` only after they confirm.

### Step 1: Configure LLM Provider (when needed)

1. Cold weather demo: do **not** offer a provider/model menu. State once:
   "Using OpenRouter `deepseek/deepseek-v4-flash`." Only change if the user
   explicitly asks. No Nous token-exchange / xAI OAuth on cold path.
   Stale IDs forbidden: `deepseek/deepseek-chat`, `gpt-4o-mini`,
   `deepseek-chat-v3-0324`, `r1-0528:free`, long pickers.
2. Tell the user to store the API key in a separate terminal (key never
   enters this conversation):
   ```
   agentpaas secret add openrouter-key
   ```
   Then: "Paste your OpenRouter API key when prompted, then tell me when done."
3. After the user confirms, verify via `agentpaas_secret_list` (labels only)
   and `agentpaas_secret_test`.
4. Call `agentpaas_llm_configure` with provider=openrouter,
   model=deepseek/deepseek-v4-flash, credential=openrouter-key.
5. Agent code uses `agent.llm()` — never reads the key from env.

Provider → hostname map (for YOU when writing policy; do not show as
`host:port` to the user):
- openrouter → openrouter.ai
- openai → api.openai.com
- anthropic → api.anthropic.com
- xai → api.x.ai
- nous → inference-api.nousresearch.com

Default port in policy.yaml for all of the above: 443.

### Step 2: Confirm Egress Hostnames (MANDATORY CONSENT GATE)

**CRITICAL — applies to BOTH new agent creation AND agent modification.**

When creating a new agent OR modifying an existing packed agent to add
new egress destinations:

1. From intent and/or source code, list external hostnames only.
2. Present briefly, no ports:
   "This agent will access: wttr.in, openrouter.ai. Allow these?"
   — OR for modification: "This agent will now also access:
   news.google.com. Allow?"
3. Wait for explicit user confirmation. Do NOT write to policy.yaml
   until the user approves.
4. Generate `policy.yaml` with ONLY confirmed hostnames. Write ports
   yourself (default 443). Never use wildcard `*:443` unless the user
   explicitly requests it.

Before packing an agent whose `policy.yaml` opens egress hosts, the skill
MUST show the user the exact hosts from `policy.yaml`, ask explicitly
`Approve egress to <hosts>? [y/N]`, and wait for an affirmative answer.
Never auto-approve egress. The user approves every host. If the user has not
approved, STOP and ask.

**BUG-031 rule:** If the user asks to add a new API or data source to
an existing agent, you MUST ask for confirmation before adding the new
hostname to policy.yaml. The egress policy is the primary security
control — the user must explicitly approve every new hostname, whether
creating or modifying.

**BUG-018 rule:** Use `domain` (not `host` or `hostname`) as the
field name for egress rules in policy.yaml. The schema field is
`domain` (`internal/policy/canonical.go` line 37). `host` and
`hostname` are NOT valid schema fields and will cause pack to fail.

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

You (turn 1): "Using OpenRouter deepseek/deepseek-v4-flash. In your terminal
run: `agentpaas secret add openrouter-key` then paste your OpenRouter API
key. Tell me when done."
You (turn 2): "This agent will access wttr.in and openrouter.ai. Allow these?"
Then: scaffold project, write main.py (http fetch + llm summarize), write
policy.yaml with hostnames + port 443, configure LLM
(openrouter + deepseek/deepseek-v4-flash + openrouter-key), pack, run.

### Pre-Pack Gate (silent checks — do not dump this list to the user)

Before `agentpaas_pack`, verify:
1. **Publisher identity exists** — call `agentpaas_identity_show`. If it returns
   an error, STOP and tell the user: "Run `agentpaas identity init --name <your-name>`
   in your terminal, then tell me when done." Do NOT proceed to pack without it.
2. Egress policy lists every external hostname the agent will access.
3. Every credential is in Keychain (`agentpaas_secret_list` — never
   `agentpaas_secret_add` with the key value as a tool parameter).
4. Every credential used by `agent.http_with_credential()` is declared in
   policy.yaml `credentials:`.
5. If LLM: agent.yaml has `llm:` pointing at the credential.
6. The LLM provider hostname is in the egress policy.

If ANY are missing, do NOT pack — ask only for the missing piece.

### Hermes Cloud Tools

The plugin exposes the cloud CLI through these structured tools: `agentpaas_cloud_whoami`,
`agentpaas_cloud_registry`, `agentpaas_cloud_push`, `agentpaas_cloud_deploy`, `agentpaas_cloud_deployments`,
`agentpaas_cloud_undeploy`, `agentpaas_cloud_invoke`, `agentpaas_cloud_result`,
`agentpaas_cloud_logs`, `agentpaas_cloud_usage`, `agentpaas_cloud_images`,
`agentpaas_cloud_secrets_list`, `agentpaas_cloud_secrets_push`, and
`agentpaas_cloud_login`. They call `agentpaas cloud ... --json` and return the
CLI's structured response; `agentpaas_cloud_invoke` waits for a terminal result
by default. Cloud login has no token argument, prints a URL, and does not open
the system browser unless `--open-browser` is explicitly requested;
cloud secret push accepts labels only and reads values from the local secure store.

Use `agentpaas_cloud_registry` (or `agentpaas cloud registry --json`) to discover tenant assets and the platform MCP catalog; its schema accepts no secret values and its output path never returns them. Cloud deployments are agents by default, while `agentpaas cloud deploy --type mcp` creates an MCP deployment, so obtain explicit user confirmation before either state-changing operation.

Treat push, deploy, undeploy, and invoke as paid or state-changing operations:
explain the plan and obtain explicit user confirmation before calling them.

### Cloud Deploy and Run (MANDATORY ORDER AND CONSENT GATES)

Cloud operations are side effects and paid cloud infrastructure. Never push
or deploy to the cloud without explicit user confirmation.

Before `agentpaas cloud push`, present the plan including the image name,
lockfile (`--lock <lock>`), deployment target, and that this uses billed
cloud infrastructure. Ask for explicit user OK. Only after the user says yes
may you run:

```bash
agentpaas cloud push --lock <lock>
```

Before `agentpaas cloud deploy latest` (or a specific digest), present the
image, deployment target, and billed-cloud-infrastructure consequence again,
then ask for explicit user OK. Only after the user says yes may you run:

```bash
agentpaas cloud deploy latest
```

Record the returned `DEPLOYMENT_ID`. Complete this REQUIRED ordered checklist
before any invoke:

1. `agentpaas cloud push --lock <lock>`
2. `agentpaas cloud deploy latest` (or digest) — record `DEPLOYMENT_ID`
3. `agentpaas cloud secrets push <secret>` for every secret that
   `agent.yaml` needs
4. `agentpaas cloud secrets bind <DEPLOYMENT_ID> <secret> --as bearer --host <host>`
   once per host
5. `agentpaas cloud secrets bindings <DEPLOYMENT_ID>` — VERIFY each expected
   binding is listed. If it says `No bindings`, STOP and redo steps 3–4.
6. Only then run `agentpaas cloud invoke ...` and
   `agentpaas cloud result <run>`.

**Single-invoke invariant:** a cloud walkthrough invokes exactly once after
deployment. The skill owns that one `agentpaas cloud invoke` call; do not also
call `agentpaas_trigger_invoke`, `agentpaas_run`, or repeat cloud invoke from
the tool layer. Invoke again only when the user asks another question/city.

A deployment with no secret bindings will return succeeded with EMPTY
`final_output`. Always verify bindings before invoke; otherwise stop rather
than retrying an invoke loop.

### Local demo single-invoke invariant

For a cold "Build a weather agent" walkthrough, the build skill owns exactly
one local `agentpaas_run` (or exactly one trigger invoke if the trigger path
was explicitly selected). The Hermes plugin/tool layer must not add a second
run after the skill returns. Invoke again only when the user asks another
question/city. Verify the existing run with status/result tools; those checks
are reads and are not new invokes.

## Build a Workflow

Use this section when the user wants more than one agent to cooperate:
pipelines, a classifier that picks a specialist, fan-out, or "A stays up
and phones B". AgentPaaS is the runtime. You write the workers and the
signed envelope. You do not draw boxes. The cloud console shows a frozen
Mermaid graph of that envelope with live stage lights.

A standalone agent is a one-node workflow. If they only asked for one
agent, stay on the single-agent path above.

### Pipeline and phone call

Infer the shape from the pack.

**Pipeline (default).** A writes a work order, then dies. B and C run.
A starts again with its notes plus their answers. Children never see
A's notes. Use this when A does not need to stay up.

**Phone call.** Use only when a living A is required. One supervisor
stays up and phones named teammates from the signed list. A call
outside that list fails. Stopping A cancels that A's children only.

Set the max duration from the human's job (seconds under the hood).
Do not tell the user to set sleepAfter.

### What you compose (v0.4)

Three envelope stage shapes. Nothing else.

1. **Linear stage** (omit `kind`). Runs one already-deployed agent.
   Requires `id`, `component_ref`, `deployment_id`, `max_context_bytes`.
2. **Fan-out stage** (`kind: "fanout"`). Spawns N copies of one child
   workflow and waits for all of them. Requires `id`,
   `child_workflow_id`, `fanout_max` (1-64), `join: "all"`,
   `max_context_bytes`. No `component_ref`. No other join policy.
3. **Choice stage** (`kind: "choice"`). Reads one key from the previous
   stage's committed handoff and starts exactly one child workflow from
   a closed `routes` map. Requires `id`, `choice_key`, `routes`,
   `max_context_bytes`. Each route value is `{ "child_workflow_id": "wf_..." }`.
   No `component_ref`. No default route. No in-envelope jump to another
   stage index.

Hard rules:

- Every name in the envelope must already be packed, signed, and
  deployed. You cannot invent a child at runtime.
- A prompt cannot add a host, a secret, a route, or a budget.
- Never set `hitl: true`. Create rejects it.
- `max_context_bytes` on the envelope and on every stage is a positive
  integer, at most 262144.
- At most 32 stages.
- Choice targets are child workflow ids only. Create the branch
  workflows first, then the parent.
- An unmatched choice value fails the run closed. That is success of
  the security model, not a bug to paper over.
- Do not `agentpaas run` a multi-stage composition locally. Local run
  is one agent. Multi-stage execution is
  `agentpaas cloud workflow create` then `start`.
- Do not put a second orchestrator (LangGraph/CrewAI driving stages)
  inside a worker. Library code may run inside one stage only.

### User-facing turns

Same tone as the weather agent. One short question at a time. Hostnames
only, never ports. Secrets stay in the user's terminal.

User: "Build a support workflow. Classify the ticket as refund, escalate,
or close, then run only the matching specialist."

You (turn 1): "Using OpenRouter. I will make four agents: classifier,
refund, escalate, close. Name a new project directory for them."
You (turn 2): same LLM + secret gate as a single agent
(`agentpaas secret add openrouter-key` in their terminal).
You (turn 3): list every hostname all four agents will call. "Allow these?"
You (turn 4): show this Mermaid, then wait for yes before any pack or
cloud write:

```text
classifier --> choice
choice -->|refund| refund
choice -->|escalate| escalate
choice -->|close| close
```

Then: write each worker with `@agent.on_invoke`, pack, push, deploy,
bind secrets, write `envelope.json`, create the workflow, start it once.

### Step order (do not skip)

1. **Directory.** Ask before any filesystem write. One parent folder,
   one subfolder per worker.
2. **Onboard each worker** with the single-agent gates (identity, LLM
   secret, hostname confirm, `policy.yaml`, `agent.yaml`). Reuse one
   LLM secret across workers unless the user asks otherwise.
3. **Show the graph.** Text or Mermaid of the closed menu. Wait for yes.
4. **Pack each worker** for cloud: `agentpaas pack <dir> --target linux/amd64`.
   Pre-pack gates still apply per worker.
5. **Cloud consent, per worker, in order:**
   `agentpaas cloud login` is the user's terminal, never yours.
   Confirm, then `agentpaas cloud push --lock <lock>`.
   Confirm, then `agentpaas cloud deploy latest`. Record `DEPLOYMENT_ID`.
   Push and bind every secret that worker needs. Verify bindings.
   Do not invoke the workers individually on a workflow walkthrough.
6. **Discover ids.** Use `agentpaas cloud deployments` and
   `agentpaas cloud registry` (or `agentpaas_cloud_registry`). Put the
   returned component/deployment ids into the envelope. Never invent them.
7. **Child workflows first** when the graph has choice or fan-out.
   A one-stage child is still its own workflow:

```json
{
  "max_context_bytes": 262144,
  "stages": [
    {
      "id": "refund",
      "component_ref": "<component id from registry>",
      "deployment_id": "<DEPLOYMENT_ID>",
      "max_context_bytes": 262144
    }
  ]
}
```

   Confirm, then:
   `agentpaas cloud workflow create --name support-refund --envelope refund.json`
   Record the returned workflow id. Repeat for each branch.

8. **Write the parent envelope** (example: classifier then choice):

```json
{
  "max_context_bytes": 262144,
  "stages": [
    {
      "id": "classify",
      "component_ref": "<classifier component id>",
      "deployment_id": "<classifier DEPLOYMENT_ID>",
      "max_context_bytes": 262144
    },
    {
      "id": "route-on-intent",
      "kind": "choice",
      "choice_key": "route",
      "max_context_bytes": 262144,
      "routes": {
        "refund": { "child_workflow_id": "<wf id from step 7>" },
        "escalate": { "child_workflow_id": "<wf id from step 7>" },
        "close": { "child_workflow_id": "<wf id from step 7>" }
      }
    }
  ]
}
```

   Classifier `return` must include a committed handoff key that matches
   `choice_key`, for example `{"status":"OK","route":"refund"}`. The
   controller reads the committed handoff, not chat text.

   Linear-only example (fetch then summarize):

```json
{
  "max_context_bytes": 262144,
  "stages": [
    {
      "id": "fetch",
      "component_ref": "<fetch component id>",
      "deployment_id": "<fetch DEPLOYMENT_ID>",
      "max_context_bytes": 262144
    },
    {
      "id": "summarize",
      "component_ref": "<summarize component id>",
      "deployment_id": "<summarize DEPLOYMENT_ID>",
      "max_context_bytes": 262144
    }
  ]
}
```

   Fan-out example (one child workflow, N copies, join all):

```json
{
  "id": "expand",
  "kind": "fanout",
  "child_workflow_id": "<wf id>",
  "fanout_max": 8,
  "join": "all",
  "max_context_bytes": 262144
}
```

9. **Create the parent.** Confirm billed cloud write, then:
   `agentpaas cloud workflow create --name support-triage --envelope envelope.json`
10. **Start once.** Confirm, then:
    `agentpaas cloud workflow start <id> --handoff-file handoff.json`
    Optional `--handoff-file` is a JSON object. Start exactly once on a
    cold walkthrough. Poll with
    `agentpaas cloud workflow instance <instance-id>`.
    Do not also `agentpaas_cloud_invoke` the workers.
11. **Show proof.** Give the user the workflow id, instance id, and
    https://cloud.agentpaas.ai Workflows page. The graph is the signed
    envelope. Only node state lights up.

### Classifier contract

The stage before a choice must return a string in the declared
`choice_key`. Allowed values are exactly the keys in `routes`.
If it returns anything else, the instance ends FAILED. Tell the user
that plainly. Do not add a default route to "make it work".

### Live mid-invoke call

Use a phone call only when a living A is required. One supervisor stays
up and phones named teammates from the signed list. A call outside that
list fails. Stopping A cancels that A's children only.

This is not a second workflow engine. A may phone only teammates already
named on the signed list. Use the installed SDK peer-call documented
in the AgentPaaS SDK on this machine. If that call is not in the
installed SDK, say so and compose a pipeline instead. Do not invent an
SDK verb. Standalone A cannot call other agents.

### Anti-fabrication for workflows

Never claim create or start succeeded unless the CLI printed an id.
Never claim a route ran unless `agentpaas cloud workflow instance`
shows that stage succeeded. If create rejects `kind`, `hitl`, a
missing child id, or `max_context_bytes`, report the error and fix
the envelope. Do not start a different graph than the one the user
approved.

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
   tell the USER to run in their own terminal: `agentpaas identity init --name <your-name>`
   and follow the prompts. Do NOT create the identity yourself.

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
- **User asked for a workflow** → Do not flatten it into one agent.
  Follow **Build a Workflow**. Create child workflows before the parent.
  Do not `agentpaas run` the composition locally.


### Cloud pull → edit → push (FEAT-1)

When the user wants to download a cloud agent, modify it, and republish:

1. Ensure logged in (`agentpaas_cloud_whoami`).
2. Tell user / run: `agentpaas cloud pull <agent_name|img_id> --dir ./my-agent [--bump-version 0.1.1]`
3. Edit code in that directory (confirm before overwrite).
4. Pack amd64, push with lock, deploy latest, bind secrets if needed, invoke.

Note: pull writes agent.yaml from cloud lock + stub main.py if source archive is absent.


### Cloud cron

Use tools `agentpaas_cloud_cron_set|disable|enable|list` (or CLI). Never tell users to edit cron in the dashboard (read-only). Expr: every_1m|every_5m|every_15m|every_1h.
