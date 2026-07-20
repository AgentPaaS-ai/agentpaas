# B30-T02 Part A — Durable async deployment invocation/job protocol

**Date:** 2026-07-19
**Branch:** feat/b30-t02-durable-invoke
**Worker commits:** a15ff77, bb955f9, 1733899, 455e8e4, 791f082
**Merge commit:** e005500 (main)
**Durations:** Part A worker 12m+ (90-iter budget exhausted); fix worker 8m 33s (75 tool calls)

## Spec reference

`docs/execution/blocks/b30-summary.md:260-328` (T02). Goal: activate
B26 deployment admission for standalone durable workers and decouple
client/control-plane request lifetime from worker lifetime.

## What changed

### A1: InvokeJob/Event/Result domain types (`internal/routedrun/types.go` +195 lines)
- `InvokeJob`: schema version, invocation ID, workflow ID, run ID,
  attempt ID, resolved deployment ID/version/digest, nested package
  digests, input digest + bounded input payload, initial active-time/
  lease/cost ceilings, progress journal configuration, artifact root,
  compatibility-safe SDK configuration. NO raw credential value.
- `InvokeJobEvent`: sequence number, timestamp, event kind
  (accepted/started/progress_ref/succeeded/failed/cancelled), HMAC,
  bounded payload.
- `InvokeJobResult`: digest, artifact references, bounded structured
  result, terminal status, timing.
- `invokeJobSchemaVersionV1 = "1.0"` constant.
- Tests: `internal/routedrun/invoke_job_types_test.go` (146 lines).

### A2: ControlJournal (`internal/routedrun/control_journal.go` +338 lines)
- Per-attempt directory `<stateRoot>/runs/<runID>/control/<attemptID>` mode 0700.
- Per-event files `event-NNNNNN.json` mode 0600 (atomic write: temp + fsync + rename).
- Symlink-safe opens (rejectSymlinkInRoot from storefs.go).
- Bounded sizes: reject writes > 64KB per event.
- Sequence numbers: monotonic, no gaps.
- HMAC: per-attempt key (32 random bytes from crypto/rand), stored at
  `<stateRoot>/runs/<runID>/control-key` mode 0600 (Python cannot read).
- `Append(event InvokeJobEvent) error` — atomic, verifies HMAC on read-back.
- `Read(fromSeq int) ([]InvokeJobEvent, error)` — for reconciliation.
- Tests: `internal/routedrun/control_journal_test.go` (304 lines, 6 tests):
  DirectoryAndFileModes, SymlinkTraversalRejected, OversizedEventRejected,
  SequenceMonotonicNoGaps, HMACVerificationFailsOnTamper, KeyFileMode0600.

### A3: InvokeDeployment wired to real AdmitInvocation (`internal/daemon/routed_handlers.go` +292 lines)
- Validates `deployment_ref`, `idempotency_key`, `caller_identity`
  (now required per B26 spec — `InvocationRequest.CallerIdentity` is
  required for idempotency scoping).
- Builds `routedrun.InvocationRequest` from proto.
- Calls `s.localStore.AdmitInvocation(ctx, admitReq, 0)` — reuses the
  existing B26 LocalStore implementation, does NOT write a new path.
- Returns `InvocationReceipt` as `InvokeDeploymentResponse`
  immediately after the durable commit. Does NOT wait for attempt
  creation, resource start, or completion.
- Attempt ID is empty in the response until T05 (supervisor) claims
  the READY intent.
- priorIDs snapshot pattern: distinguishes fresh ACCEPTED from
  IDEMPOTENT_REPLAY without short-circuiting the store's changed-intent
  conflict detection (the store must also see a same-key different-
  digest request to return ErrIdempotencyConflict).
- Error mapping (`invokeDeploymentErrorResponse`): typed response,
  never gRPC error. ALREADY_RUNNING, DEPLOYMENT_NOT_FOUND,
  DEPLOYMENT_INACTIVE, IDEMPOTENCY_CONFLICT mapped to AdmissionOutcomeCode.
- Tests: `internal/daemon/b30_t02_invoke_deployment_test.go` (458 lines, 5 tests):
  AdmitsAndReturnsReceipt, IdempotentReplayReturnsOriginal,
  ChangedIntentReturnsConflict, InactiveDeploymentRejected,
  AlreadyRunningReturnsRetryable.

### A4: Get/Status/Result APIs (stub pass-throughs, pending T05/T08)
- `GetInvocation` — reads invocation record from store (ListInvocations
  iteration since store lacks GetInvocation(id) — documented as stub
  pending T05).
- `GetRunStatus` — reads run record via RunStore.GetRun.
- `GetRunResult` — returns empty/not-found until T05 writes results.
- Proto: `api/control/v1/control.proto` additive extension (+79 lines):
  InvocationRecord, GetInvocationRequest/Response, GetRunStatusRequest/
  Response, GetRunResultRequest/Response, 3 new RPCs on ControlService.

### Fix: B26 adversary tests updated for caller_identity contract
`internal/daemon/t06_adversary_test.go` (+96 -? lines): 4 call sites
updated to add `CallerIdentity` (now required). Tests strengthened:
- `TestAdversaryT06_NotEnabledPathCreatesResources`: now asserts typed
  DEPLOYMENT_NOT_FOUND error, no ACCEPTED outcome, no invocation/run
  IDs minted (was: asserted FEATURE_NOT_ENABLED stub response).
