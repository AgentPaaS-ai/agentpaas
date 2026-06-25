# B13 Deploy E2E — Session Checkpoint #5

**Date:** 2026-06-24
**Branch:** main (local-first, not pushed)
**Goal:** Complete B13-T05 through T09 + block13-gate. Stop after Block 13.

## PROGRESS THIS SESSION

### Daemon race (server.go) — FIXED + COMMITTED (a15ceca)

**Root cause:** Data race on `d.dashboard` field between:
- `Start()` line 271-275: goroutine reads `d.dashboard` via closure without holding `d.mu`
- `Stop()` line 389-394: reads and nils `d.dashboard` without holding `d.mu` (lock released at line 343)

**Fix:**
1. In `Start()`: capture `d.dashboard` in local var `dash` before goroutine starts
2. In `Stop()`: read+nil `d.dashboard` under `d.mu.Lock()`, then shutdown outside lock

**Test added:** `TestDaemonStartStopRace` — 10 iterations of immediate Start→Stop with -race.
All daemon tests pass with -race, 3 iterations.

### BUG 7d: DENIED probe — IN PROGRESS (architecture fully traced)

## FULL ARCHITECTURE TRACE (so next session doesn't re-discover this)

### Container → Harness → Agent data flow

```
agentpaas run weather-agent
  → daemon Run handler (control_handlers.go:154)
    → creates internal-only Docker network
    → creates agent container (image: pack.LocalImageRef)
    → container starts, PID 1 = agentpaas-harness binary
      → harness Config from env vars (cmd/harness/main.go)
        cfg.Audit = nil  ← THIS IS THE GAP
      → harness starts Python worker (python_worker.go)
        → sets AGENTPAAS_RPC_ADDR env var for Python
        → Python imports agentpaas_sdk, connects to RPC socket
      → harness starts RPC server (rpc_server.go)
        → handleHTTP, handleLLM, handleMCP, handleRecordIteration
```

### SDK → Harness RPC flow

```
Agent code: agent.http("GET", "https://example.com")
  → Python SDK (_rpc.py): writes JSON to Unix socket
    → Go harness (rpc_server.go:229 handleHTTP)
      → makes actual HTTP request from inside container
      → container is on internal-only network → request fails
      → returns rpcError("http_failed") to Python
      → NO audit event emitted
      → NO otel span created
```

### Key files and their roles

| File | Role |
|------|------|
| `cmd/harness/main.go` | Harness binary entry point. Sets Config from env. **Does NOT set cfg.Audit** |
| `internal/harness/server.go` | Harness HTTP server + Python worker management |
| `internal/harness/rpc_server.go` | Unix socket RPC server. handleHTTP/handleLLM/handleMCP. **No egress audit** |
| `internal/harness/python_worker.go` | Starts Python subprocess, wires RPC socket env var |
| `internal/harness/failure_context.go` | Failure context + audit appender interface |
| `internal/harness/budget.go` | BudgetEnforcer + AuditAppender interface (duplicate) |
| `internal/daemon/control_handlers.go:154` | Run handler — creates container, emits run_start audit |
| `internal/daemon/server.go` | Daemon startup — creates otel.Store, EventBus, dashboard |
| `internal/mcpmanager/egress.go` | EgressPolicy for MCP servers. Emits `mcp_egress_decision` audit. **Not wired for harness http** |
| `internal/otel/store.go` | SQLite OTLP store. IngestTraces/IngestLogs/QuerySpans |
| `internal/dashboard/timeline.go` | Classifies spans: `egress.allowed` attr → `egress_allowed`/`egress_denied` |
| `internal/audit/record.go` | AuditRecord struct (hash-chained) |
| `internal/audit/writer.go` | AuditWriter — appends JSONL records to file |
| `python/agentpaas_sdk/agent.py` | Python Agent class — http(), llm(), mcp() methods |
| `python/agentpaas_sdk/_rpc.py` | RPCClient — Unix socket JSON line protocol |
| `python/agentpaas_sdk/runner.py` | Worker bootstrap — loads user agent, connects RPC |

### How egress denial should appear in dashboard

The dashboard timeline (`internal/dashboard/timeline.go:315 classifySpan`):
- Span name contains "egress" + attr `egress.allowed: false` → `egress_denied`
- Span name contains "egress" + attr `egress.allowed: true` → `egress_allowed`

These come from otel spans stored in `otel_spans` table. But the harness doesn't
create otel spans — only the daemon's otel.Store does, via IngestTraces.

### The gap (3 missing pieces)

1. **Test agent** (`/tmp/agentpaas-e2e-agent/main.py`): just prints JSON, never calls `agent.http()`
2. **Harness `handleHTTP`** (`rpc_server.go:229`): doesn't emit audit events for egress attempts
3. **Harness `cfg.Audit`** (`cmd/harness/main.go`): set to nil — no audit appender wired

### Network topology in e2e flow

