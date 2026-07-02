# 14A0-T01 + T03: Run Status Tracking + Invoke/Stop Synchronization

You are implementing two correctness fixes in the AgentPaaS daemon. They touch
the same code paths and must be done together.

## Repo

Working directory: `/tmp/b14a0-t01-t03`
Branch: `feat/b14a0-t01-t03` (already checked out)

## What to Fix

### T01: Run Status Tracking

**Problem:** The auto-invoke goroutine discards the invoke result. `Stop()`
always publishes `EventRunSucceeded` regardless of whether the agent crashed,
the invoke failed, or the container exited non-zero.

**Fix:**

1. Add a `status` field to `trackedRun` in `internal/daemon/stub_handlers.go`:
   ```go
   type trackedRun struct {
       Container runtime.ContainerID
       Network   string
       AuditDir  string
       Status    string // "running" | "succeeded" | "failed"
   }
   ```

2. In `trackRun()` (control_handlers.go ~line 615), set `Status: "running"` by default.

3. Add a thread-safe setter method:
   ```go
   func (s *stubControlServer) setRunStatus(runID, status string) {
       s.runMu.Lock()
       defer s.runMu.Unlock()
       if tracked, ok := s.runs[runID]; ok {
           tracked.Status = status
           s.runs[runID] = tracked
       }
   }
   ```

4. Add a thread-safe getter that returns status AND container/network/auditDir:
   ```go
   func (s *stubControlServer) lookupRunWithStatus(runID string) (trackedRun, bool) {
       s.runMu.Lock()
       defer s.runMu.Unlock()
       if s.runs == nil {
           return trackedRun{}, false
       }
       tracked, ok := s.runs[runID]
       return tracked, ok
   }
   ```

5. In the auto-invoke goroutine (control_handlers.go ~line 251): after
   `s.invokeAgent()` returns, call `s.setRunStatus(runID, "failed")` on error,
   or `s.setRunStatus(runID, "succeeded")` on success.

