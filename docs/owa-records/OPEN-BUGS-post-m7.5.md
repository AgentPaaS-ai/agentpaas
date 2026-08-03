# Open bugs & residuals — post M7.5 (must fix before real trial / before M8 if P0)

**As of:** 2026-08-03 (pre-nuclear E2E bar — A1–A7 + M8 closed)
**OSS main:** `e0c770c` + A1/A3/A6 CLI commits (local; pushed on GO)
**Cloud main:** `26e91b8` (A1 undeploy, A2 slots, A3 instance, B1 run-delete, A5 secrets)
**Brew:** v0.3.6 (rebuilt same tag after bar code merged)

This is the single checklist of **what still needs a fix**. FIXED items are listed below for audit only.

---

## P0 — fix before stranger trial traffic

| ID | Bug | Status |
|----|-----|--------|
| **P0-1** | No undeploy / slots never freed | **FIXED** — `DELETE /v1/deployments/:id` + `releaseSlot` (cloud `7634d5c`), CLI `agentpaas cloud undeploy` (OSS `526a7d4`), orphan cleanup ops step (see BUG-undeploy-slot-release.md) |
| **P0-2** | Slot pool size 3 shared (not per-tenant) | **FIXED** — pool enlarged to 10 (`0016_slot_pool_10.sql`), per-tenant slot quota 5 with customer-visible 429 (`10b757b`), actionable `no_slot_capacity` 503 message |
| **P0-3** | Orphan bound slots | **FIXED** — undeploy releases slot; orphan reconciliation via undeploy/ops SQL (`UPDATE slots SET status='free' WHERE deployment_id NOT IN (SELECT id FROM deployments)`) |

---

## P1 — fix before polished trial / scale agents

| ID | Bug | Status |
|----|-----|--------|
| **P1-1** | BUG-INSTANCE-SIZING phase 2 | **FIXED** — per-deployment instance preset (default `basic` 1/4 vCPU 1GiB; allowlist lite/basic/standard-1..4; `dev` rejected with hint because CF treats `dev` as an alias for `lite` 256MiB). Bind + rebind emit the preset (cloud `a2d4d65`); CLI `--instance-type` (OSS `4ed5dda`). Live app verified 0.25 vCPU / 1GiB |
| **P1-1b** | ARM harness silently embedded in amd64 image | **FIXED** — release/cask now ship `agentpaas-harness-linux-amd64`; pack validates harness ELF arch vs target and hard-errors on mismatch (OSS `3b4c332`). Live: brew-daemon pack now embeds x86-64 harness; cloud invoke succeeded |
| **P1-2** | wrangler deploy wipes customer image | **FIXED** — `deploy-prod.sh` always rebinds after deploy; rebind no longer emits lite custom shape (cloud `4f4ac37`, `7868f9a`) |
| **P1-3** | BUG-CF-BIND ops gap | **FIXED** — `scripts/verify-worker-secrets.sh` mechanical checklist + deploy-prod assert + `docs/worker-recreate-checklist.md` (cloud `7868f9a`) |
| **P1-4** | cloud.agentpaas.ai DNS dead | **FIXED (interim)** — CLI default URL now points at live workers.dev `https://agentpaas-cloud-api.parvezsyed.workers.dev` (OSS `5e0a592`); DNS record still pending founder |

---

## P2 — polish (not trial blockers if P0/P1 done)

| ID | Item | Notes |
|----|------|--------|
| **P2-1** | Provision-token still admin curl | Intentional until self-serve; not a code bug if runbook is clear |
| **P2-2** | Deployment secret bind was curl | **FIXED** — `cloud secrets bind` |
| **P2-3** | UX-DLOG / DDOCKER / APIHOST / LOCKPATH / deploy latest | **FIXED** on OSS 0.3.6 line |
| **P2-4** | Hermes E2E golden not yet live-run end-to-end | Doc updated with undeploy step; profile-level run = A7 evidence (nuclear optional second pass) |
| **P2-5** | Install prompt too verbose | **FIXED 2026-08-03** — canonical prompt is now one sentence: `Install from https://github.com/AgentPaaS-ai/agentpaas`. Updated README.md:156, demo/README.md:39, docs/quickstart.md:331, docs/execution/golden-loop-test.md (×2), integrations/hermes-plugin/SKILL.md:32, docs/customer/golden-loop-hermes-e2e.md Phase 2 |
| **P2-6** | Install flow says "restart Hermes" | **FIXED 2026-08-03** — toolset registers live (verified Hermes v0.19.1); restart-required removed from README.md, demo/README.md, docs/quickstart.md, docs/internal/after-install.md STEP 4, integrations/hermes-plugin/SKILL.md, golden-loop docs. Restart kept only as a fallback if tools don't appear |

---

## M8 (rescoped) — run-record persistence CLI

| Item | Status |
|------|--------|
| Full run-record retained until explicit tenant deletion | **VERIFIED** — no TTL/auto-cleanup in cloud `src/`; runs persist |
| `cloud status` / runs list, `cloud logs <run>`, `cloud result <run>` on brew bar | **EXISTS** (OSS 0.3.6 line); proven in E2E |
| Delete-run clears records + artifact URLs die | **FIXED** — `DELETE /v1/runs/:id` removes run + events + artifacts, R2 objects deleted so signed URLs 404 (cloud `0877644`) |

---

## Already FIXED (do not re-open)

| Item | Where |
|------|--------|
| Deploy invoke stuck `starting` (deferStart) | Cloud main |
| Flaky container start / exit 1 retries | Cloud main `3f2503e+` |
| Default instance_type `dev` (not lite 256MiB) | Cloud main `bb46f82` |
| AMD64 harness in pack | OSS `35856a5` |
| CLI double error, Docker stub, API error body, lock path, deploy latest, secrets bind | OSS main / brew 0.3.6 |

---

## Git cleanliness (2026-08-03 pre-nuclear)

| Repo | main == origin/main | Notes |
|------|---------------------|--------|
| agentpaas (OSS) | local ahead (A1/A3/A6/docs) — pushed when bar green | ~60 untracked owa/history files — parked before goreleaser |
| agentpaas-cloud | local ahead (A1/A2/A3/B1/A5) — pushed when bar green | remote feature branches closed (PR 44/45) |