- `TestAdversaryT06_DeactivateWithActiveRuns`: now asserts typed
  DEPLOYMENT_INACTIVE error (was: weak `if err != nil` branch).
- `TestAdversaryT06_CLIIdempotencyKeyLeak`: now exercises real
  admission path, asserts typed error and no invocation ID (was:
  discarded error, asserted nothing).
- `TestAdversaryT06_StateLeakOnFailure`: now adds caller_identity and
  asserts typed error and no state leak.

## Worker iteration budget

Part A worker hit 90/90 iteration budget exactly as pitfall #88
predicts for a task with 6 implementation items + 12 tests. Worker
emitted summary and committed the 4 implementation commits cleanly
before exhaustion. Fix worker (separate dispatch, same branch)
completed the remaining test updates in 8m 33s.

## Orchestrator code review

PASS. Key findings verified:
- Proto extension is ADDITIVE (new messages + RPCs only, no breaking
  changes to existing messages or RPCs).
- priorIDs snapshot pattern is correct: does NOT short-circuit the
  store's conflict detection.
- Legacy `s.invokeAgent` / `urlopen(timeout=60)` path unchanged for
  v0.2.3 compat.
- ControlJournal reuses existing `storefs.go` helpers (mkdirProtected,
  atomicWriteFile, rejectSymlinkInRoot, safeID, readFileStrict) —
  consistent with existing patterns.
- errcheck lint fix on HMAC writer calls uses panic (hash.Hash.Write
  never errors in practice, but errcheck is satisfied).
- All 4 B26 adversary test updates strengthened assertions (not
  weakened) per pitfall #139.

## Verification gate (orchestrator, post-merge)

- `go build ./...`: PASS (0 errors)
- `go test ./... -count=1`: ALL PASS — daemon 64.8s, routedrun 12.9s,
  harness 17.4s, compat v0.2.3, golden, redteam. No FAIL lines. No
  regressions.
- `go vet ./...`: clean
- `golangci-lint run --timeout 5m ./internal/routedrun/... ./internal/daemon/...`: 0 issues (fresh cache)
- `go test ./test/compat/... -count=1 -race`: PASS

## Adversary (deferred to block-end)

Part A unit tests cover the admission idempotency and concurrency
behaviors. The B26 admission-conformance suite for the standalone
topology and crash/replay tests are Part B (next dispatch). Full
adversary matrix runs at block-end (T09).

## Architecture review (deferred to block-end)

Per `agentpaas-owa-build-orchestration` skill, the architecture review
runs once per block after the adversary pass. T02 Part A introduces
new types and a new control-journal subsystem; the architecture
review will verify cross-block contract alignment (B26 InvocationRequest
matches, B27 progress journal compatibility, B29 EventStore
integration points).

## Acceptance criteria (Part A exit gate, from prompt)

- InvokeJob/Event/Result types added with JSON tags and schema
  version constant. PASS.
- ControlJournal implemented with 0700/0600, symlink-safe, atomic,
  HMAC'd, bounded writes. PASS.
- InvokeDeployment handler wired to real AdmitInvocation, returns
  receipt immediately, idempotent replay returns original, changed
  intent returns conflict, inactive deployment rejected, ALREADY_RUNNING
  for concurrency. PASS.
- Get/Status/Result APIs stubbed (thin pass-through to stores). PASS.
- 12 unit tests added, all green. PASS (5 daemon + 6 control journal +
  1 type round-trip = 12).
- Legacy v0.2.3 compat tests unchanged and green. PASS.
- TestB30T01_* ceiling tests still pass (no skip changes). PASS.

## Files changed

- `api/control/v1/control.proto` (+79)
- `api/control/v1/control.pb.go` (+1075 -...) — generated
- `api/control/v1/control.pb.gw.go` (+234) — generated
- `api/control/v1/control_grpc.pb.go` (+114) — generated
- `internal/routedrun/types.go` (+195)
- `internal/routedrun/control_journal.go` (+338, new)
- `internal/routedrun/control_journal_test.go` (+304, new)
- `internal/routedrun/invoke_job_types_test.go` (+146, new)
- `internal/daemon/routed_handlers.go` (+292 -...)
- `internal/daemon/b30_t02_invoke_deployment_test.go` (+458, new)
- `internal/daemon/routed_handlers_test.go` (+46 -...) — existing stub test updated
- `internal/daemon/t06_adversary_test.go` (+96 -...) — 4 call sites updated for caller_identity

Total: +3107 -270 across 12 files.

## Worktree cleanup

- `git worktree remove --force /Users/pms88/projects/ap-b30-t02`
- `git branch -D feat/b30-t02-durable-invoke`
- `git worktree prune`

## What Part A does NOT do (deferred to Part B / T05 / T08)

- Crash/replay tests (Part B).
- B26 admission-conformance suite for the standalone topology (Part B).
- Supervisor claim of the READY intent and attempt/lease/job creation
  in that transition (T05).
- Container startup of exactly one durable invoke job (T05/T08).
- `accepted` before container start, `started` before Python handler (T05).
- Result content to the protected result store (T05/T08).
- Duplicate job envelope idempotency at the harness level (T05).
- 90-second worker completes while client disconnects (T08).

## Next task unblocked

T02 Part B — conformance + crash recovery tests. Same worktree or
fresh from new main. Part B prompt prepares from `b30-summary.md:
301-322` (Tests to write first: crash/replay before/after READY claim,
before/after accepted/started/result write/terminal commit; B26
admission-conformance suite for standalone topology; duplicate job
envelope idempotency at harness level).
