# BUG — No customer undeploy / slot never freed

**When found:** 2026-08-03 (post-M7.5 founder golden / slot capacity)
**Severity:** P0 for trial stranger bar — deploy is one-way; pool fills forever
**Status:** OPEN
**M8+ coverage:** **NO** — not in M8 (run-record retention/CLI only), M8.5 (registry/MCP), or M9 (audit/metrics). Pre-M8 residual.

## Symptom

- `cloud deploy` returns `503 no_slot_capacity` when the shared D1 slot pool has no `free` rows.
- Customer has no CLI/API to take down a deployment and free the slot.
- Orphan binds remain (e.g. dep gone from list but slot still `bound` to tenant).
- Live: pool size 3; trial story is 10 agents / 5 concurrent runs — cannot match without undeploy or much larger pool.

## Root cause

1. Deploy path: `assignSlot` → slot `bound` for life of deployment.
2. `releaseSlot` exists (`src/slots.ts`) but is only called on **failed** bind/create — not on intentional teardown.
3. No `DELETE /v1/deployments/:id` (or equivalent) in Worker `src/index.ts`.
4. No `agentpaas cloud undeploy` in OSS CLI.
5. Slot pool seeded at **3** shared rows (`migrations/0004_slots.sql`), not per-tenant trial quota.

## Expected product behavior

```text
agentpaas cloud undeploy <dep_id>
→ deployment terminal/removed
→ releaseSlot(slot_id, tenant_id)
→ slot status=free
→ customer can deploy another agent
```

Optional later: idle auto-release / deploy-on-invoke so unused agents don’t hold slots.

## Evidence

- D1 remote: all 3 slots bound; 2 to founder trial tenant (one orphan dep_3216f79e…), 1 other tenant.
- `cloud deployments` only lists ready dep_22c9cc… still holding slot-2.
- m4-live-cf-smoke.md already noted: “Manual slot pool free when 503 — need product release-on-undeploy later.”

## Fix scope (pre-M8)

| Layer | Work |
|-------|------|
| Cloud API | `DELETE /v1/deployments/:id` tenant-auth → release slot + mark/delete deployment |
| Cloud | Idempotent `releaseSlot`; refuse cross-tenant |
| OSS CLI | `agentpaas cloud undeploy <dep_id>` |
| Docs | golden-path + trial: undeploy frees capacity |
| Ops | One-shot cleanup of orphan bound slots |

## Not fixed by

- M8 run-record / result/logs polish  
- Increasing CF `max_instances` alone without free slot rows + undeploy  
- Cancel **run** (run concurrency ≠ deployment slot)

## Related

- `docs/owa-records/m7.5-pre-m8-residuals.md` — add R9
- Slot assign: cloud `src/slots.ts`, `src/deployments.ts`
- Trial limits: `src/admin.ts` concurrency 5 / agents 10 (no slot_limit)
