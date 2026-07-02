# B15 Session Checkpoint — 05

**Date:** 2026-07-02
**Branch:** feat/b15-t05-mc5-capset-verify
**Goal:** Complete B15-T05 (Production Hardening) + close remaining B15 gaps

## Completed This Session

### T05 Production Hardening (code from prior sessions, verified + closed this session)

- **MC1** (3b5c5ea): Tightened RFC1918 iptables allow from broad 172.16/12,
  10/8, 192.168/16 to a single /16 derived from the gateway IP via
  `gatewaySubnetFromIP()`. Passed as `AGENTPAAS_GATEWAY_SUBNET` env var.
  Falls back to broad RFC1918 for backward compat with older daemons.
- **MC2** (2084a59): Rekor retry fallback for production image signing.
  `SignImage` retries up to 3 times with exponential backoff (2s, 4s) on
  transient errors (Rekor outage, network timeouts, 5xx). Local refs
  unaffected (tlog suppressed). Pattern matching in
  `isRetryableSignError` classifies rekor/tlog/5xx/timeout as retryable.
- **MC3** (dfea120): Checkpoint signing key encrypted at rest with
  AES-256-GCM. Passphrase derived via PBKDF2-HMAC-SHA256 (100K iterations).
  Source: env var → macOS Keychain (via `security` CLI) → passphrase file
  (0600). Legacy unencrypted DER keys read transparently (migration on
  next regen). Reuses proven crypto from `internal/identity/filestore.go`.
- **MC4** (cfa7785): Architecture decision — Option B chosen. Keep PID 1
  capset-drop approach. Full init container pattern (Option A) deferred to
  P2. See `docs/b15-t05-decisions.md`.
- **MC5** (ca29c68 + 37288e0): CAP_NET_ADMIN capset verification.
  - Docker integration test (`TestE2E_CapNetAdminDropped_AgentCannotFlushIPTables`)
    proving UID 64000 cannot run `iptables -F` after `DropNetAdminCapability()`.
  - Unit test for capset bit clearing (`TestDropNetAdminCapability_ClearsBit12`)
    on linux builds.
  - Stub test for non-Linux platforms.
  - **Bug found and fixed**: original test incorrectly asserted UID 64000
    could read `iptables -L OUTPUT` (also requires CAP_NET_ADMIN). Fixed by
    removing the agent-side read assertion; firewall state verified from
    root context only. Committed as 37288e0.
- **MC6** (5c248d2): block15-gate Makefile target updated with T05 section.
  Gate now runs T01+T02+T03+T04+T05.

### T05 Adversary Review
- Dispatched grok-4.3 via agentpaas-adversary profile on all T05 changes.
- Findings documented in risk analysis (see below).

## Verification

- `make build`: PASS (go build ./...)
- `make lint`: 0 issues (golangci-lint)
- `make block15-gate`: PASS (T01+T02+T03+T04+T05)
- MC5 Docker integration test: PASS
  (AGENTPAAS_DOCKER_TESTS=1, Colima, 15s runtime)
  - Confirmed: `iptables -F as UID 64000 → exit 4, Permission denied`
  - Confirmed: `iptables -L OUTPUT as root → DROP policy persists`
- Plugin tests: 208 pass

## Remaining Work (T06-T08)

### T06: Release Binary (macOS)
- goreleaser config has deprecated `archives.format` (→ `formats` since v2.6)
  and `brews` (soft-deprecated since v2.10). Needs migration to v2.16 syntax.
- release.yml, release-verify.yml, .goreleaser.yaml, Formula/agentpaas.rb exist.
- After config fix: `goreleaser release --snapshot` to verify local build,
  then tag v0.1.0 to trigger the release pipeline.

### T07: Clean-Machine Prerequisites
- README quickstart, docs/quickstart.md, agent doctor checks all exist.
- docs/known-limitations.md has STALE content ("No real LLM integration" —
  T02 fixed this). Needs update.
- Verification path: fresh macOS → brew install → agent doctor → agent
  init/pack/run in <15 min.

### T08: Egress Enforcement Regression Gate
- HTTP/HTTPS via gateway proxy, iptables egress firewall, IPv6 block all
  built and tested (B14B/B14E). This task is a regression gate confirmation.
- redteam-smoke target exists and runs 6 fixtures through the real pipeline.
- Wire a T08 assertion into block15-gate (or confirm redteam-smoke covers it).

## Key Facts

- Gateway subnet derivation: `gatewaySubnetFromIP(ip)` → /16 CIDR from gateway IP octets. Fail-closed if unset (no RFC1918 fallback).
- Rekor retry: 3 attempts, 2s/4s backoff, production refs only
- Checkpoint key: AES-256-GCM, PBKDF2-HMAC-SHA256 100K iterations, passphrase from Keychain
- Capset drop: clears CAP_NET_ADMIN from effective+permitted+inheritable via `unix.Capset`
- Plugin tool count: 29 (unchanged — T05 is internal hardening, no plugin tools)
- Docker test guard: `AGENTPAAS_DOCKER_TESTS=1`
- Docker socket: `unix://$HOME/.colima/default/docker.sock` (Colima)
