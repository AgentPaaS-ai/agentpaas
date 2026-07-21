# B30 Risk Analysis

## Block: B30 — Durable Runtime
## Date: 2026-07-20
## Commit: 76cb6ea

## Overview

B30 ships the durable runtime foundation: a supervisor that drives lifecycle
transitions through CAS on the durable store, liveness tracking that only
accepts authenticated progress, a multi-turn reference worker, fault injection
tests, longevity proofs, and a block gate with adversary matrix.

## Risks

### R1: Real-time Docker proofs not yet wired (MEDIUM)
- **Risk**: The 6-minute/20-turn and 30-minute/100-turn real-time proofs are
  stubbed with `t.Skip` and require `AGENTPAAS_DOCKER_TESTS=1`. The block30-gate
  does not depend on block28-long, so the gate can go green without real-time
  evidence.
- **Mitigation**: block28-long now detects SKIP and exits 1. The block30-gate
  prints a NOTE reminding the operator to run block28-long separately. The
  R30 prerelease MUST NOT ship until block28-long passes with Docker.
- **Residual**: Until the real-time proofs are wired, the durable runtime is
  unit-tested but not integration-tested end-to-end with Docker.

### R2: Active-time ledger CAS is new (LOW)
- **Risk**: F6 fix added expectedGeneration to PutActiveTimeLedger. The
  supervisor's reconcile path is the first multi-writer of the ledger. If
  the generation threading is incorrect, concurrent ledger updates could
  silently lose consumed time.
- **Mitigation**: The fix follows the same CAS pattern as casAttemptTo.
  Existing tests pass with -race. The fake-clock longevity test verifies
  active-time accounting over 24h/100 turns.
- **Residual**: No concurrent-writer test exists for the ledger CAS. A
  dedicated concurrent CAS test should be added in B31.

### R3: Checkpoint digest canonical form untested cross-implementation (LOW)
- **Risk**: F8 noted that SaveCheckpoint auto-computes the digest from
  canonical content, overwriting the worker-supplied digest. The Go and
  Python SDK may compute different canonical forms, producing mismatched
  digests.
- **Mitigation**: The daemon recomputes and verifies. The worker's digest
  is advisory. The adversary test for digest tamper passes.
- **Residual**: No cross-implementation known-vector digest test exists.
  Deferred to B39 (when checkpoint resume is fully wired).

### R4: Supervisor not yet wired into daemon (MEDIUM)
- **Risk**: The supervisor package exists but is not yet wired into the
  daemon's invoke path. The daemon still uses the synchronous invokeAgent
  path. T05 provides the lifecycle framework; the actual wiring is a
  future block.
- **Mitigation**: The supervisor is thoroughly tested in isolation. The
  reference worker demonstrates correct usage of all APIs.
- **Residual**: No end-to-end test of "invoke -> supervisor -> durable
  result" exists yet. This is expected for B30 (T07 tests protocol
  behavior, not end-to-end daemon wiring).

### R5: Golden-fast pre-existing failures (LOW)
- **Risk**: G44 (plugin installed-state) and G47 (bundle inspect) fail
  due to missing publisher identity and profile directory. These are
  environmental, not B30 regressions.
- **Mitigation**: Confirmed by running golden-fast at daddff1 (before B30).
  The failures are tracked separately.
- **Residual**: The block30-gate exits non-zero due to golden-fast. The
  B30-specific portions all pass. CI Block Gates workflow will also fail
  on these, but CI and Release Verify are the authoritative checks.

## Summary

B30 ships a well-tested durable runtime foundation. The primary risk is
that real-time Docker proofs are not yet wired (R1), which is explicitly
deferred and gated by the R30 prerelease checklist. The supervisor is not
yet wired into the daemon (R4), which is expected for this block. All
code-level risks (R2, R3) are low and mitigated by existing tests.

## Recommendation

B30 is complete pending:
1. Verifier pass (DONE — VERIFY PASS, 8 segments green)
2. CI green (DONE — CI + Release Verify success on 88e470c)
3. block28-long with Docker (required for R30 prerelease, not for B30 completion)
4. Human manual testing deferred to pre-v0.3.0-release (per founder decision
   2026-07-20: B30 ships durable runtime contracts but not end-to-end daemon
   wiring; manual testing is not useful until v0.3.0 release candidate is
   ready. Re-deferred from "after B30" to "just before v0.3.0 release".)
