# BUG-043 — Durable path: InvokeDeployment admits but never starts container

**Status:** OPEN  
**Severity:** P0 (blocks v0.3.0 release claim for durable multi-turn execution)  
**Found:** 2026-07-23 B32 pre-v0.3.0 manual testing (durable path verification)  
**Build:** CLI 0.3.0-dev commit 7da75ef  

## Symptom

`agentpaas run --deployment-ref <dep-id> --input '{...}'` returns `ADMISSION_OUTCOME_ACCEPTED` with a run_id, but the run stays PENDING forever. No container starts, no audit events, no invoke-response.

```
Invoke outcome: ADMISSION_OUTCOME_ACCEPTED (run run-d2235gi6xgx2qb66jcze3pq6yy)
```

Routed run status stays `PENDING`. Daemon restart does not pick it up. No container is created.

## Root cause

`InvokeDeployment` in `internal/daemon/routed_handlers.go` admits the invocation (writes to routed store as PENDING) but does NOT start a container. The `Run` RPC in `internal/daemon/control_handlers.go` blocks `--deployment-ref` with `routed_run_invocation_not_enabled (B28)`.

There is no code path that:
1. Takes a PENDING durable run
2. Starts a Docker container for it (like `startRun` does for legacy runs)
3. Creates an attempt
4. Wires the supervisor to supervise the attempt lifecycle

The supervisor (`internal/supervisor/`) has `Reconcile()` but it only handles runs that already have an attempt — it cannot bootstrap a PENDING run into a RUNNING container.

## What B30 delivered (unit tests only)

- Supervisor CAS state machine, stall timer, finalization: unit tests PASS
- ReferenceWorker multi-phase (progress, checkpoint, result): unit tests PASS
- Longevity: 24h fake-clock, 100+ turns: unit test PASS
- Fault injection (13 scenarios): unit tests PASS
- Adversary (checkpoint tamper, digest): unit tests PASS

All tests are in-process with fake stores and mock containers. No real Docker container is started in any B30 test.

## Expected

After `InvokeDeployment` admits a run:
1. Daemon starts a Docker container (same hardening as legacy Run path)
2. Creates an attempt with lease
3. Supervisor supervises: progress events, stall timer, checkpoint resume
4. Container runs the agent, emits progress, commits terminal result
5. Supervisor finalizes: verifies result, writes terminal state, cleans up
6. `agentpaas status <run_id>` shows RUNNING → COMPLETED
7. `invoke-response.json` appears on disk
8. Daemon restart mid-run: supervisor reconciles, revokes ambiguous lease, preserves checkpoint

## Impact

- Durable multi-turn execution (B30 headline claim) is NOT operator-reachable
- Cannot release v0.3.0 claiming "durable runtime, long-running invocation" without this
- All B30 tests pass but are library-level only

## Fix plan

The fix needs to wire `InvokeDeployment` → container start → supervisor:

1. After `InvokeDeployment` admits (PENDING), call a new `startDurableRun` method
2. `startDurableRun` reuses the existing `startRun` container creation logic (network, gateway, binds, audit)
3. After container starts, create an attempt record with lease
4. Wire supervisor to track the attempt (stall timer, progress handler)
5. On daemon restart, `Reconcile` finds PENDING runs with no attempt and starts them

This mirrors how BUG-040 wired delegation trust state — the pieces exist but aren't connected.

## Acceptance

1. `agentpaas run --deployment-ref <dep> --input '{...}'` starts a container and the agent runs
2. `agentpaas status <run_id>` shows RUNNING then COMPLETED
3. `invoke-response.json` appears on disk
4. Kill daemon mid-run → restart → supervisor reconciles (FAILED with daemon_restart or resumes from checkpoint)
5. All existing supervisor/adversary tests still pass
6. Golden gate entries for durable path added
