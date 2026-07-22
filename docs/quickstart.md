# Quickstart

Get from zero to a running, policy-enforced agent on your Mac.

## What is AgentPaaS?

AgentPaaS is a local-first runtime that runs AI agents inside locked-down
containers with default-deny network policy. Every outbound call must match
an allowlist you approve; credentials are brokered by a gateway sidecar and
never appear in agent code; every decision is written to a tamper-evident
audit trail. Even if the agent is prompt-injected or malicious, it cannot
reach arbitrary hosts or steal raw API keys.

## Prerequisites

| Requirement | Exact expectation |
|---|---|
| **OS** | macOS (Apple Silicon or Intel) |
| **AgentPaaS** | Homebrew cask `AgentPaaS-ai/homebrew-tap/agentpaas` (ships `agentpaas`, `agentpaasd`, `agentpaas-harness-linux`) |
| **Docker** | Docker CLI + a running daemon. Recommended: Colima (`brew install colima docker` then `colima start`). Docker Desktop also works. |
| **LLM API key** (optional for the hello agent below; required for LLM agents) | OpenRouter free-tier key from https://openrouter.ai/keys (recommended). OpenAI, Anthropic, xAI, and Nous also work. |
| **Hermes Agent** (optional) | Only if you want the conversational plugin flow. Install: `brew install nousresearch/tap/hermes-agent`. |

You do **not** need Go, `make`, or a source checkout to use AgentPaaS as an
operator. Install the Homebrew cask — do not run `make build-all` for day-to-day
use (dev builds are stamped `0.3.0-dev` and may omit a proper harness bundle).

## Install

Copy-paste these commands in order.

### 1. Docker (if you do not already have it)

```bash
brew install colima docker
colima start
docker info >/dev/null && echo "Docker OK"
```

### 2. AgentPaaS

```bash
brew tap AgentPaaS-ai/homebrew-tap
brew install agentpaas
```

The cask is not notarized. Clear quarantine **before** any `agentpaas`
command (skipping this yields exit 137 / “killed”):

```bash
xattr -cr \
  /opt/homebrew/bin/agentpaas \
  /opt/homebrew/bin/agentpaasd \
  /opt/homebrew/bin/agentpaas-harness-linux
```

On Intel Homebrew prefixes, paths are under `/usr/local/bin/` instead of
`/opt/homebrew/bin/`.

### 3. Start the daemon and verify

```bash
agentpaas daemon start
agentpaas doctor
agentpaas version
```

Expected `doctor` shape (7 checks; skopeo is optional and still reports `ok`
when missing):

```text
agentpaas doctor
===============
Version:           ok
                   0.3.0 (darwin/arm64)
Docker CLI:        ok
                   (29.x.x)
Docker daemon:     ok
macOS Keychain:    ok
                   (accessible)
Linux harness:     ok
                   (/opt/homebrew/bin/agentpaas-harness-linux)
Home directory:    ok
                   (/Users/<you>/.agentpaas, writable)
skopeo (optional): ok
                   ...

Overall: 7/7 checks passed
```

Expected `version` shape:

```text
CLI: 0.3.0 | Proto: v1 | Commit: <hex> | OS/Arch: darwin/arm64 | ...
```

If the daemon will not start after an upgrade:

```bash
rm -f ~/.agentpaas/state/audit-checkpoint-key.der
agentpaas daemon start
```

## Create your first agent

This section builds a minimal governed agent with the CLI only (no Hermes).
The agent returns a fixed JSON payload and needs no API key.

### 1. Scaffold a project

```bash
mkdir -p ~/hello-agent
cd ~/hello-agent
agentpaas init . --runtime python --noninteractive
```

Expected:

```text
Initialized agent project in . with runtime python
```

With `--noninteractive`, `init` also writes a default-deny `policy.yaml`.
Without that flag, only `agent.yaml`, `main.py`, `requirements.txt`, and
`.agentpaasignore` are created — then run `agentpaas policy init . --template deny-all --noninteractive`.

### 2. Confirm the scaffold

```bash
ls -la
cat agent.yaml
cat main.py
cat policy.yaml
```

`agent.yaml` looks like:

```yaml
name: hello-agent
version: 0.1.0
runtime: python3.12
description: ""
# llm:
#   provider: openrouter  # openrouter|openai|anthropic|xai|nous
#   model: deepseek/deepseek-v4-flash
#   credential: openrouter-key  # Keychain secret name (agentpaas secret add openrouter-key)
```

`main.py` uses the SDK invoke hook (plain `main()` is **not** supported):

```python
from agentpaas_sdk import agent


@agent.on_invoke
def handle_invoke(payload):
    """Called when the agent is invoked. payload is a dict from the trigger."""
    return {"status": "OK"}
```

`policy.yaml` is default-deny (no egress):

```yaml
version: "1.0"
agent:
  name: ""
  description: ""
egress: []
credentials: []
mcp_servers: []
hooks: []
ingress: []
```

