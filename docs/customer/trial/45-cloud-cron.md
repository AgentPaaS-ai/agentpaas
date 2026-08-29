# Schedule with cron (cloud)

Cloud accepts:

1. **Named intervals:** `every_5m`, `every_15m`, `every_1h`
2. **Standard 5-field cron in UTC** (minute hour day-of-month month day-of-week)

Minimum interval is **5 minutes**.

```bash
agentpaas cloud cron set <deployment> --expr every_5m
agentpaas cloud cron set <deployment> --expr "30 9 * * 1-5"   # 09:30 UTC Mon-Fri
agentpaas cloud cron list
agentpaas cloud cron disable <deployment>
agentpaas cloud cron enable <deployment>
```

`<deployment>` may be a `dep_…` id or agent name (error if ambiguous).

The dashboard **Cron** tab is read-only. Change schedules via CLI or Hermes.

Five-field times are **UTC** — convert from your local timezone when setting wall-clock jobs.
