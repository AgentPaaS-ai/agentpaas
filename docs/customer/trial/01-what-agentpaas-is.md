# What AgentPaaS is
<!-- COPY TWIN: docs-site/docs/trial/what-is-agentpaas.md — change both or neither (D-W7) -->

AgentPaaS is a secure execution platform for agentic workflows. It runs AI agents you can't trust: agents can be steered by poisoned prompts, risky dependencies, or their own generated code, so the platform assumes the agent itself may be compromised. Every agent runs in an isolated container, behind default-deny egress, with gateway-brokered credentials and a tamper-evident audit trail.

Build, test, and run agents locally with the open-source CLI (macOS). Deploy the same governed agents to AgentPaaS Cloud in one command.

## Two surfaces

| Surface | What |
|---------|------|
| **Local (Mac)** | `agentpaas` CLI + daemon + Docker/Colima. Build, pack, run on your machine. |
| **Cloud** | Managed service at https://cloud.agentpaas.ai. Push image, deploy, invoke, cron. |

## What it does not do (yet)

- Linux local runtime (Mac only for local)
- Open self-serve signup (trial is claim-link invite)
- Production-grade multi-tenant vault isolation (cloud secrets are a **preview vault**)
- Pipelines, parent/child agents, swarms
- Multiple LLM providers proven on the cold path (use **OpenRouter** first)

See also: OSS `docs/known-limitations.md` if present in your install docs set.
