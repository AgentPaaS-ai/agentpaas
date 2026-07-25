# B34-T09 Adversary Findings — pipeline runtime

**Date:** 2026-07-24  
**Branch:** feat/b34-t09-gate  
**Worktree:** ap-b34-t09  
**Test file:** `internal/workflow/pipeline/adversary_b34_test.go`  
**Command:** `go test ./internal/workflow/pipeline/ -race -count=1 -run Adversary -v`

## Summary

| Severity | Count | Status |
|----------|-------|--------|
| HIGH     | 4     | RED (left failing) |
| MEDIUM   | 1     | residual (claim blocked) |
| confirmed_safe | 12 vectors | PASS |

Do **not** delete or weaken RED tests. Fix worker owns production patches.

---

## RED / BREAK findings

### BREAK-1 HIGH — Nested reserved keys not rejected
- **Test:** `TestAdversary_B34_ReservedKeys_NestedObjectRejected`
- **File:** `internal/workflow/pipeline/handoff_validate.go` — `hasReservedKeys`
- **Evidence:** `{"meta":{"password":"nested-secret-value"}}` → `codes=[]`
- **Root cause:** Only top-level object keys are checked; nested `map[string]interface{}` values are ignored (string path checks only).
- **Impact:** Handoff context can smuggle `password` / `api_key` / `token` under wrappers and pass ValidateHandoffEnvelope.
- **Fix direction:** Recursively walk JSON objects/arrays; reject reserved keys at any depth.

### BREAK-2 HIGH — Reserved keys inside array elements not rejected
- **Test:** `TestAdversary_B34_ReservedKeys_DeepArrayRejected`
- **File:** `handoff_validate.go` — `hasReservedKeys`
- **Evidence:** `{"items":[{"token":"arr-secret"}]}` → `codes=[]`
- **Root cause:** Same non-recursive scan; arrays never walked.
- **Fix direction:** Same recursive walk as BREAK-1.

### BREAK-3 HIGH — Controller CommitStageSuccess bypasses handoff reserved-key validation
- **Test:** `TestAdversary_B34_ControllerCommitDoesNotAcceptSecretContext`
- **File:** `internal/workflow/pipeline/controller.go` — `CommitStageSuccess` (~L462–648)
- **Evidence:** `ContextJSON: {"password":"s3cr3t...","api_key":"sk-live-xxx"}` accepted; stored via `CommitHandoff` with secret body intact.
- **Root cause:** Runtime path uses `routedrun.HandoffEnvelope` and never calls `ValidateHandoffEnvelope` (B34 type). Conformance validator is orthogonal to controller commit.
- **Impact:** Any caller of CommitStageSuccess can persist secret-like context into durable handoff store; next stage / operators reading store see secrets. Inspect path does not embed body (safe) but store does.
- **Fix direction:** Validate ContextJSON against reserved-key rules (reuse/adapt hasReservedKeys) before CommitHandoff; reject with stable error. Optionally bridge B34 HandoffEnvelope validation into commit path.

### BREAK-4 HIGH — artifactPutToFS follows pre-planted symlink (escape write)
- **Test:** `TestAdversary_B34_ArtifactPutToFS_SymlinkEscape`
- **File:** `internal/workflow/pipeline/artifact_store.go` — `artifactPutToFS` (~L140–171)
- **Evidence:** Symlink `proj/out.json` → outside `pwned.txt`; `os.WriteFile` follows link; outside file contains `SYMLINK-ESCAPE-PAYLOAD`.
- **Root cause:** No `Lstat`/O_NOFOLLOW before write. `CodeArtifactSymlinkRejected` exists in `codes.go` but is never returned by write/projection paths. Comment on `BuildROProjection` claims symlink rejection; implementation does not.
- **Impact:** TOCTOU or hostile projection tree can redirect artifact materialization outside the projection directory (host write).
- **Fix direction:** Before write: if path exists and is symlink → error `ARTIFACT_SYMLINK_REJECTED`. Prefer `open(O_NOFOLLOW|O_EXCL)` / write-to-temp+rename within dir after verifying no symlink components. Refuse if any path component under proj dir is a symlink.

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

## RED GATE output (excerpt)

```
--- FAIL: TestAdversary_B34_ReservedKeys_NestedObjectRejected
--- FAIL: TestAdversary_B34_ReservedKeys_DeepArrayRejected
--- FAIL: TestAdversary_B34_ControllerCommitDoesNotAcceptSecretContext
--- FAIL: TestAdversary_B34_ArtifactPutToFS_SymlinkEscape
FAIL github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline
```

Other Adversary tests: PASS with `-race`.

---

## Deliverables

- `internal/workflow/pipeline/adversary_b34_test.go` — permanent adversary suite
- `docs/owa-records/b34-t09-adversary-findings.md` — this file

## Next (fix worker)

1. Recurse `hasReservedKeys` over objects/arrays.
2. Gate `CommitStageSuccess` handoff ContextJSON through reserved-key validation.
3. Harden `artifactPutToFS` against symlink follow; emit `ARTIFACT_SYMLINK_REJECTED`.
4. Optional: clear READY nodes on cancel.
