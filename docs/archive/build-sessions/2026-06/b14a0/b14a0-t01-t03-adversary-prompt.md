# Adversary: Break 14A0-T01+T03 (Run Status + Invoke/Stop Sync)

You are the ADVERSARY. Your job is to find BREAKS in the run status tracking
and invoke/Stop synchronization implementation. Write adversarial tests that
expose any of these failure modes. Run each test and report which ones FAIL
(revealing a real bug) vs which ones PASS (implementation is correct there).

## Repo

Working directory: `/tmp/b14a0-t01-t03` (feat/b14a0-t01-t03 branch)

## What Was Implemented

1. trackedRun now has Status ("running"/"succeeded"/"failed") and CancelInvoke
2. Auto-invoke goroutine sets status on completion
3. Stop() cancels invoke context, computes finalStatus, publishes correct event
4. Stop() adds "status" to audit payload

## Break Tests to Write

Write ALL of these in `internal/daemon/control_handlers_test.go` and RUN them.
Report which FAIL (real bug found) and which PASS (implementation handles it).

### Break 1: Status race — concurrent setRunStatus + Stop

Two goroutines: one calls setRunStatus, another calls Stop. If the lock isn't
held correctly, the status update can be lost or cause a panic.

```go
func TestAdv_ConcurrentSetStatusAndStop(t *testing.T) {
    // Launch invoke goroutine that takes 500ms
    // Meanwhile call Stop
    // Verify no panic, no data race (run with -race)
    // Verify finalStatus is deterministic (either "failed" from cancel or "succeeded")
}
```

### Break 2: Stop on unknown run after cancel

Call Stop with a run_id that was tracked, invoke completed, status set, but
then untracked by a concurrent Stop. Second Stop should get NotFound.

### Break 3: CancelInvoke nil safety

If Stop is called before setRunCancel fires (timing), CancelInvoke could be nil.
Verify the nil guard works.

```go
func TestAdv_StopBeforeCancelSet(t *testing.T) {
    // Race: Run returns, but setRunCancel hasn't been called yet
    // This is hard to trigger deterministically, but check the code path
    // Manually construct a trackedRun without CancelInvoke and call Stop
}
```

### Break 4: Multiple invokes — status overwritten

What if the invoke goroutine somehow runs twice? Or a late setRunStatus("succeeded")
fires AFTER Stop has already set "failed"? The status could flip from failed→succeeded
after Stop has already published EventRunFailed.

```go
func TestAdv_LateStatusUpdateAfterStop(t *testing.T) {
    // Run → invoke starts
    // Stop immediately → status computed as "failed" (still running, but force)
    // After Stop, the invoke goroutine completes and calls setRunStatus("succeeded")
    // Does the audit/event already have "failed"? (Should be — Stop already ran)
    // But does setRunStatus succeed after untrackRun? Check for stale writes.
}
```

### Break 5: Force stop with succeeded invoke should be "failed"

The implementation sets force=true → "failed". But the spec says force should
be EventRunCancelled, not EventRunFailed. Check if force stop of a SUCCEEDED
run incorrectly marks it as "failed" in the audit payload vs "cancelled" in
the event. Is this inconsistent?

### Break 6: Status check via rt.Status() after container removal

Stop calls rt.Status() BEFORE rt.Remove(). But if Stop already stopped the
container, Status returns ContainerStatusStopped. The implementation has:
```go
if containerStatus == runtime.ContainerStatusStopped && tracked.Status == "failed" {
    finalStatus = "failed"
}
```
This is a no-op — it only "overrides" to failed when already failed. Is this
logic meaningful or dead code? Check if Status() is ever used to DETECT a
crash (non-zero exit) that the invoke didn't catch.

## Execution

1. Write all 6 break tests
2. Run: `go test -race -count=1 -run TestAdv ./internal/daemon/...`
3. Report each test: PASS (impl is correct) or FAIL (real bug found)
4. Do NOT fix bugs — just report them
