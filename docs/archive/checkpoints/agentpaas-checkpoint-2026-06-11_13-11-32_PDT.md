# AgentPaaS Checkpoint

Date/time: 2026-06-11 13:11:32 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the continuation session after the earlier
2026-06-11 12:22:44 PDT checkpoint. The work remained in planning/spec mode:
we reviewed Block 3, clarified the security model, added Phase 2 cloud
portability guardrails, and defined the P1/P2 boundary for MCP access,
Verified User Access-style delegated access, and orchestration.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_13-11-32_PDT.md`

No implementation code has been built yet.

## Git State

Git was initialized during this session so future updates have a durable
history.

Current recent commits:
- `a570d9c docs: define mcp and delegated access boundary`
- `91a5cb6 docs: add cloud portability guardrails`
- `b189e82 docs: add block 3 security walkthrough`
- `eaa3e7e docs: checkpoint agentpaas planning state`

The initial checkpoint commit captured all existing project files, including
PRDs, execution plan, landing page assets, and the previous checkpoint.

## Block 3 Review Outcome

Block 3 is now understood as the security spine:
- local identity model
- short-lived workload certs
- audit hash-chain
- signed checkpoints
- signed export bundle
- verification semantics

Block 3 is not the network sandbox, secrets broker, scheduler, or cloud
runtime yet. It creates the trust/audit foundation those later blocks rely on.

## Block 3 Security Model Clarifications

### Identity classes are now separate

The PRD now distinguishes four identity classes:
1. Local CA key.
2. Daemon audit signing key.
3. Per-agent package identity key.
4. Per-run workload key/cert.

Important decision:
Workload certs identify event sources but do not sign the canonical audit
trail. Audit checkpoints and export manifests are signed only by the daemon
audit signing key.

Reason:
A compromised or confused workload credential should not become authority over
the audit record itself.

### Audit chain is now specified

The PRD now says:
- JSONL is authoritative.
- SQLite is a derived index and can be rebuilt.
- Each record has stable schema version, monotonic sequence, wall-clock time,
  event type, agent identity, run id where applicable, policy/image digests
  where applicable, payload hashes instead of payload bodies, `prev_hash`,
  and `record_hash`.
- `record_hash` is SHA-256 over canonical JSON with `record_hash` omitted.
- `prev_hash` points to the prior record hash.
- Genesis value is a fixed all-zero hash.
- Sequence number, not wall clock, is authoritative for ordering.
- Security-relevant actions fail closed if the audit record cannot be durably
  appended.
- Signed checkpoints are inserted into the same chain.
- Local head anchor catches tail truncation.

### Honest verification boundary

Second-machine verification proves:
- the exported bundle is internally consistent
- checkpoint/export signatures are valid
- the bundle was signed by the expected daemon audit key

It does not prove:
- a fully compromised local machine could not have deleted all local evidence
  before export
- global transparency-log anchoring

Future P2/P3 option:
remote audit anchoring or transparency log.

## Diagrams Added To PRD

The PRD now includes Mermaid diagrams for:
- Block 3 identity roles.
- Audit hash-chain.
- Step-by-step pack/run/export verification flow.
- Scheduled local cron run path.
- Local development threat model.
- Phase 2 Cloudflare promotion path.
- P2 VUA-style receipt-to-NetSuite flow.

Important PRD sections:
- `2.4 Identity model`
- `2.4.1 Audit chain model`
- `2.4.2 Block 3 security walkthrough and local threat model`
- `2.4.3 Phase 2 cloud portability guardrails`
- `2.7.1 MCP access, delegated user access, and orchestration boundary`

## Local Cron/Scheduled Runs Decision

Local cron-style agents are supported by the overall P1 architecture, but the
scheduler itself lands in Block 9.

Security rule:
Scheduled runs must be just another trigger source. They must go through the
same Trigger API, identity, policy, budget, secrets, egress, and audit path as
interactive runs.

Reason:
Cron makes the guardrails more important because the agent may run unattended
for days or weeks.

## Local Developer Machine Threat Model

On a secure local development machine, the primary adversary is not usually
the developer. The adversary is the untrusted behavior around the agent:
- AI-generated or buggy agent code.
- Prompt injection through email, tickets, docs, Slack, webpages, PDFs,
  invoices, or other inputs.
- Compromised npm/PyPI/transitive dependencies.
- Over-broad or stolen credentials.
- Automation drift from recurring scheduled execution.
- Local malware or full machine compromise, which P1 only partially addresses.

P1 trust posture:
Trust the developer and local supervisor enough to govern execution. Do not
automatically trust the AI-written agent, its inputs, its dependencies, or its
network behavior.

## Phase 2 Cloudflare PaaS Vision

New information introduced:
Phase 2 should let a developer take the same agent tested locally and promote
it to an AgentPaaS.ai hosted PaaS using Cloudflare.

Expected Cloudflare substrate:
- Workers for ingress/control edge.
- Containers for running agent images.
- Durable Objects for per-agent/per-run coordination and state.
- Cloudflare secrets/bindings for hosted credentials.
- Cloudflare Access/service auth where useful.
- Hosted audit sink with possible remote anchoring.

Decision:
P1 Block 3 does not hinder this if local identity, storage, and audit anchoring
are implemented as replaceable backends rather than product semantics.

P1 must keep portable:
- AID is environment-independent.
- Issuer is pluggable.
- SPIFFE trust domain is not hardcoded to `local.agentpaas`.
- Audit signer is environment-scoped.
- Audit storage is abstract.
- Run identity includes deployment context.
- Promotion is by digest, not rebuild.

P1 should not:
- bake OS keychain APIs into audit or identity business logic
- define audit verification as reading from `~/.agentpaas`
- make local daemon identity equal tenant identity
- require direct host filesystem semantics in record schemas
- let cron/scheduled runs bypass Trigger API semantics

P1 should do for speed:
- implement local backends first
- keep interfaces narrow
- add cloud-portability tests only at contract level
- defer hosted issuer, remote anchoring, tenant RBAC, Cloudflare deployment,
  and cloud secrets broker to Phase 2

Execution plan Block 3 now requires:
- `KeyStore`
- `IdentityIssuer`
- `AuditWriter`
- `AuditAnchor`
- `AuditVerifier`
- `AuditExporter`
- alternate trust-domain tests
- bundle verification without local filesystem assumptions
- fake/in-memory keystore and audit anchor contract tests

## MCP Access Decision

The docs previously mentioned MCP, but not enough. It appeared as:
- `agent.mcp()`
- policy `mcp_server`
- MCP calls in dashboard
- AgentPaaS MCP server integration in Block 12

Missing piece fixed:
secure agent-to-MCP-server access.

PRD now separates two roles:
1. Agent as MCP client: governed agent calls local or remote MCP servers.
2. AgentPaaS as MCP server: coding tools call AgentPaaS MCP tools.

P1 supports the first role at a basic governed level:
- MCP servers must be declared in `mcp.yaml` and referenced from `policy.yaml`.
- Dynamic MCP tool discovery never auto-allows tools.
- Local MCP servers run only as daemon-managed child processes, sidecars, or
  explicitly declared local endpoints.
- Local MCP receives minimal environment, no raw secrets by default, and the
  same audit/redaction controls as agents.
- Remote HTTP MCP servers are reached only through the gateway egress path.
- Remote domains, ports, auth mode, and allowed tools are policy-reviewed.
- MCP auth follows MCP authorization for HTTP transports where available.
- P1 supports service/app credentials via the secrets broker.
- Interactive per-end-user authorization is P2.
- Every MCP tool call is audited with agent identity, run id, server id, tool
  name, input/output payload hashes, credential id if used, user subject if
  present, decision, and policy rule id.

Execution plan updates:
- Block 4 now includes MCP declarations, allowed tools, auth mode, egress
  binding, and validation edge cases.
- Block 6 SDK helper is now `agent.mcp(server_id, tool, input)`.
- Block 6 requires undeclared MCP server/tool denial and metadata-only audit.

## Verified User Access / Delegated User Access Decision

Workato Verified User Access was reviewed conceptually using official Workato
docs.

Understanding:
VUA lets an end user authenticate with their own credentials. Actions then run
with that user's permissions, not only with a shared service account. Workato
models this through parent connections plus runtime user connections.

AgentPaaS mapping:
- P1 trusted subject = agent/run identity.
- P2 trusted subjects = agent/run identity plus end-user identity and
  delegated credential context.
- P1 credentials = brokered service/app credentials and explicit direct leases.
- P2 credentials = runtime user connections, OAuth consent, per-user token
  vaulting, revocation, and user-level authorization checks at tool-call time.

Example P2 flow captured in PRD:
End user uploads receipt photo from phone -> AgentPaaS invokes receipt agent
with `user_subject` -> agent calls NetSuite MCP server -> broker obtains or
reuses end-user OAuth connection -> NetSuite action executes as that user ->
audit records user, agent, tool, and policy decision.

Decision:
Do not build VUA in P1. Preserve the primitives needed later:
- run ids
- parent/child run correlation ids
- triggering subject
- policy decision records
- audit events that can explain who/what caused each action

## Orchestration / Workflows Decision

Multi-agent workflows, loops, master/worker patterns, agent chaining, and
workflow orchestration are P2.

P1 should not become an orchestration product.

P1 product line:
"This agent can safely use approved tools."

P2 product line:
"This agent can safely act on behalf of this specific user, inside
orchestrated workflows."

## Current Open Items

Continue reviewing execution-plan blocks before implementation.

Recommended next item:
Budget enforcement wording.

Why:
The PRD currently says budget enforcement kills at exactly configured caps.
This likely needs softening for token/USD budgets because provider usage and
cost can arrive after a response. Wall-clock and max-iterations can be exact;
tokens/USD are often enforced with best-known usage, gateway-reported usage,
and post-hoc overage audit.

Other open items from previous checkpoint still relevant:
1. Cosign wording: "keyless local mode: signed by agent identity key" is
   confusing and needs a precise signing story.
2. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
3. Policy schema reference: required/optional fields, defaults, unknown
   fields, wildcard behavior, CIDR/private-network behavior, credential
   binding behavior, MCP declarations.
4. Telemetry/privacy: no telemetry without opt-in; define opt-in UX and
   payload.
5. P1 must-ship vs can-slip: plan is large; decide what can slip without
   weakening the wedge.
6. Security review packet: threat model, policy, SBOM, signed audit export,
   enforcement proof, limitations.
7. Dashboard specifics: `docs/status.md` generation and GitHub API/CLI
   dependency.
8. GitHub Project setup once private repo exists.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-11 13:11:32 PDT. We
are reviewing the execution plan block by block before implementation. Git is
now initialized and checkpoints are committed. Latest commit is
`a570d9c docs: define mcp and delegated access boundary`. Next, review budget
enforcement wording and update the PRD/execution plan if needed."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents.

Git status before writing this checkpoint was clean.
