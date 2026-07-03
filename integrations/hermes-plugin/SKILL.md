---
name: agentpaas
description: >
  Deploy and govern AI agents with AgentPaaS — package agents as signed
  images, run them in sandboxed containers with network policy enforcement,
  brokered secrets, audit trails, and operator repair loops.
---

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

- Docker not running → `/agentpaas-doctor` for diagnostics
- Policy denial → `agentpaas_explain_policy_denial` for root cause, or
  `agentpaas_recommend_policy_patch` for a suggested fix
- Agent not found → run `agentpaas_pack` first
- No `agentpaas_*` tools visible → run `make install-plugin`, then verify
  with `hermes tools list | grep agentpaas`
- Slash commands not resolving → restart Hermes after `make install-plugin`