The daemon's `Run` handler (`control_handlers.go:154-218`):
- Creates ONE internal-only Docker network (no egress network, no gateway)
- Creates agent container on that network only
- No gateway container (unlike redteam tests which create dual-homed gateway)

This means: agent HTTP attempts fail with connection refused/timeout (network
isolation), NOT a policy denial. But this is still a valid DENIED probe — the
network itself denies the egress.

## PLANNED FIX (Option C — pragmatic)

### Step 1: Wire harness audit appender
- `cmd/harness/main.go`: set `cfg.Audit` to a file-based appender
  writing to `/audit/harness-audit.jsonl` (mounted volume)
- Create `internal/harness/file_appender.go`: simple AuditAppender that
  writes JSONL records to a file path

### Step 2: Emit egress audit events from handleHTTP
- `internal/harness/rpc_server.go:229 handleHTTP`:
  - On HTTP error (connection refused/timeout): emit `egress_denied` audit
    event with destination, method, reason
  - On HTTP success: emit `egress_allowed` audit event
  - Use the existing `s.audit` appender (already wired to rpcServer)

### Step 3: Mount audit volume in Run handler
- `internal/daemon/control_handlers.go:184 Create()`:
  - Mount a volume: host `/tmp/agentpaas-e2e-home/runs/{runID}/harness-audit/`
    → container `/audit`
  - Set `AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl` env var

### Step 4: Ingest harness audit after run
- After run completes (or during logs fetch), daemon reads the harness
  audit JSONL file and ingests records into its own audit chain
- Create otel spans from egress audit events so they show in timeline

### Step 5: Update test agent
- `/tmp/agentpaas-e2e-agent/main.py`: call `agent.http("GET", "https://example.com")`
  inside try/except, report the failure

## KEY FACTS (unchanged from v4)

- Dashboard port: 8090
- Local registry: 5001
- Docker: colima context, DOCKER_HOST=unix:///Users/pms88/.colima/default/docker.sock
- 3 binaries: bin/agentpaas, bin/agentpaasd, bin/agentpaas-harness (Mac)
- 1 internal artifact: bin/agentpaas-harness-linux (linux/arm64 for container)
- Test agent: /tmp/agentpaas-e2e-agent/ (weather-agent, python, deny-all egress)
- otel DB: /tmp/agentpaas-e2e-home/state/otel.db
- audit DB: /tmp/agentpaas-e2e-home/state/audit.db
- audit JSONL: /tmp/agentpaas-e2e-home/state/audit.jsonl

## SESSION 5 PROGRESS (2026-06-24)

### BUG 7d Step 1-2: COMPLETE (compiles, tests pass)

- `internal/harness/file_appender.go`: FileAuditAppender created. Writes JSONL
  audit records to a file path. Does NOT maintain hash chain (flat records).
- `cmd/harness/main.go`: Wires `AGENTPAAS_AUDIT_PATH` env var → FileAuditAppender.
  Sets `cfg.Audit = appender`, defers Close().
