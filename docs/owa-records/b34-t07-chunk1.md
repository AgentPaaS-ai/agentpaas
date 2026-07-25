# B34-T07 Chunk 1 — Handoff Record

**Block/Task:** B34-T07 Chunk 1 — Failure + Cancel terminal mapping (library)
**Status:** Complete
**Date:** 2026-07-24

## Commit

```
3910d7b feat(b34-t07): add CancelWorkflow, FailureReason on CommitStageFailure, reject late success
```

## Files changed

- `internal/workflow/pipeline/controller.go` — Added types, methods
- `internal/workflow/pipeline/fault_test.go` — 11 new tests

### Controller changes

1. **New types:**
   - `CancelRequest` struct with `WorkflowID` and optional `Reason` string.

2. **Extended `StageFailure`:**
   - Added optional `FailureReason *routedrun.FailureReason` field.

3. **New method `CancelWorkflow`:**
   - Finds active/launching node, cancels node → run → attempt.
   - Marks workflow CANCELLED with `TerminalReason = FailureUserCancelled`.
   - Idempotent if workflow already CANCELLED.

4. **Updated `CommitStageFailure`:**
   - Sets `FailureReason` on attempt when provided.
   - Sets `TerminalReason` on workflow when provided.

5. **Updated `CommitStageSuccess`:**
   - Rejects late success after cancel/fail via `isNodeTerminal()` check.
   - Returns new error `ErrWorkflowTerminal` for terminal nodes.

6. **New helper `isNodeTerminal`** checks for Succeeded, Failed, Cancelled, Skipped.

## Tests (all PASS)

```
=== RUN   TestFailStage0_NoNextReady              PASS
=== RUN   TestFailStage1_MidPipeline_NoStage2     PASS
=== RUN   TestFailFinalStage_WorkflowFailed       PASS
=== RUN   TestCancelWhileRunning_NoNext           PASS
=== RUN   TestCancelWhileLaunching               PASS
=== RUN   TestCancelIdempotent                   PASS
=== RUN   TestDoubleCommitStageFailureIdempotent  PASS
=== RUN   TestLateSuccessAfterCancelRejected      PASS
=== RUN   TestLateSuccessAfterFailRejected        PASS
=== RUN   TestReconcileOnceAfterCancel_Nothing    PASS
=== RUN   TestCancelThenPauseDesiredState_StillCancelled  PASS
```

## Gates

- `go test ./internal/workflow/pipeline/ -count=1 -run 'Fail|Cancel|LateSuccess' -v` — PASS
- `go test ./internal/workflow/pipeline/ -race -count=1` — PASS
- `make lint` — PASS (0 issues)
- `make test` — 1 pre-existing failure in `internal/daemon` (unrelated)

## Product decisions

- Cancel *only* cancels the active/launching node; PENDING nodes stay PENDING.
- Cancel always uses `FailureUserCancelled` as terminal reason.
- StageFailure's `FailureReason` is optional and propagated to both attempt and workflow.
- `CommitStageSuccess` rejects any call when the node is already in a terminal state (Failed, Cancelled, Skipped) — returns `ErrWorkflowTerminal`.
- `isNodeTerminal` considers Succeeded, Failed, Cancelled, Skipped as terminal.

## Known gaps / deferred

- Cancel reason string is not mapped to specific `FailureReason` codes beyond `FailureUserCancelled` (spec allows optional stable code; chunk 1 scope is library not full API).
- MCP service fence on cancel is out of scope (chunk 2 — full Docker).
- Resume API is out of scope (chunk 2).
- Admission enable and T08 CLI are out of scope.

## Next task

B34-T07 Chunk 2 — Full Docker, MCP service fence live, resume API, admission enable, T08 CLI.
