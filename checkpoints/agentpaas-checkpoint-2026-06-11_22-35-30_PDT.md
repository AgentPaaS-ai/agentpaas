# AgentPaaS Checkpoint

Date/time: 2026-06-11 22:35:30 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 8 packaging pipeline planning review after
the Block 7 secrets broker checkpoint. The work remained in planning/spec
review mode; no implementation code has been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_22-35-30_PDT.md`

Latest committed checkpoint entering this review:
- `7e1903b docs: tighten block 7 secrets scope`

## Block 8 Review Outcome

Block 8 is now scoped as the P1 packaging pipeline that turns an agent source
directory into a scanned, signed, reviewable, reproducible local artifact. The
approval unit is the signed `agent.lock` manifest, not the mutable source tree
or an image tag.

Decisions locked:
- P1 packaging remains Python-first: plain Python, LangGraph, and CrewAI.
- Node and custom Dockerfile packaging remain follow-on gates.
- P1 local signing is local key-backed cosign signing with the per-agent
  package identity key.
- P1 local packs do not use Sigstore keyless OIDC/Fulcio signing. Future
  release or enterprise flows may add Fulcio/Rekor or tenant trust roots.
- `agent.lock` is a canonical signed manifest and the exact artifact consumed
  by `agent run` and future promotion.
- `agent.lock` must include schema version, agent name/version,
  runtime/framework, target platform, base image digest, harness version,
  build input digest, image digest, SBOM digest, policy digest,
  package AID/public key, signature bundle/referrer locations, and
  reproducibility metadata.
- `agent verify agent.lock` wraps offline verification: lockfile signature,
  image signature with the AID public key, digest checks, SBOM digest checks,
  and policy digest checks.
- SBOMs are generated with syft as SPDX JSON, attached as OCI artifacts in
  the local OCI layout, and referenced from `agent.lock`.
- Registry push is deferred; local mode uses local OCI layout plus Docker
  image by digest.
- Secret scanning covers both the full source tree and the effective build
  context. `.agentpaasignore` controls what is built, not whether checked-in
  secrets are acceptable.
- `.agentpaasignore` defaults include `.git`, virtualenvs, caches,
  `node_modules`, test outputs, and large local data.
- `--allow-secret-pattern` requires a successful daemon audit append or the
  pack aborts.
- Reproducibility expectations include fixed timestamps, pinned base image
  digest, locked dependencies, deterministic tar order, and
  `SOURCE_DATE_EPOCH`.
- `osv-scanner` advisory summary appears in `agent pack` output without
  failing on non-critical findings.

## Tests / Gates Added To Plan

Block 8 now requires:
- three Python reference agents (`plain-py`, `langgraph`, `crewai`) packing
  green
- `agent verify agent.lock` passing
- explicit offline `cosign verify --key <AID pubkey>` passing for the image
  signature
- lockfile signature verification
- SBOM top-level dependency assertions
- `osv-scanner` advisory summary in pack output
- planted secret tests across normal source, ignored source, and build
  context
- golden fixture assertions for expected `agent.lock` fields
- reproducibility testing where rebuilds without changes produce identical
  image digests

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 8
- `agentpaas-prd-v4-master.md` §2.8 Packaging pipeline

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 9, the Trigger API, events,
webhooks, and cron. Focus on the loopback/exposed API auth model,
idempotency semantics, webhook policy enforcement, cron downtime/DST behavior,
and how cancelation interacts with in-flight LLM/MCP calls.

Other open items still relevant:
1. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
2. Policy schema reference: required/optional fields, defaults, unknown
   fields, wildcard behavior, CIDR/private-network behavior, credential
   binding behavior, MCP declarations.
3. Telemetry/privacy: no telemetry without opt-in; define opt-in UX and
   payload.
4. P1 must-ship vs can-slip: plan is large; decide what can slip without
   weakening the wedge.
5. Security review packet: threat model, policy, SBOM, signed audit export,
   enforcement proof, limitations.
6. Dashboard specifics: `docs/status.md` generation and GitHub API/CLI
   dependency.
7. GitHub Project setup once private repo exists.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-11 22:35:30 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `7e1903b`. Next, review Block 9: Trigger API,
events/webhooks, and cron, especially auth exposure, idempotency, webhook
policy enforcement, downtime/DST cron behavior, and cancelation semantics."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents.
