# 14A0-T01+T03: Fix Issues Found by Adversary + Verifier

The initial implementation passed build/test/lint but the adversary and verifier
found 3 real bugs. Fix ALL of them in this dispatch.

## Repo

Working directory: `/tmp/b14a0-t01-t03` (feat/b14a0-t01-t03 branch)

## Current State

The trackedRun struct, status tracking, and cancel func are already implemented.
The issues are in Stop() logic and missing synchronization. Read the current
code first, then apply fixes.

## Bug 1 (HIGH): Double-Stop race

Two concurrent Stop() calls both succeed because there's no serialization
between lookupRunWithStatus and untrackRun.

**Fix:** Change the runs map to store pointers (`map[string]*trackedRun`), and
add a `claimRun` method that atomically deletes from the map under lock:

```go
func (s *stubControlServer) claimRun(runID string) (*trackedRun, bool) {
    s.runMu.Lock()
    defer s.runMu.Unlock()
    if s.runs == nil {
        return nil, false
    }
    tracked, ok := s.runs[runID]
    if !ok {
        return nil, false
    }
    delete(s.runs, runID)
    return tracked, true
}
```

Stop() uses claimRun instead of lookupRunWithStatus + untrackRun. The second
concurrent Stop gets false → NotFound.

**IMPORTANT:** This requires changing `map[string]trackedRun` to
`map[string]*trackedRun` throughout ALL methods that touch s.runs:
- trackRun: create `&trackedRun{...}`, store pointer
- setRunStatus: lock, get pointer, update `tracked.Status` directly (no need to reassign since it's a pointer)
- setRunCancel: same pattern
- lookupRunWithStatus: return the pointer's values (for compat with callers that expect a value) — or change return type
- activeRunCount: unchanged
- lookupRun (the old method used by Logs): unchanged, just follows pointer

Actually, since trackedRun is now a pointer in the map, setRunStatus becomes:
```go
func (s *stubControlServer) setRunStatus(runID, status string) {
    s.runMu.Lock()
    defer s.runMu.Unlock()
    if tracked, ok := s.runs[runID]; ok {
        tracked.status = status
    }
}
```
No need to reassign since it's a pointer.

## Bug 2 (HIGH): Missing invoke goroutine wait

T03 spec requires: after cancelling the invoke context, WAIT briefly for the
goroutine to finish before proceeding with container removal.

**Fix:** Add `invokeDone chan struct{}` to trackedRun. The invoke goroutine
closes it when done. Stop() waits on it (with a 3-second timeout) after
cancelling.

```go
type trackedRun struct {
    Container    runtime.ContainerID
    Network      string
    AuditDir     string
    status       string           // protected by runMu
    cancelInvoke context.CancelFunc
    invokeDone   chan struct{}    // closed when invoke goroutine exits
    invokeErr    error            // written before close(invokeDone); safe to read after channel receive
}
```

In the Run handler's auto-invoke goroutine:
```go
invokeDone := make(chan struct{})
// ... store in trackedRun ...
go func() {
    defer close(invokeDone)
    timeoutCtx, timeoutCancel := context.WithTimeout(invokeCtx, 2*time.Minute)
    defer timeoutCancel()
    err := s.invokeAgent(timeoutCtx, containerID)
    s.setRunStatus(runID, ...)
    s.invokeErr = err  // NO — set on the pointer, not on s
}()
```

Wait — the goroutine captures runID but needs to write invokeErr to the
trackedRun. Since we're using pointers now, the goroutine can capture the
*trackedRun pointer. But the pointer is created in trackRun... Let me think.

Better approach: create the trackedRun pointer, the done channel, and the
cancel func all BEFORE launching the goroutine. The goroutine captures the
pointer:

```go
// In Run handler, after container start:
tracked := &trackedRun{
    Container:  containerID,
    Network:    string(netID),
    AuditDir:   hostAuditDir,
    status:     "running",
    invokeDone: make(chan struct{}),
}
s.trackRunPtr(runID, tracked)

invokeCtx, cancel := context.WithCancel(context.Background())
tracked.cancelInvoke = cancel

go func(tr *trackedRun) {
    defer close(tr.invokeDone)
    timeoutCtx, timeoutCancel := context.WithTimeout(invokeCtx, 2*time.Minute)
    defer timeoutCancel()
    if err := s.invokeAgent(timeoutCtx, containerID); err != nil {
        s.setRunStatus(runID, "failed")
        tr.invokeErr = err
        fmt.Fprintf(os.Stderr, "daemon: auto-invoke (%s): %v\n", runID, err)
    } else {
        s.setRunStatus(runID, "succeeded")
    }
}(tracked)
```

trackRunPtr:
```go
func (s *stubControlServer) trackRunPtr(runID string, tr *trackedRun) {
    s.runMu.Lock()
    defer s.runMu.Unlock()
    if s.runs == nil {
        s.runs = make(map[string]*trackedRun)
    }
    s.runs[runID] = tr
}
```

In Stop(), after claimRun:
```go
tracked, ok := s.claimRun(runID)
if !ok {
    return nil, status.Errorf(codes.NotFound, "run %q not found", runID)
}

// Cancel invoke goroutine and wait for it to finish.
if tracked.cancelInvoke != nil {
    tracked.cancelInvoke()
}
select {
case <-tracked.invokeDone:
    // goroutine finished; status is up-to-date
case <-time.After(3 * time.Second):
    // timeout — mark as failed
    tracked.status = "failed"
}
```

After this wait, read `tracked.status` and `tracked.invokeErr` directly from
the pointer (no lock needed — the goroutine has finished or timed out, and
we've claimed the run so no one else can access it).

## Bug 3 (MEDIUM): Force-stop event/audit inconsistency

Force stop publishes EventRunCancelled but audit status is "failed".

**Fix:** Add "cancelled" as a fourth status value. When force=true, set
finalStatus = "cancelled". This aligns with EventRunCancelled.

```go
finalStatus := tracked.status
if req.GetForce() {
    finalStatus = "cancelled"
}
```

And the event logic stays the same:
```go
switch {
case req.GetForce():
    eventType = trigger.EventRunCancelled
case finalStatus == "failed":
    eventType = trigger.EventRunFailed
}
```

Update existing tests:
- TestStop_Force_SetsCancelledStatus: audit status should now be "cancelled" (not "failed")

## Bug 4 (LOW): Remove dead rt.Status() code

The rt.Status() call in Stop() only re-asserts "failed" when already "failed"
— it's dead code. Remove the entire block:
```go
// REMOVE THIS:
if containerStatus, statusErr := rt.Status(ctx, containerID); statusErr == nil {
    if containerStatus == runtime.ContainerStatusStopped && tracked.Status == "failed" {
        finalStatus = "failed"
    }
}
```

## Bug 5: Add missing test — normal success stop

Add `TestStop_NormalSuccess_Succeeds`:
```go
func TestStop_NormalSuccess_Succeeds(t *testing.T) {
    // Mock where invoke succeeds (readyz ok, invoke ok)
    // Run → wait for status "succeeded"
    // Stop (non-force)
    // Assert: EventRunSucceeded published
    // Assert: audit status = "succeeded"
}
```

## Build and Test

```bash
cd /tmp/b14a0-t01-t03
go build ./...
go test -race -count=1 ./internal/daemon/...
make lint
```

All must pass. Also verify the adversary tests (TestAdv_*) still compile if
they were written — you may need to update them for the new pointer-based map
and "cancelled" status. Fix them to match the corrected behavior.

## Constraints

- Keep changes focused on these 5 bugs
- Do NOT touch T02 (orphan reconciliation) or T04 (Docker e2e) or T05 (rename)
- The runs map change to `map[string]*trackedRun` must not break existing callers
  (lookupRun is used by Logs handler — verify it still works)
- All existing tests must pass
