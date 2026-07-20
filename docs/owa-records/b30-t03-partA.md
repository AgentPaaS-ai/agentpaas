# B30-T03 Part A — TimeEnvelope: authoritative active-time/operation-deadline/cancellation

**Date:** 2026-07-19
**Branch:** feat/b30-t03-time-envelope
**Worker commit:** c0bb968
**Merge commit:** ff0d6e0 (main)

## Spec reference

`docs/execution/blocks/b30-summary.md:329-393` (T03). Part A is pure
new code; Part B (next dispatch) wires the TimeEnvelope into the 4
ceiling locations and unskips the T01 ceiling tests.

## What changed (3 new files, +1183 lines)

### `internal/routedrun/types.go` (+173 lines)
- `TimeEnvelope` struct with: SchemaVersion, CurrentMaxActiveDurationMs,
  ConsumedActiveDurationMs, RunningSegmentStartMs (*int64), AttemptLeaseRemainingMs (*int64),
  StallTimeoutMs, ModelCallTimeoutMs, LifecycleAuthorityGeneration (starts at 1),
  CancellationGeneration (starts at 0), FrozenConsumedMs.
- `NewTimeEnvelope(...)` constructor (authority=1, cancellation=0).
- `timeEnvelopeSchemaVersionV1 = "1.0"` constant.
- `EffectiveOperationDeadlineMs(nowMs, operationTimeoutMs) int64` — min of
  operation_timeout, attempt_lease_remaining, active_time_remaining.
- `ActiveTimeRemainingMs(nowMs) int64` — max - consumed, includes running segment elapsed.
- `IsExpired(nowMs) bool` — active_time <= 0 or lease <= 0.

### `internal/routedrun/clock.go` (+449 lines, new)
- `Clock` interface: `Now() time.Time` (UTC wall), `NowMonotonic() time.Time`,
  `NowMonotonicUnixMs() int64`.
- `Timer` interface: `After(d) <-chan time.Time`, `NewTimer(d) TimerHandle`.
- `TimerHandle` interface: `Stop() bool`, `Reset(d) bool`.
- `SystemClock` — production (time.Now).
- `SystemTimer` — production (time.After / time.NewTimer).
- `FakeClock` — test: SetWall, AdvanceMonotonic, timers fire in deadline order,
  wall and monotonic axes fully independent (for T08 24h/100-turn test).