- `internal/harness/rpc_server.go`:
  - `auditEgressDecision()` method IMPLEMENTED (was called but missing — code
    wouldn't compile). Mirrors `auditMCPDenied` pattern. EventType = "egress_allowed"
    or "egress_denied". Payload: destination, method, decision, credential_id,
    status_code, reason.
  - `handleHTTP` calls `auditEgressDecision` at 5 points:
    1. Invalid HTTP request → denied
    2. Credential not declared → denied
    3. HTTP request failed (connection refused/timeout) → denied
    4. Response read failed → denied
    5. HTTP success → allowed

### ContainerSpec.Binds: ADDED

- `internal/runtime/driver.go`: Added `Binds []string` field to ContainerSpec.
  Format: "host_path:container_path" or "host_path:container_path:ro".
- `internal/runtime/docker.go`: Wired `Binds: spec.Binds` into hostConfig.
- Rationale: needed to mount audit volume from host into container so the
  harness can write audit JSONL that the daemon reads after the run.
- All runtime tests pass.

### Build status

- `go build ./...` — clean
- `go test ./internal/harness/... ./internal/runtime/...` — all pass

### REMAINING for BUG 7d (Steps 3-5, not yet started)

**Step 3: Mount audit volume in Run handler**
- `internal/daemon/control_handlers.go:184` (Run handler, ContainerSpec.Create):
  - Create host dir: `{homePaths.State}/runs/{runID}/harness-audit/`
  - Add to ContainerSpec.Binds: `{hostAuditDir}:/audit`
  - Add to ContainerSpec.Env: `AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl`
- trackedRun struct (stub_handlers.go) may need to store the audit path for
  Step 4 ingestion. Or derive it from runID + homePaths.

**Step 4: Ingest harness audit after run**
- In the Stop handler (control_handlers.go:221), after container stops:
  - Read `{hostAuditDir}/harness-audit.jsonl`
  - Parse each JSONL record (audit.AuditRecord format)
  - Append each to the daemon's audit chain via `s.auditWriter.Append(record)`
  - Refresh auditIndex
- Note: harness records are flat (no hash chain). Daemon re-chains them.
- Note: NO otel span conversion (deferred to Block 13.5).

**Step 5: Update test agent**
- `/tmp/agentpaas-e2e-agent/main.py`: call `agent.http("GET", "https://example.com")`
  inside try/except, report the failure.
- Rebuild agent image after update.

### E2E test plan for BUG 7d verification (after Steps 3-5)

1. Build all 3 binaries: `bin/agentpaas`, `bin/agentpaasd`, `bin/agentpaas-harness`
   + `bin/agentpaas-harness-linux` (linux/arm64 for container).
2. Start daemon with e2e home: `AGENTPAAS_HOME=/tmp/agentpaas-e2e-home`
3. Pack weather-agent: `bin/agentpaas pack weather-agent`
4. Run: `bin/agentpaas run weather-agent`
5. Wait for run to complete (container will exit after Python agent finishes).
6. Stop: `bin/agentpaas stop <runID>`
7. Query audit: `bin/agentpaas audit --run-id <runID>`
8. VERIFY: audit records contain `egress_denied` event with destination
   `https://example.com`, method `GET`, reason containing connection error.
9. No mock keys, no bypassed services — real Docker container on internal-only
   network, real HTTP attempt that fails due to network isolation.

## Block 13.5 — Real-time Egress Timeline (post-B13)

**Scope:** Live egress events in the dashboard timeline during long-running agents.
**Status:** Planned — not implemented in B13.

### Architecture (Option B — deferred from BUG 7d)

The harness runs inside an isolated container with only a Unix socket RPC to
the daemon. The daemon's EventBus (trigger.EventBus) drives the dashboard
SSE timeline. To get live egress events, the harness must surface egress
decisions to the daemon in real time.

**Approach:**
- Harness `handleHTTP` already emits audit events via FileAuditAppender.
- Add an RPC method on the harness Unix socket: `egress_event` — the
  harness writes egress decisions as they happen, the daemon tails them.
- Alternatively: the daemon tails the harness audit JSONL file via a
  mounted volume, converts each new line to an otel span (IngestTraces),
  and publishes an EventBus event.
- The dashboard timeline (classifySpan) already classifies spans with
  name "egress" + attr `egress.allowed` -> `egress_allowed`/`egress_denied`.
  So ingested spans appear live in the timeline.

**Implementation steps:**
1. Harness: emit otel-formatted egress spans to a shared file or RPC channel.
2. Daemon: tail the egress event stream during the run, call otelStore.IngestTraces per event.
3. Dashboard: no changes needed — classifySpan already handles egress spans.
4. Test: long-running agent that makes multiple HTTP calls; verify egress_allowed/egress_denied rows appear in timeline SSE stream in real time.

**Prerequisite:** B13 must be complete first (harness audit appender + volume
mount + post-run audit ingestion provides the foundation).

## REMAINING WORK (from v4, updated)

1. ~~BUG 7d: DENIED probe~~ — Steps 1-2 done, Steps 3-5 pending
2. ~~Pre-existing daemon race (server.go:242)~~ — FIXED (a15ceca)
3. B13-T05: complete e2e acceptance (DENIED probe is the last piece)
4. B13-T06: prompt-change immutable redeploy
5. B13-T07: demo matrix fixtures
6. B13-T08: /agentpaas slash commands
7. B13-T09: bundled SKILL.md + plugin.yaml
8. `make block13-gate` implementation
9. Block-end verifier + b13-block-end.md

## Block 13.5 — Real-time Egress Timeline (post-B13)

**Scope:** Live egress events in the dashboard timeline during long-running agents.
**Status:** Planned — not implemented in B13.

### Architecture (Option B — deferred from BUG 7d)

The harness runs inside an isolated container with only a Unix socket RPC to
the daemon. The daemon's EventBus (trigger.EventBus) drives the dashboard
SSE timeline. To get live egress events, the harness must surface egress
decisions to the daemon in real time.

**Approach:**
- Harness `handleHTTP` already emits audit events via FileAuditAppender.
- Add an RPC method on the harness Unix socket: `egress_event` — the
  harness writes egress decisions as they happen, the daemon tails them.
- Alternatively: the daemon tails the harness audit JSONL file via a
  mounted volume, converts each new line to an otel span (IngestTraces),
  and publishes an EventBus event.
- The dashboard timeline (classifySpan) already classifies spans with
  name "egress" + attr `egress.allowed` -> `egress_allowed`/`egress_denied`.
  So ingested spans appear live in the timeline.

**Implementation steps:**
1. Harness: emit otel-formatted egress spans to a shared file or RPC channel.
2. Daemon: tail the egress event stream during the run, call otelStore.IngestTraces per event.
3. Dashboard: no changes needed — classifySpan already handles egress spans.
4. Test: long-running agent that makes multiple HTTP calls; verify egress_allowed/egress_denied rows appear in timeline SSE stream in real time.

**Prerequisite:** B13 must be complete first (harness audit appender + volume
mount + post-run audit ingestion provides the foundation).
