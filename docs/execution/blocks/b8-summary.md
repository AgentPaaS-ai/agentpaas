# Block 8 — Packaging Pipeline (agent pack)

**Status:** COMPLETE
**Gate:** make block8-gate
**Date:** Unknown

## Scope
- Full Block 8 gate: build, test, race, lint, osv, pack-specific tests, adversary tests - Cross-subtask integration review (T01→T02→T04→T05→T06 pipeline) - Adversary test verification (B8-T02..T05 regression suite) - Security review: fail-closed scan, signature enforcement, immutability, advisory failOnCritical - Acceptance criteria check against docs/owa-records/b8-t01.md..b8-t06.md

## Subtasks Completed
| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| b8-t01 | B8-T01: Agent Project Detection and Init Scaffold | COMPLETE | 33 tests | 9 files | 0 adversary breaks |
| b8-t02 | B8-T02: BuildKit Image Assembly | COMPLETE | 19 tests | 3 files | 0 adversary breaks |
| b8-t03 | B8-T03: Secret Scan and Build Context Control | COMPLETE | 19 tests | 3 files | 0 adversary breaks |
| b8-t04 | B8-T04: SBOM, Signing, and agent.lock | COMPLETE | 14 tests | 0 adversary breaks | - Tests added: 14 |
| b8-t05 | B8-T05: Immutable Prompt and Config Update Path | COMPLETE | 10 tests | 4 adversary breaks found | - Tests added: 10+ |
| b8-t06 | B8-T06: OSV Advisory Reporting and Local OCI Repair | COMPLETE | 15 tests | 0 adversary breaks | - Tests added: 15 |

## Block-End Verification
- ## Verifier Scope - Full Block 8 gate: build, test, race, lint, osv, pack-specific tests, adversary tests - Cross-subtask integration review (T01→T02→T04→T05→T06 pipeline) - Adversary test verification (B8-T02..T05 regression suite) - Security review: fail-closed scan, signature enforcement, immutab
- ## Findings
- ### verifier 8a — BLOCKER: TOCTOU adversary test fails (RESOLVED) **Finding:** `TestAdversaryB8T05_TOCTOURace_TamperDuringVerify` failed 4/5 runs. `VerifyDeployedIntegrity` read the lockfile twice (lines 107 + 144) with a `bytes.Equal` comparison, but the second read was ineffective against TOCTOU —
- ### verifier 8b — Cross-subtask integration (PASS) Pipeline wiring verified: - T01→T02: DetectProject finds agent.yaml; BuildImage consumes BuildConfig with detected project dir - T02→T04: CreateAgentLock records BuildResult.ImageDigest into lock.ImageDigest - T03 before T02: ScanSecrets is standalo
- ### verifier 8c — Security review (PASS post-fix) - Secret scan (T03): Fail-closed confirmed - agent.lock (T04): Signature verification enforced, not skippable - Immutability (T05): Full-file SHA-256 sidecar + atomic deploy + read-once TOCTOU (fixed) - Advisory (T06): Non-critical findings don't fai

## Risk Analysis Summary
- | Subtask | Merged | Tests Pass | Adversary Pass | Acceptance Met | |---------|--------|------------|----------------|----------------| | B8-T01 (detect/init) | cce5991 | YES | N/A (not required) | YES | | B8-T02 (build) | 01ae72a | YES | YES (11/11) | YES | | B8-T03 (scan) | c2b2948 | YES | YES (12

## Commits
`01860aa`, `01ae72a`, `1cb2e6c`, `76990ef`, `8519c5f`, `c2b2948`, `cb3bc3b`, `cce5991`, `e8c96ca`, `eca01ab`
