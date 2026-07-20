# Phase 2 Execution Plan v1 — Sharing, Portability, and Enterprise Proof (B21-B27)

**Status:** HISTORICAL EXECUTION BASELINE — superseded for B26+
**Date:** 2026-07-10
**Reads with:** `phase2-sharing-prd-v1.md` (WHY/WHAT, normative schemas)
and `blocks/b21..b27-summary.md` (block specs with tests + gates).

> Historical note (2026-07-18): this file records the original B21–B27 plan.
> Current B26/B27 definitions and the B28–B41 v0.3–v0.5 release train are
> governed by `Agentpaas-pitch.md`, `docs/roadmap.md`,
> `docs/execution/planning/prd-v4-master.md`, and the current block summaries.
> Stable closure is at B32 (`v0.3.0`), B35 (`v0.4.0`), and B41 (`v0.5.0`),
> with GitHub prereleases after B30 and B40.

## Sequencing and preconditions

```
B20 (v0.1.2, security claim closure)   ← hard prerequisite, in flight
B19 P0 subset (v0.1.3: T2 budgets/rate limits + T11 retest)
B21 → B22 → B23 → B24 → B25 (v0.2.0) → B26 T01–T03 → B27
                                                   ↘ B26 T04/deferred rails (signal-gated)
```

- B21 and B22 may start the moment B20's 11 gates pass; they are pure
  Go/CLI and do not touch B19 surfaces. If calendar pressure demands,
  B21/B22 can run in PARALLEL with B19 P0 on separate worktrees — the
  only shared file risk is `internal/policy/schema.go` (B19 T2) vs none
  in B21/B22. B23 must wait for both.
- B23 hard-depends on B20 T03 and T07 behavior; re-read those specs
  before starting.
- B25 is serialized last and owns all Hermes plugin product behavior changes.
  Its T00 security dependency closure executes before all sharing/release
  chunks. B26 may add conformance adapters/fixtures for Hermes but may not
  change Hermes authority semantics without a separately reviewed spec.
- B26 T01–T03 are an ordered execution-ready block: Codex adapter, shared
  conformance, then Agent Approval Pack. Generic MCP is a nonblocking decision
  gate; distribution rails remain outside the completion gate.
- B27 starts only after B25 and B26 T03. Follow its 14-chunk dependency table;
  do not begin multi-LLM/demo work before the Broker/gateway and keyless
  credential foundation is green.

## Build method (unchanged from B16+ practice)

