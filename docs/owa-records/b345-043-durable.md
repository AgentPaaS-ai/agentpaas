# B345.5-B — BUG-043 durable start tests + gap audit

**Date:** 2026-07-26
**Branch:** feat/b345-043
**Scope:** InvokeDeployment ACCEPTED path → container lifecycle via mock driver

## What was done

### Testability seam (minimal)
Added `testRuntime runtime.RuntimeDriver` field to `controlServer` (stub_handlers.go:108). When set, `getOrCreateRuntime` returns `runtime.NewDockerRuntimeWithDriver(s.testRuntime)` instead of creating a real Docker client. This is the same pattern already used in `testServerWithMockRuntime`.

### Unit/integration tests (b345_durable_start_test.go)
9 tests, all pass with `-race`:

| Test | What it proves |
|------|---------------|
| `TestB345_DurableStart_AcceptedCallsCreateAndStart` | Create+Start called >=2 times (gateway + agent containers) |
| `TestB345_DurableStart_IdempotentReplayDoesNotLaunch` | IDEMPOTENT_REPLAY does NOT call startDurableRun again |
| `TestB345_DurableStart_DisableContainerLaunchPreventsLaunch` | disableContainerLaunch=true prevents all runtime calls |
| `TestB345_DurableStart_PendingRunsAreListable` | Admission creates listable PENDING run |
| `TestB345_DurableStart_RunStatusUpdatesToRunning` | Routed store status moves off PENDING to RUNNING |
| `TestB345_DurableStart_InvokeResponseWritten` | Auto-invoke writes invoke-response.json |
| `TestB345_UpdateLegacyRunStatus_UsesGetRunGeneration` | Status update uses GetRunGeneration, not hardcoded gen=1 |
| `TestB345_UpdateLegacyRunStatus_RetryOnCASConflict` | CAS conflict retry loop re-fetches run+gen |
| `TestB345_DurableStart_AllExistingInvokeDeploymentTestsStillPass` | Regression gate for existing InvokeDeployment tests |

### Number of runtime calls verified
With mock driver, startDurableRun through ACCEPTED path:
- 2x CreateNetwork (internal + egress)
- 1x Create (gateway container)
- 1x Start (gateway container)
- 1x Create (agent container)
- 1x Start (agent container)
- Then auto-invoke: readyz probe + invoke exec

Total: 2 network creates, 2 container creates, 2 container starts, cleanup in finalizeRun.

## Gaps found (resolved or benign)

### 1. `updateLegacyRunStatus` generation conflict — ✅ FIXED (B345.5-B follow-up)
The routed run status update in startDurableRun (line 1714) hardcoded generation=1. The progress tailer (started at line 1701) may increment generation before the update runs, causing a silent CAS failure.

**Fix:** `updateLegacyRunStatus` now:
- Uses `GetRunGeneration` via type assertion on the store (falls back to gen=1)
- Retries up to 3 times on `ErrCASConflict`, re-fetching the run and generation each attempt

Two new tests validate the fix:
- `TestB345_UpdateLegacyRunStatus_UsesGetRunGeneration` — gen>1 before status update
- `TestB345_UpdateLegacyRunStatus_RetryOnCASConflict` — two successive updates at different generations

### 2. Pre-existing test failure
`TestFailClosedRoutedRun_PromotionGateRejectsUnpromotedPackage` fails on main HEAD (8eedf9c) — unrelated to B345 changes. The test writes a workflow.yaml to a directory where the promotion gate picks it up but the pipeline validation ("pipeline requires at least 2 stages") fires before the promotion gate check.

## Residual risk (NOT eliminated by these tests)

- No live Docker container validation (mock-only)
- No multi-turn soak (24h, 100+ turns)
- No supervisor Reconcile path (daemon restart mid-run)
- No cancel workflow path
- No pipeline multi-stage
- Current tests exercise the in-process mock path end-to-end through auto-invoke but the mock exec returns immediate success — real harness startup latency not modeled

## Acceptance

```bash
cd /Users/pms88/projects/ap-b345-043
go test ./internal/daemon/ -count=1 -race -run 'B345|InvokeDeployment'  # all pass
go test ./internal/daemon/ -count=1 -race -run 'InvokeDeployment'       # no regress
go build ./...                                                            # clean
```

Docker test: not added. Mock driver path is green; live Docker test skipped per spec ("optional, only if cheap").
