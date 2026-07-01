# B15 Session Checkpoint — 04

**Date:** 2026-06-30
**Branch:** main
**Goal:** Complete B15-T04 (Trigger / Cron / Event Surface)

## Completed This Session

- **MC1** (0a0b429): CronScheduler runtime management API — AddSchedule/ListSchedules/RemoveSchedule + JSON state persistence (cron-schedules.json). 7 unit tests.
- **MC2** (456f352): CronAdd/CronList/CronRemove RPCs added to control.proto, codegen'd, wired into daemon Start(). CronScheduler now starts automatically with the daemon, persists schedules to state/cron-schedules.json, invokes agents through daemon Run handler via TriggerService adapter. 7 handler tests.
- **MC3** (a9d561a): CLI commands — `agentpaas trigger invoke <agent>` + `agentpaas cron add/list/remove`. trigger invoke calls REST API with API-key auth; cron commands connect to daemon gRPC. 9 CLI tests.
- **MC4** (2a62ed4): Hermes plugin tools — agentpaas_trigger_invoke, agentpaas_cron_add, agentpaas_cron_list, agentpaas_cron_remove (26th-29th tools). Schemas updated. 11 plugin tests.
- **MC5**: block15-gate T04 section added. Gate passes (T01+T02+T03+T04).

## Verification

- `make block15-gate`: PASS
  - T01: secrets (pass)
  - T02: LLM (pass)
  - T03: policy (pass)
  - T04: trigger/cron (pass) — 46s trigger tests, 10s daemon, 4s CLI
  - Plugin: 208 tests pass (was 197, +11 new)
- `make lint`: 0 issues
- Plugin tool count: 29 (was 25, +4 trigger/cron tools)

## Architecture Summary

The trigger backend (B9) was fully built but had no user-facing surface. This
task wired it end-to-end:

1. **CronScheduler management** (internal/trigger/cron_management.go):
   AddSchedule validates cron expr, generates ScheduleID, persists to JSON.
   ListSchedules returns a snapshot. RemoveSchedule deletes by ID.
   State survives daemon restart via cron-schedules.json in AGENTPAAS_HOME/state/.

2. **Control RPCs** (api/control/v1/control.proto):
   CronAdd/CronList/CronRemove added to ControlService. REST endpoints at
   /v1/control/cron (POST), /v1/control/cron (GET), /v1/control/cron/{id} (DELETE).

3. **Daemon wiring** (internal/daemon/server.go):
   CronScheduler created in Start(), wired with TriggerService that calls
   controlServer.Run() — same path as trigger server invoke. Schedules loaded
   from state file on startup. CronScheduler.Stop() added to daemon shutdown.

4. **CLI** (internal/cli/trigger.go, cron.go):
   `agentpaas trigger invoke <agent> [--payload <file>] [--content-type <type>]`
   `agentpaas cron add <agent> --expr "*/5 * * * *" [--version <v>] [--timezone <tz>]`
   `agentpaas cron list`
   `agentpaas cron remove <schedule-id>`

5. **Plugin tools** (integrations/hermes-plugin/tools.py):
   4 new tools wrapping the CLI commands. Schemas in schemas.py.

## In Progress
- Nothing — T04 is complete.

## Next Session Start
- **Immediate next action:** Start B15-T05 (Production Hardening). This includes:
  - R17 init container pattern (remove CAP_NET_ADMIN from agent container)
  - R17 tighten RFC1918 allow (specific gateway subnet only)
  - R1 Rekor retry fallback for production image signing
  - Checkpoint key encryption at rest (AES or Keychain Secure Enclave)
  - CAP_NET_ADMIN capset verification test
- **File to read first:** agentpaas-execution-plan-v1.md, search "15-T05"
- **Block:** B15, Subtask: T05

## Key Facts
- CronScheduler state file: <AGENTPAAS_HOME>/state/cron-schedules.json
- Trigger REST: 127.0.0.1:7717 (default), POST /v1/trigger/invoke
- Trigger gRPC: 127.0.0.1:7718 (default)
- Trigger API key: AGENTPAAS_TRIGGER_API_KEY env var (optional, for --expose)
- Plugin tool count: 29 total
- All workers used grok-composer-2.5-fast (per user instruction), no stalls
