# Quickstart

Build and run a governed AI agent entirely through Hermes. This guide
takes you from zero to a running, policy-enforced agent.

See the [main README](../README.md) for install instructions
(Hermes, Docker, AgentPaaS binary).

## Step 1: Launch Hermes

```bash
hermes
```

## Step 2: Install the AgentPaaS Plugin

Tell Hermes:

> Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas

Hermes installs the plugin, registers the toolset, and creates the skill
pointer. When it tells you to restart:

```
/quit
hermes
```

## Step 3: Verify Your Setup

Tell Hermes:

> Run agentpaas_doctor to check my setup

All 6 checks should pass (version, Docker CLI, Docker daemon, Keychain,
harness binary, home directory).

## Step 4: Build an Agent

Tell Hermes:

> Build an agent that takes a question as input and uses an LLM to answer it.

Hermes will ask you:
- Which LLM provider? (we recommend OpenRouter — see below)
- Which model?
- Your API key

Then it writes the agent code, creates an egress policy allowing only the
LLM provider's domain, packs a signed container image, and runs it under
governance.

## Step 5: Invoke the Agent

Tell Hermes:

> Ask the agent: "What is the capital of France?"

The agent calls the LLM through the gateway (credential brokered, egress
enforced) and returns the answer.

## Step 6: Check the Audit Trail

Tell Hermes:

> Show me the audit trail for the last run

Every allowed and denied network call is logged: timestamp, agent
identity, destination, credential used, policy decision.

## Adding Your LLM API Key

### Recommended: OpenRouter

OpenRouter keys are standard API keys that don't expire. Get one at
[openrouter.ai](https://openrouter.ai).

**Option A — Let Hermes handle it (easiest):**

Tell Hermes:

> I have an OpenRouter API key. Store it in AgentPaaS as "openrouter-key".

Hermes will ask for the key value and store it.

**Option B — Pipe from a file (if you have the key saved):**

Tell Hermes:

> Pipe my OpenRouter key from /tmp/openrouter-key.txt into agentpaas:
> cat /tmp/openrouter-key.txt | agentpaas secret add openrouter-key

Then delete the temp file:

> Delete /tmp/openrouter-key.txt

**Why the pipe?** Hermes redacts anything that looks like an API key or
JWT in terminal output. If you use command substitution like
`agentpaas secret add "$(cat key.txt)"`, the agent sees a redacted
preview (e.g. `sk-or-v1-...xxxx`) and stores THAT instead of the real
key. Piping directly via stdin avoids this.

### xAI and Nous: Not Recommended

xAI OAuth tokens expire after ~6 hours. Nous agent_keys expire after
~15 minutes. If multiple Hermes profiles share the same OAuth client,
refreshing in one revokes the other's token. Use OpenRouter instead.

## Available Slash Commands

| Command | Description |
|---------|-------------|
| `/agentpaas-init <path>` | Create a new agent project |
| `/agentpaas-deploy <path>` | Pack + run in one step |
| `/agentpaas-status` | Show daemon status and active runs |
| `/agentpaas-list` | List runs by status |
| `/agentpaas-logs <run_id>` | Tail logs for a run |
| `/agentpaas-timeline <run_id>` | Show events for a run |
| `/agentpaas-summarize <run_id>` | Summarize a run |
| `/agentpaas-explain-failure <run_id>` | Diagnose a failed run |
| `/agentpaas-doctor` | Run system diagnostics |
| `/agentpaas-audit [run_id]` | Show audit events |
| `/agentpaas-policy-show [dir\|run_id]` | Show active policy |
| `/agentpaas-trigger <agent_name>` | Invoke an agent |

All commands also work as natural language requests.

## Requesting Features or Contributing

- **Request a feature:** Open an issue on
  [GitHub](https://github.com/AgentPaaS-ai/agentpaas/issues)
- **Build your own:** Fork the repo, build, test (`make test`), open a PR
- See [known-limitations.md](known-limitations.md) for current development status
