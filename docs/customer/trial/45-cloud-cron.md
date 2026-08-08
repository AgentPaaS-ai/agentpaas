# Schedule with cron

Named intervals only: `every_1m`, `every_5m`, `every_15m`, `every_1h`.

```bash
agentpaas cloud cron set <deployment> --expr every_5m
agentpaas cloud cron list
agentpaas cloud cron disable <deployment>
agentpaas cloud cron enable <deployment>
```

`<deployment>` may be a `dep_…` id or agent name (error if ambiguous).

The dashboard **Cron** tab is read-only. It shows schedule, enabled state, last fire, next fire estimate. Change schedules only via CLI or Hermes.

Hermes: ask to run the agent every 5 minutes (tools call the same verbs).
