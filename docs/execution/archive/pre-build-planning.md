# Pre-Build Planning Checkpoints

These 14 checkpoints capture the planning review sessions from June 11-12, 2026, before any implementation code was written. They document the iterative review of the PRD and execution plan. The final state of these reviews IS the PRD and execution plan themselves (see ../planning/).

## Table of Contents

- **2026-06-11 12:22:44 PDT** — This checkpoint captures the decisions, reasoning, document edits, and context
- **2026-06-11 13:11:32 PDT** — This checkpoint captures the continuation session after the earlier
- **2026-06-11 15:50:46 PDT** — This checkpoint captures the continuation after the 2026-06-11 13:11:32 PDT
- **2026-06-11 22:05:18 PDT** — This checkpoint captures the Block 6 planning review after the Block 5
- **2026-06-11 22:23:31 PDT** — This checkpoint captures the Block 7 secrets broker planning review after the
- **2026-06-11 22:35:30 PDT** — This checkpoint captures the Block 8 packaging pipeline planning review after
- **2026-06-11 22:53:09 PDT** — This checkpoint captures the Block 9 Trigger API, events/webhooks, and cron
- **2026-06-11 23:02:26 PDT** — This checkpoint captures the Block 10 OTel pipeline and dashboard planning
- **2026-06-11 23:15:03 PDT** — This checkpoint captures the critical vision-alignment update after the Block
- **2026-06-11 23:23:36 PDT** — This checkpoint captures the Block 11 red-team scope review after the Block
- **2026-06-11 23:35:54 PDT** — This checkpoint captures the Block 12 integration review and edits. The review
- **2026-06-11 23:52:02 PDT** — This checkpoint captures the Block 13 install/docs/demo/release review and the
- **2026-06-12 00:03:18 PDT** — This checkpoint captures the Block 15 sequencing review. The review converted
- **2026-06-12 00:10:39 PDT** — This checkpoint captures the final whole-plan consistency pass requested after

---

# AgentPaaS Checkpoint

