# BUG — Post-deploy Worker secrets incomplete → deploy 503 cf_bind_not_configured

**When found:** 2026-08-01 M7.5 T11 founder cold path  
**Severity:** P0 for live Cloud (blocks `cloud deploy` after successful push)  
**Status:** OPEN as process/product gap; partial ops fix applied same session

## Symptom

```text
agentpaas cloud deploy sha256:…
Error: cloud deploy: create deployment: unexpected status 503
```

API body (CLI currently swallows it — separate UX issue):

```json
{"error":"cf_bind_not_configured"}
```

## Root cause

`createDeployment` requires CF Containers bind env when the admitted image has `registry_ref` (`src/deployments.ts` ~200–215):

- `CF_API_TOKEN` or `CLOUDFLARE_API_TOKEN`
- `CF_ACCOUNT_ID`
- `CF_CONTAINER_APP_ID`

Optional but needed for weather egress: `CF_EGRESS_ALLOWLIST`.

Fresh `wrangler deploy` after teardown only had what we set ad hoc (`ADMIN_SECRET`, later `SECRETS_MASTER_KEY`). **Bind trio was never set** → every deploy with a registry-backed image returns 503.

## Why orch missed it (honest)

1. Bring-up checklist was incomplete: health/whoami/provision/secrets ≠ deploy-bind path.
2. M4-R8 / T05 docs list these vars; not enforced in deploy script or doctor.
3. CLI maps all non-2xx to `unexpected status 503` without surfacing `error` body → looked like a generic outage, not “operator misconfigured Worker”.
4. Eng unit tests inject bind env; live empty env only fails in prod.

## Same class (already hit this session)

| Secret/var | When missing |
|------------|----------------|
| `ADMIN_SECRET` | admin provision fails |
| `SECRETS_MASTER_KEY` | secrets push 503 `secrets_misconfigured` |
| `CF_*` bind trio | deploy 503 `cf_bind_not_configured` |
| R2 enable + bucket | wrangler deploy fails entirely |
| `ARTIFACT_SIGNING_SECRET` | may break signed URLs later |

## Fix applied (ops, this session)

```text
wrangler secret put CF_API_TOKEN
wrangler secret put CF_ACCOUNT_ID          # d9d17d2b…
wrangler secret put CF_CONTAINER_APP_ID    # a0318019-… runcontainer app
wrangler secret put CF_EGRESS_ALLOWLIST    # wttr.in,openrouter.ai
```

## Product fixes (do after T11 or block-stop)

1. **Deploy-time preflight** on Worker: if `registry_ref` and bind env missing, return 503 with explicit `error` + `hint` listing missing keys (already have error code; ensure always).
2. **CLI:** print API `error` field on non-2xx (`cf_bind_not_configured`, not only status code) — UX-HTTP-BODY.
3. **`scripts/deploy-prod.sh` / T05 checklist:** require bind secrets present (wrangler secret list assert) before declaring API live.
4. **Post-teardown runbook:** single checklist ADMIN + MASTER + CF bind + R2 + ARTIFACT_SIGNING.
5. Optional: `agentpaas cloud doctor` against live API that probes config (no secret values).

## Customer impact

First-time path after empty account: push works, deploy hard-fails. Unacceptable stranger bar without checklist automation.
