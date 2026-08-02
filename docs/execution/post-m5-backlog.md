# Post-M5 backlog (working plan)

**As of:** 2026-07-30  
**Status:** M1–M5 CLOSED (live CF secrets+LLM proven). Live Worker tear down after smoke.  
**Founder:** M6 Stripe deferred (incorporate + Stripe approval). No website frontend build until planned. A+B: hold heavy commercial eng; light polish OK.

## Done
- M0–M5 cloud path: control plane, pack/push amd64, RunContainer+egress, DoVault secrets, materialize credentials, invoke llm merge, CF CA, sleepAfter=30s
- Live E2E: weather + openrouter-key → real answers (Folsom/Palo Alto); tear down after use
- Arch: CF = no agentgateway sidecar (N agents = N harness containers); egress = CF outbound intercept; secrets = per-tenant DO; tokens metered in harness (M6-lite rollup later)

## Explicitly deferred
- M6 Stripe products/checkout/webhooks/credits
- W1 marketing site + dashboard UI (plan first, then build)
- M10 skinny dashboard / Stripe onboarding gate
- M11–M12 K8s / V2 B35 until chosen

## Do next (no Stripe, no website UI) — preferred order

### Tier 1
1. **M6-lite** — concurrency/quota admission + CPU/usage metering + `cloud usage` API/CLI (no money)
2. **M7** — triggers: cron, HTTP invoke+token, webhook-in + audit
3. **M8** — artifacts (R2), completion webhooks (HMAC+SSRF), delivery webhook, runs/logs/result CLI
4. **M9 APIs** — audit export + metrics APIs for coding-tool ops (no dashboard page)
5. **B polish** — cloud.agentpaas.ai DNS when ready; wrangler deploy must not wipe slot image; push→invoke stopwatch; founder demo runbook
6. **V1 skins** — Cursor/Codex/Claude/Grok templates + golden path docs (parallel OK)

### Tier 2
7. **M8.5** — platform registry + BYO MCP hosting on CF
8. **M11→M12** — K8s sidecar then shared-gateway spike (override M10 gate only if multi-agent is the bet)

### Tier 3 (later)
- Stripe/M6 full, W1 site, M10 dashboard, V2 B35 after M12

### CLI UX polish (light — founder T11 2026-08-01)
Not blockers for M7.5 close; do not interleave into T11/T12. Pick up in a small polish pass (post-T11 or with M8 CLI work).

| ID | Issue | Evidence / notes |
|----|--------|------------------|
| **UX-DLOG** | CLI errors print **twice** (`Error: …` then `error: …`) | `identity init` when identity exists; `daemon start` when already running. One failure, two print paths (cobra/main wrapper). Fix: single user-facing error line. |
| **UX-DDOCKER** | `agentpaas daemon status` / version line shows `Docker: unknown \| Docker API: unknown` even when Docker is healthy | `internal/daemon/version.go` documents DockerContext/DockerAPIVersion as **stubs**; CLI maps empty → `"unknown"`. `doctor` already probes Docker for real. Fix: fill from live probe or omit until implemented. |
| **UX-APIHOST** | Default Cloud API host `https://cloud.agentpaas.ai` does not resolve; T11 live is workers.dev only | Without `AGENTPAAS_CLOUD_API_URL`, `whoami`/`secrets list`/`push` fail: `lookup cloud.agentpaas.ai: no such host` (double-printed). Fix: DNS + always-on hostname, or document/require env until DNS exists; consider better error (“set AGENTPAAS_CLOUD_API_URL”). |
| **UX-SECRETS-OUT** | `cloud secrets push/list` success output too quiet | push should always show `pushed: <name>` (code has it); list prints bare names only — easy to miss. Prefer header + count. |

## Paths
- OSS: `~/projects/agentpaas`
- Cloud: `~/projects/agentpaas-local/repos/agentpaas-cloud`
- Plan: `docs/execution/planning/managed-cloud-m-series-plan.md`
- Decisions: `docs/execution/m1/founder-decisions.md`
- OWA: skill `agentpaas-owa-build-orchestration` + `agentpaas-cloud-m-series`
