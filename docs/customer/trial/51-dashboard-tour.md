# Dashboard tour

URL: https://cloud.agentpaas.ai

| Tab | Question it answers |
|-----|---------------------|
| Overview | Trial days, CPU, runs, agents used |
| Agents | Pushed images, version, status, latest invoke |
| Deployments | Live deps, cron chip, copyable invoke command |
| Runs | Invoke history, search, row detail with summary/final output |
| Cron | Schedules (read-only) |
| Secrets | Labels only (preview vault) |
| Tokens | Mint/revoke; bootstrap vs cli-login |
| Usage | Quota and period |
| Plan | Tier limits |

Write actions in UI: **tokens and secrets only**. Everything else mutates via CLI/Hermes.

Trial ending within 7 days: banner at top. Dismiss for this browser session; it returns next session.

Empty tabs show the one command that fills them.
