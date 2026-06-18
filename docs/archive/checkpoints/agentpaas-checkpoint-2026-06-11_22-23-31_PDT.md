# AgentPaaS Checkpoint

Date/time: 2026-06-11 22:23:31 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 7 secrets broker planning review after the
Block 6 harness-scope checkpoint. The work remained in planning/spec review
mode; no implementation code has been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_22-23-31_PDT.md`

Latest committed checkpoint entering this review:
- `a851a8c docs: clarify block 6 harness scope`

## Block 7 Review Outcome

Block 7 is now scoped as the P1 secrets broker and runtime credential-use
contract. It should make secrets usable by governed agents without handing raw
brokered secrets to agent code.

Decisions locked:
- Brokered gateway-side credential injection remains the default and preferred
  mode.
- P1 credential injection is header-only. Query-string and body injection are
  rejected by validation.
- `env_lease` is removed from P1. Environment-variable leases are too leaky for
  the core security promise.
- Direct leases are file-only in P1 and exist only for explicit legacy
  compatibility.
- Runtime `file_lease` mounts are tmpfs files, mode 0400, owned by the agent
  uid, and removed at stop.
- Real secret files must never be generated into the source tree, build
  context, image layers, or packed artifacts. Codex/Hermes/generated code
  should create credential references and policy entries, not real secret
  values.
- `agent secret set` reads from stdin or an interactive prompt, never argv.
- Individual secret values are capped at 64 KiB, aligning with common cloud
  secret-manager limits.
- `agent secret list` shows metadata only: id, created time, updated time,
  last used time, and referenced policies/agents. It never shows value,
  prefix, suffix, or hash-derived hints.
- Secret store names are case-sensitive local-profile entries with no
  whitespace or control characters.
- Policy credential ids are policy-local stable ids that bind egress/MCP
  rules to stored physical secret names.
- One stored physical secret can be referenced by multiple reviewed policies,
  but each agent must opt in through its own policy binding.
- `SecretStore` has P1 implementations for macOS Keychain, Linux libsecret,
  and an explicit fake test store only. There is no silent plaintext fallback.
- Credentialed redirects are disabled by default; noncredentialed redirects
  are re-evaluated against policy per hop.
- Secret-related CLI, dashboard, runtime, and validation errors must redact
  values and must not reveal value prefixes, suffixes, or hash-derived hints.
- Direct-lease revocation stops future access after restart, but cannot claw
  back a secret already visible to agent code.
- Dashboard run detail now refers to audit checkpoint markers, not agent
  checkpoint/resume markers.

## Tests / Gates Added To Plan

Block 7 now requires negative tests that grep the process list, shell history
fixture, Docker inspect, gateway logs, compiled configs, exported image
layers, build context, packed artifacts, CLI/dashboard errors, and
agent filesystem/proc probes for a brokered sentinel secret. All must return
zero hits.

The brokered positive path still requires a real OpenAI-style request where
the upstream receives the Authorization header, while agent logs/proc/env
never contain the key.

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 7
- `agentpaas-prd-v4-master.md` §2.5 Secrets model
- `agentpaas-prd-v4-master.md` §2.5.1 Secret access guarantees
- `agentpaas-prd-v4-master.md` §2.9 agent.yaml + policy.yaml
- `agentpaas-prd-v4-master.md` §2.10 Dashboard

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 8, the packaging pipeline. Focus on
the signing story, because "keyless local mode: signed by the agent identity
key" remains confusing and should be made precise before implementation.

Other open items still relevant:
1. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
2. Policy schema reference: required/optional fields, defaults, unknown fields,
   wildcard behavior, CIDR/private-network behavior, credential binding
   behavior, MCP declarations.
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

"Continue from the AgentPaaS checkpoint dated 2026-06-11 22:23:31 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `a851a8c`. Next, review Block 8: Packaging pipeline,
especially the signing/cosign story, Python-only P1 packaging, secret scanning,
SBOM, reproducibility, and `agent.lock`."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents.
