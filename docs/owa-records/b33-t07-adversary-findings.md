# B33-T07 Adversary Findings — Fix Brief (2026-07-24)

Worktree: `/Users/pms88/projects/ap-b33-t07`
Branch: `feat/b33-t07-mcp-evidence`
HEAD: `65d51f2`

## Goal

Make ALL of these pass without deleting/weakening tests (fix production code):

```
cd /Users/pms88/projects/ap-b33-t07
go test ./internal/mcpmanager/ -count=1 -race -run 'TestAdversaryT07|TestCallEvidence|TestHealth|TestRestart'
go test ./internal/mcpmanager/ -count=1 -race
```

Do NOT change adversary test assertions to hide bugs. Fix `evidence.go`, `health.go`, `redact.go`, `router.go`, `service_registry.go` (and helpers) only. Existing non-adversary T07 tests must keep passing.

## HIGH severity (must fix)

### H1 — Reason not sanitized on evidence write paths
- `recordCallEvidence` (`router.go:527-544`) sets `existing.Reason = reason` raw.
- Lifecycle fail/fence reasons stored unsanitized (`service_registry.go`).
- Fix: always `sanitizeLastError(reason)` (or stronger) before any evidence/lifecycle/health Reason field is stored.
- Evidence: `TestAdversaryT07_RouterEvidenceReasonLeaksMCPErrorSecrets`, `TestAdversaryT07_LifecycleFailReasonUnsanitized`, `TestAdversaryT07_FenceReasonUnsanitized`, `TestAdversaryT07_RecordCallEvidenceReasonBypassesSanitizeLastError`

### H2 — Terminal call status can be overwritten (fabricated commit / demotion)
- `InMemoryCallEvidenceStore.RecordCall` blindly overwrites.
- `recordCallEvidence` does not refuse terminal→SUCCEEDED or SUCCEEDED→UNKNOWN reopen.
- Required terminal-state machine (once terminal, never fabricate success; never demote SUCCEEDED to UNKNOWN/in-flight):
  - Terminal: SUCCEEDED, FAILED, CANCELLED, TIMEOUT, OVERLOADED, and restart UNKNOWN with FinishedAt set.
  - Allow in-flight UNKNOWN (FinishedAt zero) → any terminal.
  - Allow restart MarkInFlightUnknown only for in-flight (inFlight map).
  - Reject: FAILED/TIMEOUT/CANCELLED/SUCCEEDED ← SUCCEEDED overwrite; SUCCEEDED ← UNKNOWN reopen into inFlight; late SUCCEEDED after restart UNKNOWN.
- Clear forged `OutputDigest` when MarkInFlightUnknown marks restart UNKNOWN.
- Evidence: `TestAdversaryT07_LateSuccessOverwritesRestartUnknown`, `TestAdversaryT07_LateSuccessOverwritesCancelled`, `TestAdversaryT07b_TerminalFailedOverwrittenBySucceeded`, `TestAdversaryT07b_SucceededDemotedToUnknownReopensInFlight`, `TestAdversaryT07b_TimeoutOverwrittenBySucceeded`, `TestAdversaryT07b_RecordCallEvidenceIgnoresTerminalRestartStatus`, `TestAdversaryT07b_MarkInFlightUnknownPreservesForgedOutputDigest`

### H3 — Cleanup/Stop leave capability usable and do not cancel in-flight
- `CleanupServiceResources`: clear Capability/Endpoint **before** slow I/O (or under lock before releasing for I/O); call CancelTracker.CancelAll (same as Fence) at start of cleanup/stop.
- Gen-mismatch: if generation changed during I/O, do **not** destroy the live container belonging to the new gen; only clear residual state for the generation you captured, or re-check gen before Remove.
- `Stop` must cancel in-flight calls like Fence.
- Evidence: `TestAdversaryT07b_CleanupIOWindowLeavesCapabilityUsable`, `TestAdversaryT07b_CleanupDoesNotCancelInFlightCalls`, `TestAdversaryT07b_CleanupGenMismatchDestroysLiveContainer`, `TestAdversaryT07b_StopDoesNotCancelInFlightCalls`

## MEDIUM severity (must fix)

### M1 — Redaction gaps
- Extend `sentinelSecretPatternsList` / `sanitizeToolOutputString` for: `ASIA` (AWS temp keys), `Bearer ` tokens, `"api_key"` JSON field values, zero-width/homoglyph stripping before pattern match (strip `\u200b` and similar format chars).
- Evidence: `TestAdversaryT07_RedactionGap_ASIAAndBearer`, `TestAdversaryT07b_HomoglyphSecretBypassesHealthRedaction`

### M2 — Unbounded evidence growth
- Cap call records and lifecycle events per workflow (or global) with ring/drop-oldest; document constants (e.g. MaxCallRecordsPerWorkflow=1024, MaxLifecycleEventsPerKey=256).
- Evidence: `TestAdversaryT07_LifecycleEventsUnbounded`, `TestAdversaryT07_CallEvidenceStoreUnbounded`

### M3 — Key collision on `workflowID + "/" + bindingID`
- Use a collision-safe composite key (length-prefixed or `\x00`-separated encoding), not naive slash join, for lifecycle map and healthStates.
- Evidence: `TestAdversaryT07_LifecycleKeySlashInjection`, `TestAdversaryT07b_HealthKeySlashInjection`

### M4 — Empty / NUL correlation IDs
- Reject empty CorrelationID on RecordCall; reject or sanitize embedded NUL in CorrelationID (strip/reject `\x00`).
- Evidence: `TestAdversaryT07_EmptyCorrelationIDCollides`, `TestAdversaryT07b_NullByteCorrelationIDTruncation`

### M5 — CR/LF injection in identity fields
- Sanitize WorkflowID, BindingID, Tool, Reason, CorrelationID: strip/replace `\r`, `\n`, other controls (same as tool output control map).
- Evidence: `TestAdversaryT07b_EvidenceToolNewlineInjection`

### M6 — DiscoverOrphans treats STOPPING/FAILED owned containers as orphans
- Track containers for STOPPING and FAILED (and any non-empty ContainerID still owned) so they are not reported as orphans.
- Evidence: `TestAdversaryT07b_DiscoverOrphansMissesStoppingAndFailed`

### M7 — Network remove uses context.Background()
- `cleanupNetworkIfEmptyLocked` / `RemoveServiceNetwork` must honor parent ctx (pass ctx through CleanupServiceResources path); cancelled ctx must abort hang.
- Evidence: `TestAdversaryT07b_CleanupNetworkRemoveUsesBackgroundContext`

## Implementation notes

1. Prefer fixing at store boundary (`RecordCall`/`RecordLifecycleEvent`) so all writers get monotic/terminal + sanitize + field scrub for free.
2. `recordCallEvidence` should still sanitize reason and respect store rejection of illegal transitions (check error or re-get).
3. Keep public API stable where possible; private helpers OK.
4. After fixes: full `go test ./internal/mcpmanager/ -count=1 -race` PASS.
5. Commit with conventional messages on `feat/b33-t07-mcp-evidence`. Do not merge to main.
6. Update `docs/owa-records/b33-t07.md` Open risks if any remain; append a short "Adversary pass" section with test command output summary.

## Verify commands

```bash
cd /Users/pms88/projects/ap-b33-t07
go test ./internal/mcpmanager/ -count=1 -race
go test ./internal/harness/ -count=1 -race
go build ./...
```
