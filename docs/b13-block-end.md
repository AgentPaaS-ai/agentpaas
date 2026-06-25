# Block 13 — Block-End Report

**Date:** 2026-06-25
**Status:** COMPLETE — `make block13-gate` passes
**Branch:** main (local-first)

## Summary

Block 13 delivers the Hermes integration plugin, e2e governance verification
(BUG 7d solved), demo matrix fixtures, and the `block13-gate` success gate.
The full deploy → govern → audit lifecycle works end-to-end with real Docker
containers on isolated networks.

## Completed Subtasks

### T05: E2E Deploy Acceptance (BUG 7d) — SOLVED

**The invoke gap was the final blocker.** The harness HTTP server binds to
127.0.0.1:8080 inside the container. The daemon started the container but
never invoked the agent, so no egress audit events were produced.

**Solution:** Added `Exec` to the `RuntimeDriver` interface and `DockerRuntime`
implementation. The daemon auto-invokes the agent after container start via
`docker exec` (polls `/readyz`, then POSTs `/invoke`).

**E2E verification (live Docker):**
```
pack weather-agent → run-384416e8208ae4e0 → auto-invoke → agent.http("GET", "https://example.com")
→ network isolation → egress_denied audit event → stop → audit chain ingested
```

Audit chain (hash-chained, verified):
```
seq 1: pack (agent deployed)
seq 2: run_start (container started)
seq 3: egress_denied (destination=https://example.com, method=GET, reason=http request failed)
seq 4: run_stop (clean shutdown)
```

Commit: `13e489c` — "fix(b13): add docker Exec + auto-invoke harness after container start"

**Research validation:** The docker exec invoke pattern is the industry
standard — AWS Bedrock AgentCore uses `InvokeAgentRuntimeCommand` for the
same purpose (execute inside a running container to trigger agent code).
ZenML Kitaru and OpenAI Sandboxes use the same control-plane-invokes-sandbox
architecture.

### T06: Prompt-Change Immutable Redeploy — VERIFIED

Already fully tested in `internal/pack/update_e2e_test.go`:
- Source digest changes when prompt is edited (digest1 != digest2)
- `VerifyDeployedIntegrity` passes on untampered deployment
- Tampered deployment rejected with `ErrImmutableViolation`
- Audit record emitted for violations

The immutable redeploy path works: edit project → validate → pack → verify →
run produces a new run with distinct digests. The old container/image/lockfile
is never mutated in place (`RecordDeployment` uses atomic staging-dir swap).

### T07: Demo Matrix Fixtures — CREATED

Three P1 differentiation demo agents in `demo/`:

1. **governed-weather/** — allowed weather API + denied exfil probe
2. **secret-saas/** — brokered credential through harness (agent code never sees secret)
3. **repair-loop/** — LLM + egress + MCP denial chain for the repair loop

Each demo has `agent.yaml`, `main.py`, `requirements.txt`. README documents
run instructions and expected audit events.

Commit: `ac54377` — "feat(b13): demo matrix fixtures"

### T08: Slash Commands — IMPLEMENTED

Four slash commands registered via `ctx.register_command` in the Hermes plugin:

| Command | Function | Tools Called |
|---------|----------|-------------|
| `/agentpaas deploy <path>` | pack → run | `agentpaas_pack`, `agentpaas_run` |
| `/agentpaas status` | show active runs | `agentpaas_status` |
| `/agentpaas logs [run_id]` | tail logs | `agentpaas_logs` |
| `/agentpaas audit [run_id]` | show audit events | `agentpaas_audit_query` |

Each is a thin orchestrator over the plugin's own tools — no logic duplication.

### T09: Bundled SKILL.md — CREATED

`integrations/hermes-plugin/SKILL.md` registered via `ctx.register_skill`.
Teaches the detect → init → validate → pack → run → inspect → audit flow
with pitfalls (Docker not running, policy denial, agent not found).

### Plugin (17 Tools) — COMPLETE

The Hermes plugin (`integrations/hermes-plugin/`) exposes all 17 required P1
operator-contract tools:

`agentpaas_init_project`, `agentpaas_reconcile_project`,
`agentpaas_validate_project`, `agentpaas_doctor`, `agentpaas_pack`,
`agentpaas_run`, `agentpaas_stop`, `agentpaas_logs`, `agentpaas_status`,
`agentpaas_get_run_timeline`, `agentpaas_policy_show`,
`agentpaas_explain_policy_denial`, `agentpaas_recommend_policy_patch`,
`agentpaas_audit_query`, `agentpaas_export_audit`, `agentpaas_summarize_run`,
`agentpaas_explain_failure`, `agentpaas_next_action`.

Includes: `schemas.py` (tool schemas), `tools.py` (CLI-shelling handlers),
`sanitizer.py` (prompt-injection boundary), `contracts.py` (parity tests),
`tests/` (conformance tests).

### block13-gate — IMPLEMENTED

`make block13-gate` verifies:
- Build + lint clean (golangci-lint, 0 issues)
- Unit tests with -race (harness, daemon, runtime)
- Immutable redeploy test (prompt-change → distinct digests)
- Plugin file existence (plugin.yaml, __init__.py, tools.py, SKILL.md)
- Demo fixture existence (3 scenarios)
- Plugin Python syntax validation

Result: **PASS**

Commit: `bd5afc9` — "feat(b13): Hermes plugin slash commands + SKILL.md + block13-gate"

## Commits This Session

```
bd5afc9 feat(b13): Hermes plugin slash commands + SKILL.md + block13-gate
ac54377 feat(b13): demo matrix fixtures (3 P1 differentiation scenarios)
13e489c fix(b13): add docker Exec + auto-invoke harness after container start (BUG 7d)
d0113af docs(b13): resume prompt turn 35 — invoke gap is the blocker  [prior session]
```

## Architecture Decisions

### Invoke Mechanism: Docker Exec (not trigger server)

The B13 spec mentions three approaches for the invoke gap. We chose docker
exec because:
- Simplest for local-first P1 (no separate trigger server needed)
- Harness binds to 127.0.0.1 inside the container (security: unreachable from host)
- Docker exec reaches the container's network namespace directly
- Matches industry standard (AWS Bedrock AgentCore InvokeAgentRuntimeCommand)
- No port forwarding, no network exposure

The daemon's `invokeAgent` method:
1. Polls `/readyz` via `docker exec` (up to 30 attempts, 1s apart)
2. POSTs to `/invoke` via `docker exec` with a 60s timeout
3. Runs in a goroutine after container start (non-blocking)
4. Errors are logged, not fatal (run is still tracked, Stop still works)

### Exec on RuntimeDriver Interface

Added `Exec(ctx, id, cmd) (stdout, stderr string, exitCode int, err error)`
to the `RuntimeDriver` interface. This is a permanent, broadly useful
addition — any container runtime needs exec capability for orchestration.
Implemented on `DockerRuntime` using `ContainerExecCreate/Attach` with
`stdcopy.StdCopy` for stream demuxing.

## What's Next (Block 14)

Block 13 is complete. Block 14 (consolidated post-B13 work) can begin:

- **14A:** Security remediation (9 tasks from B13 security audit)
- **14B:** Real-time egress timeline (harness → daemon → dashboard SSE)
- **14C:** Install path, docs, demo video, v0.1.0 release

All three depend on B13 being fully done — which it now is.
