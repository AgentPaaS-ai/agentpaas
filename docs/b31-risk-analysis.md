# B31 Post-Build Risk Analysis

**Block:** B31 — Local Package Registry and Promotion (Reduced)  
**Date:** 2026-07-21  
**Head at analysis:** after 8ed0955 merge

## What shipped

Local registry read API (CLI + daemon RPC) over B23 install + B26 deployment
stores; promote/demote with audit; workflow validation rejecting un-promoted
package names at pack and deploy admission.

## Residual risks

| ID | Risk | Severity | Mitigation / status |
|----|------|----------|---------------------|
| B31-R1 | CLI promote concurrent with daemon without flock was chain-fork risk | HIGH→LOW | AuditWriter.Append now flocks; concurrent append test added |
| B31-R2 | Pack TOCTOU on workflow.yaml between gate and lock | MED→LOW | recheckWorkflowPromotion immediately before CreateAgentLock |
| B31-R3 | Hand-edit promoted=true without audit | MED | Gate requires last relevant audit event is package_promoted |
| B31-R4 | Credential leak via registry JSON | LOW | Only credential IDs in RegistryEntry; adversary + daemon tests |
| B31-R5 | Non-installed packages skip promotion gate | LOW | Spec-blessed local-owner carve-out (F9) |
| B31-R6 | Capabilities metadata trust | LOW | Now in lockCanonicalMap; adversary tamper tests |
| B31-R7 | Supervisor not daemon-wired | PRE-EXISTING | B30 R4; not B31 scope |
| B31-R8 | moby/moby govulncheck CVEs | PRE-EXISTING | Docker v29 migration before v0.4.0 |

## NO-GO conditions (exit gate)

1. Orchestrator can delegate to un-promoted package → **blocked** (pack + deploy paths + integration test)
2. Raw endpoint reaches worker via registry → **not introduced** (joined view only)
3. Registry reads expose credential values → **blocked** (IDs only)

## Testing evidence

- `make block31-gate` PASS (MAKE_EXIT=0), golden-fast 19/19
- Adversary suite TestAdversary_B31_* PASS
- Architecture review R1 + R2 completed; WARNINGs fixed

## Manual testing

Deferred to pre-v0.3.0-release (user decision 2026-07-20).

## Handoff

B32+ can consume `registry show` / ListRegistry for identity display and
authoring-time package selection. Pin digests at B26 admission remains
authoritative.
