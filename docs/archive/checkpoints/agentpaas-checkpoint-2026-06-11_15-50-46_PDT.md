# AgentPaaS Checkpoint

Date/time: 2026-06-11 15:50:46 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the continuation after the 2026-06-11 13:11:32 PDT
checkpoint. The work remained in planning/spec review mode. We reviewed the
next execution-plan sections before implementation, clarified Block 4 policy
semantics, clarified Block 5 gateway-only network topology, and recorded a
future enterprise managed-secret concern for corporate employee machines
behind VPN.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_15-50-46_PDT.md`

No implementation code has been built yet.

## Git State Before This Checkpoint Commit

Latest committed checkpoint entering this closeout:
- `c75ea80 docs: clarify block 4 policy contract`
- `df0a22b docs: add checkpoint 2026-06-11 13-11`
- `a570d9c docs: define mcp and delegated access boundary`

The working tree before this checkpoint file was created had modified
planning docs for Block 5 and future secret posture. This checkpoint should
be committed together with those doc updates.

## Block 4 Policy Contract Review Outcome

Block 4 was clarified and committed in `c75ea80`.

Decisions locked:
- `policy.yaml` is the only canonical policy file for egress, credentials,
  MCP servers, hooks, and ingress.
- Domain matching is exact by default. `domain: example.com` does not allow
  `api.example.com`.
- Wildcards require `allow_wildcard: true`.
- Private CIDRs require `allow_private: true`.
- Brokered credential injection is header-only in P1.
- Query-string and body credential injection are rejected by validation.
- Hook destinations are validated as policy data in Block 4 and revalidated
  at delivery time in Block 9.
- Canonical policy digest ignores comments and YAML key order but changes on
  semantic differences.
- IDNs normalize to ASCII punycode; confusable-character UX is deferred.
- Credentialed brokered request redirects are disabled by default;
  noncredentialed redirects are re-evaluated against policy per hop.

Important files/sections:
- `agentpaas-prd-v4-master.md` §2.3, §2.5, §2.9
- `agentpaas-execution-plan-v1.md` Block 4

## Block 5 Runtime / Network Topology Review Outcome

Block 5 was reviewed as the product's physical enforcement proof. It turns
policy from advisory config into a network topology that the agent cannot
bypass in normal operation.

Decisions locked:
- Gateway-only in both directions.
- Daemon/caller ingress goes through gateway before reaching the harness.
- Agent outbound goes through gateway before reaching upstream services.
- No direct daemon-to-harness calls.
- No agent-to-host shortcuts.
- No host networking in P1.
- Per-agent `internal: true` bridge.
- Dedicated AgentPaaS egress network.
- Gateway sidecar is dual-homed: internal bridge plus egress network.
- Agent container is never attached to the egress network.
- Agent and gateway never share a network namespace.
- IPv6 is disabled for P1 agent networks.
- Rootless Docker is best-effort only, not a P1 release gate.
- P1 runtime gates are Docker Desktop, Colima's Docker-compatible socket,
  and Linux `dockerd`.

Block 5 now explicitly requires:
- positive path test: invoke reaches harness only through gateway ingress
- positive egress test: allowed outbound succeeds only through gateway egress
- policy decision and audit event on the allowed path
- raw IP / direct DNS / host probing / gateway IP probing / daemon port
  probing blocked
- UDP non-DNS, ICMP, raw sockets, and CONNECT tunnel bypasses blocked
- Docker inspect assertions for no default route, no egress attachment, no
  host networking, no shared namespace, and exactly expected networks
- resource assertions for non-root UID, read-only rootfs, tmpfs `/tmp`,
  dropped capabilities, no-new-privileges, seccomp, pids limit, memory, CPU
- deterministic AgentPaaS labels/names for containers and networks
- cleanup on partial create/start failure
- startup reconciliation kills half-fenced agents
- Docker inspect/log/network dumps contain no raw secret values and remain
  useful for debugging

Important files/sections:
- `agentpaas-prd-v4-master.md` §2.2.1, §2.3, §2.6, §2.7
- `agentpaas-execution-plan-v1.md` Block 5

## Gateway-Only Black-Box Runtime Discussion

Current understanding:
After the agent container and gateway sidecar are running, normal users,
coding agents, and external callers should interact only through the
AgentPaaS control plane. Runtime changes such as key rotation, prompt/config
updates, policy changes, stop/restart, and credential revocation must go
through the daemon/control plane and then through secure gateway ingress.

No agent ports should be published directly. The daemon should not call the
harness directly. The agent should not be given the Docker socket. The
runtime should behave as a managed black box to normal callers and the agent
code.

Honest boundary:
P1 local mode cannot claim to hide the container from the trusted owner of
the local machine or a local Docker administrator. AgentPaaS protects the
developer and organization from untrusted agent behavior, prompt injection,
buggy code, dependency compromise, accidental egress, and credential exposure.
It does not claim to defeat the trusted local machine owner.

## Future Enterprise Managed-Secret Posture

New concern captured:
If AgentPaaS enables corporate employees to run agents on their own machines
behind a corporate VPN, business technology/security teams may refuse to let
long-lived enterprise secrets reside on employee machines without additional
safeguards.

This is not a P1 implementation requirement, but it must be tackled before
team/enterprise deployment.

Future design options to evaluate:
- corporate-network or cloud managed secrets broker where raw secrets remain
  in a managed vault
- local gateway receives only short-lived, scoped use authorization or
  request-time injection
- device posture checks, MDM enrollment, disk encryption, and VPN/network
  location requirements before credential use
- tenant-admin policy that can disable direct leases entirely
- per-user delegated authorization and revocation for enterprise apps
- remote audit anchoring / tenant-visible audit for credential use from
  employee machines

Principle:
Local agents may run on employee machines, but enterprise secrets should not
have to permanently reside there. Credential use should be brokered,
short-lived, policy-scoped, revocable, and audited under tenant control.

The PRD now has §2.5.2 for this future posture. Execution-plan Block 7 now
requires adding a follow-up enterprise design issue.

## Current Open Items

Recommended next item:
Review the future enterprise managed-secret posture before continuing normal
block-by-block review. Specifically decide whether this remains a P2/P3
design note only, or whether P1 Block 7 must include a stronger primitive
that makes later corporate rollout easier.

Other open items still relevant:
1. Budget enforcement wording in Block 6: wall-clock/iterations exact,
   token/USD likely best-known usage plus post-hoc overage audit.
2. Cosign wording: "keyless local mode: signed by agent identity key" is
   confusing and needs a precise signing story.
3. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
4. Policy schema reference: required/optional fields, defaults, unknown
   fields, wildcard behavior, CIDR/private-network behavior, credential
   binding behavior, MCP declarations.
5. Telemetry/privacy: no telemetry without opt-in; define opt-in UX and
   payload.
6. P1 must-ship vs can-slip: plan is large; decide what can slip without
   weakening the wedge.
7. Security review packet: threat model, policy, SBOM, signed audit export,
   enforcement proof, limitations.
8. Dashboard specifics: `docs/status.md` generation and GitHub API/CLI
   dependency.
9. GitHub Project setup once private repo exists.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-11 15:50:46 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `c75ea80`. Next, review the future enterprise
managed-secret posture for corporate employee machines behind VPN, then
continue to Block 6 budget/harness review."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents.
