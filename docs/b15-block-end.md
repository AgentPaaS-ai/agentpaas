# B15 Block End — Complete

**Date:** 2026-07-02
**Block:** B15 — P1 Completion Items (Pre-Release Gap Closure)
**Status:** MERGED TO MAIN. Block 15 complete.

## What shipped

Block 15 closed all P1 pre-release gaps. Merged to main at commit `4609452`,
pushed to `origin/main`.

### Subtasks completed

| Task | Description | Status |
|------|-------------|--------|
| T01 | Credential onboarding (secret add/list/remove/rotate/test) | DONE |
| T02 | LLM provider integration via unified gateway egress | DONE |
| T03 | Policy authoring (policy init + pack-time validation) | DONE |
| T04 | Trigger/cron/event surface (29 plugin tools) | DONE |
| T05 | Production hardening (5 micro-chunks) | DONE |
| T06 | goreleaser v2.16 config migrated | DONE |
| T07 | Clean-machine prerequisites docs | DONE |
| T08 | Egress enforcement regression gate | DONE |

### T05 Production Hardening detail

- **MC1:** RFC1918 tightened to gateway /16, fail-closed (no RFC1918 fallback)
- **MC2:** Rekor retry fallback (3 attempts, tightened error patterns)
- **MC3:** Checkpoint key encrypted at rest (AES-256-GCM, legacy migration on load)
- **MC4:** Init container Option B decision (capset drop, full pattern is P2)
- **MC5:** CAP_NET_ADMIN capset verification (Docker e2e, fail-closed)

## OWA process

- **Workers:** grok-composer-2.5-fast (Grok CLI), deepseek-v4-pro (fallback)
- **Adversary:** grok-4.3 via agentpaas-adversary profile
- **Verifier:** GLM-5.2 via agentpaas-verifier profile (VERIFY PASS x2)
- **Orchestrator:** z-ai/glm-5.2 via z.ai direct API

### Adversary findings (all resolved)

1. HIGH: RFC1918 fallback when gateway subnet unknown → FIXED (fail-closed)
2. HIGH: 500-substring false retry classification → FIXED (tightened patterns)
3. MEDIUM: Legacy plaintext key persisted on disk → FIXED (migrate on load)
4. MEDIUM: Fail-open on capset syscall error → FIXED (exit(1) when firewall on)
5. MEDIUM: Silent iptables failures → Accepted P1 trade-off (documented)
6. MEDIUM: /16 lateral egress → P2 improvement (documented)

## Verification (on merged main)

- `make build`: PASS
- `make lint`: 0 issues
- `make block15-gate`: PASS (T01+T02+T03+T04+T05+T08 + 208 plugin tests)
- `goreleaser check`: PASS (0 deprecation warnings)
- `goreleaser release --snapshot`: PASS (full build pipeline)
- MC5 Docker integration test: PASS (AGENTPAAS_DOCKER_TESTS=1)

## Next steps

1. **Tag v0.1.0** — `git tag v0.1.0 && git push origin v0.1.0`
   Triggers release.yml: goreleaser builds darwin/amd64+arm64 binaries,
   cosign keyless signing, syft SBOMs, Homebrew tap publish.
2. **Block 16** — Manual 2-user <15 min test on fresh macOS.
   Only starts after block15-gate passes on main (confirmed).
