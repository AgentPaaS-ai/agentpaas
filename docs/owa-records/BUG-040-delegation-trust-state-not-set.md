# BUG-040 — Delegation trust state not set in packed runs (R32 step 3 BLOCKED)

**Status:** OPEN  
**Severity:** P1 (blocks R32 quickstart step 3 — multi-agent A2A live path)  
**Found:** 2026-07-23 B32 pre-v0.3.0 manual testing (R32 Tier B, step 3)  
**Build:** CLI 0.3.0-dev commit ed22b0f  

## Symptom

Parent agent packed with `workflow.yaml` `delegations:` binding `r32.echo` → promoted `worker@c9bca5b8`. On invoke, `agent.delegate("r32.echo", ...)` raises:

```python
agentpaas_sdk._rpc.RPCError: RPCError(no_trust_state): delegation trust state not set
```

Both parent runs failed immediately (`run-ccf368d049a88f2f`, `run-3cca1d3d21c18ca0`).

## Root cause

`workflow.yaml` `delegations:` validates at pack time (`ValidateWorkflowYAML`, `BuildCommunicationSnapshot`), but the trust state / communication snapshot is **not injected** into the harness RPC server running inside the Docker container during a live packed run.

Unit tests pass because they construct `harnessRPCServer` with in-process trust state / delegation bindings. Packed runs start the harness via the Linux binary, which has no delegation state from the workflow.

## Expected

Pack or run must:
1. Read `workflow.yaml` delegations from the packed image.
2. Build communication snapshot.
3. Inject delegation trust state (bindings, capabilities, snapshot digest) into the harness at container start.
4. `agent.delegate(capability=...)` resolves via the injected trust state.

## Actual

Pack validates workflow.yaml but does not pass delegation state to the harness. Harness starts with empty trust state. `delegate_task` RPC returns `no_trust_state`.

## Impact

- R32 step 3 (logical delegate) **cannot be proven in a live operator run**.
- R32 steps 4–6 (artifact, wait/wake, denials) likely also blocked (same trust state path).
- Unit/adversary tests (Tier A) pass — code is correct in isolation.
- Product claim must say: "A2A delegation is library/harness-proven; live packed-run wiring is incomplete in v0.3.0."

## Evidence

- Audit: `run_failed` + `failure_context` with `RPCError(no_trust_state)` for both parent runs
- `~/.agentpaas/state/audit.jsonl` seq 4–5 and 8–9
- Harness audit: `~/.agentpaas/state/runs/run-ccf368d049a88f2f/harness-audit/harness-audit.jsonl`
- Parent code: `/tmp/r32-a2a/parent/main.py` uses `agent.delegate("r32.echo", ...)` correctly
- Workflow: `/tmp/r32-a2a/parent/workflow.yaml` has valid `delegations:` section

## Related

- B32 risk: "east-west multi-container live path thin; unit simulation"
- B32 summary: T03 "Logical invocation and gateway enforcement" — SDK/control API works in tests
- This bug is the gap between T03 unit tests and live packed runs
- All R32 steps 3–6 blocked until this is wired

## Proposed fix (later, not this manual test)

1. Pack or run startup reads workflow.yaml delegations from image.
2. Builds `CommunicationSnapshot` (already exists in `internal/pack`).
3. Passes snapshot + trust state to harness via env var, file mount, or RPC init.
4. Harness `delegate_task` handler resolves capability from injected state.

This is a daemon/harness wiring task, not a new schema or SDK change.
