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
| **P2-9** | "Apple could not verify agentpaas is free of malware" on first run | **OPEN — 2026-08-03 founder E2E** — binaries not Apple-notarized; Gatekeeper flags them. Workaround `xattr -cr` (runbook) or right-click→Open. Real fix: Apple Developer ID notarization (~$99/yr) before stranger trial; else ship a one-line "macOS may warn — click Open" note in install docs |
| **P2-10** | Secret onboarding fires BEFORE agent build — asks for openrouter-key when nothing needs it | **OPEN — 2026-08-03 founder E2E** — golden-path order is build → push → secret → deploy → bind; the install/setup skill prompts `secret add openrouter-key` too early. Fix: setup skill must not ask for LLM keys; the build skill asks only when packing/llm config is reached (fix ordering in integrations/hermes-plugin skills) |
| **P2-11** | Agent auto-opens egress (wttr.in) without waiting for user approval | **OPEN — 2026-08-03 founder E2E** — first-time flow granted wttr.in egress on the agent's own decision instead of presenting policy.yaml for explicit user OK. This is a consent breach of the default-deny model. Fix: pack/build skill must surface the egress allowlist and wait for explicit user approval before proceeding; never auto-approve policy changes |
| **P2-12** | Open-ended "build me an agent" prompt → session scaffolds a whole new agent project without asking | **OPEN — 2026-08-03 founder E2E** — on "build me a weather agent, agent gets weather details then uses an LLM…", ap-testing created ~/projects/weather-agent/ (main.py, agent.yaml, policy.yaml, requirements.txt) at 15:08 uninvited, then ran daemon writes — instead of using the existing demo/weather-agent or asking which to use. Root cause: build skill's "ask first" onboarding ignored; session proceeded after one clarify and under repeated 503 retries. Fix: agentpaas-build skill must REQUIRE project selection/confirmation before any write_file/terminal scaffold; never create a project dir the user didn't name |
| **P2-13** | "make it run in the agentpaas cloud" → push+deploy auto-executed with no confirmation gate | **OPEN — 2026-08-03 founder E2E** — session pushed image + started deploy immediately on an open-ended run request, no [y/N] on what/where/cost. A paid-cloud action needs explicit confirmation (image, deployment name, instance preset) before pushing. Fix: cloud-push/deploy skill step must present the plan and wait for user OK; never auto-push to billed infra |
| **P2-14** | Docs send first-time users to `cloud login` with no claim-link guidance — "Open your claim link first" error | **FIXED 2026-08-03** — golden-path.md restructured claim-link-primary + token fallback; quickstart gained cloud section with claim-link guidance; golden-loop/run-sheet/verifier docs aligned (OSS `f9d73d4`) |
| **P2-15** | Deploy path forgets secret push + bind → invoke "succeeds" with null final_output, session loops | **OPEN — 2026-08-03 founder E2E (option A)** — claim-link login OK, push OK, deploy OK, but session never ran `cloud secrets push` + `cloud secrets bind <dep> <secret> --as bearer --host openrouter.ai`; deployment had "No bindings", so every invoke returned succeeded-with-null-final_output and the session retried in circles. Fix: cloud deploy skill must run push+deploy+secret-push+bind as a required sequence before invoke, and detect "no bindings" to surface a clear error instead of looping |

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
