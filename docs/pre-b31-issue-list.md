# Pre-B31 Comprehensive Issue List — All Deferred Warnings, Vulns, and Ignored Failures

## Source: B5-B30 architecture review notes, risk analyses, golden gate, govulncheck
## Date: 2026-07-20

## CATEGORY 1: Golden Gate Failures (G44 + G47)

| # | Issue | Source | Fix |
|---|-------|--------|-----|
| G1 | G44: profile dir ~/.hermes/profiles/agentpaas doesn't exist | golden-fast | Create the profile, install the agentpaas plugin, run verify-installed-state.py |
| G2 | G47: no publisher identity — agentpaas identity init never run | golden-fast | Run `agentpaas identity init`, then G47 can pack+inspect |

## CATEGORY 2: govulncheck — 5 vulns in moby/moby (GO-2026-5746/5668/5617/4887/4883)

| # | Issue | Source | Fix |
|---|-------|--------|-----|
| V1 | 5 vulns in github.com/moby/moby v28.5.2+incompatible | govulncheck | Pre-existing (pitfall #142). Replace directive already redirects docker/docker -> moby/moby. All 5 have "Fixed in: N/A" — upstream has not released fixes. Track upstream. NOT a B30 regression. |

## CATEGORY 3: B27 Deferred Items (review-notes.md)

| # | Issue | Severity | Fix |
|---|-------|----------|-----|
| B27-1 | F9: Dead runStore parameter on ProgressTailer | NOTE | Wire or remove — B30 supervisor now exists, can wire lease validation |
| B27-2 | V4: ArtifactWorkspace ValidateAndAccept/RemoveUnreferenced not wired into production | WARNING | Wire artifact validation into the tailer/daemon finalize path |
| B27-3 | V6: Heartbeat journal records not fsync'd (only safe_to_resume records are) | WARNING | Add fsync for heartbeats OR add crash test proving no false checkpoint |
| B27-4 | V7: leaseExpired atomic but never set to true in production (dead code) | WARNING | Wire lease expiry producer in the supervisor (B30 now exists) |

## CATEGORY 4: B28 Deferred Items (review-notes.md — 7 WARNINGs)

| # | Issue | Severity | Fix |
|---|-------|----------|-----|
| B28-1 | 4.2: Integration suites cover 5 of 10 steps (2,3,5,6,8 missing) | WARNING | Add missing integration test steps |
| B28-2 | 4.3: K8s security test omits capabilities-drop assertion | WARNING | 3-line fix — add capabilities-drop assert |
| B28-3 | 5.3: Docker Fence is a no-op | WARNING | Bridge to B26/B27 fencing path |
| B28-4 | 5.4: K8s Signal ignores signal type | WARNING | Implement signal type handling |
| B28-5 | 5.5: K8s NetworkPolicy doesn't encode allow rules | WARNING | Add allow rules to NetworkPolicy |
| B28-6 | 5.6: K8s proof runs sleep 30 in busybox | WARNING | Use real signed AgentPaaS fixture image |
| B28-7 | 5.7: Prepare drops PIDsLimit/Disk/Activation/Credentials | WARNING | Add dropped fields to Prepare |

## CATEGORY 5: B29 Deferred Items (review-notes.md — 8 WARNINGs)

| # | Issue | Severity | Fix |
|---|-------|----------|-----|
| B29-1 | 6: buffered_release unbounded memory (streaming.go:369) | WARNING | Cap state.buffered total bytes |
| B29-2 | 7: Append fan-out under mutex (durable_eventstore.go:218-233) | WARNING | Async fan-out |
| B29-3 | 8: Outbox no rollback (outbox.go:60-76) + Outbox is dead code | WARNING | Invert order or add undo; wire or document |
| B29-4 | 9: MarkDelivered flips in-memory before WAL (inbox.go:313-327) | WARNING | Persist first |
| B29-5 | 10: CoT denylist evadable (envelope.go:110-119) | WARNING | Add structural request-side control |
| B29-6 | 11: No Docker streaming/activation integration in gate | WARNING | Add integration steps or amend spec |
| B29-7 | 12: Zero-authority bookkeeping-only (activation_lifecycle.go:158) | WARNING | Fence egress at sandbox level on warm->idle |
| B29-8 | 13-18: 6 NOTEs (parent-dir fsync, zero ResourceCharge, WaitForWake replay, credential denylist depth, no proto wire contract, dual WAL formats) | NOTE | Document or fix individually |

## CATEGORY 6: B30 Deferred Items (review-notes.md)

| # | Issue | Severity | Fix |
|---|-------|----------|-----|
| B30-1 | F3: Claim is an exported dead stub | NOTE | Unexport or delete |
| B30-2 | F8: ComputeDigest auto-overwrites worker digest (no cross-impl test) | NOTE | Verify caller digest vs recompute; add known-vector test |
| B30-3 | F20: Restart monotonic-clock reset (partially fixed — wall-clock fallback added but needs real-restart test) | WARNING | Add real-restart test verifying wall-clock fallback |
| B30-4 | R1: Real-time Docker proofs not wired (block28-long stubs) | MEDIUM | Wire real-time Docker proofs for R30 |
| B30-5 | R2: No concurrent-writer test for ledger CAS | LOW | Add concurrent CAS test |
| B30-6 | R4: Supervisor not wired into daemon invoke path | MEDIUM | Wire supervisor into daemon (future block) |

## CATEGORY 7: B5/B7/B20 Batch Review (3 NOTEs)

| # | Issue | Severity | Fix |
|---|-------|----------|-----|
| BB-1 | F1: Sidecar credentials file transiently exists (read-write mount) | NOTE | Read-only mount / tmpfs / socket handshake |
| BB-2 | F2: MCP egress_binding parsed but not compiled into gateway route | NOTE | Wire EgressBinding into buildMCPTarget/buildMCPBinds |
| BB-3 | F3: block7-gate under-scoped (only ./internal/secrets/...) | NOTE | One-line Makefile fix |
