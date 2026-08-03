# Open bugs & residuals — post M7.5 (must fix before real trial / before M8 if P0)

**As of:** 2026-08-03  
**OSS main:** `f9b2cb7` (synced GitHub)  
**Cloud main:** `5d0dc1b` (merged container reliability; synced GitHub)  
**Brew:** v0.3.6 @ `9ce0bcb` (docs-only commits after tag OK)

This is the single checklist of **what still needs a fix**. FIXED items are listed at the bottom for audit only.

---

## P0 — fix before stranger trial traffic

| ID | Bug | What breaks | Fix | Doc |
|----|-----|-------------|-----|-----|
| **P0-1** | **No undeploy / slots never freed** | Deploy is one-way; shared pool fills → forever `no_slot_capacity`. Trial cannot match “10 agents / invoke anytime.” | Cloud `DELETE /v1/deployments/:id` + `releaseSlot`; CLI `cloud undeploy`; orphan slot cleanup | `BUG-undeploy-slot-release.md` |
| **P0-2** | **Slot pool size 3 shared** (not per-tenant) | Whole Worker only 3 bound deployments; multi-tenant trial collides | Per-tenant slot quota (trial≈5) and/or larger pool + undeploy; align with concurrency story | same + product decision |
| **P0-3** | **Orphan bound slots** | Slot stays `bound` when dep missing from customer list | Undeploy path + reconciliation job/ops script | same |

---

## P1 — fix before polished trial / scale agents

| ID | Bug | What breaks | Fix | Doc |
|----|-----|-------------|-----|-----|
| **P1-1** | **BUG-INSTANCE-SIZING phase 2** | Heavy agents may still OOM/`exit 1` if lite sneaks back or default `dev` too small | Pack-time peak RSS → lock `resources` → CF preset map at bind | cloud `ADV-GAPS` / residuals R2 |
| **P1-2** | **wrangler deploy wipes customer image** | Every API redeploy resets container app to platform Dockerfile | Automate rebind in `deploy-prod` / post-wrangler checklist always | residuals R3 |
| **P1-3** | **BUG-CF-BIND ops gap** | Fresh Worker missing CF_* secrets → deploy 503 | Ops checklist enforced; optional `cloud doctor` remote config probe | `BUG-cf-bind-not-configured-t11.md` (CLI body FIXED; ops PARTIAL) |
| **P1-4** | **cloud.agentpaas.ai DNS dead** | Default host fails; stranger needs env every shell | CF zone DNS → `cloud.agentpaas.ai` or change default URL | residuals R4 |

---

## P2 — polish (not trial blockers if P0/P1 done)

| ID | Item | Notes |
|----|------|--------|
| **P2-1** | Provision-token still admin curl | Intentional until self-serve; not a code bug if runbook is clear |
| **P2-2** | Deployment secret bind was curl | **FIXED** — `cloud secrets bind` |
| **P2-3** | UX-DLOG / DDOCKER / APIHOST / LOCKPATH / deploy latest | **FIXED** on OSS 0.3.6 line |
| **P2-4** | Hermes E2E golden not yet live-run end-to-end | Doc exists (`docs/customer/golden-loop-hermes-e2e.md`); full profile run still open |

---

## Not M8+ (do not wait for M8)

M8 = run-record retention / CLI polish only.  
M8.5 = registry + MCP.  
M9 = audit/metrics APIs.

**None of P0-1…P0-3 or P1-1…P1-4 are scheduled in M8+.** They are pre-M8.

---

## Already FIXED (do not re-open)

| Item | Where |
|------|--------|
| Deploy invoke stuck `starting` (deferStart) | Cloud main |
| Flaky container start / exit 1 retries | Cloud main `3f2503e+` |
| Default instance_type `dev` (not lite 256MiB) | Cloud main `bb46f82` |
| AMD64 harness in pack | OSS `35856a5` |
| CLI double error, Docker stub, API error body, lock path, deploy latest, secrets bind | OSS main / brew 0.3.6 |
| BUG-036…043 local product bugs | FIXED historically |

---

## Git cleanliness (2026-08-03)

| Repo | main == origin/main | Notes |
|------|---------------------|--------|
| agentpaas (OSS) | YES `f9b2cb7` | ~59 untracked local owa/history files — not on GH; safe to ignore or park |
| agentpaas-cloud | YES `5d0dc1b` | `fix/retry-flaky-container-start` merged FF into main |

Optional desk: delete merged cloud feature branch locally; park OSS `??` under `/tmp` if goreleaser needed again.