Date/time: 2026-06-11 12:22:44 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the decisions, reasoning, document edits, and context
from the current review session so a future session can continue without
reconstructing the conversation.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`

No implementation code has been built yet. This session updated the product
spec and execution plan only.

## Initial Review Outcome

The PRD was judged strong enough for founder/technical-cofounder review, with
a clear wedge:

AI coding tools make agent creation easy, but security/platform approval is
the blocker. AgentPaaS should own governed execution: signed packaging,
sandboxed runtime, default-deny egress, brokered credentials, and audit.

Main gaps identified:
- Gateway topology was ambiguous.
- Secret access guarantees were too broad.
- Budget enforcement wording may be too absolute for tokens/USD.
- Cosign "keyless local mode" wording may be confusing.
- First-run UX needs a precise happy path.
- Policy schema needs stricter required/optional/default behavior.
- Telemetry/privacy needs opt-in detail.
- P1 scope needs "must ship" vs "can slip" discipline.
- Security review packet should be explicit.

Only the first several areas have been edited so far. Budget enforcement,
cosign wording, first-run UX, policy schema details, telemetry, scope tiers,
and security review packet remain future review topics.

## Major Decisions Made

### 1. P1 Gateway Topology

Decision:
Use one logical agent deployment made of two containers:
- agent/harness container
- ingress/egress gateway sidecar container

Reasoning:
Putting gateway and untrusted agent code in the same Linux container/network
namespace weakens the core egress guarantee. A sidecar gives a cleaner
security story: the agent container has no default route and cannot share the
gateway network namespace.

Spec effect:
- PRD diagram now shows agent container plus gateway sidecar.
- Execution plan Block 5 now requires tests that agent and gateway do not
  share a network namespace.

Important nuance:
This is still "one logical deployable unit" from the user/CLI perspective.
The user should not need to think about the sidecar. The topology is an
implementation detail that strengthens enforcement.

### 2. No Shared Multi-Agent Gateway in P1

Question considered:
For multi-agent use cases, could multiple agent containers share one
ingress/egress gateway?

Decision:
Do not add this to P1. Keep one gateway sidecar per agent.

Reasoning:
Shared gateways are a natural P2 optimization, but they introduce additional
complexity:
- per-agent workload identity inside one shared gateway
- per-agent policy, audit, budget, and credential scope
- ingress routing among multiple agent containers
- explicit policy for agent-to-agent traffic

The measured gateway image overhead is small enough that simplicity wins in
P1.

Approx size findings:
- `cr.agentgateway.dev/agentgateway:v1.3.0-alpha.1` OCI manifest:
  - amd64 compressed total: about 32.0 MB
  - arm64 compressed total: about 31.1 MB
- Comparable proxy images:
  - Envoy distroless: roughly 31-35 MB compressed
  - Traefik v3: roughly 46-51 MB compressed
- Typical agent images:
  - minimal Python agent: roughly 80-250 MB compressed, dependency-dependent
  - minimal Node agent: roughly 60-180 MB compressed, dependency-dependent

Conclusion:
Five agents with five gateways might add only about 150-250 MB compressed disk
for gateway images, often less with shared layers. Runtime memory should be
observed later, but this does not justify P1 topology complexity.

### 3. P1 Container Tech

Decision:
P1 container substrate is Docker Engine API.

Supported P1 paths:
- Docker Desktop on macOS
- Colima using its Docker-compatible socket
- Linux `dockerd`

Not P1 gates:
- Podman
- containerd
- Kubernetes

Reasoning:
Docker Engine gives the fastest practical route to local-first adoption.
`RuntimeDriver` stays as an interface so Podman/containerd can be added
later without changing product shape.

Spec effect:
Added PRD section "Local runtime conventions (P1)" with Docker Engine as the
P1 substrate and Podman/containerd as future RuntimeDriver implementations.

### 4. Secrets Model

Original concern:
The PRD initially described direct secret leases into the agent container.
That means the agent can read the secret, and raw file reads cannot be
reliably audited.

Decision:
Default to brokered gateway-side credential injection. Keep direct leases as
an explicit compatibility escape hatch.

Default model:
- Secret is stored in OS keychain/libsecret.
- `policy.yaml` binds a keychain secret to an allowed egress rule via a
  credential ID and injection template.
- Agent sends an unsigned logical HTTP/LLM/MCP request to the gateway via SDK
  or configured proxy.
- Gateway validates destination/method/port/policy rule.
- Gateway injects the credential, such as an Authorization header.
- Gateway originates the upstream TLS request.
- Agent never receives the raw secret value.

Direct lease escape hatch:
- `file_lease` or `env_lease`
- Must be explicitly requested in `policy.yaml`
- Must include a reason
- Treated as disclosure to that run
- Raw file reads cannot be reliably audited in P1

Reasoning:
Brokered injection is a much stronger security story:
"By default, agents do not receive secrets. Secrets are injected only by the
gateway for approved destinations."

Important nuance:
The gateway cannot magically inject credentials into an already-encrypted
arbitrary TLS connection initiated by the agent. For brokered credentials,
the gateway must own/originate the upstream request after receiving a logical
request from the agent/SDK/proxy path. Raw TLS/socket attempts from the agent
get no brokered secret and have no direct internet route.

Spec effect:
- PRD `2.5 Secrets model` rewritten.
- PRD `2.5.1 Secret access guarantees` added/rewritten.
- PRD sample `policy.yaml` now uses:
  - `credentials.brokered`
  - `credentials.direct_leases`
- Execution plan Block 4 validates credential references and direct lease
  schema.
- Execution plan Block 6 removes `agent.secrets.get()` from normal SDK path
  and adds `agent.http(credential_id, ...)`.
- Execution plan Block 7 now tests brokered injection and proves brokered
  secrets are absent from agent env/proc/files/logs, Docker inspect, gateway
  logs, compiled configs, and image layers.
- Red-team Block 11 now includes brokered-secret discovery and wrong
  destination misuse.

### 5. GitHub Tracking and Dashboard

Decision:
Use GitHub only for planning/tracking:
- private GitHub repo
- GitHub Issues as work items
- Pull Requests as execution units
- GitHub Project "AgentPaaS P1" as dashboard
- generated `docs/status.md` for local/portable status

No Linear or Plane for now.

Required GitHub Project views:
- Board by status
- Table by block
- Roadmap by target week
- PR Review queue
- Security Gates

Required fields:
- Block
- Area
- Status
- Priority
- Model tier
- Gate command
- PR link
- Owner
- Target date

Required labels:
- `block:N`
- `area:api|runtime|policy|secrets|audit|docs`
- `kind:plan|impl|test|security|docs`
- `model:strong|model:cheap`
- `status:ready|blocked|review|done`

Temporary local-only fallback:
Use markdown issues under `docs/issues/` only before the private GitHub repo
exists. Move to GitHub before implementation PRs begin.

### 6. Cost-Effective LLM Execution Loop

Decision:
Use a strong model for planning/architecture and cheaper models for
execution/review where the scope is constrained.

Loop:
1. Planner pass: strong model, e.g. ChatGPT 5.5 high + Codex.
   Output: PR breakdown, contracts, invariants, tests, likely files,
   non-goals. This becomes GitHub issues.
2. Executor pass: cheaper model, e.g. DeepSeek flash-class.
   One issue at a time. No architecture decisions unless issue grants them.
3. Verifier pass: cheaper/different model.
   Receives issue, diff, test output. Must answer PASS or numbered defects.
4. Adversary pass: cheaper/different model for ordinary PRs, strong model for
   security PRs.
   Writes or suggests negative tests against the issue claims.
5. Escalation rule:
   Use the strong model only when API/security contracts change, executor
   fails the same gate twice, reviewer/executor disagree, or fix broadens
   scope beyond the issue.

PR sizing rule:
One behavioral claim per PR. Target under 500 changed production LOC plus
tests. Split larger PRs before coding.

PR merge rule:
No merge without green CI, reviewer PASS, adversary PASS, and refreshed
status dashboard.

## Block 1: Current Understanding

Block 1 is the repository and API foundation. It does not implement runtime
behavior yet.

What Block 1 now includes:
- monorepo layout
- local git initialized
- GitHub-ready issue templates
- PR template
- CODEOWNERS placeholder
- `docs/status.md`
- `scripts/update-status-dashboard.sh`
- `SECURITY.md`
- Apache-2.0 LICENSE
- Makefile targets:
  - `build`
  - `test`
  - `proto`
  - `e2e`
  - `redteam`
- GitHub Actions:
  - lint
  - test
  - race
  - osv-scanner
  - proto generation checks
- `api/trigger/v1/trigger.proto`
- `api/control/v1/control.proto`
- `buf` and grpc-gateway codegen
- committed generated code

Block 1 API/proto details added:
- stable proto packages:
  - `agentpaas.trigger.v1`
  - `agentpaas.control.v1`
- explicit `go_package`
- reserved field numbers/names when deleting
- Run fields:
  - `run_id`
  - `agent_name`
  - `agent_version`
  - `status`
  - `created_at`
  - `started_at`
  - `finished_at`
  - `error`
  - `budget_summary`
  - `policy_digest`
  - `image_digest`
- pagination:
  - `page_size`
  - `page_token`
  - `next_page_token`
- idempotency semantics:
  - same key + same payload returns original run
  - same key + different payload returns `ALREADY_EXISTS` / HTTP 409
- HTTP annotations for grpc-gateway routes
- `InvokeStream` documented as REST SSE

Block 1 edge/gate additions:
- codegen reproducible
- generated code up to date in CI
- buf breaking-change check catches field renumbering
- HTTP route table golden test
- SSE mapping documented
- `ListRuns` pagination tests
- idempotency replay and mismatch tests
- PR template includes Definition of Done
- status dashboard renders even before GitHub is connected
- success gate includes:
  - `make proto build test`
  - `scripts/update-status-dashboard.sh`
  - GitHub issues/Project or local fallback issues

## Block 2: Current Understanding

Block 2 builds the local operator shell: daemon lifecycle, CLI plumbing,
diagnostics, service files, and local environment conventions. It still does
not implement full agent runtime behavior.

What Block 2 now includes:
- `agentpaasd` lifecycle:
  - start
  - stop
  - status
- launchd plist generator
- systemd user unit generator
- local path layout under `~/.agentpaas`
  - directory mode 0700
  - `daemon.sock` mode 0600
  - `agentpaasd.pid`
  - `logs/`
  - `state/`
  - `config/`
  - `cache/`
  - `tmp/`
- unix socket gRPC server
- readiness handshake
- Control API stub handlers
- `agent` CLI wired to control RPCs
- daemon commands:
  - `agent daemon install`
  - `agent daemon uninstall`
  - `agent daemon start`
  - `agent daemon stop`
  - `agent daemon restart`
  - `agent daemon status`
- `agent version`
- `agent daemon status`
- `agent doctor` v0
- structured JSON logging with redaction from day one

Block 2 diagnostic/version requirements:
`agent version` and `agent daemon status` must show:
- CLI version
- daemon version
- proto version
- git commit
- OS/arch
- Docker context
- Docker API version

Block 2 `agent doctor` checks:
- Docker reachable
- current Docker context
- Docker Desktop / Colima / Linux dockerd detection where possible
- socket permissions
- ports 7700, 7717, 7718 free
- home directory permissions
- daemon readiness
- CLI/daemon proto compatibility

Block 2 dev/test overrides:
- `AGENTPAAS_HOME`
- `AGENTPAAS_SOCKET`
- `AGENTPAAS_DASHBOARD_PORT`
- `AGENTPAAS_TRIGGER_REST_PORT`
- `AGENTPAAS_TRIGGER_GRPC_PORT`

Block 2 edge cases:
- daemon not running -> CLI clear error + start hint
- daemon started but not ready -> CLI waits with timeout, then actionable
  error
- stale socket/pid/lock files -> auto-recover only after proving no live
  daemon owns them
- two daemons racing -> flock prevents
- broadened home/socket perms -> daemon refuses to serve
- daemon run as root -> refuses unless `--allow-root-for-test`
- SIGTERM -> graceful drain of in-flight RPCs
- service file generation deterministic and unit-tested
- lifecycle e2e runs where host supports user services
- Docker stopped/context missing/API too old -> doctor names exact issue
- port squatted -> doctor names process/port where OS permits
- log redaction masks high-entropy/API-key-looking values in CLI and daemon
  logs

Block 2 success gate:
- `agent doctor` exits 0 on healthy machine
- nonzero/actionable errors for induced failures:
  - Docker stopped
  - port squatted
  - bad socket perms
  - bad home perms
  - daemon not ready
  - CLI/daemon version mismatch
- `agent version` and `agent daemon status` print expected version/context
  fields
- service-unit golden tests pass on macOS and Linux
- redaction test proves planted secret-looking values do not appear in logs

## Files Changed This Session

### `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`

Major edits:
- Updated architecture diagram to show an agent container plus gateway
  sidecar.
- Changed `agentgateway embedded` to `agentgateway sidecar`.
- Clarified that agent container never shares the gateway network namespace.
- Rewrote secrets model to default to brokered gateway-side credential
  injection.
- Added direct leases as explicit compatibility mode only.
- Added secret access guarantees.
- Clarified gateway originates upstream TLS for brokered credentials.
- Updated data flow to include optional brokered credential injection.
- Updated event list to include `secret_injected`.
- Updated SDK contract:
  - normal path: `agent.llm()`, `agent.http()`, `agent.mcp()`
  - direct lease escape hatch: `agent.secrets.file()`
- Updated sample `policy.yaml`:
  - added `credentials.brokered`
  - added `credentials.direct_leases`
  - added egress rule `credential` references
- Updated threat model for brokered credentials.
- Updated red-team/security actions for brokered secret discovery and wrong
  destination misuse.
- Added `2.2.1 Local runtime conventions (P1)`:
  - Docker Engine API
  - `~/.agentpaas` layout
  - env var overrides
  - user-service daemon
  - root refusal
  - version/status fields
  - JSON log redaction

### `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`

Major edits:
- Added cost-effective LLM execution loop.
- Added PR contract template.
- Added GitHub tracking/dashboard process.
- Removed Linear/Plane after user chose GitHub only.
- Expanded repo layout:
  - `scripts/update-status-dashboard.sh`
  - `.github/workflows`
  - `.github/ISSUE_TEMPLATE`
  - `.github/pull_request_template.md`
  - `docs/status.md`
  - `docs/issues/`
- Expanded Block 1:
  - local git/GitHub setup
  - status dashboard
  - richer proto contracts
  - versioned packages
  - HTTP annotations
  - SSE mapping
  - pagination
  - idempotency semantics
  - generated-code policy
- Expanded Block 2:
  - local runtime paths
  - daemon commands
  - readiness handshake
  - version/status metadata
  - Docker context diagnostics
  - dev/test overrides
  - root refusal
  - stale socket/pid/lock cleanup
  - service-file test strategy
  - log redaction tests
- Updated Block 4:
  - brokered credential policy bindings
  - direct lease validation
  - compiled config must not contain raw secrets
- Updated Block 6:
  - normal SDK path uses brokered credentials
  - direct lease helper is explicit and discouraged
- Updated Block 7:
  - brokered credential injection implementation/test requirements
  - no raw value to agent container
  - gateway originates upstream TLS
  - audit events:
    - `secret_injected` with `visible_to_agent=false`
    - `secret_leased` with `visible_to_agent=true`
    - `secret_read` for SDK lease-helper reads
- Updated Block 11:
  - brokered secret discovery red-team test
  - wrong-destination brokered credential misuse test
  - direct-lease exfiltration treated separately

## Important Open Items For Future Sessions

Review and possibly update these next:

1. Block 3: Identity service + audit hash-chain.
   Need explain step by step and check for missing items.
2. Budget enforcement wording.
   Current PRD may say token/USD caps are exact; this likely needs softening
   because provider usage may arrive after response.
3. Cosign wording.
   "keyless local mode: signed by agent identity key" may be confusing.
   Decide exact signing story.
4. First-run happy path.
   Add exact developer journey and errors:
   install, doctor, init, pack, run, dashboard, first denied egress.
5. Policy schema reference.
   Required vs optional fields, defaults, unknown fields, wildcard behavior,
   CIDR/private network behavior, credential binding behavior.
6. Telemetry/privacy.
   No telemetry without opt-in, but GTM metrics depend on opt-in ping.
   Need opt-in UX and payload definition.
7. P1 must-ship vs can-slip.
   The plan is still large. Decide what can slip without weakening the wedge.
8. Security review packet.
   Define artifact exported for design partners/security reviewers:
   threat model, policy, SBOM, signed audit export, enforcement proof,
   limitations.
9. Dashboard specifics.
   Need decide how `docs/status.md` is generated and what GitHub API/CLI
   dependency is acceptable.
10. Exact GitHub Project setup.
   Need create issue templates, labels, project fields/views once repo exists.

## Current Recommendation For Next Session

Start with Block 3 review:
"Explain Block 3 step by step, identify overlooked items, then update PRD and
execution plan."

Before coding begins:
1. Finish review passes through all blocks.
2. Create private GitHub repo.
3. Convert execution-plan blocks into GitHub issues.
4. Create GitHub Project "AgentPaaS P1".
5. Add `docs/status.md` generation in Block 1.
6. Then begin implementation PRs.

## How To Continue In A Fresh Session

Paste or link this checkpoint and say:

"Continue from the AgentPaaS checkpoint dated 2026-06-11 12:22:44 PDT. We
are reviewing the execution plan block by block before implementation. Start
with Block 3: Identity service + audit hash-chain. Explain it clearly,
identify overlooked items, and update the PRD/execution plan if needed."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents.

This directory is not currently a git repository:
`git status` returned "not a git repository".

Therefore, file changes are local filesystem edits only, not committed.

---

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

---

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

---

# AgentPaaS Checkpoint

Date/time: 2026-06-11 22:05:18 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 6 planning review after the Block 5
network-topology checkpoint. The work remained in planning/spec review mode;
no implementation code has been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_22-05-18_PDT.md`

