# B34-T04 Chunk 1 — Handoff Record

**Date:** 2026-07-24
**Commit:** e140d2c
**Branch:** feat/b34-t04-scheduler

## Summary

Implemented the linear pipeline controller (library only, no Docker, no daemon wire).

## Files

- `internal/workflow/pipeline/controller.go` — Controller, PipelineStore interface, types, SeedPipelineWorkflow, ClaimNextReady, AcknowledgeRunning, CommitStageSuccess, CommitStageFailure
- `internal/workflow/pipeline/controller_test.go` — 9 tests covering all spec scenarios

## Design Decisions

1. **Generation tracking**: Controller maintains `nodeGen`, `runGen`, `attGen` maps internally since MemoryStore doesn't expose generations via the PipelineStore interface. This avoids exposing generation details in the public API types (Claim, StageSuccess, StageFailure).

2. **SeedPipelineWorkflow takes *Controller**: This ensures the seed function initializes the controller's generation maps correctly.

3. **Idempotency**: CommitStageSuccess checks if node is already SUCCEEDED and returns nil. CommitStageFailure does the same for FAILED. MemoryStore.CommitHandoff is already idempotent (returns nil if handoff ID exists).

4. **Attempt generation handling**: CommitStageSuccess/Failure try the attempt update with the tracked generation. If CAS fails, the method returns an error (the caller should not retry without coordination).

## Test Results

```
=== RUN   TestControllerTwoStageHappyPath       — PASS
=== RUN   TestControllerThreeStageHappyPath     — PASS
=== RUN   TestDoubleClaimNextReady              — PASS
=== RUN   TestDoubleCommitStageSuccess          — PASS
=== RUN   TestCommitStageSuccessWithoutHandoffNonFinal — PASS
=== RUN   TestCommitStageFailureMarkWorkflowFailed     — PASS
=== RUN   TestClaimAfterFinal                   — PASS
=== RUN   TestCASConflictSimulation             — PASS
=== RUN   TestRestartSimulation                 — PASS
```

All tests pass with `-race`.

## Next: Chunk 2

TBD — container launch, daemon wire, pause matrix (T05+).