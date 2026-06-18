# AgentPaaS Checkpoint

Date/time: 2026-06-11 23:35:54 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 12 integration review and edits. The review
focused on whether the MCP server, Claude Code plugin, and Hermes skill
faithfully wrap the Block 10.5 agentic operator contract, enforce
prompt-injection boundaries, and preserve the under-10-minute post-install
demo flow.

The work remained in planning/spec review mode; no implementation code has
been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_23-35-54_PDT.md`

Latest committed checkpoint entering this review:
- `efed107 docs: scope block 11 redteam smoke gate`

## Block 12 Review Outcome

Block 12 now treats `agentpaas-mcp` as the single canonical integration
adapter for coding agents. Claude Code, Hermes, Codex, Cursor, and similar
tools get thin per-tool skins, but behavior is required to come from the
Block 10.5 operator contract.

Decisions locked:
- The MCP server is the canonical integration artifact.
- Claude Code plugin and Hermes skill are thin wrappers around the MCP server.
- Hermes P1 flow is native-MCP-first, with terminal CLI commands documented as
  fallback.
- The clean-machine integration gate measures only the post-install deploy
  flow, not AgentPaaS/Docker/plugin installation.
- Post-install Claude Code and Hermes native MCP demos must each reach a
  governed running agent with visible denial/audit evidence in under 10
  minutes.
- `agentpaas_stop` is exposed in P1, but it may stop only the active run
  created by the client session by default.
- Stopping unrelated runs or performing trust-boundary actions requires the
  daemon/UI/CLI confirmation protocol.
- MCP spec revision and generated schema fixtures are pinned in the integration
  package/test fixtures, not in a user's `agent.lock`.
- P1 should include two to three differentiated demos, not only a weather demo.

## Added To Block 12

Required P1 MCP tools now include:
- `agentpaas_init_project` / `agentpaas_reconcile_project`
- `agentpaas_validate_project`
- `agentpaas_doctor`
- `agentpaas_pack`
- `agentpaas_run`
- `agentpaas_stop`
- `agentpaas_logs`
- `agentpaas_status`
- `agentpaas_get_run_timeline`
- `agentpaas_policy_show`
- `agentpaas_explain_policy_denial`
- `agentpaas_recommend_policy_patch`
- `agentpaas_audit_query`
- `agentpaas_export_audit`
- `agentpaas_summarize_run`
- `agentpaas_explain_failure`
- `agentpaas_next_action`

Contract parity gate:
- CI fails if a Block 10.5 operator method lacks an MCP wrapper.
- CI fails if an MCP wrapper returns fields outside the versioned operator
  schema.
- CI fails if a wrapper drops required evidence refs or stable error
  categories.
- CI fails if a trust-boundary action can complete without daemon
  confirmation.

Prompt-injection boundary:
- MCP responses must separate trusted control fields from untrusted evidence.
- Trusted fields include status, error category, next action, confirmation
  fields, risk level, and evidence refs.
- Untrusted fields include redacted excerpts from source, comments, logs,
  traces, MCP resources, tool output, and external payloads.
- Instructions found in untrusted content cannot broaden policy, reveal
  secrets, delete audit, disable gates, stop unrelated runs, or trigger
  destructive operations.

Confirmation protocol:
- Trust-boundary actions return `requires_confirmation: true`,
  `confirmation_id`, `risk_level`, rationale, and evidence refs.
- Only daemon/UI/CLI confirmation can apply the change.
- Confirmed changes are audited.

## P1 Demo Matrix

The P1 integration demo set now includes:
1. **Governed weather/API agent:** generated agent attempts allowed weather API
   plus a denied exfil probe; dashboard shows policy denial and signed audit
   evidence.
2. **Secret-brokered SaaS action:** generated ticket/CRM-style agent uses a
   brokered credential through the gateway; secret value is never visible to
   code/logs, but the upstream fixture receives the authorized request.
3. **Agentic repair loop:** generated agent has a dependency/code defect and
   missing egress policy; MCP `next_action` fixes code automatically, proposes
   policy only, waits for confirmation, reruns, and exports a signed audit
   bundle.

## PRD Alignment

The PRD integration section now mirrors the Block 12 decisions:
- `agentpaas-mcp` exposes the same `agentpaas_*` tool names as the execution
  plan.
- Hermes is native-MCP-first.
- Contract parity is release-gated.
- Prompt-injection boundaries are explicit.
- Trust-boundary changes require confirmation and audit.
- The under-10-minute proof is post-install only.

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 13, the install path, docs, demo,
and v0.1.0 release. Focus on whether install/docs/demo can support the
post-install under-10-minute integration proof, whether the installer models
the trust posture, and what must ship versus can slip for v0.1.0.

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

"Continue from the AgentPaaS checkpoint dated 2026-06-11 23:35:54 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after Block 12. Next, review Block 13: install path,
docs, demo, and v0.1.0 release. Focus on whether install/docs/demo can
support the post-install under-10-minute integration proof, whether the
installer models the trust posture, and what must ship versus can slip for
v0.1.0."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.
