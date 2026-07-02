# Daemon Startup Bug Fix (Pre-T05)

**Block:** 13 (pre-T05 housekeeping)
**Date:** 2026-06-24
**Status:** MERGED to main (247181c)

## Scope

Fix nil-pointer panic in `internal/cli/daemon.go` `runDaemonStart()`: the code
called `cmdDaemon.ProcessState.Exited()` after `Start()`, but `ProcessState` is
nil until `Wait()` returns. This panicked whenever the daemon exited before the
500ms sleep completed (crash, bad config, missing binary).

## Worker

- Model: grok-composer-2.5-fast via Grok CLI ($0)
- Commits: 131d523, 4510133
- Files: internal/cli/daemon.go, internal/cli/cli_test.go

### Changes

1. Replaced `time.Sleep` + `ProcessState.Exited()` with goroutine+select pattern:
   - `Wait()` in goroutine, race against 500ms timeout
   - Early exit → error with exit code (nil-safe ProcessState access)
   - Survives 500ms → success
2. Extracted `resolveDaemonBinary()` into injectable package var for testing
3. Fixed env var overwrite bug (found by adversary): second
   `append(os.Environ(),...)` was clobbering AGENTPAAS_HOME when both --home and
   socket path were set. Now builds cumulatively.

## Adversary

- Model: grok-4.3 via agentpaas-adversary ($0)
- 6 breaks reported:
  1. **HIGH — Env var overwrite** (pre-existing, lines 232-237). REAL. Fixed.
  2. **MEDIUM — Goroutine leak** (success path). Benign — CLI process exits shortly after, reaping goroutine. Not fixed (standard CLI spawner pattern).
  3. **MEDIUM/HIGH — Race on waitCh/ProcessState**. FALSE POSITIVE — race was in adversary's own test code (captureStdout from goroutine), not in daemon.go. Verified clean under -race.
  4. **LOW — Exit code 0 during grace = error**. Correct behavior — a daemon shouldn't exit in 500ms.
  5. **MEDIUM — Success-path test gap**. REAL. Fixed (TestDaemonStart_StaysAlive_Success).
  6. **MEDIUM — Global resolver state fragility** under t.Parallel(). Valid fragility, not a current break. Deferred.

## Gate

- `go build ./...` — PASS
- `go vet ./internal/cli/...` — PASS
- `go test -race -count=1 ./internal/cli/...` — PASS (5 daemon tests, including 2 new regression tests)

## Tests Added

- `TestDaemonStart_ExitImmediate_NoPanic` — fake daemon exits 1, verifies error (not panic)
- `TestDaemonStart_StaysAlive_Success` — fake daemon sleeps 2s, verifies success + PID cleanup

## Verifier

Deferred to block-end verification.