- Segment accounting (immutable — returns new TimeEnvelope per pitfall #134):
  - `StartActiveSegment(env, nowMs) (TimeEnvelope, error)` — error if already open.
  - `CloseActiveSegment(env, nowMs) TimeEnvelope` — idempotent.
  - `FreezeActiveSegment(env, nowMs) TimeEnvelope` — close + set FrozenConsumedMs.
  - `UnfreezeActiveSegment(env, nowMs) TimeEnvelope` — clear frozen (does NOT auto-start).
- Termination:
  - `TerminationReason` enum: UserCancel, ActiveTimeExhausted, LeaseExpired, Stall, ProcessFailure.
  - `ResolveTermination(events []TerminationEvent) TerminationReason` — precedence:
    user cancel > active-time > lease > stall > process failure.
- B39 amendment seam:
  - `WithAmendedCeiling(env, newMaxActiveMs, newAuthorityGeneration) TimeEnvelope` —
    preserves ConsumedActiveDurationMs (no time lost on amendment).

### `internal/routedrun/time_envelope_test.go` (+561 lines, new, 22 tests)
1. EffectiveDeadline_MinOfThree (minus/at/plus-one boundaries)
2. ActiveTimeRemaining_IncludesRunningSegment
3. IsExpired_WhenActiveTimeExhausted
4. IsExpired_WhenLeaseExpired
5. Clock_SystemNowUTC
6. Clock_FakeClockAdvance (no drift)
7. Clock_FakeClockTimersFireInOrder
8. ActiveSegment_StartThenClose_AccruesElapsed
9. ActiveSegment_CloseIsIdempotent
10. ActiveSegment_FreezeAndUnfreeze (frozen interval not charged)
11. ActiveSegment_StartOnAlreadyOpen_IsError
12. Termination_Precedence_UserCancelWins
13. Termination_Precedence_ActiveTimeOverLease
14. Termination_Precedence_LeaseOverStall
15. Termination_Precedence_StallOverProcessFailure
16. Termination_Precedence_AllFive
17. AuthorityGeneration_StartsAtOne
18. CancellationGeneration_StartsAtZero
19. JSONRoundTrip (all fields preserved)
20. AmendedCeiling_PreservesConsumedTime (B39 seam)
21. LongDurationNoOverflow (math.MaxInt64, no panic/wrap)
22. WallClockJumpDoesNotAffectMonotonicDuration (backward jump, monotonic safe)

## Design decisions for Part B (worker flagged)

1. `EffectiveOperationDeadlineMs(nowMs, operationTimeoutMs)` takes the
   operation timeout as an ARGUMENT (not read from StallTimeoutMs/
   ModelCallTimeoutMs internally) — Part B at each ceiling selects
   which per-operation ceiling applies.
2. FakeClock keeps wall and monotonic axes fully independent:
   `SetWall` jumps only wall, `AdvanceMonotonic` moves only monotonic
   and fires timers in deadline order. `NowMonotonicUnixMs()` is the
   helper Part B/T08 should use when calling segment-accounting methods.
3. Segment-accounting methods return a NEW TimeEnvelope (immutable);
   `StartActiveSegment` returns `(TimeEnvelope, error)`. Part B must
   assign the returned envelope back rather than relying on mutation.
4. `UnfreezeActiveSegment` clears `FrozenConsumedMs` but does NOT
   auto-start a new segment — Part B must call `StartActiveSegment` to
   resume accrual.

## Orchestrator code review

PASS. All 22 tests are real assertions (t.Fatal/t.Errorf, no t.Log-only).
Immutable segment accounting correctly returns new TimeEnvelope (pitfall
#134 CAS lesson applied). FakeClock's separate wall/monotonic axes
directly support the T08 24h/100-turn fake-clock test and the wall-clock-
jump safety test (#22). Precedence function is a pure function (no side
effects). Amendment seam preserves consumed time (B39-ready).

## Verification gate (orchestrator, post-merge)

- `go build ./...`: PASS (0 errors)
- `go test ./... -count=1`: ALL PASS — daemon 65.7s, routedrun 15.8s,
  harness 18.3s, compat v0.2.3, golden, redteam. No FAIL. No regressions.
- `go vet ./...`: clean
- `golangci-lint run --timeout 5m ./internal/routedrun/...`: 0 issues (fresh cache)

## Acceptance criteria (Part A exit gate)

- TimeEnvelope type with all fields from A1 and JSON tags. PASS.
- EffectiveOperationDeadlineMs, ActiveTimeRemainingMs, IsExpired. PASS.
- Clock/Timer interfaces with SystemClock, SystemTimer, FakeClock. PASS.
- Active-time segment accounting (immutable). PASS.
- TerminationReason enum + ResolveTermination. PASS.
- Ceiling-update seam (WithAmendedCeiling) reserved for B39. PASS.
- 22 tests, all green. PASS.
- No changes to the 4 ceiling source locations. PASS (Part B owns wiring).

## Files changed

- `internal/routedrun/types.go` (+173)
- `internal/routedrun/clock.go` (+449, new)
- `internal/routedrun/time_envelope_test.go` (+561, new)

## Worktree cleanup

- `git worktree remove --force /Users/pms88/projects/ap-b30-t03`
- `git branch -D feat/b30-t03-time-envelope`
- `git worktree prune`

## Next task unblocked

T03 Part B — wire TimeEnvelope into the 4 ceiling locations and unskip
the T01 ceiling tests. Part B will:
- Replace `context.WithTimeout(invokeCtx, 2*time.Minute)` in
  control_handlers.go:909 with TimeEnvelope-derived deadline.
- Replace `300*time.Second` in cmd/harness/main.go:24 with TimeEnvelope.
- Replace `defaultWallClockBudget = 120 * time.Second` in budget.go:17
  with TimeEnvelope-derived active-time.
- Replace `http.Client{Timeout: 120 * time.Second}` in rpc_server.go:471
  with effective operation deadline.
- Unskip `TestB30T01_DaemonInvokeTimeoutIsTwoMinutes_Failing`,
  `TestB30T01_HarnessInvokeTimeoutDefault5Min_Failing`,
  `TestB30T01_HarnessBudgetDefault120s_Failing`,
  `TestB30T01_ModelClientTimeout120s_Failing` — invert assertions to
  verify the new policy-derived behavior.

Also can run in parallel with T04 (rlimits — owns ceilings 6, 7, no
code dependency on T03).
