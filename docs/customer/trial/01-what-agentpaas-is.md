# What AgentPaaS is

AgentPaaS turns agent code into a **signed, sandboxed, policy-controlled, audited** workload.

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
