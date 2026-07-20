# B30-T05 Part A — OWA Record

## Task
Durable Supervisor and Liveness Reconciliation (Part A — implementation + basic tests)

## Date
2026-07-20

## Commits
- `e2f0b8a` B30-T05 Part A: durable supervisor, liveness tracker, reconcile, cleanup (3 test failures pending fix)
- `0d2b6c5` B30-T05 Part A fix: resolve 3 test failures (stdout stall, op exemption clamping, journal close)
- `9d90af1` B30-T05 Part A: orchestrator lint fixes (errcheck, staticcheck, unused)
- Merge: `B30-T05 Part A: durable supervisor, liveness tracker, reconcile, cleanup`

## Worker model
- Part A initial: z-ai/glm-5.2 @ nous (hit 90-iteration budget after writing 8 files + 1 localstore modification)
- Part A fix: deepseek/deepseek-v4-pro @ nous (user-requested model switch mid-session)

## Files created (9 files, +2667 lines)
- `internal/supervisor/supervisor.go` (386 lines) — Supervisor interface, core implementation, governed-op handlers
- `internal/supervisor/tracker.go` (273 lines) — Liveness tracker, stall detection, operation-deadline exemption
- `internal/supervisor/lifecycle.go` (537 lines) — Claim, TrackProgress, HandleCheckpoint, HandleResult, Finalize, Cancel
- `internal/supervisor/reconcile.go` (250 lines) — Daemon restart reconciliation
- `internal/supervisor/cleanup.go` (71 lines) — Resource cleanup (idempotent)
- `internal/supervisor/journal.go` (102 lines) — Control-journal event helpers, key management
- `internal/supervisor/hmac.go` (55 lines) — HMAC verification for progress/result events
- `internal/supervisor/supervisor_test.go` (957 lines) — 15 tests (all pass with -race)
- `internal/routedrun/localstore.go` (+36 lines) — GetRunGeneration, GetAttemptGeneration for CAS

## Tests written (15 tests, all pass with -race)
1. TestHeartbeatPreventsStall — authenticated heartbeat resets stall timer
2. TestStdoutSpamDoesNotPreventStall — unauthenticated stdout does NOT reset stall timer
3. TestGovernedOperationExemption — in-flight model call extends stall to operation deadline
4. TestForgedProgressRejected — progress without valid HMAC is rejected
5. TestNoSuccessWithoutVerifiedResult — container exit 0 does NOT mark success
6. TestHandleResultFinalizesSuccess — verified result event finalizes success
7. TestDuplicateFinalizer — calling Finalize twice is idempotent
8. TestCancelPrecedenceOverLateSuccess — cancel wins over late result
9. TestCancelIsIdempotent — cancel is idempotent
10. TestForgedResultRejected — result without valid HMAC is rejected
11. TestLateResultRejected — late result after terminal is rejected
12. TestReconcileRevokesAmbiguousLease — restart with active lease but no terminal revokes lease
13. TestReconcileAcceptsCommittedTerminal — restart with committed terminal does not replay
14. TestReconcilePreservesCheckpoint — restart preserves safe checkpoint
15. TestActiveTimeFrozenDuringPause / TestActiveTimeFrozenDuringNeedsReplan / TestActiveTimeChargedDuringPauseRequested — active-time accounting

## Bugs found and fixed
1. **stallDeadlineMonotonicMs clamping bug** (tracker.go:134): `if start < nowMs { start = nowMs }` clamped the in-flight start to now, making the deadline always in the future. Fix: use `t.inFlightStartedMonotonicMs + deadline` directly.
2. **Fake journal Close() permanently closes** (supervisor_test.go:132): `Close()` set `closed = true`, making all future appends fail since the factory returns the same instance. Fix: `Close()` is now a no-op (matches real ControlJournal behavior where each Open returns a new handle).
3. **TestStdoutSpam wrong advance** (supervisor_test.go:506): Test advanced only 900ms (less than 1000ms stall timeout) but expected stall. Fix: advance 1100ms past the stall timeout.

## Design decisions
- Supervisor is independent of CLI request context (daemon-level goroutine)
- All state transitions through CAS on durable store (UpdateRun with expectedGeneration)
- Stall timer reads TimeEnvelope.StallTimeoutMs authoritatively; in-flight governed operations extend to EffectiveOperationDeadlineMs
- Active-time reconciliation: PAUSED/NEEDS_REPLAN close open segment without charging; PAUSE_REQUESTED/RUNNING charge elapsed
- Control-key loading supports both file-backed (real journals) and factory-backed (test journals via KeyFor interface)
- Audit events published AFTER durable state commit (CAS success)

## Verification
- go build ./... — PASS
- go test ./... -count=1 — ALL PASS (including compat v0.2.3)
- go test ./internal/supervisor/... -count=1 -race — ALL 15 PASS
- go vet ./... — clean
- golangci-lint run ./internal/supervisor/... — 0 issues (after orchestrator lint fixes)
- No em-dash in Go string literals (grep confirmed)

## Spec coverage (T05 required work, lines 431-477)
- [x] 1. Supervisor interface independent of CLI request context
- [x] 2. State transitions only after durable CAS writes
- [x] 3. Track authenticated activity (progress, model/HTTP/MCP start/end, checkpoint, result)
- [x] 4. Do not count stdout/stderr spam, process existence, or unauthenticated file writes as progress
- [x] 5. Stall timer and governed-operation exemptions bounded by operation deadlines
- [x] 6. Finalize success only from verified result event for active lease
- [x] 7. Finalization, cancellation, cleanup idempotent under races
- [x] 8. Audit/timeline events only after durable state commit
- [x] 9. Reconcile daemon restart: revoke ambiguous lease, ingest committed result, never blindly reinvoke
- [x] 10. Preserve safe checkpoint/artifact state for B39 continuation
- [x] 11. Reconcile active-time: frozen during PAUSED/NEEDS_REPLAN, charged during PAUSE_REQUESTED

## What T05 Part A does NOT cover (deferred to T05 Part B / T07 / T08)
- Conformance tests at each lifecycle boundary (T07)
- Crash recovery at checkpoint commit and final result commit (T07)
- Docker container cleanup verification (T07)
- Longevity matrix (T08)
- block30-gate Makefile target (T09)

## Compatibility impact
- Additive: new `internal/supervisor/` package, no existing APIs changed
- `localstore.go`: two new exported methods (GetRunGeneration, GetAttemptGeneration) — additive
- No backward compat regressions (compat v0.2.3 tests pass)

## Next task unblocked
T05 Part B (conformance + crash recovery tests) or T06 (multi-turn reference worker) — both can proceed independently from this merged base.
