# Block 7 — Secrets Brokering & MCP Server Lifecycle

**Status:** COMPLETE
**Gate:** make block7-gate
**Date:** 2026-06-21

## Scope
Two tracks: - **Block 7 (secrets/revocation):** B7-T01 SecretStore, B7-T02 Brokered Gateway, B7-T03 Invisibility Suite, B7-T04 Direct Lease, B7-T05 Revocation - **Block 7.5 (MCP Server Lifecycle Manager):** B7M-T01 Resource Model, B7M-T02 Lifecycle Supervision, B7M-T03 Gateway Routing, B7M-T04 Egress Policy, B7M-T05 Status Reporting, B7M-T06 Audit + Capability Guard

## Subtasks Completed
| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| b7-t01 | B7-T01: SecretStore and CLI Lifecycle | COMPLETE | 13 tests | 1 adversary breaks found | - Tests added: 13 (store_test.go + keychain_test.go) |
| b7-t02 | B7-T02: Brokered Gateway Credential Flow | COMPLETE | 10 tests | 1 adversary breaks found | - Tests added: 10 |
| b7-t03 | B7-T03: Brokered Secret Invisibility Suite | COMPLETE | 11 tests | 3 adversary breaks found | - Tests added: 11 (invisibility) + adversary suite |
| b7-t04 | B7-T04: Direct Lease Compatibility Mode | COMPLETE | 11 tests | 0 adversary breaks | - Tests added: 11 + adversary regression suite |
| b7-t05 | B7-T05: Revocation and Enterprise Follow-Up | COMPLETE | 2 adversary breaks found |
| b7m-t01 | B7M-T01: MCP Resource Model and Policy Binding | COMPLETE | 0 adversary breaks |
| b7m-t02 | B7M-T02: Local MCP Process and Sidecar Supervision | COMPLETE | 0 adversary breaks |
| b7m-t03 | B7M-T03: Gateway-Mediated MCP Routing | COMPLETE | 2 adversary breaks found |
| b7m-t04 | B7M-T04: MCP Workload Egress Policy | COMPLETE | 5 adversary breaks found |
| b7m-t05 | B7M-T05: MCP Status, Dashboard Data, and Hermes Contract | COMPLETE | 4 adversary breaks found |
| b7m-t06 | B7M-T06: MCP Tool Auditing and Host-Affecting Capability Guard | COMPLETE | 3 adversary breaks found |

## Block-End Verification
- verifier profile

## Risk Analysis Summary
- - T02 ReconcileAfterCrash / ReconcileMCPServers live in internal/runtime/reconcile.go (orphan cleanup at runtime/Docker layer, removes MCP sidecars + agents after daemon crash) - T06 RedactToolOutput (redact.go) sanitizes output on allowed path + map keys redacted, control chars escaped, sentinel se
- | Subtask | Worker | Adversary | Breaks | Fix | Gate | OWA Record | |---------|--------|-----------|--------|-----|------|------------| | B7-T01 | PASS | PASS | — | — | PASS | ✓ | | B7-T02 | PASS | PASS | — | — | PASS | ✓ | | B7-T03 | PASS | PASS | — | — | PASS | ✓ |

## Commits
`017a509`, `08242e6`, `09f6c49`, `0b9e493`, `0d6cd73`, `100760a`, `13a0f04`, `18024ef`, `206815c`, `224482d`, `24698a4`, `2606b92`, `2a9f4a5`, `30591fd`, `389435d`
