# BUG — cloud invoke admits run as starting, never starts container / agent

**When:** 2026-08-01 M7.5 T11  
**Severity:** P0 — blocks golden-path invoke→result  
**Status:** FIXED — deployment invoke starts container + forwards /invoke

## Symptom
`agentpaas cloud invoke dep_…` returns Run ID with `status: starting` forever.
`started_at` null. `/v1/runs/:id/invoke` → `run_not_running: starting`.
healthz may show container exit 1 if DO probes.

## Root cause
`POST /v1/deployments/:id/invoke` → `invokeDeployment()` → `admitRun(..., { deferStart: true })` then **returns**.
No `configureRunEgress`, no `startRunContainer`, no container `/invoke`, no terminal/final_output.

Contrast: `POST /v1/runs` path (handleCreateRun ~900–950) does deferStart + configure + start and marks running/failed.

CLI `cloud invoke` only hits deployment invoke — so customers never get a running agent.

## Fix direction
After successful admit in deployment-invoke path (with body):
1. Resolve allowed_hosts + secret bindings (same as create-run)
2. configureRunEgress
3. startRunContainer → running or failed
4. Forward body to container `/invoke` (enrich LLM from lock)
5. On invoke completion, terminal run + assemble artifacts with final_output

Keep auth/rate-limit behavior. Tests for: start failure marks failed; happy path running + invoke response; deferStart not left dangling.

## Evidence
- trigger_http.ts invokeDeployment deferStart: true only
- index.ts handleDeploymentInvoke returns admit JSON 201
- Live run_957e76ae… stuck starting
