# M7.5 — Customer Readiness Regression

**Status:** EXECUTION-READY SPEC
**Date:** 2026-07-31
**Depends on:** M7 CLOSED eng (07fd802 / cloud 57a394e)
**Founder decisions:** D112 (deferred items must become D-entries),
D113 (customer-ready gate non-deferrable). Vault posture per Q2
2026-07-31 (label "preview", harden at M11 K8s seam).
**Testing tier:** adversary session REQUIRED (T01–T05 carry M8 SC1–SC3;
this block ships the customer-facing output loop). Not a light block.

---

## Why this block exists

M1–M7 closed the control plane, triggers, and quota skeleton against a
"founder can demo with a runbook" bar. That bar was wrong. The result:
M7 closed with its manual gate deferred (live CF smoke skipped), its
plan work item 5 (D105 rate-limit matrix) dropped silently, and the
commercial gate ("M7+ requires 5 ACTIVE trial tenants") ignored. The
product can run agents but a customer cannot complete the loop.

M7.5 is a regression, not new scope. It pulls the M8 output loop
forward (artifacts, completion webhook, delivery channel) and closes
the onboarding, reliability, and honesty gaps that make the difference
between "eng green" and "a stranger finishes without the founder."

**Kill condition (honest):** if the T11 cold-onboard external tester
cannot finish in 30 minutes after fixes, do not run POC outreach. Every
POC dollar on a broken funnel is wasted (plan kill conditions).

---

## Non-goals (deferred, each recorded not dropped)

- Dashboard UI (tokens, secrets, usage pages) — M10.
- Stripe self-serve billing — M6.5, trigger per D103.
- Team / multi-user tenants / RBAC — pre-5-paying-POC (findings §12).
- Full cron expression parser — named intervals only; parser is post-POC.
- Multi-region / tenant data export — D79 deferred.
- Per-tenant DEK wrap / dedicated OpenBao prod — labeled "preview vault"
  per D114; hardened at the M11 K8s gateway seam, not here.

---

## Security claims (adversary targets)

- SC1: Artifact signed URLs are tenant-scoped and expiring. A URL from
  tenant A is useless to tenant B even unexpired (M8 SC1 carried).
- SC2: Completion webhooks are HMAC-signed; a receiver can verify the
  sender; unsigned or replayed deliveries are detectable (M8 SC2).
- SC3: The delivery channel sends the run's DECLARED final output only.
  Adversary attempts to exfiltrate logs, secrets, or intermediate events
  via the delivery path fail (M8 SC3).
- SC4 (new): Rate limiting returns a typed 429 + retry-after under
  flood. Never a silent drop, never a 500, on both invoke and
  management endpoints.
- SC5 (new): A leaked inv_ token revoked via the customer-facing path
  dies within one TTL. The old token admits no run after revocation.

---

## Work items

### P0 — the output loop and the front door

1. **T01 Artifacts + signed R2 URLs.** Run-time writes to tenant R2
   prefix; result package (logs, artifacts, files) assembled at
   completion; signed URLs with expiry. SC1.
2. **T02 Completion webhook.** Per-deployment endpoint config,
   HMAC-signed payload (run ID, terminal state, result links), bounded
   retries. P5 SSRF: destinations validated at config time AND delivery
   time — public IPs only, no RFC1918/loopback/link-local, no redirects
   into private ranges. SC2.
3. **T03 Delivery channel.** Generic webhook delivery only (narrows
   D84.3). Platform delivers declared final output to a per-deployment
   HTTPS destination at completion. Native Slack/Telegram/email are
   post-POC. SC3.
4. **T04 `cloud result <run>` + `cloud logs <run>` polish.**
   Customer-readable "why failed" without reading DO logs. Typed
   failure reasons surface in CLI output.
5. **T05 Always-on prod API on cloud.agentpaas.ai.** No teardown habit
   as the product posture. Plus: wrangler deploy no longer resets the
   image binding — re-bind is productized on deploy, not a founder
   runbook step.

### P1 — onboarding truth

6. **T06 Golden-path customer runbook.** One page, NOT owa-records:
   provision-token → login → pack → push → secrets → deploy → invoke →
   cron → result → usage. Validated by following it cold (T11).
7. **T07 Invoke-token storage.** Keychain (or `~/.agentpaas` store) per
   deployment, plus a rotation doc. Kills the flag/env-only assumption
   for inv_.
8. **T08 D105 rate-limit matrix finished.** The M7 work item 5 that was
   deferred: edge rules (blunt outer) + per-token DO counters (precise)
   on all invoke and management endpoints. Trial: 60 invoke/min per
   deployment token, 30 mgmt/min per tenant token, per-tenant
   overridable via admin verb (D104). Typed 429 + retry-after. Limits
   set as a small multiple of what the concurrency cap makes useful so
   customers see quota_exceeded (meaningful) not 429 (mechanical). SC4.
9. **T09 Quota UX.** Period reset / monthly rollover automation; refuse
   message carries a next action (upgrade contact); trial-expiry
   message points at a real support path, not an empty "contact us".

### P2 — honesty and hardening

10. **T10 Secrets production posture: label "preview vault".** Per Q2
    2026-07-31 / D114: do NOT build per-tenant DEK wrap now. Mark the
    vault "preview" in docs and CLI output; remove any OpenBao-isolation
    marketing until the isolation is true. DEK wrap / master-key work
    lands at the M11 K8s gateway seam.
11. **T11 Cold-onboard external gate.** One external tester, clean
    machine, 30-minute timer, runbook only. This is the gate that proves
    the whole block.
12. **T12 Live CF soak of M6+M7+M7.5 paths with a $ cap.** Cron actually
    fires in prod (not unit-only), invoke admits end to end, artifact
    URL resolves, completion webhook delivers. Teardown scoped per the
    CF-live rule (only agentpaas-cloud-api + its containers; never
    plain-cloud-0e69 / agentpaas.ai DNS).

---

## MANUAL GATE (founder, ~30 min) — NON-DEFERRABLE per D113

Follow the T06 runbook cold on a clean VM (no shell history, no env).
Provision a fresh trial token, deploy an agent that writes a CSV, set a
5-minute cron, walk away, come back to a completion webhook holding a
working signed URL. Fetch it with `cloud result`. Then watch the T11
external tester do the same unaided. Approve if neither of you asked
the founder anything. **This gate cannot be deferred. If it is red, the
block stays OPEN and engineering stops.**

---

## Sequencing rules introduced by this block (recorded as decisions)

1. **D112** — every "Deferred" line in a block close record becomes a
   D-entry with a re-entry trigger, or the block does not close.
2. **D113** — a block cannot close with its customer-facing manual gate
   deferred. A red gate keeps the block OPEN and stops the next block.
3. **D114** — vault production posture: label "preview", defer DEK wrap
   to the M11 K8s seam, no isolation marketing until true.