Latest committed checkpoint entering this review:
- `81adc8f docs: checkpoint block 5 network review`

## Block 6 Review Outcome

Block 6 is now scoped as the P1 harness and Python SDK contract. It is the
execution boundary for Python agent code: loading, invoking, budget
enforcement, failure reporting, developer-visible logs, and SDK-mediated
egress/tool calls.

Decisions locked:
- P1 SDK is Python first. Node SDK and Node packaging are deferred.
- Custom Dockerfile packaging is removed from P1. Python framework detection
  covers plain Python, LangGraph, and CrewAI only.
- Agent code loads once per container; invokes are serialized by default with
  `concurrency: 1`.
- `startup_timeout` covers import/readiness.
- `max_wall_clock` measures only run receive-invoke to run finishes, using a
  monotonic clock.
- `max_iterations` means agent turns. SDK-observed LLM/tool cycles count, and
  direct `agent.llm()` calls count if there is no higher-level loop.
- Token/USD enforcement uses gateway-reported best-known usage. Provider
  usage that arrives after termination is recorded as post-hoc overage in the
  audit trail.
- On budget breach: SIGTERM, 10s grace, SIGKILL, `BUDGET_EXCEEDED`, audit
  event.
- Brokered credentials are never returned to SDK callers.
- SDK exposes noncredentialed `agent.http(...)` and brokered
  `agent.http_with_credential(credential_id, ...)`.
