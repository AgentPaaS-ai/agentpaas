# Dashboard tour (cloud.agentpaas.ai)

The console shows what your agents did. It does **not** replace the CLI for deploys, cron, or invokes (except Tokens and Secrets forms).

## Tabs

| Tab | What you see | How you change things |
|-----|--------------|------------------------|
| Overview | Trial days, CPU, runs, agents | — |
| Agents | Images; expand row for provenance, invoke URLs | `cloud push` / Hermes |
| Deployments | Live slots; expand for invoke + cron hint | `cloud deploy` / `undeploy` |
| Runs | History; expand under row for summary/events | `cloud invoke` / cron |
| Cron | Schedules (read-only) | `cloud cron set|disable|enable` |
| Secrets | Labels only | UI or `cloud secrets push|bind` |
| Tokens | API tokens | UI mint/revoke or CLI login |
| Usage / Plan | Quota | Contact agentpaas.ai |

## Navigation

Agents, Deployments, and Runs support filter, page size, prev/next, and a **count on the top right**. Click a row to expand detail underneath (not a side drawer).
