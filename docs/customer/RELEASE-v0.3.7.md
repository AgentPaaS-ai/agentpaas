# AgentPaaS CLI + Cloud — Release v0.3.7 (M10.6)

**Shipped:** 2026-08-08  
**CLI tag:** `v0.3.7` (brew cask `AgentPaaS-ai/homebrew-tap/agentpaas`)  
**Cloud console:** https://cloud.agentpaas.ai  
**Marketing site docs:** https://agentpaas.ai/docs/

This release covers the OSS CLI/daemon **and** the cloud dashboard surfaces that go with it. The console is for **seeing** what happened. Almost all changes go through the **CLI**, **Hermes**, or the **HTTP API**.

## Install / upgrade CLI

```bash
brew update
brew upgrade --cask AgentPaaS-ai/homebrew-tap/agentpaas
# or first install:
# brew install --cask AgentPaaS-ai/homebrew-tap/agentpaas

xattr -cr "$(brew --prefix)/bin/agentpaas" \
  "$(brew --prefix)/bin/agentpaasd" \
  "$(brew --prefix)/bin/agentpaas-harness-linux"

agentpaas version
# expect: CLI: 0.3.7 | Commit: 509b4c8… (or later patch)

agentpaas daemon stop 2>/dev/null || true
agentpaas daemon start
agentpaas doctor
```

## What is new

### CLI

| Command | Purpose |
|---------|---------|
| `agentpaas cloud cron set <dep> --expr every_5m` | Create/change schedule (enables it) |
| `agentpaas cloud cron disable <dep>` | Stop firing; keeps stored expression |
| `agentpaas cloud cron enable <dep>` | Resume previous expression |
| `agentpaas cloud cron list` | List schedules for this tenant |
| `agentpaas cloud pull <agent>` | Download cloud agent metadata into a local dir (edit → pack → push loop) |
| `agentpaas cloud login` | Browser login (does **not** auto-open; you open the URL) |

Cron: named intervals (`every_1m`, `every_5m`, `every_15m`, `every_1h`) **or** standard 5-field UTC cron (e.g. `30 9 * * 1-5`).

Hermes plugin tools: `agentpaas_cloud_cron_set|disable|enable|list` (plus existing cloud tools). Cloud login from Hermes **coaches** you to run CLI yourself (non-blocking).

### Cloud API

| Method | Path | Notes |
|--------|------|-------|
| GET | `/v1/cron` | Tenant-scoped schedule list + next_fire estimate |
| PUT | `/v1/deployments/:id/cron` | `{expr, enabled}` or `{enabled}` only for toggle |
| GET | `/v1/usage` | Trial quota (no Cloudflare branding in customer copy) |
| GET | `/v1/deployments`, `/v1/runs`, `/v1/images` | Unchanged; dashboard uses these read-only |

### Dashboard (cloud.agentpaas.ai)

Tabs: **Overview · Agents · Deployments · Runs · Cron · Secrets · Tokens · Usage · Plan**

- **Coach boxes** on every operational tab (not Overview/Usage/Plan): CLI + Hermes prompts. Console is verify-first.
- **Agents / Deployments / Runs:** click a row → detail expands **under that row**; filter + page size + prev/next; count top-right.
- **Cron:** read-only list; mutate via CLI/Hermes only.
- **Provenance:** multi-version author chain `alice (0.1.0) → bob (0.2.0 current)`.
- **Tokens / Secrets:** still writable in UI; CLI preferred for day-to-day.

## Trial golden path (short)

1. Claim invite → set password → open dashboard.
2. Install brew CLI 0.3.7 + Hermes plugin.
3. `agentpaas secret add openrouter-key` then build weather agent (Hermes: "Make a weather agent").
4. `agentpaas cloud login` in **your** terminal → approve in the claim browser.
5. Push / deploy / bind secret / invoke.
6. Optional: `agentpaas cloud cron set <dep> --expr every_5m` then `disable` when done.
7. Confirm Runs + Cron + lineage in the dashboard.

Full spine: [docs/customer/trial/00-index.md](customer/trial/00-index.md)

## Hermes one-liners

```
Make it run in the AgentPaaS cloud
Schedule deployment dep_… to run every 5 minutes
Disable cron on dep_…
Invoke the weather agent in cloud for San Francisco and show the result
Push my openrouter-key to cloud and bind it to the weather deployment
```

## Honesty / limits

- Preview vault (not isolation-grade).
- OpenRouter is the proven cold LLM path; other providers listed but not stranger-proven yet.
- Cloud cron: named intervals or 5-field UTC cron (day/time schedules). Local `agentpaas cron` already supported 5-field + timezone.
- `cloud pull` is metadata + stub source MVP; full image extract may be incomplete without a local pack.
- Trial is invite-gated (claim links), not open self-serve signup.

## Version matrix (this cut)

| Surface | Version / marker |
|---------|------------------|
| CLI / daemon brew | **0.3.7** |
| OSS git tag | **v0.3.7** |
| Cloud worker | live at cloud.agentpaas.ai (M10.6 dashboard) |
| Site docs | /docs/ (this release hub + trial) |

See also: [docs/VERSIONING.md](VERSIONING.md) for how we version docs, site, cloud, and CLI going forward.