- Gateway policy can require every outbound HTTP call to use a named
  credential binding via `egress.require_credential_binding: true`.
- Blocked egress/tool calls are visible to developers in CLI/dashboard logs
  with reason, run id, policy digest, and strict secret/payload redaction.
- MCP calls to undeclared server/tool pairs are denied before execution and
  audited; MCP input/output bodies are not logged, only hashes and metadata.

## P2 Deferrals Captured

Agent-level checkpoint/resume and half-done job recovery are explicitly
deferred to P2. P1 restarts failed runs from a fresh container and records
structured failure context outside the container. P2 must revisit long-running
or partially completed jobs, including idempotency, external side effects,
resume state, and operator-visible recovery decisions.

Automated correction/repair loops are also P2. P1 only needs enough structured
failure context to make the future loop possible: prompt/task/code failure
reasons, stderr/stdout pointers, policy decision ids, MCP/tool availability,
SaaS/upstream availability, and prior attempt context.

P2 may also use model/context-window health, performance degradation, and
turn-count guidance to decide whether to modify an agent and retry in a fresh
container until success.

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 6
- `agentpaas-execution-plan-v1.md` Block 8
- `agentpaas-prd-v4-master.md` §2.7 API surfaces
- `agentpaas-prd-v4-master.md` §2.8 Packaging pipeline
- `agentpaas-prd-v4-master.md` §2.9 agent.yaml + policy.yaml
- `agentpaas-prd-v4-master.md` §3.1 Threat model
- `agentpaas-prd-v4-master.md` §8 Success definition

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 7, the secrets broker. Pay special
attention to the local P1 direct-lease compatibility path versus the future
enterprise managed-secret posture for corporate employee machines behind VPN.

Other open items still relevant:
1. Cosign wording: "keyless local mode: signed by agent identity key" remains
   confusing and needs a precise signing story.
2. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
3. Policy schema reference: required/optional fields, defaults, unknown fields,
   wildcard behavior, CIDR/private-network behavior, credential binding
   behavior, MCP declarations.
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

"Continue from the AgentPaaS checkpoint dated 2026-06-11 22:05:18 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `81adc8f`. Next, review Block 7: Secrets broker,
including the P1 direct-lease compatibility path and the future enterprise
managed-secret posture."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents.

---

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

---

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

---

# AgentPaaS Checkpoint