### 3. Customize the handler (optional)

Edit `main.py` so the agent echoes the payload:

```python
from agentpaas_sdk import agent


@agent.on_invoke
def handle_invoke(payload):
    name = "world"
    if isinstance(payload, dict):
        name = payload.get("name", name)
    return {"status": "OK", "message": f"hello, {name}"}
```

Do **not** add `agentpaas-sdk` to `requirements.txt` — the SDK is injected at
pack time. List only your own third-party packages.

### 4. Validate the project

The daemon must be running (`agentpaas daemon start`).

```bash
agentpaas validate .
```

Expected when ready:

```text
Project ready: /Users/<you>/hello-agent (runtime: python3.12)
```

If not ready, the CLI prints issues with a suggested next action, for example:

```text
Project NOT ready: /Users/<you>/hello-agent
  [policy] missing policy.yaml → run agentpaas policy init
```

## Pack and run

### 1. Pack (build the signed image)

```bash
cd ~/hello-agent
agentpaas pack .
```

Expected (digest values are unique per build):

```text
Image: sha256:<64-hex-chars>
Digest: sha256:<64-hex-chars>
```

Pack talks to the daemon (default timeout 5 minutes). Wildcard egress
(`domain: '*'`) is refused unless you pass `--allow-wildcard`.

### 2. Start a run

```bash
agentpaas run hello-agent
```

You can also pass the project path (`agentpaas run .`) if the source still
matches the last pack. Expected:

```text
Run started: <run-id> (agent hello-agent)
```

### 3. Invoke the agent

```bash
agentpaas trigger invoke hello-agent \
  --payload '{"name":"AgentPaaS"}' \
  --wait
```

Expected shape:

```text
Triggered agent: run_id=<run-id> status=RUNNING
Invoke Response:
{"status":"OK","message":"hello, AgentPaaS"}
```

Without `--wait`, the command returns as soon as the trigger is accepted and
does not print `invoke-response.json`.

### 4. Inspect status and audit

```bash
agentpaas status
agentpaas run list
agentpaas audit query
agentpaas audit verify
```

Expected verify shape:

```text
Audit chain valid: N records, N checkpoints
```

Exit code `0` means the hash chain and checkpoints are intact.

### Optional: run the weather demo (real HTTP + LLM)

If you have the source tree (or copy `demo/weather-agent` from the repo):

```bash
# Store the key in macOS Keychain (prompted on a TTY; never paste into Hermes chat)
agentpaas secret add openrouter-key
# Secret value: <paste OpenRouter key, press Enter>
# → secret "openrouter-key" stored

cd /path/to/agentpaas/demo/weather-agent
# agent.yaml already pins openrouter + deepseek/deepseek-v4-flash + openrouter-key
# policy.yaml allows wttr.in:443 and openrouter.ai:443
agentpaas validate .
agentpaas pack .
agentpaas run weather-agent
agentpaas trigger invoke weather-agent \
  --payload '{"query":"What is the weather in Folsom?"}' \
  --wait
agentpaas audit query
```

Cross-check live data:

```bash
curl -s "https://wttr.in/Folsom?format=j1" | python3 -c "
import json,sys
d=json.load(sys.stdin)
c=d['current_condition'][0]
print(f\"{c['temp_F']}F, {c['weatherDesc'][0]['value']}, {c['humidity']}% humidity\")
"
```

The agent summary should reflect those values (not invent weather).

**Note:** There is no `agentpaas llm configure` command. Configure LLM via the
`llm:` block in `agent.yaml` and store the secret with `agentpaas secret add`.

## Prefer Hermes?

After the binary install above:

```bash
hermes
```

Then tell Hermes:

> Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas

Restart when prompted (`/quit`, then `hermes`), run `/agentpaas-doctor`, store
keys with `agentpaas secret add` in a **separate** terminal, and build agents
in natural language. See the [main README](../README.md) for the full Hermes
flow and slash commands.

## Next steps

| Topic | Doc |
|---|---|
| Policy fields and templates | [policy-reference.md](policy-reference.md) |
| How topology + gateway enforce egress | [how-enforcement-works.md](how-enforcement-works.md) |
| Secrets and brokering | [secrets.md](secrets.md) |
| Share / install signed bundles | [sharing.md](sharing.md) |
| What signatures prove (and do not) | [trust-model.md](trust-model.md) |
| Threat model | [threat-model.md](threat-model.md) |
| Audit export | [audit-export.md](audit-export.md) |
| Developer test suite (unit → redteam) | [manual-testing.md](manual-testing.md) |
| Release golden loop | [execution/reference/e2e-test-plan.md](execution/reference/e2e-test-plan.md) |
| Current limits | [known-limitations.md](known-limitations.md) |
| Weather demo | [../demo/README.md](../demo/README.md) |

Feature requests and bugs: https://github.com/AgentPaaS-ai/agentpaas/issues
