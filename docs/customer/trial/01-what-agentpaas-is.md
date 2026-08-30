# What AgentPaaS is
<!-- COPY TWIN: docs-site/docs/trial/what-is-agentpaas.md. Change both or neither. -->

AgentPaaS is a secure execution platform for agents you cannot trust. Every agent runs in an isolated container, behind default-deny egress, with gateway-brokered credentials and a tamper-evident audit trail.

Build, pack, and run on macOS with the open-source CLI. Deploy the same governed agents to AgentPaaS Cloud.

## Four controls

1. Isolated container
2. Default-deny egress
3. Gateway-brokered credentials
4. Tamper-evident audit

The documentation word for that trail is Audit. The cloud console navigation label is Logs.

## The object path

Hermes packs and pushes a component into the registry. Deploy the parts you want live; they show under Deployments and take slots. Compose a workflow from those components; it shows under Workflows and is not live compute. Invoke a deployed agent (no workflow required) or invoke a workflow; both show under Runs. Evidence of what ran is under Logs.

A single agent does not need a workflow. Deploy it, invoke it. That is a run. The weather trial is this path. A workflow is only for more than one step: A then B, A phones B, choice, or fan-out. There is no deploy-the-workflow object. Ready means every member component is already deployed. That status belongs on the Workflows card, not as a Deployments row.

The console is read-only. Create, pack, deploy, and invoke happen in Hermes or the CLI.

## Two surfaces

| Surface | What |
|---------|------|
| Local (macOS) | `agentpaas` CLI, daemon, Docker/Colima. Pack and run on your machine. |
| Cloud | https://cloud.agentpaas.ai. Push, deploy, invoke, cron. Claim-link trial, not open signup. |

## What 0.4 includes

- Governed MCP and tool deployments (`--type mcp`, `--type tool`)
- Signed cloud workflows: linear, fan-out (join all), choice (fail-closed), phone-call (depth 1)
- Read-only console: Components, Workflows, Deployments, Runs, Logs
- Cron (named minimum every_5m), webhooks via Hermes or the API, file inputs (50 MiB)

## What it does not do yet

- Linux or Windows local runtime (macOS only for local)
- Open self-serve signup (trial is a claim-link invite)
- Production-grade multi-tenant vault isolation (cloud secrets are a preview vault)
- Local multi-stage `workflow.yaml` run (fail-closed; use a cloud envelope)
- Native HITL, join-any, for-each, wait/delay, spawn deeper than 1
- OpenAI or Anthropic as the stranger cold LLM path (use OpenRouter first)

See platform limits and workflow kinds pages for numbers and shapes.
