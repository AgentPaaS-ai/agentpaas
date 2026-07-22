# Changelog

All notable changes to AgentPaaS are documented in this file.
Format follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased] — v0.3.0-dev

### Changed
- **Bumped dev version to 0.3.0-dev**: default `CLIVersion`/`DaemonVersion`
  constants, Makefile ldflags, Formula template, docs, and tests all
  reference 0.3.0. Old 0.1.x/0.2.x version strings removed or updated.
- **Homebrew tap path corrected**: docs now point to
  `AgentPaaS-ai/homebrew-tap` (not `agentpaas-ai/tap`).
- **Version hygiene script**: `scripts/check-release-versions.sh` gate
  fails CI if any binary embeds a stale 0.1.x or 0.2.x version string.

### Added
- **B32 delegation adversary break tests**: 14-pass gate proving
  delegation RPC correctness, freshness, replay rejection, scope
  isolation, and multi-tenant safety.

## [0.2.3] — 2026-07-13

### Added
- **Consolidated defect log** (`docs/defects.md`): single source of truth
  for all tracked bugs with root cause, reproduce steps, fix, and
  post-fix verification. Supersedes scattered bug logs.

### Fixed
- Bug 032: Export source_digest mismatch caused by `.agentpaas.tmp` file
  contaminating the source digest. Fixed: `.agentpaas.tmp` and
  `audit-export.json` added to default ignore patterns.
- Bug 033: Gateway no longer follows HTTP redirects. CheckRedirect on
  both http.Client instances returns ErrUseLastResponse. Redirect
  responses returned to agent with redirect_url field. Audited as
  egress_denied.
- Bug 034: TLS handshake error on redirect URLs resolved by Bug 033 fix
  (redirects not followed, so no direct TLS connection to redirect target).
- Bug 031: SKILL.md now explicitly requires egress hostname confirmation
  for agent modification, not just creation.
- Bug 018: SKILL.md now explicitly instructs agent to use `domain` field
  name (not `host` or `hostname`) in policy.yaml.
- Bug 027: Install error message for missing uv.lock now clearly explains
  the issue and provides the exact command to fix it.
- Bug 028: Added `--limit` as alias for `--page-size` on `audit query`.
- Bug 035: Audit checkpoint verification now passes during daemon operation
  AND after shutdown. Lowered DefaultCheckpointCadence from 25 to 1 so every
  audit record gets an immediate checkpoint. AuditWriter.Close() final
  checkpoint remains as a safety net for crash scenarios.

## [0.2.2] — 2026-07-12

### Added
- **After-install Step 0 prerequisite check**: `hermes plugins install` now
  detects missing prerequisites (colima, docker, agentpaas binaries) and
  installs them via brew before proceeding. Includes anti-clone guard.

### Fixed
- Bug 030: Wall clock budget increased from 30s to 120s. The first
  `agent.llm()` call to an LLM provider can take 10-30s (cold connection,
  model loading). The 30s default caused `wall_clock_budget_exceeded` kills
  before the LLM responded.

## [0.2.1] — 2026-07-12

### Added
- **Post-build pack verification** (S30-002 through S30-010): Pack now runs
  a 4-step verification checklist after `docker build` succeeds — SDK presence,
  image content audit (harness + entry + SDK in image), harness freshness
  (MD5 match), and smoke test (healthz + readyz). Pack fails closed if any
  check fails, preventing silent pack-then-runtime-failure cycles.
- **Doctor skopeo check** (info-level): Doctor now reports whether skopeo is
  available. Skopeo is an optional dependency for prebuilt image
  install/export. Default install path (rebuild from source) works without it.
- **Golden dataset tasks for sharing**: G46-G48 cover bundle export, TOFU
  install, and fork-chain provenance for pass^k regression testing.

### Fixed
- S30-002: Pack no longer succeeds silently when SDK is missing.
- S30-003/004: Post-build verification confirms harness and SDK are in image.
- S30-007: Smoke test catches broken harness, missing SDK, and import errors
  before pack is declared done.
- S30-008: Harness freshness check prevents stale harness embedded in images.
- S30-010: Pack is not declared successful until the image actually runs.

### Changed
- Doctor check count increased from 11 to 12 (added skopeo check).

## [0.2.0] — 2026-07-11

### Added
- **Phase 2 sharing**: Bundle export/import, TOFU trust, provenance chains,
  fork support, identity onboarding, credential mapping.
- **Hermes plugin sharing tools**: 8 new tools for identity, export, inspect,
  install, list, provenance, trust, fork.
- **Docker Engine readiness gate**: Doctor checks Docker server version and
  rejects known-vulnerable versions.
- **Moby SDK migration**: Replaced deprecated `github.com/docker/docker` with
  `github.com/moby/moby` via go.mod replace directive.
- **Go 1.26.5**: Upgraded toolchain to fix reachable `govulncheck` findings.
- **Seven-claim red-team gate**: Integrity, provenance, policy transparency,
  no-secret-export, no-credential-sharing, lineage integrity, human consent.

### Fixed
- Bug 021 regression: HTTP_PROXY re-added alongside AGENTPAAS_GATEWAY_URL.
- Bug 024: SDK `agent.llm()` returns both `text` and `content` keys.
- Bug 026: Trigger invoke timeout increased to 90s for installed agents.
- Bug 029: Doctor now respects AGENTPAAS_HOME env var.

## [0.1.1] — 2026-07-06

### Added
- Credential brokering: raw secrets never cross Docker exec boundary.
- Gateway-native HTTP routing for rate limiting and guardrails.
- LLM token budget enforcement at runtime.
- Cost tracking and observability in harness audit.

### Fixed
- Bug 019: Daemon wires policy budget to harness BudgetEnforcer.
- Bug 021: Gateway-native HTTP routing replaces CONNECT tunneling.

## [0.1.0] — 2026-07-02

Initial release.
