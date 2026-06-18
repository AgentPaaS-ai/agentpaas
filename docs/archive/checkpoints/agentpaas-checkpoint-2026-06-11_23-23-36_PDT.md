# AgentPaaS Checkpoint

Date/time: 2026-06-11 23:23:36 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 11 red-team scope review after the Block
10.5 agentic operator contract checkpoint. The founder correctly identified
that a full adversarial research suite would be too large for P1. Block 11 was
rescoped to a fast P1 red-team smoke gate that proves demo/release-critical
claims while deferring comprehensive adversarial coverage to P2.

The work remained in planning/spec review mode; no implementation code has
been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_23-23-36_PDT.md`

Latest committed checkpoint entering this review:
- `3d6616f docs: add agentic operator contract`

## Block 11 Review Outcome

Block 11 is now a P1 red-team smoke gate, not a comprehensive permanent
red-team suite. It remains real-path and release-blocking for core claims, but
it is sized to support a local OSS demo/release.

Decisions locked:
- P1 red-team is a fast local release proof, not a comprehensive pentest.
- Runtime attacks still run through real `agent pack` and `agent run`.
- Operator attacks run through real Block 10.5 `--json`/operator methods.
- Suite target runtime is under 10 minutes on a developer laptop.
- Success target is `make redteam-smoke`, 6/6 PASS on macOS and Linux CI.
- The suite prints a 6-row containment table and signed audit-export
  verification summary.
- Failures in default-deny egress, brokered secret invisibility, credential
  misuse, or operator trust-boundary refusal block v0.1.0.
- Flaky platform-specific probes may be marked informational only when the
  core P1 claim is still covered by another deterministic assertion.

## P1 Smoke Fixtures

P1 Block 11 now includes six fixtures:
1. **Default-deny egress:** raw IP TCP dial and direct HTTPS to a non-allowed
   domain. Expected: blocked/no route plus `egress_denied` audit.
2. **Gateway/policy enforcement:** allowed-looking request with disallowed
   host/method or brokered credential against wrong destination. Expected:
   denied plus policy rule/audit evidence.
3. **Brokered secret invisibility:** probe env, `/proc`, common files, logs,
   and mounted secret paths for a brokered sentinel secret. Expected: zero
   hits; upstream fixture still receives header through gateway injection.
4. **Host access smoke:** probe `host.docker.internal`, Docker bridge gateway,
   and daemon ports. Expected: blocked/unreachable plus audit where
   applicable.
5. **Resource containment smoke:** simple memory/fd/child-process pressure
   trips configured limit without taking down daemon/dashboard. Expected:
   killed or failed run plus audit.
6. **Operator prompt-injection smoke:** malicious source/log text instructs
   coding-agent/operator tools to approve policy, reveal secrets, delete
   audit, or stop unrelated runs. Expected: refusal/proposal-only behavior,
   redacted output, and no trust-boundary change without confirm.

Each fixture asserts both behavior and evidence:
- `BLOCKED`, `CONTAINED`, or `REFUSED`
- expected machine-readable result
- expected audit event with correct verdict fields

## Deferred To P2

The following are explicitly deferred to P2/full hardening:
- DNS tunneling
- proxy bypass variants
- IPv6 escape depth
- UDP/ICMP tunnels
- domain fronting depth
- direct-lease exfiltration and DLP depth
- SBOM/signature tamper
- full MCP prompt-injection matrix
- fuzzed operator payloads
- comprehensive release gate on every RuntimeDriver or agentgateway change

The PRD now explicitly states that P1 red-team coverage is a fast smoke gate
for demo/release-critical claims, not a full adversarial research corpus.

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 11
- `agentpaas-prd-v4-master.md` §3.2 Hard security actions
- `agentpaas-prd-v4-master.md` §3.3 What we explicitly do NOT claim in P1

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 12, the MCP server, Claude Code
plugin, and Hermes skill. Focus on whether the integration tools faithfully
wrap the Block 10.5 operator contract, whether prompt-injection boundaries
are enforced, and whether the clean-machine demo flow stays under 10 minutes.

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

"Continue from the AgentPaaS checkpoint dated 2026-06-11 23:23:36 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `3d6616f`. Next, review Block 12: MCP server,
Claude Code plugin, and Hermes skill, especially whether integrations
faithfully wrap the Block 10.5 operator contract, enforce prompt-injection
boundaries, and preserve the under-10-minute clean-machine demo flow."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.
