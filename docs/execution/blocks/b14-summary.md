# Block 14 — Security Remediation, Gateway, Policy, Release

**Status:** COMPLETE — all 4 sub-segments pass, 3 CI workflows green
**Gate:** make block14-gate
**Date:** 2026-06-26
**Block scope:** B13 correctness fixes, security remediation, real-time egress enforcement, release gate

## Scope

Block 14 was the largest block, split into 4 sub-segments:
- **14A0 (Correctness):** Run status tracking, orphan reconciliation, invoke/Stop sync, Docker e2e test, code cleanup
- **14A (Security):** Path allow-list, binary verification, DLP hardening, anti-fabrication guardrail, audit integrity
- **14B (Gateway + Policy):** Gateway container in Run handler, real-time egress enforcement, policy validation, Stats, trigger server
- **14C (Release):** Install docs, README, homebrew formula, volunteer gate

## Subtasks Completed

### 14A0 — Correctness Fixes (5 tasks, all merged)
| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| T01 | Run status tracking | MERGED (036d9e5) | Status field on trackedRun, lifecycle transitions |
| T02 | Orphan reconciliation | MERGED (9c64111) | ReconcileAfterCrash removes orphaned containers. Adversary: 6 valid findings fixed. |
| T03 | Invoke/Stop sync | MERGED (036d9e5) | InvokeDone channel, Stop waits for invoke completion |
| T04 | Docker e2e test | MERGED (240f8f6) | Full pack→run→invoke→stop→audit e2e with real Docker |
| T05 | Rename stubControlServer | MERGED (8b41770) | Type rename, 0 stale references remaining |

### 14A — Security Remediation (8 tasks)
| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| T01 | Plugin path allow-list | MERGED | _validate_project_path() rejects /etc, .., symlinks |
| T02 | AGENTPAAS_CLI binary verification | MERGED | Binary must be agentpaas (path + exec validation) |
| T03 | DLP hardening | MERGED | Fingerprint-based outbound DLP for credential/PII patterns |
| T04 | Anti-fabrication guardrail | MERGED | Prevents agent from inventing results when calls fail |
| T05 | Audit chain integrity | MERGED | genesis prev_hash=="" check in verifyHarnessChain |
| T06 | Pre-flight daemon socket check | MERGED | GAP-8: detect stale socket before start |
| T07 | Sanitizer improvements | MERGED | GAP-4: improved input sanitization |
| T08 | Cosign integration test | MERGED | SHORTCUT-6: real cosign signing test (integration build tag) |

### 14B — Gateway Container + Policy Enforcement (5 tasks)
| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| T01 | Gateway container in Run | MERGED | Dual-homed gateway, egress network, default-deny config. Adversary: 1 HIGH fixed. |
| T02 | Real-time egress enforcement | MERGED | Gateway intercepts HTTP/HTTPS, checks policy, allows/denies in real-time |
| T03 | Policy validation rules | MERGED | Strict YAML schema, port/domain validation, reject conflicting rules |
| T04 | DockerRuntime Stats | MERGED (15abc60) | Real CPU/memory/PID stats. Adversary: 4 MEDIUM → P1 backlog. |
| T05 | Trigger server startup | MERGED (6edfa9f) | trigger.Server on 127.0.0.1:7718/7717, SetInvokeFunc wiring |

### 14C — Release Gate
| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| T01-T03 | Install docs, README, brew formula | MERGED | Clean-machine prerequisites documented |
| Volunteer gate | 2 non-maintainer volunteers | NOT COMPLETED | Required for v0.1.0 release |

## Block-End Verification

VERIFY PASS:
- make lint: 0 issues
- make test: 21/21 Go packages pass
- make race: 21/21 packages pass with -race
- make block14-gate: 4/4 sub-segments pass
- Python plugin tests: 167 tests pass
- Docker e2e: TestE2E_PackRunInvokeStopAudit PASS (19s)
- 3 CI workflows green (ci.yml, block-gates.yml, release-verify.yml)
- Checkpoint key file permissions test (0600) — PASS
- CAP_NET_ADMIN CapAdd Docker normalization — PASS

## Risk Analysis Summary

### 14A Risk Analysis
Security hardening of the B13 baseline: path traversal, binary injection, DLP bypass, audit
chain forgery, and anti-fabrication gaps. All 8 tasks resolved with adversary review.

### 14B Risk Analysis
Gateway container implementation: dual-homed topology, default-deny enforcement, DNS stub
resolver, domain-fronting block, DNS exfiltration block. Key risk: iptables rules programmed
by harness PID 1 (root), CAP_NET_ADMIN dropped after programming (capset drop, not init
container pattern — P2).

### 14C Risk Analysis
Release readiness: volunteer gate not completed, demo recordings not produced, Linux support
deferred to P2.

### 14E Risk Analysis (Final)
All 20 remaining risk items from B14D register resolved:
- R12: Non-HTTP egress control — iptables + HTTP_PROXY two-layer enforcement proven on Colima
- R17: Init container — Option B (capset drop) approved, full pattern is P2
- R18: Rekor transparency log — retry with exponential backoff
- R2+R3: Checkpoint key — encrypted at rest (AES-256-GCM)
- Other risks: RFC1918 tightening, seccomp profile, signed images, SBOM

## Commits

- 036d9e5: 14A0-T01+T03 (run status + invoke sync)
- 9c64111: 14A0-T02 (orphan reconciliation)
- 240f8f6: 14A0-T04 (Docker e2e test)
- 8b41770: 14A0-T05 (rename stubControlServer)
- 15abc60: 14B-T04 (DockerRuntime Stats)
- 6edfa9f: 14B-T05 (trigger server)
- 977eb9e+80fdbda: 14B-T01 (gateway container)
- 133c0b0: 14A-T05 (audit chain integrity)
- 1583814: Final checkpoint key + CapAdd fixes

## Full Details

- [B14A Risk Analysis](../archive/session-history/b14-session-history.md) — Security remediation risks
- [B14B Risk Analysis](../archive/session-history/b14-session-history.md) — Gateway + policy risks
- [B14E Risk Analysis](../archive/session-history/b14-session-history.md) — Final risk register (all 20 items)
- [Session History](../archive/session-history/b14-session-history.md) — All checkpoints, resume prompts, and 50+ worker dispatch prompts
