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

| **P0-4** | **cloud push requires a customer Cloudflare API token** | **FIXED + LIVE-PROVEN 2026-08-03** — server-side admission; CLI streams docker save with TENANT token; Worker stages R2 + pushes to CF registry with platform credential (cloud `b90d77d` mig 0018, OSS `f4c9135`). Re-proven end-to-end with zero CF token in CLI env. |
| **P0-5** | **New admission path rejects every real push: "image digest does not match an archive platform config"** | **FIXED + LIVE-PROVEN 2026-08-03** — three-layer fix: deep-sort lock canonicalization + setIfNotEmpty agent_yaml (P0-5a), config-vs-manifest digest domain pin, scoped registry creds minted via containers API instead of raw Bearer (P0-5b, cloud `35c6b04`). Live: push admitted img_d6671d6b → deploy → invoke real LLM weather → result → undeploy, all tenant-token-only. |

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
| **P2-7** | cloud push leaks internals ("wraps wrangler", "set CLOUDFLARE_API_TOKEN") | **FIXED** — help/errors reworded to user-facing abstraction (OSS `f4c9135` line) |
| **P2-8** | `cloud invoke` printed empty Run ID, dropped final_output | **FIXED + LIVE-PROVEN 2026-08-03** — deploy-invoke wire shape is `{run_id,status,final_output,...}` not a run record; CLI now decodes typed response (OSS `df5b09c`) |
| **P2-9** | "Apple could not verify agentpaas is free of malware" on first run | **FIXED 2026-08-03** — setup skill now hard-gates `xattr -cr` on all four binaries with quarantine verification + Gatekeeper troubleshooting (OSS `2618ed3`); the E2E failure was the agent skipping the step, not notarization |
| **P2-10** | Secret onboarding fires BEFORE agent build — asks for openrouter-key when nothing needs it | **FIXED 2026-08-03** — setup no longer asks for LLM secrets; key ask moved to build/llm-config time (OSS `1f6dc07`, `9278880`) |
| **P2-11** | Agent auto-opens egress (wttr.in) without waiting for user approval | **FIXED 2026-08-03** — egress approval mandatory per host: show hosts, ask `Approve egress to <hosts>? [y/N]`, STOP without approval (OSS `9278880`) |
| **P2-12** | Open-ended "build me an agent" prompt → session scaffolds a whole new agent project without asking | **FIXED 2026-08-03** — build skill requires demo-vs-new-project choice before ANY file/dir creation (OSS `1f6dc07`) |
| **P2-13** | "make it run in the agentpaas cloud" → push+deploy auto-executed with no confirmation gate | **FIXED 2026-08-03** — cloud push and deploy each require explicit user confirmation (OSS `1f6dc07`) |
| **P2-14** | Docs send first-time users to `cloud login` with no claim-link guidance — "Open your claim link first" error | **FIXED 2026-08-03** — golden-path.md restructured claim-link-primary + token fallback; quickstart gained cloud section with claim-link guidance; golden-loop/run-sheet/verifier docs aligned (OSS `f9d73d4`) |
| **P2-15** | Deploy path forgets secret push + bind → invoke “succeeds” with null final_output, session loops | **FIXED 2026-08-03** — deploy skill encodes push→deploy→secret push→bind→verify-bindings→invoke as required sequence; `No bindings` → STOP (OSS `1f6dc07`) |
| **P2-16** | Onboarding asks “which LLM provider? (openrouter / openai / anthropic / xai / nous)” but **stranger-bar only proven for OpenRouter** | **OPEN 2026-08-05** — founder cold gate. Until fixed, treat OpenRouter as the only supported answer in cold E2E. See `BUG-P2-16-multi-llm-provider-parity.md`. Work: first-class openai/anthropic/xai/nous (auth headers, endpoints, model catalogs, cloud bind hosts, skill copy); borrow correct patterns from Hermes OSS (`NousResearch/hermes-agent` provider stack). Note: `internal/llm/*` adapters exist but are not stranger-complete (e.g. xAI `Name()` returns `"xiai"`; xAI/Nous OAuth TTL caveats). |
| **P2-16a** | Prove **OpenAI + Anthropic API-key** cold+cloud E2E (secret/bind/invoke/onboarding); parent P2-16 | **OPEN 2026-08-07** — founder gate ask. Until green, cold coach OpenRouter only. |
| **P2-16b** | OpenRouter model picker **hardcodes failing IDs** (deepseek-chat-v3-0324 / r1-0528:free) | **OPEN 2026-08-07** — founder gate. Need live known-good cheap models, not stale SKILL strings. |
| **P2-17** | Single custom harness only — investigate **5 popular harness/runtime choices** incl. **Hermes-as-harness** | **OPEN 2026-08-05** — founder note. Today agents are plain Python + `agentpaas_sdk` + Go harness only (not LangChain/etc.). Product direction: research and eventually offer ~5 popular agent runtimes/harnesses as selectable options, **including Hermes itself as a harness**. See `BUG-P2-17-multi-harness-choices.md`. Investigate-only until scoped; do not block cold gate. |
| **P2-18** | Cloud first-run: skill says bare `agentpaas cloud login` (browser) without claim-link / Start free trial | **OPEN 2026-08-05** — founder cold gate. ap-testing told user to run `cloud login` only; browser then shows “Open your claim link first”. Correct flow: if not logged in, **get a claim link first** (product: Agentpaas.ai **Start free trial**), open claim URL, then `agentpaas cloud login` / whoami. See `BUG-P2-18-cloud-login-needs-claim-first.md`. |
| **P2-18b** | Hermes `agentpaas_cloud_login` **blocks** capturing stderr; approve URL not shown until timeout; browser never auto-opens (by design) → stuck UX | **OPEN 2026-08-07** — founder gate. Return URL immediately; coach open-in-claim-browser; keep `--open-browser` default false. |
| **P2-19** | Cloud browser auth pages (claim + Approve CLI Login) blank white | **FIXED LIVE 2026-08-05** — dashboard-styled `authPage`; claim/approve live with `/styles.css`. |
| **P2-20** | Dashboard refresh claim flash | **FIXED LIVE 2026-08-05** — loading = “Loading dashboard”. |
| **P2-21** | Trial browser login/logoff | **FIXED LIVE 2026-08-05** — `/login` email+password, set password after claim, logout; Google optional (needs GOOGLE_CLIENT_*). Cloud `2b9b643`+ mig 0023 deployed. |
| **P2-22** | Trial CPU metering | **PROVEN 2026-08-05** — golden CPU delta (live). |
| **P2-23** | Trial >30d expiry block | **PROVEN edge 2026-08-05** — Case 11 PASS. |
| **P2-28** | CPU exhausted same message | **FIXED LIVE 2026-08-05** — founder copy; Case 10 PASS; CLI usage banner. |
| **P2-29** | Trial >10 agents | **PROVEN edge 2026-08-05** — Case 12 PASS. |
| **P2-24** | Cron golden | **PROVEN edge 2026-08-05** — Case 13 cron tick → run PASS. |
| **P2-25** | Dashboard invoke endpoint | **FIXED LIVE 2026-08-05** — full invoke URL + copy on Inspect. |
| **P2-26** | Inspect lineage/fingerprint | **PARTIAL LIVE** — fingerprint/platform/version/registry; full provenance chain still thin. |
| **P2-27** | Dashboard full logs | **FIXED LIVE 2026-08-05** — Logs tab: runs + events + audit. |

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
