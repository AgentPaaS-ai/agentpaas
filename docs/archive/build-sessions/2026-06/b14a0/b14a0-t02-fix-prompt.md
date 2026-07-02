# Worker Task: Fix T02 Adversary Findings — Orphan Reconciliation Hardening

## Context
AgentPaaS repo at /Users/pms88/projects/agentpaas, on main branch.
The adversary reviewed the T02 orphan reconciliation code and found 6 valid issues.
Create branch `feat/b14a0-t02-fix` and fix all 6 issues.

## Files to Edit
- `internal/daemon/control_handlers.go` — method `reconcileOrphanedContainers` (line ~1024)
- `internal/daemon/control_handlers_test.go` — add test cases for error paths

## Fixes Required

### Fix 1: Add managed-by label filter to container listing (HIGH — Security)
**Problem:** `ListContainers` at line ~1040 filters only on `LabelResourceType=ResourceTypeAgent`. A non-AgentPaaS container with a spoofed `agentpaas.resource-type=agent` label would be stopped and removed.

**Fix:** Change the container filter to also include `LabelManagedBy=ManagedByValue`. Use multiple label filters:
```go
containers, err := rt.ListContainers(ctx,
    runtime.LabelManagedBy+"="+runtime.ManagedByValue,
    runtime.LabelResourceType+"="+runtime.ResourceTypeAgent,
)
```
This matches the pattern in `internal/runtime/reconcile.go` line 34 which lists owned containers with `LabelManagedBy+"="+ManagedByValue`.

### Fix 2: Move ListNetworks outside the per-container loop (LOW — Efficiency)
**Problem:** `ListNetworks` is called inside the per-container orphan loop (line ~1060), causing O(n) redundant Docker API calls — once per orphaned container.

**Fix:** Move the network listing to ONCE before the container loop. Store the result and filter by runID inside the loop.
```go
// List internal networks once for cleanup.
networks, netErr := rt.ListNetworks(ctx,
    runtime.LabelManagedBy+"="+runtime.ManagedByValue,
    runtime.LabelResourceType+"="+runtime.ResourceTypeNetInternal,
)
```
Then inside the container loop, iterate `networks` to find matching runIDs.

### Fix 3: Always emit reconciliation_complete audit event (LOW — Audit)
**Problem:** `reconciliation_complete` only fires when `removals > 0` (line ~1106). If orphans existed but all removals failed, or if no orphans were found, there's no audit trail of the reconciliation attempt.

**Fix:** Always emit `reconciliation_complete` with the removal count (including 0). Move it outside the `if removals > 0` check.

### Fix 4: Fix container_reconciled action text (LOW — Audit)
**Problem:** `action` is always `"stopped_and_removed"` even when the container was already stopped (not running, so Stop was skipped).

**Fix:** Track whether Stop was called:
```go
action := "removed"
if c.Status == runtime.ContainerStatusRunning {
    timeout := 10 * time.Second
    if err := rt.Stop(ctx, runtime.ContainerID(c.ID), &timeout); err != nil {
        fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: stop container %s: %v\n", c.ID, err)
    } else {
        action = "stopped_and_removed"
    }
}
```

### Fix 5: Emit audit event on Remove failure (MEDIUM — Audit)
**Problem:** If `rt.Remove` fails (line ~1054), the `continue` skips the `container_reconciled` audit event entirely. Failed removals leave no audit trail.

**Fix:** Before the `continue` on Remove failure, emit a `container_reconciled` event with `action: "remove_failed"`:
```go
if err := rt.Remove(ctx, runtime.ContainerID(c.ID), true); err != nil {
    fmt.Fprintf(os.Stderr, "daemon: orphan reconciliation: remove container %s: %v\n", c.ID, err)
    s.recordAudit("container_reconciled", "daemon", map[string]interface{}{
        "run_id":       c.RunID,
        "container_id": c.ID,
        "action":       "remove_failed",
    })
    continue
}
```

### Fix 6: Add test cases for error paths (MEDIUM — Test Coverage)
Add two new test functions in `internal/daemon/control_handlers_test.go`:

**Test 1: `TestReconcileOrphans_RemoveFailure_EmitsAuditEvent`**
- Mock driver: `listContainersFunc` returns one orphaned container (running).
- `stopFunc` succeeds, `removeFunc` returns an error.
- Assert: `container_reconciled` audit event exists with `action: "remove_failed"`.
- Assert: `reconciliation_complete` audit event exists (always emitted now).

**Test 2: `TestReconcileOrphans_ListContainersError_ProceedsToNetworkReconciliation`**
- Mock driver: `listContainersFunc` returns an error.
- `listNetworksFunc` returns one orphaned network.
- Assert: the orphaned network IS removed (reconciliation falls through to network cleanup).
- Assert: `reconciliation_complete` audit event exists.

## Verification
1. `cd /Users/pms88/projects/agentpaas && go build ./...` — must compile
2. `go test ./internal/daemon/... -count=1 -race -timeout 120s` — ALL tests must pass
3. `golangci-lint run ./internal/daemon/...` — must be clean (if available)

## Rules
- Do NOT change any logic beyond the 6 fixes described above
- Do NOT change the function signature of `reconcileOrphanedContainers`
- Do NOT touch any other files
- Commit with message: `fix(14a0-t02): harden orphan reconciliation per adversary review`
