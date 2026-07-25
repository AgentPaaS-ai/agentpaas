# B34-T07 Chunk 1 — Failure + Cancel terminal mapping (library)

**IMPLEMENT B34-T07 CHUNK 1 NOW. Do not ask questions. Do not do T08/T09.**

**Worktree:** `/Users/pms88/projects/ap-b34-t07`
**Branch:** `feat/b34-t07-faults`
**Spec:** docs/execution/blocks/b34-summary.md § T07 items 1–4 (partial)
**PRE:** `git rev-parse HEAD | tee /tmp/b34-t07c1-pre.sha`

## Scope (chunk 1 only)

1. Map stage failures to workflow FAILED with stable reasons
2. Cancel workflow: active node → CANCELLED, no next READY, later stages stay PENDING
3. Idempotent repeated Cancel / CommitStageFailure
4. Never launch next after non-success
5. Late CommitStageSuccess from old attempt after cancel/fail → reject
6. Cancel while LAUNCHING and while RUNNING
7. Failure at stage0, stage1 mid pipeline, final stage

OUT: full Docker, MCP service fence live, resume API (chunk 2), admission enable, T08 CLI

## API additions (controller.go or control.go)

```go
type CancelRequest struct {
  WorkflowID routedrun.WorkflowID
  Reason string // optional stable code
}

// CancelWorkflow fences active/launching node if any, marks workflow CANCELLED.
// Idempotent if already terminal CANCELLED.
func (c *Controller) CancelWorkflow(ctx, req CancelRequest) error

// Optionally extend CommitStageFailure to accept FailureReason string.
```

Ensure CommitStageFailure sets workflow TerminalReason when possible.

## Tests (fault_test.go) — must PASS -race

1. TestFailStage0_NoNextReady
2. TestFailStage1_MidPipeline_NoStage2
3. TestFailFinalStage_WorkflowFailed
4. TestCancelWhileRunning_NoNext
5. TestCancelWhileLaunching
6. TestCancelIdempotent
7. TestDoubleCommitStageFailureIdempotent
8. TestLateSuccessAfterCancelRejected
9. TestLateSuccessAfterFailRejected
10. TestReconcileOnceAfterCancel_Nothing
11. TestCancelThenPauseDesiredState_StillCancelled

## RED GATE

```bash
cd /Users/pms88/projects/ap-b34-t07
go test ./internal/workflow/pipeline/ -count=1 -run 'Fail|Cancel|LateSuccess' -v
go test ./internal/workflow/pipeline/ -race -count=1
test -f docs/owa-records/b34-t07-chunk1.md
git log --oneline $(cat /tmp/b34-t07c1-pre.sha)..HEAD
```

Conventional commits feat/test(b34-t07).
Handoff docs/owa-records/b34-t07-chunk1.md
START NOW. Chunk 1 only. WORKER_DONE after RED GATE green.
