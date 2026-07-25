# B34-T04 Chunk 2 — Handoff Record

**Date:** 2026-07-24
**Commit:** c22f69e
**Branch:** feat/b34-t04-scheduler
**PR:** #194

## Summary

Implemented PAUSE_REQUESTED desired-state seam and expanded crash/idempotency matrix for the linear pipeline controller.

## Changes

### controller.go
- Added `GetDesiredState` to `PipelineStore` interface
- `ClaimNextReady`: now checks DesiredState for pause (returns nil,nil when pause requested)
- `CommitStageSuccess`: for non-final stages, transitions workflow to PAUSED instead of next READY when pause is requested; handoff still commits
- `CommitStageSuccess`: for final stage, transitions PAUSE_REQUESTED→RUNNING→SUCCEEDED (valid transition path)
- New `isPauseRequested` helper checks both DesiredState and workflow status

### controller_test.go
- 6 new tests covering the pause matrix:
  1. TestPauseBeforeClaim — PAUSE desired state prevents claiming
  2. TestPauseDuringStage — active stage succeeds, workflow PAUSED, next stage NOT READY
  3. TestPauseAtFinalStage — final stage terminates SUCCEEDED despite pause
  4. TestDoubleCommitStageSuccessWithPause — idempotent commit under pause
  5. TestPauseRequestedMidClaim — 3-stage pipeline, pause prevents all downstream advances
  6. TestPauseNoDesiredState — regression: normal path unaffected by pause infra

## Test Results

```
=== RUN   TestPauseBeforeClaim                    — PASS
=== RUN   TestPauseDuringStage                    — PASS
=== RUN   TestPauseAtFinalStage                   — PASS
=== RUN   TestDoubleCommitStageSuccessWithPause   — PASS
=== RUN   TestPauseRequestedMidClaim              — PASS
=== RUN   TestPauseNoDesiredState                 — PASS
```

All 15 controller tests pass with `-race`. Routedrun tests also pass. `make lint` 0 issues.

## Design Decisions

1. **isPauseRequested checks both sources**: The helper checks `GetDesiredState` (ControlPause) first, then falls back to workflow status (PAUSE_REQUESTED). This handles both the control-plane-committed and the desired-state-only cases.

2. **Final stage PAUSE_REQUESTED→RUNNING→SUCCEEDED**: The WorkflowTransitions map doesn't allow PAUSE_REQUESTED→SUCCEEDED directly. The controller transitions through RUNNING as an intermediate step. Both transitions are valid.

3. **Non-final pause commits handoff**: The handoff is still committed before the workflow goes to PAUSED. This preserves the stage result for later resume.

4. **No Docker/daemon wire yet**: Still library-only. No containers launched.

## Next: Chunk 3

TBD — crash/recovery daemon reconciliation, container launch + daemon wire.