Date/time: 2026-06-11 22:53:09 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 9 Trigger API, events/webhooks, and cron
planning review after the Block 8 packaging checkpoint. The work remained in
planning/spec review mode; no implementation code has been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_22-53-09_PDT.md`

Latest committed checkpoint entering this review:
- `b36f211 docs: clarify block 8 packaging pipeline`

## Block 9 Review Outcome

Block 9 is now scoped as the P1 callable/unattended execution surface. It
turns a packed local agent into something Codex, Hermes, Claude Code, the
AgentPaaS CLI, local apps, CI jobs, or cron can invoke without bypassing the
identity, policy, gateway, budget, secrets, audit, and observability controls
from earlier blocks.

Decisions locked:
- Trigger API serves gRPC on `:7718` and grpc-gateway REST on `:7717`,
  loopback by default.
- Trigger API requires AgentPaaS API-key or mTLS auth even on loopback.
  Loopback reduces network exposure but is not the authorization boundary.
- `--expose` refuses to start without an API key.
- AgentPaaS API keys are Trigger API credentials used to access an agent
  being tested or run locally. They are shown once, stored hashed, scoped by
  agent/action, revocable/rotatable, and audited by key id.
- REST CORS is deny-by-default. Browser-originated local requests receive no
  ambient trust from being on localhost.
- Stable caller ids are `api_key:<id>`, `spiffe:<subject>`,
  `system:cron:<agent>`, and `local_user:<uid>`.
- Rate limiting is token-bucket per caller.
- Idempotency is durable, survives daemon restart, and uses a 24-hour replay
  window to protect client retries from causing duplicate external effects.
- The canonical idempotency hash covers caller id, agent name, `agent.lock`
  digest, payload bytes, content type, and API version.
- Same idempotency key with the same canonical request returns the original
  `run_id`; same key with a different request returns 409. Expired keys return
  an explicit expired-key error.
- Invoke payloads are capped at 1 MiB by default. Larger inputs should be
  stored externally and passed by reference or future managed blob handle.
- `InvokeStream` exists for live progress in CLI, dashboard, and coding-tool
  integrations. REST uses SSE.
- P1 event types include `run_queued`, `run_started`, `run_log`, `run_span`,
  `egress_allowed`, `egress_denied`, `secret_injected`, `budget_warning`,
  `budget_exceeded`, `cancel_requested`, `run_canceled`, `run_failed`, and
  `run_succeeded`.
- SSE supports ordered event ids, heartbeat, and `Last-Event-ID` reconnect
  without duplicating terminal events.
- P1 supports URL webhooks only; local command hooks are deferred.
- Webhook deliveries are HMAC-signed with timestamp/replay-window protection,
  retried 3x with exponential backoff, and dead-lettered to audit.
- Webhook destinations are policy-checked egress.
- P1 cron uses 5-field syntax only, local timezone by default, and optional
  explicit timezone.
- DST behavior is explicit: nonexistent local time is skipped; repeated local
  time runs once.
- Missed-run policy defaults to `skip`; `catchup: 1` is explicit opt-in.
- Cron concurrency defaults to `forbid`; a tick is skipped and audited if the
  prior run is still active.
- `CancelRun` records `cancel_requested`, asks the harness/gateway path to
  stop gracefully, waits 30s, then force-stops the container if needed.
- Dashboard read-only loopback SSE may remain unauthenticated in P1, but
  exposed dashboard routes require API key, and mutating Trigger API calls
  require auth even on loopback.

## Tests / Gates Added To Plan

Block 9 now requires:
- API conformance suite generated from proto
- API-key lifecycle e2e: create, shown once, hashed storage, list, revoke,
  rotate, auth failure audit
- CORS/preflight tests proving random browser-originated localhost requests
  without API key are denied
- idempotency replay, conflict, expiry, and daemon-restart durability tests
- rate-limit tests with `Retry-After`
- malformed JSON and 1 MiB payload-limit tests
- SSE reconnect tests with `Last-Event-ID`, heartbeat, ordered event ids, and
  no duplicate terminal events
- webhook delivery, retry, HMAC, replay rejection, bad-signature rejection,
  policy-denied destination, and dead-letter audit tests
- cron tests for missed-run behavior, `catchup: 1`, DST skip/repeated-time
  behavior, and `concurrency_policy: forbid`
- cancelation e2e proving graceful then forced behavior and final audit
  outcome
- fuzz on REST JSON ingestion: 100k executions, 0 crashes
- tests proving cron and webhooks use the same policy/audit path as manual
  Invoke

Required Trigger/API audit events now include:
- `api_key_created`
- `api_key_revoked`
- `auth_failed`
- `invoke_accepted`
- `invoke_rejected`
- `idempotency_replayed`
- `idempotency_conflict`
- `rate_limited`
- `webhook_delivered`
- `webhook_dead_lettered`
- `cron_missed`
- `cron_skipped_concurrency`
- `cancel_requested`
- `cancel_graceful`
- `cancel_forced`

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 9
- `agentpaas-prd-v4-master.md` §2.7 API surfaces
- `agentpaas-prd-v4-master.md` §2.7.1 Trigger semantics
- `agentpaas-prd-v4-master.md` §2.10 Dashboard

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 10, the OTel pipeline and
dashboard. Focus on local data retention, SQLite/WAL behavior, dashboard auth
boundaries, SSE reconnects from Block 9, XSS/log escaping, run timeline
shape, and audit export/verify UX.

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

"Continue from the AgentPaaS checkpoint dated 2026-06-11 22:53:09 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `b36f211`. Next, review Block 10: OTel pipeline and
Dashboard, especially retention, SQLite/WAL behavior, dashboard auth
boundaries, SSE reconnects, XSS/log escaping, run timeline shape, and audit
export/verify UX."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.

---

# AgentPaaS Checkpoint

Date/time: 2026-06-11 23:02:26 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 10 OTel pipeline and dashboard planning
review after the Block 9 Trigger API checkpoint. This is a major checkpoint:
Blocks 1-10 have now been reviewed and tightened before implementation. The
work remained in planning/spec review mode; no implementation code has been
built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_23-02-26_PDT.md`

Latest committed checkpoint entering this review:
- `315b75a docs: clarify block 9 trigger semantics`

## Block 10 Review Outcome

Block 10 is now scoped as the local observability and review surface. It
turns runtime events, spans, logs, policy state, and audit records into a
dashboard that developers and security reviewers can use to understand what
ran, what was allowed or denied, what it cost, and how to export signed proof.

