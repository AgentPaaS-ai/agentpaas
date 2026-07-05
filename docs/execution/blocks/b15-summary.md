# Block 15 — P1 Completion Items (Pre-Release Gap Closure)

**Status:** COMPLETE — MERGED TO MAIN (commit 4609452)
**Gate:** make block15-gate
**Date:** 2026-07-02
**Block scope:** Credential onboarding, LLM integration, policy authoring, trigger/cron surface, production hardening

## Scope

Block 15 closed all P1 pre-release gaps: secret management CLI, LLM provider integration via
unified gateway egress, policy authoring with pack-time validation, trigger/cron/event surface
(29 plugin tools), and production hardening (5 micro-chunks). After B15, the product is
feature-complete for v0.1.0.

## Subtasks Completed

| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| T01 | Credential onboarding | DONE | secret add/list/remove/rotate/test + provider adapters (OpenAI/Anthropic/xAI). Keychain-backed. 11 Python tests. |
| T02 | LLM provider integration | DONE | agent.llm() = sugar over http_with_credential. Unified gateway egress. Interactive provider selection. Pre-deployment validation via secret test. |
| T03 | Policy authoring | DONE | policy init with 4 templates, pack-time validation. agentpaas reconcile auto-derives policy from agent.yaml. |
| T04 | Trigger/cron/event surface | DONE | 29 plugin tools total. Trigger server on 127.0.0.1:7718/7717. Cron scheduling with payload support. |
| T05 | Production hardening | DONE | 5 micro-chunks (see below). |
| T06 | goreleaser config | DONE | Migrated to goreleaser v2.16 |
| T07 | Clean-machine docs | DONE | Prerequisites, README quickstart rewritten |
| T08 | Egress regression gate | DONE | Already passing, wired into Makefile |

### T05 Production Hardening Detail

| MC | Title | Key Findings |
|----|-------|-------------|
| MC1 | RFC1918 tightening | /16 derived from gateway IP, fail-closed (no broad RFC1918 fallback). LOW risk. |
| MC2 | Rekor retry | 3 attempts, exponential backoff (2s/4s). Local refs skip tlog. |
| MC3 | Checkpoint key encryption | AES-256-GCM, PBKDF2-HMAC-SHA256 100K iterations. Passphrase from Keychain. Legacy migration on load. |
| MC4 | Init container decision | Option B (capset drop) approved. Full init-container pattern is P2. |
| MC5 | CAP_NET_ADMIN verification | Docker e2e test confirms capset drop after firewall programming. Fail-closed. |

## Block-End Verification

VERIFY PASS:
- All Go tests pass
- All Python plugin tests pass (167+)
- block15-gate green
- E2e Docker test passes
- LLM integration verified end-to-end (provider adapters, secret test, gateway egress)

## Risk Analysis Summary

**T05 Production Hardening:**
- MC1: /16 is broader than strictly necessary (/24 or /28 would be tighter) but acceptable
  since gateway + harness RPC are the only reachable hosts
- MC3: Keychain passphrase retrieval is macOS-specific — Linux needs alternative (P2)
- MC4: capset drop is not the full init-container pattern — Docker inspect still shows
  NET_ADMIN in CapAdd, but the process cannot use it. P2 implements full pattern.

**T02 LLM Integration:**
- LLM calls route through gateway as credentialed HTTP egress (no special-casing)
- Provider, model, and credential binding in agent.yaml
- Pre-deployment validation via `agentpaas secret test <name>`

**Cross-references:** execution plan §15, B14E risk register (R17, R1), B14A/B/C risk analyses.

## Key Architecture Decision: MC4 — Init Container Pattern

**Option B (DECIDED, user-approved):** Capset drop — harness binary (PID 1, root) programs
iptables rules, then DropNetAdminCapability() removes CAP_NET_ADMIN from effective/permitted/
inheritable sets before Python worker starts. Docker inspect still shows NET_ADMIN, but the
runtime process cannot use it.

**Option A (deferred to P2):** Full init-container pattern — separate firewall-init container
with NET_ADMIN, agent container joins via --net=container:<init-id>, zero capabilities.
Requires new Dockerfile, ContainerSpec.NetworkMode field, topology restructuring.

## OWA Process

- Workers: grok-composer-2.5-fast (Grok CLI), deepseek-v4-pro (fallback)
- Adversary: grok-4.3 via agentpaas-adversary profile
- Verifier: GLM-5.2 via agentpaas-verifier profile (VERIFY PASS x2)

## Commits

- 0633252: MC1 secret add/remove with aliases
- 0212fe0: MC2 secret rotate
- 4c72682: MC3 secret test + provider adapters
- c6def6c: MC4 Hermes plugin 5 secret tools
- b09ea0a: MC5 credential onboarding docs
- 3b5c5ea: MC1 RFC1918 tightening
- 2084a59: MC2 Rekor retry
- 4609452: Block 15 merged to main

## Full Details

- [Risk Analysis](../archive/session-history/b15-session-history.md) — 13KB risk analysis
- [T05 Architecture Decisions](../archive/session-history/b15-session-history.md) — MC4 Option B decision
- [E2E Test Plan](../reference/e2e-test-plan.md) — Lifecycle use cases LC-01..LC-05
- [Session History](../archive/session-history/b15-session-history.md) — All checkpoints, resume prompts, MC worker prompts