6. In `Stop()` (control_handlers.go ~line 263): replace the existing
   `lookupRun` call with `lookupRunWithStatus`. After stopping the container
   but BEFORE removing it, check the container exit code via `rt.Status()`.
   If the container status is `ContainerStatusStopped` and the invoke status
   was `"succeeded"`, keep it. If invoke status was `"failed"`, keep `"failed"`.
   If the container exited with a non-zero code (check via Docker inspect or
   the exit code from the invoke goroutine), override to `"failed"`.

   Since `ContainerStatus` doesn't carry an exit code, use this logic:
   - If `tracked.Status == "running"` when Stop is called, the invoke hasn't
     completed yet — mark it based on whether the invoke context was cancelled
     (T03's cancel func) vs completed. If still running, default to `"succeeded"`
     unless force-stopped.
   - If `tracked.Status == "failed"`, keep `"failed"`.
   - If `tracked.Status == "succeeded"`, keep `"succeeded"`.
   - If `req.GetForce()` is true, set status to `"failed"` (forced stop).

7. In Stop's event publishing (~line 296): publish `EventRunFailed` instead of
   `EventRunSucceeded` when status is `"failed"`. Keep `EventRunCancelled` for
   force stops. The logic becomes:
   ```go
   eventType := trigger.EventRunSucceeded
   switch {
   case req.GetForce():
       eventType = trigger.EventRunCancelled
   case finalStatus == "failed":
       eventType = trigger.EventRunFailed
   }
   ```

8. Add `"status": finalStatus` to the `run_stop` audit payload.

NOTE: `EventRunFailed` already exists in `internal/trigger/eventbus.go` (line 16).
You do NOT need to add it.

### T03: Invoke/Stop Synchronization

**Problem:** The auto-invoke goroutine uses `context.WithTimeout(context.Background(), 2*time.Minute)`
which is detached from the run lifecycle. If `Stop()` removes the container
while `invokeAgent()` is polling `/readyz`, the next `rt.Exec()` fails against
a removed container.

**Fix:**

1. Add a `cancelInvoke context.CancelFunc` field to `trackedRun`:
   ```go
   type trackedRun struct {
       Container    runtime.ContainerID
       Network      string
       AuditDir     string
       Status       string
       CancelInvoke context.CancelFunc
   }
   ```

2. In the Run handler's auto-invoke goroutine (~line 251): create a cancellable
   context that is tied to the run lifecycle:
   ```go
   invokeCtx, cancel := context.WithCancel(context.Background())
   timeoutCtx, timeoutCancel := context.WithTimeout(invokeCtx, 2*time.Minute)
   defer timeoutCancel()
   ```

   Store `cancel` in the tracked run BEFORE launching the goroutine. You need
   to update the trackedRun after creating the cancel func. Approach:
   - Call `s.trackRun(...)` as before (with Status: "running").
   - Then create the cancel func and update the tracked run:
     ```go
     invokeCtx, cancel := context.WithCancel(context.Background())
     s.setRunCancel(runID, cancel)
     go func() {
         defer cancel()
         timeoutCtx, timeoutCancel := context.WithTimeout(invokeCtx, 2*time.Minute)
         defer timeoutCancel()
         if err := s.invokeAgent(timeoutCtx, containerID); err != nil {
             s.setRunStatus(runID, "failed")
             fmt.Fprintf(os.Stderr, "daemon: auto-invoke (%s): %v\n", runID, err)
         } else {
             s.setRunStatus(runID, "succeeded")
         }
     }()
     ```

   Add the `setRunCancel` method:
   ```go
   func (s *stubControlServer) setRunCancel(runID string, cancel context.CancelFunc) {
       s.runMu.Lock()
       defer s.runMu.Unlock()
       if tracked, ok := s.runs[runID]; ok {
           tracked.CancelInvoke = cancel
           s.runs[runID] = tracked
       }
   }
   ```

3. In `Stop()`, BEFORE stopping/removing the container: call
   `tracked.CancelInvoke()` if non-nil. This signals the invoke goroutine to
   exit. The goroutine's `invokeAgent` already checks `ctx.Done()` in its
   polling loop and returns `ctx.Err()` cleanly.

   The invoke goroutine runs asynchronously — we cancel and continue. We do
   NOT need to wait for it (it will exit on its own after ctx.Done()).

4. Import `"context"` is already present in stub_handlers.go and control_handlers.go.

## Tests to Write

Write tests in `internal/daemon/control_handlers_test.go`:

### Test 1: TestRun_FailedInvoke_SetsFailedStatus

```go
func TestRun_FailedInvoke_SetsFailedStatus(t *testing.T) {
    // Use mock Docker driver that returns error on Exec
    // Call Run (which will launch the auto-invoke goroutine)
    // Wait for invoke to fail (poll setRunStatus or sleep briefly)
    // Call Stop
    // Assert: event published is EventRunFailed (not EventRunSucceeded)
    // Assert: run_stop audit payload has "status": "failed"
}
```

### Test 2: TestStop_CancelsInvokeContext

```go
func TestStop_CancelsInvokeContext(t *testing.T) {
    // Use mock Docker driver where Exec blocks/hangs on readyz
    // Call Run — invoke goroutine starts polling readyz
    // Immediately call Stop
    // Assert: no panic, no "container not found" errors
    // Assert: invoke goroutine exits cleanly (context cancelled)
}
```

### Test 3: TestStop_Force_SetsCancelledStatus

```go
func TestStop_Force_SetsCancelledStatus(t *testing.T) {
    // Call Run, wait for invoke success
    // Call Stop with Force=true
    // Assert: event published is EventRunCancelled
    // Assert: audit payload has appropriate status
}
```

Use the existing mock driver pattern from `NewDockerRuntimeWithDriver` /
`mockRuntimeDriver` in `internal/runtime/driver_test.go`. Look at existing tests
like `TestRun_RejectsWhenConcurrentLimitReached` in
`control_handlers_test.go` for the pattern.

## Build and Test

After making changes:
```bash
cd /tmp/b14a0-t01-t03
go build ./...
go test -race -count=1 ./internal/daemon/...
make lint
```

All must pass. Fix any lint issues (ST1005: lowercase error strings, etc.).

## Constraints

- Do NOT rename `stubControlServer` (that's T05, a separate task)
- Do NOT touch T02 (orphan reconciliation) or T04 (Docker e2e)
- Keep changes minimal — only what's needed for status tracking + sync
- All existing tests must continue to pass
- Use `go vet` and `make lint` to verify