Core distinction locked:
- OTel/SQLite is for visibility, correlation, and dashboard speed.
- Canonical audit JSONL is the authoritative security/compliance record.
- SQLite audit indexes are derived/rebuildable and must not be presented as
  the source of truth.

Decisions locked:
- P1 uses an in-process OTLP collector writing to SQLite in WAL mode.
- Dashboard telemetry retention defaults to 7 days and is configurable.
- OTel retention applies only to dashboard traces/logs/metrics.
- Canonical audit JSONL is not pruned by dashboard retention; it is retained
  until an explicit future user retention/purge command.
- Agent, harness, and gateway logs are ingested as OTel log records for
  dashboard correlation.
- Daemon operational logs remain bounded structured JSON files under
  `~/.agentpaas/logs/` with rotation and redaction.
- Dashboard logs and daemon operational logs are not canonical audit records.
- Log and trace rendering treats agent-controlled text as hostile.
- HTML, binary/control characters, huge attributes, and sentinel secrets are
  escaped, truncated, and/or redacted before display.
- Cost estimates record provider, model, price-table version, token counts,
  and `estimated=true`.
- P1 ships a built-in price table; P2 allows user or tenant-modified price
  tables.
- Policy view shows both human git-file diff and normalized effective policy
  digest used for enforcement/audit.
- Audit search is labeled as an indexed view over canonical audit records.
- One-click signed audit export UX shows trust-anchor fingerprint, included
  sequence range, verification command, and verification result.
- Dashboard read-only loopback SSE may be unauthenticated in P1.
- Exposed dashboard routes require API key/session.
- Mutating Trigger API calls require auth even on loopback.
- Dashboard SSE reuses Block 9 ordered event id, heartbeat, and
  `Last-Event-ID` reconnect semantics.
- Dashboard security requires strict CSP, no inline JS, CSRF tokens on
  mutating routes, no runtime CDN, no API keys stored in browser localStorage,
  and no sampling/removal of security-relevant canonical audit events even
  when OTel telemetry is pruned.

## Tests / Gates Added To Plan

Block 10 now requires:
- Playwright e2e: launch agent, watch live run, see DENIED egress row, export
  audit, verify export
- Lighthouse performance score >= 90 local
- 10k-span run rendering with virtualized lists
- SSE reconnect behavior using `Last-Event-ID`
- SQLite WAL/read-pool behavior under concurrent writes
- SQLite migration, WAL checkpoint, vacuum/prune, and corruption recovery
- XSS escape test with planted `<script>` in agent output
- sentinel-secret redaction tests across logs, spans, trace attributes, and
  errors
- binary/control-character and huge-log/attribute truncation tests
- empty-state rendering for zero agents and zero runs
- policy diff tests for both git-file diff and normalized effective digest
- signed audit export/verify UX test
- accessibility and keyboard smoke test

## Files / Sections Updated

- `agentpaas-execution-plan-v1.md` Block 10
- `agentpaas-prd-v4-master.md` §2.10 Dashboard

## Current Open Items

Recommended next item:
Continue block-by-block review with Block 11, the red-team suite. Focus on
whether the attacker library exercises the real pack/run path, whether every
prior security promise has a malicious fixture, and whether the suite is
stable enough to become a permanent CI release gate.

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

"Continue from the AgentPaaS checkpoint dated 2026-06-11 23:02:26 PDT. We
are reviewing the execution plan block by block before implementation. Git is
initialized and checkpoints are committed. Latest commit should be the
checkpoint closeout after `315b75a`. Next, review Block 11: Red-team suite,
especially whether every attacker runs through the real pack/run path,
whether the attack library covers prior security promises, and whether the
suite is suitable as a permanent CI release gate."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.

---

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

---

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

---

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

---

# AgentPaaS Checkpoint

Date/time: 2026-06-11 23:52:02 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 13 install/docs/demo/release review and the
follow-up clarification that Phase 1 is not the full customer-facing
commercial release. P1 is now explicitly scoped as a macOS-first OSS/demo
delivery used to prove the wedge, create credible demos, publish verifiable
open-source artifacts, and gather design-partner feedback without telemetry.
P2 is the first customer-facing release track.