- Three-role loop per block: Builder (fresh session, block spec + PRD
  sections + repo), Spec Reviewer (does code match spec exactly),
  Adversary (gets the block's security claims, writes breaking tests).
  Founder runs each block's success gate personally.
- TDD; every task lands as scoped `feat/test/docs:` commits on a
  `feat/b2N-...` branch; block summary updated with a subtask table and
  bug log before merge (B18 format).
- Stop-on-bug during S-card and gate execution; fix, then re-run from
  the top of the affected suite.

## Builder session start prompt template (per block)

```
You are the builder for AgentPaaS Block B2N. Read, in order:
1. docs/execution/planning/phase2-sharing-prd-v1.md  (sections 6, 7, 9)
2. docs/execution/blocks/b2N-summary.md              (your spec — binding)
3. docs/execution/blocks/b20-summary.md              (security baseline)
Skills: agentpaas-internal-architecture (control flow),
agentpaas-startup-execution (conventions, pitfalls).
Rules:
- Implement tasks in the listed order; each task's "How an agent tests
  it" items become real tests BEFORE implementation where feasible.
- Reuse existing substrate (identity keystore, canonical JSON, gitleaks,
  audit writer, B20 verification). New crypto primitives are forbidden.
- Fail closed everywhere; typed errors with actionable messages.
- Do not touch integrations/hermes-plugin/ unless you are B25.
- Update the block summary's subtask table and bug log as you go.
Exit only when the block success gate passes end-to-end with real
command output pasted into the summary.
```

## Task-size discipline

Every T-task above is sized to one builder session (~PR-sized). If a
task exceeds a session, split at the test boundary (e.g. B22 T01 writer
vs reader/verify are a legitimate split) and record the split in the
block summary. Never merge partial tasks without their adversary tests.

## Cross-block invariants (adversary checklist, every block)

1. No private key material in any struct return, log, audit payload, CLI
   output, or test fixture.
2. No secret value in bundle, manifest, transcript, or install output
   (sentinel tests are cumulative — later blocks re-run earlier
   fixtures).
3. Every verification failure: typed error, exit non-zero, zero state
   mutation, audit record where a daemon is involved.
4. Every new file under ~/.agentpaas: explicit mode (0600/0700) with a
   permission test.
5. v1 locks and identity-less local workflows keep working untouched
   (Phase 1 golden dataset green at every block gate).
6. D3 language: scripted forbidden-phrase grep runs in every block's doc
   task ("verified safe", "safe to run", "trusted publisher means").

## Version and release mapping

| Version | Content | Gate |
|---------|---------|------|
| v0.1.2 | B20 | B20 gates 1-11 |
| v0.1.3 | B19 T2+T11 (P0 subset) | B19 gates, B18 retest |
| v0.2.0 | B21+B22+B23+B24+B25 | B25 S1-S10 + 7-claim red-team + brew |
| v0.2.x | B26 T01–T03 | `make block26-gate`; Codex + shared conformance + Approval Pack |
| enterprise demo candidate | B27 T01–T14 | `make block27-gate` + recorded real-sandbox Alice/Bob gate |

B19 P1/P2 (ingress auth, egress OAuth, guardrails, transformations,
observability, MCP tool ACLs) interleave after v0.2.0 by demand; none
block Phase 2.

## Risk register (plan-level)

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| B20 slips, Phase 2 idles | Med | B21/B22 specs are B20-independent enough to prototype on a branch; do not merge before B20 gates |
| name@pub8 threading breaks Phase 1 paths (B18-003 class) | High | B23 T05 has its own regression matrix; golden dataset at every gate |
| Cross-machine rebuild flakiness (deps, arch) | Med | uv.lock required-path + A10 warning telemetry in install manifest; S10 clean-machine card |
| Consent fatigue makes cards decorative | Med | Lints + diff-only re-consent (PRD 13 response); observe partners in B25 |
| Keychain UX friction (prompts during export/install) | Med | Explicit error paths + docs; founder manual pass in S4 |
| Scope creep toward registry before signal | High | B26 entry criteria are contractual; PRD 13 kill condition |

## Definition of done for Phase 2 planning (this document set)

- [x] PRD: planning/phase2-sharing-prd-v1.md
- [x] Block specs: blocks/b21..b27-summary.md
- [x] Execution plan: this file
- [x] Manual testing plan: blocks/b24.5-manual-testing-plan.md (52 test cards T1-T52, covering B18-B24)
- [x] docs/execution/README.md index updated
- [x] docs/roadmap.md execution sequence synchronized without shipped claims

### v0.2.0 Final Manual Testing Gate (Block 24.5)

After B21-B24 are code-complete and unit/integration tests pass, the
final release gate is the manual testing plan at
[blocks/b24.5-manual-testing-plan.md](../blocks/b24.5-manual-testing-plan.md).

This plan contains 52 test cards (T1-T52) covering:

- **T1-T10:** B18 regression — verify all Phase 1 capabilities still work
- **T11-T18:** B19 — gateway-native policy (budgets, rate limits, provider
  lock, credential injection, ingress auth, guardrails, observability, transformations)
- **T19-T22:** B20 — security claim closure (credential invisibility, audit
  integrity, immutable verification, stale artifact detection)
- **T23-T29:** B21 — publisher identity, trust store, provenance
- **T30-T34:** B22 — bundle export, inspect, integrity, secret-scan
- **T35-T41:** B23 — install, consent, credential mapping, run, update/downgrade
- **T42-T52:** B24 — fork, modify, redistribute, multi-hop E2E

Execute all 52 cards from a clean slate. Each card takes 2-5 minutes
manually. v0.2.0 ships only when all 52 cards pass.
