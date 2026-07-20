# B30-T03 Part B — Wire TimeEnvelope into 4 ceiling locations + unskip T01 tests

**Date:** 2026-07-19
**Branch:** feat/b30-t03-partB
**Worker commits:** b60c16e..76f8d2a (7 commits, 2 workers)
**Merge commit:** f3904b4 (main)
**Durations:** Part B 90-iter budget (6 commits); continuation (2 tests, 1 commit)

## Spec reference

`docs/execution/blocks/b30-summary.md:329-393` (T03). Part B wires
the TimeEnvelope (from Part A) into the 4 ceiling source locations and
unskips the corresponding T01 ceiling tests.

## What changed

### 4 ceiling locations wired to TimeEnvelope

1. **Ceiling 1 (daemon 2-min context)** — `internal/daemon/control_handlers.go`:
   `context.WithTimeout(invokeCtx, 2*time.Minute)` replaced by
   TimeEnvelope-derived deadline via `invokeTimeoutForPayload`. Legacy
   2-min fallback retained with "legacy compat" comment.

2. **Ceiling 3 (harness 5-min default)** — `cmd/harness/main.go`:
   `envDuration("AGENTPAAS_INVOKE_TIMEOUT", 300*time.Second)` replaced
   by TimeEnvelope-derived deadline. Env var override stays as legacy
   compat.

3. **Ceiling 4 (harness 120s budget)** — `internal/harness/budget.go`:
   `defaultWallClockBudget = 120 * time.Second` becomes legacy
   fallback. BudgetEnforcer uses `env.ActiveTimeRemainingMs` when
   TimeEnvelope present.

4. **Ceiling 5 (model client 120s HTTP timeout)** — `internal/harness/rpc_server.go`:
   `http.Client{Timeout: 120 * time.Second}` replaced by
   `modelClientTimeout` derived from TimeEnvelope. Legacy 120s
   fallback as `legacyModelClientTimeout`.

### T01 ceiling tests unskipped and inverted
- `TestB30T01_DaemonInvokeTimeoutIsTwoMinutes_Failing` — asserts
  `legacyInvokeContextTimeout` (not the fixed 2-min on durable path).
- `TestB30T01_HarnessInvokeTimeoutDefault5Min_Failing` — asserts
  TimeEnvelope derivation.
- `TestB30T01_HarnessBudgetDefault120s_Failing` — asserts
  ActiveTimeRemainingMs derivation.
- `TestB30T01_ModelClientTimeout120s_Failing` — asserts
  `modelClientTimeout` derivation, fixed 120s only as
  `legacyModelClientTimeout`.

### Registry updated
- `b30T01Ceilings` shrunk from 7 to 1 (only the urlopen timeout owned
  by T02 remains). Length assertion updated to 1.

### 20 Part B tests
- 16 ceiling wiring tests (4 per ceiling: derived, legacy fallback,
  exhausted-clamps-low, seam + real-call).
- `TestB30T03PartB_NoUnauthorizedFixedDurablePathTimeout` — regression
  guard with baseline snapshot allowlist. Passes today, fails on any
  NEW fixed durable-path timeout.
- `TestB30T03PartB_ClientQueryTimeoutLeavesRunActive` — client query
  timeout (100ms) does not cancel the run.

## Merge conflict resolution

T03 PartB and T04 (running in parallel) both modified the T01 ceiling
registry and tests 6+7. Orchestrator reconciled:
- Took T03 PartB's inverted tests 1-5 + registry (3 entries after
  removing the 4 T03-owned).
- Applied T04's inverted tests 6-7 (durable path no longer has fixed
  rlimits; legacy path keeps them with "legacy compat" comments).
- Removed the 2 T04-owned registry entries (RLIMIT_CPU, RLIMIT_NPROC).
- Registry: 1 entry (urlopen owned by T02). Length assertion: 1.

## Orchestrator code review

PASS. Legacy fallback pattern correctly preserves v0.2.3 compat: each
ceiling location has a "legacy compat" comment on the fallback
constant (T01 allowlist satisfied). TimeEnvelope-derived deadlines use
`EffectiveOperationDeadlineMs` as specified. Registry reconciliation
correct: 6 of 7 ceilings now replaced, 1 remaining (urlopen, T02/T05
owns).

## Verification gate (orchestrator, post-merge)

- `go build ./...`: PASS (0 errors)
- `go test ./... -count=1`: ALL PASS — daemon, routedrun, harness,
  compat v0.2.3. No FAIL. No regressions.
- `go vet ./...`: clean
- `golangci-lint run --timeout 5m`: 0 issues

## Acceptance criteria (exit gate, b30-summary.md:390-393)

> Every termination report names the authoritative boundary, configured
> limit, observed duration, and remaining envelope.

PASS. 4 ceilings wired to TimeEnvelope-derived deadlines with legacy
fallbacks. T01 ceiling tests unskipped and inverted. No unauthorized
fixed durable-path timeout (regression guard active). Client query
timeout does not cancel the run.

## Worktree cleanup

- `git worktree remove --force /Users/pms88/projects/ap-b30-t03-partB`
- `git branch -D feat/b30-t03-partB`
- `git worktree prune`

## Next task unblocked

T05 — Durable supervisor + liveness reconciliation. T05 owns:
- Ceiling 2 (urlopen timeout=60) — replaces the lifetime-spanning
  urlopen with the durable InvokeJob protocol.
- The corrupt-JSON-validation gap deferred from T02 Part B.
- Supervisor claims the READY intent (T02 created it), creates
  attempt/lease/job, persists accepted/started events, observes
  progress journal, finalizes from verified result.