The work remained in planning/spec review mode; no implementation code has
been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-11_23-52-02_PDT.md`

Latest committed checkpoint entering this review:
- `a1622f6 docs: close block 12 integration review`

## Block 13 Review Outcome

Block 13 now frames v0.1.0 as a trusted OSS/demo release rather than a
customer-facing commercial launch.

Decisions locked:
- P1 is macOS-first.
- P1 primary install path is Homebrew for darwin/arm64 and darwin/amd64.
- Linux native packages, Linux CI release certification, deb/rpm packaging,
  Windows/WSL2 docs, and Linux libsecret/systemd support move to P2 unless a
  design partner creates a hard pre-launch requirement.
- Docker Desktop or Colima is an explicit prerequisite for the P1 README
  activation gate.
- Homebrew installs the binary but does not silently start background
  services.
- First run is explicit through `agent doctor`, then `agent setup launchd` or
  documented `brew services start agentpaas`.
- Release verification follows Sigstore keyless best practice using GitHub
  Actions OIDC, with a documented `cosign verify-blob` command and easier
  `agent verify-release` helper.
- P1 includes a macOS air-gapped/offline verification bundle.
- P1 has zero telemetry: no analytics, update checks, crash reports, usage
  pings, or automatic adoption metrics leave the machine.
- Any future telemetry must be separate, explicit opt-in, and absent from the
  P1 demo path.
- P1 requires one recorded Block 12 differentiation demo; the secret-brokered
  SaaS action and agentic repair loop are stretch launch demos.
- P2 is the first customer-facing release track: Linux certification,
  fleet/team management, enterprise packaging, support posture, and commercial
  observability.

## Added To Block 13

Block 13 now includes:
- macOS-first release scope.
- No blind `curl|bash` installer posture.
- explicit first-run `agent doctor` checks for Docker Desktop/Colima,
  keychain, loopback ports, daemon socket permissions, release signature
  status, and dashboard port.
- user-visible launchd setup semantics.
- goreleaser darwin artifacts, checksums, SBOMs, provenance, and cosign
  signatures.
- docs for quickstart, policy, secrets, enforcement topology, threat model,
  known limitations, audit verification, privacy/telemetry, Claude Code plugin
  setup, Hermes native MCP setup, and demo scripts.
- README requirements: 60-second story, `make redteam-smoke` containment proof,
  zero telemetry statement, prerequisites, and known limitations link.
- macOS offline bundle with signed binaries, checksums, SBOMs, container
  images, policy/demo fixtures, and verification instructions.
- docs/release CI: broken-link check, command-snippet smoke scripts, README
  quickstart smoke, release-artifact verification matrix, screenshot/asciinema
  freshness check, and docs issue filing for clean-machine deviations.
- uninstall and upgrade edge cases, including daemon-state migration rollback
  or manual recovery path.

## PRD Alignment

The PRD now mirrors the Block 13 decisions:
- Product definition says P1 is OSS/demo proof and P2 is the first
  customer-facing release track.
- P1 non-goals exclude Linux-native, Windows-native, and WSL2 support.
- Local runtime conventions are macOS Docker Desktop/Colima only in P1.
- launchd is P1; systemd is P2.
- macOS Keychain is P1; Linux libsecret is P2.
- P1 hardening is macOS Docker Desktop/Colima container hardening; certified
  Linux seccomp/AppArmor profiles are P2.
- GTM metrics no longer depend on automatic telemetry.
- Phase 1 success criteria now require two clean macOS volunteer machines, P1
  redteam-smoke 6/6, post-install Claude Code and Hermes deploy demos under
  10 minutes, and offline bundle verification.

## Current Open Items

Recommended next item:
Decide whether the current planning pass is complete enough to begin Block 1
implementation, or whether to do one final must-ship/can-slip pass across the
whole execution plan before coding.

Other open items still relevant:
1. First-run happy path: install, doctor, init, pack, run, dashboard, first
   denied egress.
2. Policy schema reference: required/optional fields, defaults, unknown
   fields, wildcard behavior, CIDR/private-network behavior, credential
   binding behavior, MCP declarations.
3. Security review packet: threat model, policy, SBOM, signed audit export,
   enforcement proof, limitations.
4. Dashboard specifics: `docs/status.md` generation and GitHub API/CLI
   dependency.
5. GitHub Project setup once private repo exists.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-11 23:52:02 PDT. We
are reviewing the execution plan before implementation. Git is initialized
and checkpoints are committed. Latest commit should be the checkpoint closeout
after Block 13. P1 is now explicitly macOS-first OSS/demo delivery with zero
telemetry; P2 is the first customer-facing release track. Next, decide whether
to start Block 1 implementation or do a final must-ship/can-slip pass across
the whole plan."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.

---

# AgentPaaS Checkpoint

Date/time: 2026-06-12 00:03:18 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 15 sequencing review. The review converted
the old rough sequencing note into a founder calendar and execution-control
block, renumbered the execution plan into 15 blocks, and aligned the PRD with
the aggressive P1/P2 timing.

The work remained in planning/spec review mode; no implementation code has
been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-12_00-03-18_PDT.md`

Latest committed checkpoint entering this review:
- `249c23f docs: close block 13 release scope review`

## Block 15 Review Outcome

Block 15 is now the founder calendar and execution-control block. It is not a
product feature; it governs build order, parallelism, timing, and what cannot
be cut once implementation starts.

Decisions locked:
- The execution plan now has 15 blocks: 14 product/release build blocks plus
  Block 15 as the calendar/control block.
- Former Block 10.5 is now Block 11: Agentic operator contract.
- Former Block 11 is now Block 12: P1 red-team smoke gate.
- Former Block 12 is now Block 13: MCP server + Claude Code plugin + Hermes
  skill.
- Former Block 13 is now Block 14: install path, docs, demo, and v0.1.0
  release.
- P1 target is week 4/5.
- P2 customer-facing release track target is four additional weeks after P1.
- Once implementation starts, P1 blocks do not silently slip. If the calendar
  becomes impossible, stop and explicitly rescope before continuing.
- No Block 13 integration items are skipped for P1.
- Extra demo recordings beyond the minimum launch video are launch-asset
  prioritization, not skipped product functionality.
- CrewAI is an input framework, not an AgentPaaS product feature. P1 support
  means generated CrewAI Python projects pack/run through the generic Python
  harness; AgentPaaS does not build CrewAI authoring, orchestration, or a
  custom CrewAI adapter in P1.
- Node SDK remains deferred and is not part of the P1 gate.

## Founder Calendar

P1 rough founder calendar:
- **Week 1:** Blocks 1-3 green. Repo/protos/CI, daemon/CLI skeleton, identity
  and audit spine.
- **Week 2:** Blocks 4-8 green. Policy compiler, macOS Docker Desktop/Colima
  fenced runtime, harness/Python SDK, secrets broker, packaging.
- **Week 3:** Blocks 9-12 green. Trigger API/events/cron, dashboard, operator
  contract, redteam-smoke.
- **Week 4:** Blocks 13-14 green. MCP server, Claude Code plugin, Hermes
  native MCP skill, install/docs/demo/release path.
- **Week 5:** P1 release buffer only: bug fixes, volunteer clean-machine
  verification, offline bundle verification, video/asciinema polish, v0.1.0
  tag.

P2 rough calendar:
- **Week 6:** Linux certification track.
- **Week 7:** Customer-facing control-plane foundations.
- **Week 8:** Commercial observability and opt-in telemetry.
- **Week 9:** P2 customer release hardening.

## PRD Alignment

The PRD now mirrors the Block 15 decisions:
- GTM sequence uses Weeks 1-4/5 for P1 and Weeks 6-9 for P2.
- P1 is described as macOS-first OSS/demo delivery, not the full
  customer-facing release.
- P2 is described as the customer-facing release track.
- The MCP integration reference points to Block 13 and uses `agentpaas_*` tool
  names.
- CrewAI is described as a generated Python input shape handled by the generic
  Python harness, not a custom adapter.
