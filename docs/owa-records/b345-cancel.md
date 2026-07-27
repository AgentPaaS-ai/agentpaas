# B34.5-A — CancelWorkflow + MCP Fence

**Branch:** `feat/b345-cancel`  
**Base:** `main`  
**Commit:** `f89fe15`  
**Date:** 2026-07-26

## Summary

Implemented CancelWorkflow in `controlServer` (previously always returned
FEATURE_NOT_ENABLED). Now loads the workflow from the store, propagates
cancellation through the pipeline controller (node/run/attempt all transitioned),
and fences MCP services when the registry or fencer is available. Fully idempotent.

## Changed Files

| File | Change |
|---|---|
| `internal/daemon/stub_handlers.go` | Added `MCPFencer` interface + `mcpFencer` field on `controlServer` |
| `internal/daemon/routed_handlers.go` | Replaced CancelWorkflow stub with real implementation; added pipeline import |
| `internal/daemon/routed_handlers_test.go` | Updated existing test + 5 new tests |

## Implementation

- **CancelWorkflow** loads workflow from `s.workflowStore`, type-asserts to
  `pipeline.PipelineStore`, and uses `pipeline.Controller.CancelWorkflow` when
  possible (clears all non-terminal nodes/runs/attempts). Falls back to direct
  durable transition for non-PipelineStore implementations.
- **Idempotent**: already CANCELLED → returns success with current state.
- **MCP fence**: calls `mcpFencer.WorkflowTerminal` (test hook) or
  `mcpRegistry.WorkflowTerminal` (production) — best-effort, non-fatal.
- **Errors**: NotFound (missing workflow), InvalidArgument (empty ids),
  FailedPrecondition (invalid transition), Internal (store failure).

## Tests

| Test | Status |
|---|---|
| `TestCancelWorkflow_RunningWorkflow_AllNodesTerminal` | PASS |
| `TestCancelWorkflow_Idempotent` | PASS |
| `TestCancelWorkflow_MissingWorkflow_NotFound` | PASS |
| `TestCancelWorkflow_MissingIdempotencyKey_InvalidArgument` | PASS |
| `TestCancelWorkflow_WithMCPFencer_CallsWorkflowTerminal` | PASS |
| `TestControlAndAmendment_CancelWorks_AmendNotEnabled` (updated) | PASS |
| All pipeline `TestCancel*` tests | PASS (12 tests) |

## Not Changed

- `SetWorkflowDesiredState` — still returns FEATURE_NOT_ENABLED (B35)
- `RestartWorkflow` — still returns FEATURE_NOT_ENABLED (B35)
- `AmendLimits` — still returns FEATURE_NOT_ENABLED (B35)
