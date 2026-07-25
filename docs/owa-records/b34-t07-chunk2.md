# B34-T07 Chunk 2 — Resume from PAUSED + control races

**Date:** 2026-07-24
**Branch:** feat/b34-t07-faults
**Pre-impl SHA:** 2dedf47f034bfc9eb1535c011a6748ade76008b8

## Changes

### controller.go
- Added `ResumeRequest` type with `WorkflowID` field
- Added `ResumeWorkflow(ctx, ResumeRequest) error` method:
  - Transitions PAUSED → RUNNING on the workflow
  - Marks the earliest PENDING node READY (exactly once)
  - Idempotent if workflow already RUNNING with active node
  - Rejected if workflow is CANCELLED/FAILED/SUCCEEDED (returns ErrInvalidTransition)
  - Handles CAS conflicts for crash-recovery (new Controller, same store)
  - Note: MemoryStore lacks SetDesiredState — the method only transitions workflow status; control plane clears PAUSE desired state via RequestControl(RESUME)

### fault_test.go
- Added `"errors"` import
- 7 new tests:
  1. `TestPauseThenResume_NextStageReadyOnce` — pause after stage0, resume marks exactly one node READY
  2. `TestResumeIdempotent` — double resume doesn't double-advance
  3. `TestResumeRejectedWhenCancelled` — resume on CANCELLED returns error
  4. `TestResumeRejectedWhenFailed` — resume on FAILED returns error
  5. `TestCancelBeatsPause` — cancel after pause before claim → CANCELLED
  6. `TestResumeAfterControllerRestart_NoDoubleReady` — new controller same store, only one READY
  7. `TestPauseDuringStage_ResumeLaunchesNext` — pause during stage0, resume enables ClaimNextReady

## Verification
- `make build` — passes
- `make lint` — 0 issues
- `go test ./internal/workflow/pipeline/ -race -count=1` — all pass
- All 7 new tests pass individually and together
- No regressions in existing pipeline tests
