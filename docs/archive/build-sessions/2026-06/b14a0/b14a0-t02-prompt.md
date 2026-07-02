# Task: 14A0-T02 — Orphan Container Reconciliation

You are implementing orphan container reconciliation for the AgentPaaS daemon.
This is a correctness fix: when the daemon crashes, all running agent containers
become invisible to the restarted daemon because the runs map is in-memory only.

## Scope (3 changes)

### Change 1: Fix DockerRuntime delegation bug (runtime/docker.go)

`DockerRuntime.ListContainers` and `DockerRuntime.ListNetworks` do NOT delegate
to the mock driver when one is set. All other methods (Create, Start, Stop, Remove,
Exec, CreateNetwork, RemoveNetwork) have this guard:

```go
if d.driver != nil {
    return d.driver.ListContainers(ctx, labelFilters...)
}
```

Add this guard to BOTH `ListContainers` (line ~477) and `ListNetworks` (line ~535)
in `internal/runtime/docker.go`, right after the function signature, BEFORE the
`d.cli == nil` check.

### Change 2: Add reconcileOrphanedContainers method (daemon/control_handlers.go)

Add a method on `*stubControlServer`:

```go
func (s *stubControlServer) reconcileOrphanedContainers(ctx context.Context)
```

It should:
1. Call `s.getOrCreateRuntime()`. If error, log to stderr and return (best-effort).
2. List all agent containers: `rt.ListContainers(ctx, runtime.LabelResourceType+"="+runtime.ResourceTypeAgent)`
3. Build a set of known run IDs from `s.runs` (under `s.runMu` lock).
4. For each container NOT in knownRuns:
   - Stop it if running (10s timeout)
   - Remove it (force=true)
   - Find and remove its internal network via `rt.ListNetworks(ctx, runtime.LabelResourceType+"="+runtime.ResourceTypeNetInternal)` matching `net.Labels[runtime.LabelRunID] == c.RunID`
   - Record audit: `s.recordAudit("container_reconciled", "daemon", map[string]interface{}{"run_id": c.RunID, "container_id": c.ID, "action": "stopped_and_removed"})`
5. Also list ALL managed networks (`rt.ListNetworks(ctx, runtime.LabelManagedBy+"="+runtime.ManagedByValue)`) and remove orphaned internal ones (runID not in knownRuns, resource-type is net-internal).
6. If any removals happened, record `s.recordAudit("reconciliation_complete", "daemon", ...)`.
7. All errors should be logged to stderr but not abort — reconciliation is best-effort.

Insert this method BEFORE the `resolveExecutable` variable declaration near the
end of the file.

### Change 3: Wire into Daemon.Start() (daemon/server.go)

In `Daemon.Start()`, AFTER `controlv1.RegisterControlServiceServer(d.server, controlServer)`
and BEFORE `d.started = true`, add:

```go
reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer reconcileCancel()
controlServer.reconcileOrphanedContainers(reconcileCtx)
```

### Change 4: Add mock driver support (daemon/control_handlers_test.go)

The `mockRuntimeDriver` struct needs `listContainersFunc` and `listNetworksFunc`
fields. Currently `ListContainers` and `ListNetworks` are hardcoded to return errors.

1. Add fields to the struct:
```go
listContainersFunc func(ctx context.Context, labelFilters ...string) ([]runtime.ContainerInfo, error)
listNetworksFunc   func(ctx context.Context, labelFilters ...string) ([]runtime.NetworkInfo, error)
```

2. Update the methods to use them:
```go
func (m *mockRuntimeDriver) ListContainers(ctx context.Context, labelFilters ...string) ([]runtime.ContainerInfo, error) {
    if m.listContainersFunc != nil {
        return m.listContainersFunc(ctx, labelFilters...)
    }
    return nil, fmt.Errorf("not implemented")
}
```
Same pattern for `ListNetworks`.

### Change 5: Write tests (daemon/control_handlers_test.go)

Add these tests at the end of the file:

1. `TestReconcileOrphans_StopsOrphanedContainers` — mock returns one running
   agent container with runID "run-deadbeef" and one internal network with same
   runID. Server's runs map is empty. After reconcile: container stopped, removed,
   network removed. Assert stopFunc/removeFunc/removeNetworkFunc were called.

2. `TestReconcileOrphans_KeepsTrackedContainers` — mock returns container with
   runID "run-active". Server pre-tracks this run via `server.trackRun(...)`.
   After reconcile: no containers or networks removed.

3. `TestReconcileOrphans_NoDocker_SkipsGracefully` — server with no runtime
   initialized (getOrCreateRuntime will fail). reconcileOrphanedContainers should
   not panic.

Use `testServerWithMockRuntime(t, mock)` to create the server. Look at
`defaultMockRuntimeDriver()` for the pattern. Import `home` package if needed.

## Key References

- `runtime.LabelResourceType`, `runtime.ResourceTypeAgent`, `runtime.ResourceTypeNetInternal` — in `internal/runtime/naming.go`
- `runtime.LabelManagedBy`, `runtime.ManagedByValue` — same file
- `runtime.LabelRunID` — same file
- `runtime.ContainerStatusRunning`, `runtime.ContainerStatusStopped` — in `internal/runtime/driver.go`
- `s.recordAudit(eventType, actor, payload)` — method on stubControlServer
- `s.getOrCreateRuntime()` — returns `*runtime.DockerRuntime`
- `s.runMu` / `s.runs` — mutex and map on stubControlServer
- `testServerWithMockRuntime(t, mock)` — creates server with mock driver wired

## Verify

After all changes:
```bash
go build ./internal/daemon/... ./internal/runtime/...
go test -v -count=1 -run TestReconcileOrphans ./internal/daemon/... -timeout 60s
go test -v -count=1 -race ./internal/daemon/... -timeout 120s  # all daemon tests pass
```

All must pass. Fix any failures.
