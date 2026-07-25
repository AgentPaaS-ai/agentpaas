# B34-T09 Adversary Findings — pipeline runtime

**Date:** 2026-07-24  
**Branch:** feat/b34-t09-gate  
**Worktree:** ap-b34-t09  
**Test file:** `internal/workflow/pipeline/adversary_b34_test.go`  
**Command:** `go test ./internal/workflow/pipeline/ -race -count=1 -run Adversary -v`

## Resolution Status: ALL RESOLVED — 2026-07-24

| Fix | SHA | Description |
|-----|-----|-------------|
| BREAK-1 | `5048943` | Recursive `hasReservedKeys` — walks maps + arrays |
| BREAK-2 | `5048943` | Same recursive walk fix |
| BREAK-3 | `5048943` | CommitStageSuccess validates ContextJSON before CommitHandoff |
| BREAK-4 | `5048943` | artifactPutToFS Lstat check before write; returns ARTIFACT_SYMLINK_REJECTED |

All 18 adversary tests PASS with `-race` after fixes.

## Summary

| Severity | Count | Status |
|----------|-------|--------|
| HIGH     | 4     | RESOLVED |
| MEDIUM   | 1     | residual (claim blocked) |
| confirmed_safe | 12 vectors | PASS |

**Do not** delete or weaken RED tests.

---

## RED / BREAK findings

### BREAK-1 HIGH — Nested reserved keys not rejected (RESOLVED)
- **Test:** `TestAdversary_B34_ReservedKeys_NestedObjectRejected`
- **File:** `internal/workflow/pipeline/handoff_validate.go` — `hasReservedKeys`
- **Fix:** Replaced flat-map scan with recursive `hasReservedKeysRecursive` that walks `map[string]interface{}` and `[]interface{}` at any depth.

### BREAK-2 HIGH — Reserved keys inside array elements not rejected (RESOLVED)
- **Test:** `TestAdversary_B34_ReservedKeys_DeepArrayRejected`
- **File:** `handoff_validate.go` — `hasReservedKeys`
- **Fix:** Same recursive walk as BREAK-1.

### BREAK-3 HIGH — Controller CommitStageSuccess bypasses handoff reserved-key validation (RESOLVED)
- **Test:** `TestAdversary_B34_ControllerCommitDoesNotAcceptSecretContext`
- **File:** `internal/workflow/pipeline/controller.go` — `CommitStageSuccess` (~L592)
- **Fix:** Added `hasReservedKeys(json.RawMessage(req.Handoff.ContextJSON))` check before `CommitHandoff`; rejects with `HANDOFF_RESERVED_KEY`.

### BREAK-4 HIGH — artifactPutToFS follows pre-planted symlink (escape write) (RESOLVED)
- **Test:** `TestAdversary_B34_ArtifactPutToFS_SymlinkEscape`
- **File:** `internal/workflow/pipeline/artifact_store.go` — `artifactPutToFS` (~L164)
- **Fix:** Added `os.Lstat` check after path containment validation and `MkdirAll`; if the target path exists and is a symlink, returns `ARTIFACT_SYMLINK_REJECTED` before `os.WriteFile`.

---

## MEDIUM residual (not RED)

### RES-1 MEDIUM — CancelWorkflow does not clear READY nodes
- **Test:** `TestAdversary_B34_CancelLeavesReadyNode_ClaimStillBlocked` (PASS)
- **File:** `controller.go` — `CancelWorkflow` only transitions LAUNCHING/RUNNING → CANCELLED.
- **Behavior:** Cancel before claim leaves stage0 READY while workflow is CANCELLED.
- **Mitigation in place:** `ClaimNextReady` / `ResumeWorkflow` fail closed on terminal workflow (property held).
- **Fix direction (optional):** On cancel, mark READY/PENDING nodes CANCELLED or SKIPPED for consistent terminal topology.

---

## confirmed_safe (property tests PASS)

1. **Concurrent ClaimNextReady** — 32 goroutines → exactly 1 claim, 1 attempt, LAUNCHING (`TestAdversary_B34_ConcurrentClaimNextReady_SingleWinner`).
2. **Concurrent ReconcileOnce** — 16 goroutines → exactly 1 launch job/key for stage0 (`TestAdversary_B34_ConcurrentReconcileOnce_NoDoubleLaunch`).
3. **Two controllers shared store** — CAS serializes dual ClaimNextReady (`TestAdversary_B34_TwoControllersSharedStore_NoDoubleClaim`).
4. **Handoff after cancel** — late CommitStageSuccess rejected; no handoff stored; next node not advanced (`TestAdversary_B34_HandoffAfterCancelRejected`).
5. **Resume after cancel** — ResumeWorkflow errors; node statuses unchanged; claim nil (`TestAdversary_B34_ResumeAfterCancelRejected`).
6. **Top-level reserved keys** — full denylist rejected by ValidateHandoffEnvelope (`TestAdversary_B34_ReservedKeys_TopLevelRejected`).
7. **Promote path traversal** — `../`, absolute, nested `..`, Windows-style `..\` rejected (`TestAdversary_B34_PromoteRejectsPathTraversal`).
8. **BuildROProjection traversal ref** — `data.json/../../escape.txt` rejected (`TestAdversary_B34_BuildROProjection_RejectsSymlinkInProjectionTree`).
9. **isSafeRef edges** — null-byte+`..`, dot segments unsafe; normal relative names safe (`TestAdversary_B34_IsSafeRef_NullByteAndDotSegments`).
10. **Pause on success** — next stays PENDING; Claim/ReconcileOnce no-op while pause desired (`TestAdversary_B34_PauseDoesNotReadyNextOnSuccess`).
11. **Pause after next already READY** — ClaimNextReady blocked (`TestAdversary_B34_PauseAfterNextAlreadyReady_BlocksClaim`).
12. **BuildPipelineInspect** — marshaled summary does not embed handoff ContextJSON secret markers (`TestAdversary_B34_InspectOmitsHandoffContextSecrets`).
13. **CodePipelineNotEnabled** stable string pin (`TestAdversary_B34_PipelineNotEnabledCodeStable`).

### Pipeline enable default-off (daemon)
- Enforced in `internal/daemon/routed_handlers.go` `pipelineRuntimeEnabled()` — default field false; env only `AGENTPAAS_PIPELINE_ENABLED=1`.
- Existing daemon tests: not-enabled deploy path + `TestPipelineEnabled_AllowsDeployCheck`.
- Package pin: `CodePipelineNotEnabled = "PIPELINE_NOT_ENABLED"`.
- No break found on default-off contract in this pass (daemon tests not re-run as part of pipeline package RED gate).

---

## Attack surface checklist

| # | Vector | Result |
|---|--------|--------|
| 1 | Double claim / double launch concurrency | SAFE |
| 2 | Handoff after cancel | SAFE |
| 3 | Resume after cancel | SAFE |
| 4 | Secret-like context (reserved keys) | **BREAK nested + controller path** |
| 5 | Path traversal in artifact promote | SAFE (string refs); **BREAK symlink write** |
| 6 | Pause still launches next | SAFE |
| 7 | BuildPipelineInspect secret body | SAFE |
| 8 | Pipeline enable default-off | SAFE (daemon + code pin) |

---

## Deliverables

- `internal/workflow/pipeline/adversary_b34_test.go` — permanent adversary suite
- `docs/owa-records/b34-t09-adversary-findings.md` — this file (adversary findings + resolution record)
