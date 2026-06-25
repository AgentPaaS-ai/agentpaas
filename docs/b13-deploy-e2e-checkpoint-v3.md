# B13 Deploy E2E — Session Checkpoint #3

**Date:** 2026-06-24
**Branch:** main (local-first, not pushed)
**Last commit:** (pending merge after BUG 7)
**Goal:** Complete B13-T05 through T09 + block13-gate. Stop after Block 13.

## PROGRESS THIS SESSION

### Completed
- BUG 6 FIXED + MERGED: harness binary cross-compiled for linux/arm64
  - Makefile: `build-harness-linux` target (GOOS=linux GOARCH=arm64 CGO_ENABLED=0)
  - `resolveHarnessBinary()` now prefers `agentpaas-harness-linux` over Mac binary
  - Verified: e2e pack→run→logs now shows "harness: listening on 127.0.0.1:8080"
  - Agent container execs correctly, Python main.py runs

### E2E flow status: detect→validate→pack→run ALL WORKING
- daemon start ✓
- validate ✓ (Project ready)
- pack ✓ (image built, digest, pushed to registry, signed)
- run ✓ (run-id returned, container started on internal network)
- logs ✓ (harness output visible)
- dashboard health ✓ (/api/health returns {"status":"ok"})

## ACTIVE BUG (next worker dispatch)

**BUG 7: Dashboard not wired to daemon runtime state**
- File: `internal/daemon/server.go` line 235
- Current: `NewServerWithAudit(addr, "", nil, nil, d.auditIndex)`
  - store (otel.Store) = nil → timeline/logViewer unavailable
  - mgr (ResourceManager) = nil → /api/resources empty
  - No EventBus wired → no run timeline events
- Symptom:
  - `GET /api/runs/{id}/timeline` → {"error":"timeline unavailable"}
  - `GET /api/runs/{id}/logs` → {"error":"log viewer unavailable"}
  - `GET /api/runs/{id}/cost` → {"error":"cost data unavailable"}
  - `GET /api/resources` → (empty, nil pointer safe but no data)
- Impact: T05 acceptance "dashboard shows DENIED probe" cannot be met
  because the dashboard has no run/policy-denial data to show.
- Also: the audit chain shows 0 entries — pack/run events are NOT being
  recorded to the audit log. The audit indexer exists but nothing writes
  run/pack events to it.

## ROOT CAUSE ANALYSIS

The daemon was wired up in pieces across B10/B11/B13:
1. B10 built the dashboard server with otel.Store + EventBus + ResourceManager
   but those were test-time wirings. The daemon startup (server.go Start())
   never instantiated them.
2. The Run handler (control_handlers.go) starts containers but doesn't:
   - Emit audit events (pack, run start, run stop)
   - Feed run state to the dashboard's ResourceManager
   - Connect the otel pipeline for trace/span collection
3. The DENIED probe requires: agent attempts outbound connection →
   gateway blocks it → audit records the denial → dashboard surfaces it.
   Currently steps 2-4 are missing because the pipeline isn't connected.

## SUB-BUGS OF BUG 7

7a. Dashboard needs otel.Store (for timeline/logs/cost)
7b. Dashboard needs ResourceManager backed by daemon's run state
7c. Pack/Run/Stop handlers must emit audit events
7d. DENIED probe: agent must actually attempt egress (currently the
    test agent's main.py just prints JSON, doesn't try to connect out)

## HOW TO REPRODUCE

```bash
export TMPDIR=/tmp
export DOCKER_HOST="unix:///Users/pms88/.colima/default/docker.sock"
cd ~/projects/agentpaas
make build-all
pkill -f agentpaasd; rm -rf /tmp/agentpaas-e2e-home && mkdir -p /tmp/agentpaas-e2e-home
./bin/agentpaas --home /tmp/agentpaas-e2e-home daemon start
sleep 2
./bin/agentpaas --home /tmp/agentpaas-e2e-home validate /tmp/agentpaas-e2e-agent
./bin/agentpaas --home /tmp/agentpaas-e2e-home pack /tmp/agentpaas-e2e-agent
./bin/agentpaas --home /tmp/agentpaas-e2e-home run weather-agent
sleep 3
# BUG 7 manifests here:
curl -s http://localhost:8090/api/runs/<run_id>/timeline  # "timeline unavailable"
./bin/agentpaas --home /tmp/agentpaas-e2e-home audit query  # 0 entries
```

## REMAINING WORK

1. Fix BUG 7 (dashboard wiring + audit events + DENIED probe)
2. Complete B13-T05 e2e acceptance
3. B13-T06: prompt-change immutable redeploy
4. B13-T07: demo matrix fixtures
5. B13-T08: /agentpaas slash commands
6. B13-T09: bundled SKILL.md + plugin.yaml
7. `make block13-gate` implementation
8. Block-end verifier + b13-block-end.md
9. Pre-existing daemon race (server.go:242) — fix after BUG 7

## KEY FACTS

- Dashboard port: 8090
- Local registry: 5001
- Docker: colima context, DOCKER_HOST=unix:///Users/pms88/.colima/default/docker.sock
- 3 binaries: bin/agentpaas, bin/agentpaasd, bin/agentpaas-harness (Mac)
- 1 internal artifact: bin/agentpaas-harness-linux (linux/arm64 for container)
- Test agent: /tmp/agentpaas-e2e-agent/ (weather-agent, python, deny-all egress)
- Worker dispatch: Grok CLI via tmux on worktree