- Phase 1 success criteria require a CrewAI-generated Python project, a
  LangGraph project, and a plain-Python agent to pack/run through the generic
  Python harness.

## What Is Left

Planning/spec work remaining before implementation:
1. Decide whether to start Block 1 immediately or do one final whole-plan
   consistency pass.
2. Create/confirm the GitHub repo and Project once the private repo exists.
3. Convert the 15 blocks into implementation issues with acceptance criteria.
4. Confirm local development prerequisites for P1 execution: macOS host,
   Docker Desktop and/or Colima, Homebrew, cosign, Go toolchain, and browser
   test tooling.

Implementation work remaining:
1. Build Blocks 1-14 in order under Block 15's founder calendar.
2. Keep every block gated by its Makefile command and negative/security tests.
3. Produce P1 release artifacts, docs, demos, offline bundle, volunteer
   clean-machine validation, and v0.1.0 tag.
4. Begin P2 only after P1 ships or is explicitly rescoped.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-12 00:03:18 PDT. We
finished the planning/spec block review. Git is initialized and checkpoints
are committed. Latest commit should be the checkpoint closeout after Block 15.
P1 is macOS-first OSS/demo delivery with zero telemetry and a week 4/5 target;
P2 is the first customer-facing release track over the following four weeks.
Next, decide whether to start Block 1 implementation immediately or run one
final whole-plan consistency pass."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.

---

# AgentPaaS Checkpoint

Date/time: 2026-06-12 00:10:39 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the final whole-plan consistency pass requested after
the Block 15 sequencing review and before Block 1 implementation.

The review covered:
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- latest checkpoint context, especially
  `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-12_00-03-18_PDT.md`

Latest committed checkpoint entering this review:
- `260e56f docs: close block 15 sequencing review`

## Review Outcome

The plan is now internally consistent and implementation-ready after the small
documentation fixes in this checkpoint.

Locked strategy remains intact:
- P1 is macOS-first OSS/demo delivery, not the full customer-facing commercial
  release.
- P1 has zero telemetry.
- P1 target is week 4/5.
- P2 is the first customer-facing release track over the following four weeks.
- Blocks 1-14 are product/release build blocks.
- Block 15 is sequencing, founder calendar, and execution control.
- Former Block 10.5 is now Block 11.
- Block 12 is redteam-smoke.
- Block 13 is MCP server + Claude Code plugin + Hermes native MCP skill.
- Block 14 is install/docs/demo/release.
- No Block 13 integration items are skipped for P1.
- CrewAI is only a generated Python input shape through the generic Python
  harness, not a custom adapter.
- Node SDK, Linux, WSL2, deb/rpm, systemd, libsecret, customer-facing
  fleet/team/commercial features, and telemetry remain P2 or later.

## Findings Fixed

1. **Gate command ambiguity before implementation.**
   The execution plan said every gate command lives in the Makefile, but several
   block success gates used prose or raw commands instead of canonical Make
   targets, and Block 1 still listed `redteam` while later release gates use
   `redteam-smoke`.

   Fix:
   - Added §0.2.2a with canonical `make blockN-gate` wrappers for Blocks 1-15.
   - Updated every block success gate to cite its canonical wrapper.
   - Clarified that `make block12-gate` wraps `make redteam-smoke`.
   - Clarified Block 15's pre-Block-1 docs-only gate and post-Block-1
     `make block15-gate`.
   - Updated the Phase 1 Definition of Done to require command exit 0 plus
     recorded evidence.

2. **Stale P1 Linux diagnostic wording.**
   Block 2's doctor prompt still grouped Docker Desktop, Colima, and Linux
   `dockerd` together in a way that could be misread as a P1 Linux support gate.

   Fix:
   - Reworded Block 2 so Docker Desktop/Colima are the macOS P1 check and Linux
     `dockerd` is reported as P2/not-a-P1-gate.

3. **Stale Node hint in the P1 architecture diagram.**
   The PRD component diagram still said user code execution was "Py / Node",
   while Node SDK/package support is explicitly deferred.

   Fix:
   - Changed the diagram to "Python P1".

4. **Installer trust wording.**
   The PRD technology table still said "brew/curl install", while Block 14
   explicitly rejects blind `curl|bash`.

   Fix:
   - Changed the table to "simple Homebrew/macOS install".

5. **MCP configuration shape inconsistency.**
   PRD §2.7.2 said MCP servers were declared in `mcp.yaml`, while the normative
   policy schema and examples use `policy.yaml` with `mcp_servers`.

   Fix:
   - Reworded the MCP client policy text to declare local/remote MCP servers in
     `policy.yaml`'s `mcp_servers` section.

6. **Stale PRD red-team cross-reference.**
   PRD §8 pointed to `§3.2.4`, but the red-team statement lives in §3.2 item 4.

   Fix:
   - Corrected the reference to `§3.2 item 4`.

## Review Notes

No blocking inconsistencies remain in the active PRD or execution plan.

Remaining search hits were reviewed and are intentional:
- "Former Block 10.5 is now Block 11" appears only as a renumbering note or as
  PRD subsection `2.10.5`, not as live block numbering.
- "monthly" appears only in post-P2 channel work.
- "weekly-active-runtime" appears only as a future opt-in telemetry note outside
  the P1 launch path.
- Linux, WSL2, deb/rpm, systemd, libsecret, fleet/team/commercial features, and
  telemetry are consistently marked P2 or later.
- Older checkpoint files before 00:03 may contain historical pre-renumbering
  language, but the latest checkpoint and active plans supersede them.

## Verification

Commands run:
- `git log --oneline -5` confirmed `260e56f docs: close block 15 sequencing review`
  as HEAD entering the pass.
- `rg` scans for stale block numbering, Linux P1 wording, redteam 10/10,
  telemetry assumptions, generic MCP tool names, Node/Linux/P2 scope drift, and
  Makefile gate consistency.
- `git diff --check` passed before this checkpoint was written.

Next concrete Block 1 action:
- Create the monorepo skeleton and Makefile namespace, starting with
  `make block1-gate` wrapping `make proto build test`, then author the trigger
  and control protobuf contracts from Block 1.
