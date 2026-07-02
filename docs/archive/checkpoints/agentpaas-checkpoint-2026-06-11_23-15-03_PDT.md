# AgentPaaS Checkpoint

Date/time: 2026-06-11 23:15:03 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the critical vision-alignment update after the Block
10 dashboard checkpoint. The founder clarified that P1 should be designed
primarily for Codex, Claude Code, Hermes, Cursor, and other agentic
development tools to build, test, diagnose, repair, and run agents through
AgentPaaS on the user's machine with little human hand-holding. Humans should
mainly approve sensitive trust-boundary changes.

The work remained in planning/spec review mode; no implementation code has
been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_23-15-03_PDT.md`

Latest committed checkpoint entering this review:
- `e200d59 docs: checkpoint block 10 dashboard review`

## Agentic Operator Contract Outcome

Added Block 10.5: Agentic operator contract. This block makes the agentic
tooling vision concrete and defensible before implementation. It states that
AgentPaaS P1 is not just a human CLI/dashboard product; it is a secure local
runtime operated primarily by coding agents through stable machine-readable
contracts.

Decisions locked:
- Codex, Claude Code, Hermes, Cursor, and similar coding agents are primary
  P1 operators.
- Human CLI/dashboard output is a view, not the core contract.
- Every user-visible operation must also expose a machine-readable JSON path.
- `internal/operator/` is added as the place for agentic diagnostics, repair
  hints, and JSON schemas.
- Block 1 proto/control API now includes operator methods:
  `ValidateAgentProject`, `SummarizeRun`, `ExplainFailure`,
  `ExplainPolicyDenial`, `RecommendPolicyPatch`, `GetRunTimeline`, and
  `NextAction`.
- CLI/dashboard/MCP integrations must render or wrap the same operator data,
  not create separate behavior surfaces.
- Agentic tools may automatically repair code, tests, `agent.yaml`,
  dependency declarations, and non-security config inside the project root.
- Agentic tools may propose but must not silently apply trust-boundary
  changes: new egress, wildcard domains, credential bindings, direct leases,
  webhook destinations, exposed listeners, retention purges, destructive
  actions, or disabling gates.
- P1 requires explicit user/daemon confirmation for trust-boundary changes.
- Tools cannot read secret values, broaden policy silently, delete audit,
  disable red-team gates, or operate outside the invoking project root.
- Prompt-injected instructions inside source, logs, traces, or payloads are
  untrusted data and must not cause policy broadening, secret disclosure,
  audit deletion, or destructive operations.

## Retroactive Impact On Blocks 1-10

Block 10.5 defines a retroactive invariant for the reviewed P1 foundation:
- Block 1 APIs/protos define stable machine-readable methods and error enums.
- Block 2 daemon lifecycle/doctor reports structured readiness and repair
  hints.
- Block 3 audit exposes query/export results as signed, verifiable machine
  data with trust-anchor fingerprints.
- Block 4 policy compiler emits structured denial reasons and safe patch
  proposals, never silent policy broadening.
- Block 5 network/runtime returns structured egress decisions and containment
  evidence.
- Block 6 harness/SDK emits run lifecycle, health, budget, and exception
  events in schemas that tools can reason over.
- Block 7 secrets broker exposes missing-binding/revocation/lease diagnostics
  without revealing secret values.
- Block 8 packaging returns signed `agent.lock`, SBOM, scan, advisory, and
  reproducibility results as JSON.
- Block 9 Trigger API uses stable caller ids, idempotency, SSE event ids, and
  cancel outcomes that tools can resume from.
- Block 10 dashboard/OTel exposes the same timeline/audit/policy data as
  JSON; the UI is a view, not the source of truth.

## Agentic Workflow Contract Added

The P1 golden workflow is now:
1. A coding agent creates or modifies a Python/LangGraph/CrewAI agent.
2. It runs `agent init --from-code --noninteractive`.
3. It runs `agent validate --json` and repairs local code/config issues.
4. It runs `agent pack --json`.
5. It runs `agent run --json` or `InvokeStream`.
6. If the run fails, it calls `agent explain run <run_id> --json`.
7. If egress is denied, it calls `agent policy explain ... --json`.
8. It receives a policy patch proposal, not an auto-applied allow rule.
9. After explicit approval where required, it reruns the agent.
10. It summarizes the final run and exports a signed audit bundle if
    requested.

`agent next-action <run_id> --json` returns one of:
- `fix_code`
- `install_dependency`
- `start_docker`
- `set_secret`
- `review_policy_patch`
- `increase_budget`
- `rerun`
- `export_audit`
- `ask_user`

Stable P1 failure categories include:
- `dependency_conflict`
- `docker_unavailable`
- `policy_denied`
- `missing_secret_binding`
- `budget_exceeded`
- `trigger_auth_failed`
- `harness_health_failed`
- `agent_runtime_exception`
- `policy_validation_failed`
- `network_sandbox_failed`
- `secret_scan_failed`
- `package_verification_failed`
- `dashboard_unavailable`

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` standing rules
- `agentpaas-execution-plan-v1.md` repo layout
- `agentpaas-execution-plan-v1.md` Block 1 proto/control API scope
- `agentpaas-execution-plan-v1.md` Block 10.5
- `agentpaas-execution-plan-v1.md` Block 12 MCP tools
- `agentpaas-prd-v4-master.md` §1.4 Product principles
- `agentpaas-prd-v4-master.md` §2.10.5 Agentic operator contract
- `agentpaas-prd-v4-master.md` §4 Coding-tool integrations

## Tests / Gates Added To Plan

Block 10.5 success gate:
- scripted Codex/Claude/Hermes-like client creates a deliberately incomplete
  Python agent
- runs `agent init --from-code --noninteractive`
- validates, packs, and runs
- sees a policy denial
- receives structured denial explanation
- receives a policy patch proposal but cannot apply it without confirmation
- automatically fixes a code/dependency issue
- reruns after approved policy
- exports a signed audit bundle
- summarizes final result in JSON

Negative tests:
- prompt-injected source/log instructions cannot broaden policy
- prompt-injected source/log instructions cannot reveal secrets
- prompt-injected source/log instructions cannot delete audit
- prompt-injected source/log instructions cannot stop unrelated runs
- JSON schema golden tests prove backward-compatible outputs for every
  operator method

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 11, the red-team suite. Now also
ensure red-team tests cover the agentic operator contract: prompt injection
through source/logs/traces, policy-patch proposal boundaries, path allow-list
enforcement, and MCP/operator refusal behavior.

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

"Continue from the AgentPaaS checkpoint dated 2026-06-11 23:15:03 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `e200d59`. Next, review Block 11: Red-team suite,
especially whether every attacker runs through the real pack/run path,
whether the attack library covers prior security promises, and whether it now
also tests the Block 10.5 agentic operator contract and prompt-injection
boundaries."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.
