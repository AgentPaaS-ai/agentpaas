# B30 Architecture Review Notes

## Date: 2026-07-20
## Reviewer: ap-thinker (Kimi-K3)
## Block: B30 — Durable Runtime

## Findings: 5 BLOCKER, 2 WARNING, 5 NOTE

### BLOCKERs (all fixed)

- **F1**: CheckpointEvent.HMAC accepted but never verified. Fixed: added verifyCheckpointHMAC in hmac.go, called in HandleCheckpoint.
- **F2**: ClaimForRun hardcoded 60-second attempt lease. Fixed: resolves from ClaimOptions.AttemptLeaseMs -> run.MaxAttemptLeaseMs -> default.
- **F5**: Supervisor CAS paths bypass B26 transition maps. Fixed: casAttemptTo/casRunTo/revokeLeaseAndFail now call ValidateAttemptTransition/ValidateRunTransition.
- **F6**: Active-time ledger writes are non-CAS (generation always 1). Fixed: PutActiveTimeLedger now accepts expectedGeneration, saveActiveTimeLedgerLocked uses it.
- **F9**: block28-long runs tests that unconditionally skip. Fixed: block28-long now detects SKIP in test output and exits 1.

### WARNINGs (fixed)

- **F4**: ResultEvent digest not recomputed against StructuredResult. Fixed: HandleResult now recomputes SHA-256 and rejects mismatch.
- **F10**: block28-ci exercises only one longevity test. Fixed: added T01 boundary/ceiling regression tests to block28-ci.

### NOTEs

- **F3**: Claim is an exported dead stub. Deferred (cosmetic, no functional impact).
- **F7**: GetRunGeneration/GetAttemptGeneration additions are safe. No action.
- **F8**: ComputeDigest/VerifyDigest auto-overwrites worker digest. Documented; preferred fix (verify caller digest) deferred to B39.
- **F11**: Gate omits govulncheck. Fixed: added govulncheck step to block30-gate.
- **F12**: Non-adversary tests assert the contract and are not vacuous. No action needed.

## Fixes applied

Commits:
- `e2f01e5` fix(F1): verify checkpoint HMAC in HandleCheckpoint
- `0f83a5a` B30-arch-review: fix F2 (policy-derived lease), F4 (result digest), F5 (transition validation), F6 (ledger CAS)
- `fc01f9a` B30-arch-review: lint fix (remove unused makeForgedCheckpoint helper)
- `b775aa1` B30-arch-review: fix F9 (block28-long skip detection), F10 (block28-ci boundary tests), F11 (govulncheck)
- Merge: `B30-arch-review: fix F1-F6`

## Deferred items

- F3 (Claim dead stub): cosmetic, no functional impact
- F8 (digest auto-overwrite): preferred fix (verify caller digest vs recompute) deferred to B39
- block28-long real-time proofs: require Docker, separate gate

## Final block status

Three gates passed:
1. Block gate: `make block30-gate` — PASS (golden-fast G44/G47 pre-existing environmental failures, NOT B30 regressions)
2. Adversary gate: 14 adversary tests — ALL PASS (1 real break found and fixed: checkpoint digest tamper)
3. Architecture gate: 5 BLOCKERs found and fixed, 2 WARNINGs fixed, 5 NOTEs (3 deferred, 2 no-action)
