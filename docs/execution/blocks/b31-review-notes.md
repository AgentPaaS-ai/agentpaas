# B31 Architecture Review Notes

**Date:** 2026-07-21  
**Block:** B31 — Local Package Registry and Promotion (Reduced)  
**Range:** 5727fc5..HEAD  
**Thinker:** ap-thinker (kimi-k3 @ nous)

## Gate status

| Gate | Result | Evidence |
|------|--------|----------|
| `make block31-gate` | PASS | `/tmp/block31-gate-r2.log` ends `Block 31 gate: PASS`; `/tmp/block31-gate-r2.exit` = `MAKE_EXIT:0` |
| Adversary B31 | PASS | `internal/registry/adversary_test.go` + gate segment |
| Architecture R1 | 1 BLOCKER, 4 WARNING, 4 NOTE | `/tmp/b31-arch-review-findings.md` |
| Architecture R2 | 0 BLOCKER, residuals fixed | `/tmp/b31-arch-review-findings-r2.md` |

## Round 1 findings disposition

| ID | Severity | Disposition | Fix commit(s) |
|----|----------|-------------|-----------------|
| F1 | BLOCKER | FIXED — production callers in Pack + failClosedRoutedRun | d53da76 / e9ee43f |
| F2 | WARNING | FIXED — ListRegistry/ShowRegistry RPC + CLI store join | cabf599, 42ca1a1 |
| F3 | NOTE | FIXED — capabilities in lockCanonicalMap | 32c342d |
| F4 | WARNING | FIXED — integration test + gate segments | d53da76, 32c342d |
| F5 | WARNING | FIXED — order-aware promotion audit | d53da76 |
| F6 | WARNING | FIXED then residual R4 | d53da76 then 8ed0955 |
| F7 | WARNING | FIXED — fsync tmp + parent dir | d53da76 |
| F8 | NOTE | FIXED — promote hint name@version | 42ca1a1 |
| F9 | NOTE | Intentional (spec-blessed non-installed skip) | deferred |

## Round 2 findings disposition

| ID | Severity | Disposition | Fix commit |
|----|----------|-------------|------------|
| R1 | WARNING | FIXED — recheckWorkflowPromotion before lock | 8ed0955 |
| R2 | NOTE | Deferred — LocalStore Open on read path / orphan tmp cleanup | post-v0.3 optional |
| R3 | NOTE | FIXED — removed `\|\| true` from gate | 8ed0955 |
| R4 | WARNING | FIXED — flock inside AuditWriter.Append | 8ed0955 |

## Final architecture gate verdict

**PASS** — no residual BLOCKERs; all WARNINGs from R1/R2 fixed and `make block31-gate` green.

## Deferred (NOTES)

- R2: optional read-only LocalStore open mode (skip cleanupOrphanTemps)
- F9: surface non-installed workflow refs at pack time (B32+ polish)
- block28-long real-time Docker proofs remain separate (pre-R30 / pre-v0.3.0)
- Manual testing deferred to pre-v0.3.0-release (standing user decision 2026-07-20)

## Key production surfaces shipped

- `agentpaas registry list|show|promote|demote`
- Daemon `ListRegistry` / `ShowRegistry` RPCs
- Workflow promotion gate on Pack + deploy admission
- Audit events `package_promoted` / `package_demoted`
- Capabilities covered by lock publisher signature
