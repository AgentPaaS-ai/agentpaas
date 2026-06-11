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
