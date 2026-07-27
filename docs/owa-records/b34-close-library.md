# B34 Close — library residuals (Cancel READY + gate label)

**Date:** 2026-07-26
**Branch:** feat/b34-close-library
**Base:** 4234e73

## SHAs

TBD after commit.

## Summary

Closed two code residuals from B34 before B35 begins:

1. **CancelWorkflow leaves READY/PENDING nodes** (adversary RES-1 MEDIUM)
2. **child_spawn feature gate mislabeled B31** — corrected to B35

## Task 1 — CancelWorkflow clears non-terminal nodes

### Before

`CancelWorkflow` only transitioned LAUNCHING/RUNNING nodes → CANCELLED, then broke
after the first active node. READY and PENDING nodes were left untouched,
creating a residual where a cancelled workflow still had non-terminal nodes.

### After

`CancelWorkflow` now iterates ALL nodes and cancels every non-terminal node:
- LAUNCHING/RUNNING: cancelled with attempt cancellation + FailureUserCancelled
- READY/PENDING: cancelled with run cancellation (PENDING→CANCELLED)
- Terminal nodes (SUCCEEDED/FAILED/CANCELLED/SKIPPED): skipped

After cancel, no node remains in PENDING, READY, LAUNCHING, or RUNNING.

### Files changed

- `internal/workflow/pipeline/controller.go` — CancelWorkflow restructured
- `internal/workflow/pipeline/fault_test.go` — Updated 3 tests + added 1 new
- `internal/workflow/pipeline/adversary_b34_test.go` — Repurposed adversary test

### Tests

| Test | Change |
|------|--------|
| `TestCancelWhileRunning_NoNext` | Updated: stage1/stage2 expect CANCELLED (was PENDING) |
| `TestCancelIdempotent` | Updated: stage1 expects CANCELLED (was PENDING) |
| `TestCancelClearsAllNonTerminalNodes` | NEW: 3-stage seed, cancel, all CANCELLED, claim nil, resume errors |
| `TestAdversary_B34_CancelClearsReadyAndPendingNodes` | REPURPOSED: asserts no READY/PENDING survivors after cancel |

## Task 2 — child_spawn block label B31 → B35

### Files changed

- `internal/daemon/routed_handlers.go` lines 975, 1253: `"B31"` → `"B35"` for child_spawn only
- B31 promotion-gate comments (lines 955, control_handlers.go:121) left unchanged

### Tests

- `TestWorkflowKindNotEnabledCodes` — only asserts code string, not block label, so no change needed

## Acceptance

```bash
go test ./internal/workflow/pipeline/ -race -count=1          # PASS
go test ./internal/workflow/pipeline/ -race -count=1 -run Adversary  # PASS
go test ./internal/daemon/ -count=1 -run 'Pipeline|NotEnabled|workflowKind|child_spawn|ChildSpawn'  # PASS
go build ./...                                                  # PASS
```

## Out of scope

- Docker e2e, daemon reconcile loop, Hermes-absent pack
- BUG doc rewrites
- parent_child pack bound changes (B35 T01)
- Weakening any adversary RED test