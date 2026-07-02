# B13 Deploy E2E — Session Checkpoint #4

**Date:** 2026-06-24
**Branch:** main (local-first, not pushed)
**Goal:** Complete B13-T05 through T09 + block13-gate. Stop after Block 13.

## PROGRESS THIS SESSION

### BUG 7 FIXED (all sub-bugs)

**BUG 7a: otel.Store wired to dashboard** ✅
- `internal/daemon/server.go`: creates `otel.NewStore()` at daemon startup
  in `Start()`, stored in `d.otelStore`
- `internal/daemon/server.go`: creates `trigger.NewEventBus()` at startup,
  stored in `d.eventBus`
- Dashboard constructor now receives the otel.Store (not nil)
- `d.otelStore.Close()` added to Stop() cleanup
- Added imports: `otel`, `trigger` to daemon/server.go

**BUG 7b: ResourceManager wired** ✅ (was already done in prior session)
- `server.go` line ~294: `d.dashboard.SetResourceManager(NewDockerResourceManager(rt))`
- Verified: `/api/resources` returns running containers

**BUG 7c: Audit events emitted from Pack/Run/Stop** ✅ (was already done)
- `control_handlers.go`: `s.recordAudit("pack"/"run_start"/"run_stop", ...)`
- EventBus events published: `EventRunStarted` on Run, `EventRunSucceeded`/`EventRunCancelled` on Stop
- Added `eventBus *trigger.EventBus` field to `stubControlServer`
- Wired in `server.go`: `controlServer.eventBus = d.eventBus`
- Added `SetEventBus()` method to `dashboard.Server`
- Added `trigger` import to `control_handlers.go` and `stub_handlers.go`

**BUG 7d: DENIED probe** — NOT YET DONE
- Test agent's main.py just prints JSON, doesn't attempt egress
- Need to add an egress attempt to the test agent so the gateway can block it
- Then verify the denial shows up in audit + dashboard

### E2E flow status (verified this session)
- daemon start ✓
- validate ✓ (Project ready: python)
- pack ✓ (image built, digest, pushed to registry, signed)
- run ✓ (run-50e9a719d335b280 returned, container started)
- logs ✓ (returns [] — no longer "log viewer unavailable")
- dashboard health ✓ (/api/health returns {"status":"ok"})
- timeline ✓ (HTTP 200 text/event-stream — no longer "timeline unavailable")
- cost ✓ (returns JSON with total_cost — no longer "cost data unavailable")
- resources ✓ (returns running agents, gateways)
- audit ✓ (2 entries: pack + run_start)

### Daemon race (server.go:242) — NOT YET DONE
- The race in dashboard startup (goroutine starting dashboard while
  control server is being created) needs fixing
- Low priority — doesn't block e2e flow

## FILES CHANGED THIS SESSION

1. `internal/daemon/server.go`:
   - Renamed `auditIndex` field to `auditIndexer` (avoid confusion with stubControlServer.auditIndex)
   - Added `otelStore *otel.Store` and `eventBus *trigger.EventBus` fields
   - Added `otel` and `trigger` imports
   - Created otel.Store and EventBus in Start()
   - Wired otel.Store to dashboard constructor (was nil)
   - Wired EventBus to dashboard via SetEventBus()
   - Wired EventBus to controlServer
   - Added otelStore.Close() to Stop() cleanup

2. `internal/dashboard/server.go`:
   - Added `SetEventBus(bus *trigger.EventBus)` method
   - Creates TimelineHandler if not already created

3. `internal/daemon/stub_handlers.go`:
   - Added `eventBus *trigger.EventBus` field to stubControlServer
   - Added `trigger` import

4. `internal/daemon/control_handlers.go`:
   - Added `trigger` import
   - Publish `EventRunStarted` on Run
   - Publish `EventRunSucceeded`/`EventRunCancelled` on Stop

## REMAINING WORK

1. BUG 7d: DENIED probe — agent must attempt egress
2. Pre-existing daemon race (server.go:242)
3. B13-T05: complete e2e acceptance (DENIED probe is the last piece)
4. B13-T06: prompt-change immutable redeploy
5. B13-T07: demo matrix fixtures
6. B13-T08: /agentpaas slash commands
7. B13-T09: bundled SKILL.md + plugin.yaml
8. `make block13-gate` implementation
9. Block-end verifier + b13-block-end.md

## KEY FACTS

- Dashboard port: 8090
- Local registry: 5001
- Docker: colima context, DOCKER_HOST=unix:///Users/pms88/.colima/default/docker.sock
- 3 binaries: bin/agentpaas, bin/agentpaasd, bin/agentpaas-harness (Mac)
- 1 internal artifact: bin/agentpaas-harness-linux (linux/arm64 for container)
- Test agent: /tmp/agentpaas-e2e-agent/ (weather-agent, python, deny-all egress)
- otel DB: /tmp/agentpaas-e2e-home/state/otel.db
- audit DB: /tmp/agentpaas-e2e-home/state/audit.db
- audit JSONL: /tmp/agentpaas-e2e-home/state/audit.jsonl
