# B32 Post-Build Risk Analysis

## What shipped
- `internal/delegation` package: tasks, messages, two-sided snapshot authz, gateway tokens, artifact broker, wait/wake
- Harness RPC + Python `Agent.delegate`
- `pack` workflow `delegations:` section
- `make block32-gate`
- Packaging: default **0.3.0-dev**, Makefile ldflags, Formula template 0.3.0, `scripts/check-release-versions.sh`

## Residual risks
1. **Memory-only task store** — not durable across daemon restart until daemon-wired store (B33+). Wait/wake survives process only within one harness lifecycle.
2. **East-west multi-container proof** — unit/simulated gateway pair; live two-container Docker path not in default gate.
3. **Homebrew 0.2.3 on PATH** — until v0.3.0 cask published, operators must prefer repo `bin/` or PATH order; check script catches stale embeds in *built* bins.
4. **Outbox atomicity** — ordered CAS+event (N3); same class as B29.
5. **block28-long** — still separate pre-release Docker long-run gate.

## Release readiness (v0.3.0)
- Code gates: local `block32-gate` PASS; verifier PASS; arch W1–W5 fixed
- **Blocked on:** founder manual testing + explicit approval before `git tag v0.3.0` / Homebrew publish
- Do not retag on failure; use v0.3.1 for compatible fixes

## Manual testing deferred from earlier blocks
Now due: full first-time-user path before release (standing 2026-07-20 decision).
