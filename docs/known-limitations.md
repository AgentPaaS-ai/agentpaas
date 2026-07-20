# Known Limitations

AgentPaaS v0.2.3 is a local-first governed runtime for macOS with secure
agent sharing. This document records both accepted trade-offs and capability
gaps found during review. A listed gap must not be mistaken for a shipped
feature merely because a v0.3 plan exists to close it.

The Durable Routed Run work described in the roadmap targets v0.5.0 (with
v0.3.0 and v0.4.0 as cumulative intermediate releases) and is not part of
the shipped product yet. A planned block or execution-ready specification
must not be described as a current capability.

For the full security posture, see [threat-model.md](threat-model.md).
For workarounds and authoring guidance, see
[policy-reference.md](policy-reference.md) and
[how-enforcement-works.md](how-enforcement-works.md).

## Current Development Status

Blocks are numbered per the internal execution plan. Tracked status:

| Block | Description | Status |
|---|---|---|
| B17 | LLM egress auto-derivation and secure terminal secret ingestion | ✅ Complete |
| B18 | Manual testing suite (T1-T10 lifecycle), red-team smoke | ✅ Complete |
| B19 | AgentGateway policy integration (token budgets, rate limiting, gateway-native ingress) | ✅ Complete |
| B20 | Security claim closure — docs truth-sync, audit integrity, credential zero-visibility | ✅ Complete |
| B21 | Publisher identity, trust store, provenance schema | ✅ Complete |
| B22 | Bundle format, export, inspect | ✅ Complete |
| B23 | Verified install, consent, credential mapping, run integration | ✅ Complete |
| B24 | Fork, modify, redistribute: provenance chains | ✅ Complete |
| B25 | Hermes sharing UX, vulnerability closure, v0.2.x release hardening | ✅ Complete |
| B26 | Durable deployment, invocation, run, and workflow contracts/state foundation | ✅ Complete (unreleased v0.3.0) |
| B27 | SDK progress, checkpoint, and artifact protocol | ✅ Complete (unreleased v0.3.0) |
| B28 | Runtime portability and managed-PaaS feasibility gate | ⏭️ Planned for v0.3.0 |
| B29 | Agent runtime profiles, durable events, streaming, and efficiency | ⏭️ Planned for v0.3.0 |
| B30 | Long-running multi-turn execution and proof | ⏭️ Planned for v0.3.0-alpha.1 |
| B31 | Unified component catalog and constrained capability resolution | ⏭️ Planned for v0.3.0 |
| B32 | Secure A2A tasks, messages, events, and artifact transfer | ⏭️ Planned for v0.3.0 |
| B33 | AgentPaaS-container MCP services | ⏭️ Planned for v0.4.0 |
| B34 | Runtime-native sequential pipelines | ⏭️ Planned for v0.4.0 |
| B35 | Parent/child fan-out, fan-in, and collation | ⏭️ Planned for v0.4.0 |
| B36 | Model catalog, route compiler, and deterministic selector | ⏭️ Planned for v0.5.0 |
| B37 | Model-call failure classification and recovery | ⏭️ Planned for v0.5.0 |
| B38 | Shared workflow LLM spend budget and cost ledger | ⏭️ Planned for v0.5.0 |
| B39 | Integrated supervision, leases, guardrails, and operator control | ⏭️ Planned for v0.5.0 |
| B40 | Hermes authoring, packaging, deployment, and operations skills/UX | ⏭️ Planned for v0.5.0 |
| B41 | Golden proofs, hardening, and release | ⏭️ Planned for v0.5.0 |

## Durable Routed Run is not available in v0.2.3

The shipped v0.2.3 path has one configured provider/model/credential target.
It has not proven a long-running multi-turn agent and does not currently
provide:

- A durable asynchronous invocation path. The normal daemon path waits on one
  HTTP response and ordinarily times out at approximately 60 seconds.
- Immutable deployment versions, audited aliases, atomic promotion/rollback,
  deactivation, or exact-version pinning for independent cron/API invocation.
- Durable invocation idempotency and receiver-local per-deployment top-level
  workflow concurrency admission suitable for external schedulers.
- One authoritative time model. Fixed 60-second, 2-minute, 5-minute, and
  120-second layers conflict. Existing routed wall-clock/iteration request
  fields are not a contract to wire through: the v0.3 plan supersedes them with
  one accumulated workflow-active-time model that freezes only in fully
  `PAUSED`/`NEEDS_REPLAN` states.
- Policy-derived worker CPU/process limits. The worker bootstrap currently
  applies a fixed 30-second CPU rlimit and zero child-process allowance.
- A long-running proof. The manual test labelled “Long-Running Mixed-Egress”
  only repeated the weather HTTP+LLM case and did not test duration, turn
  count, restart, or resume.
- First-class conversation/session state. Repeated `agent.llm()` calls work,
  but the worker must explicitly carry prior context.
- Logical route selection across an approved local/cloud model pool.
- Automatic cross-model fallback.
- A shared hard USD LLM spend limit across workflow stages, parents, children,
  calls, and worker attempts.
- `agent.progress(...)`, semantic checkpoints, or safe worker resume.
- Explicit worker attempts, leases, fencing, or one bounded continuation.
- Operator cancel, safe-boundary pause/resume, provenance-linked restart, or
  active-time accounting that freezes only when fully paused/`NEEDS_REPLAN`.
- A scoped append-only way to raise current active-time, attempt-lease, or LLM
  spend ceilings before terminal exhaustion.
- Deterministic repeated-action/no-progress recovery guardrails.
- Complete cached-input/cache-write/subscription/local cost representation.
- A real production-wired AgentPaaS-container MCP registry/router. The current
  harness path can validate an allowlist but may return a synthetic result
  when no router is installed. Scheduled for fail-closed closure in B30 T00
  (pulled forward from B33 per the 2026-07-19 architecture audit, risk R5);
  after T00 the no-router path returns a typed not-enabled error in
  production, and B33 wires the real router in v0.4.
- A workflow controller, durable stage handoff, or a way to run a sequence of
  agents in separate containers without Hermes relaying each transition.
- Parent/child spawn and join. An agent cannot currently request bounded child
  runs, await their results, collate them, and continue.

Current model timeout, quota, authentication, context, or subscription
failures can therefore fail the worker. The v0.3–v0.5 B26–B41 plan closes
these gaps across three cumulative releases. B30 is the mandatory
long-running foundation gate and routing does not begin until B35 completes
the native multi-container patterns. No intermediate block should be
presented as shipped Durable Routed Run support.

## Network enforcement

### HTTP_PROXY only (no transparent proxy for non-HTTP)

Outbound policy enforcement routes agent HTTP/HTTPS traffic through the
gateway via `HTTP_PROXY` / `HTTPS_PROXY` environment variables. Non-HTTP
protocols (raw TCP, UDP, ICMP) are blocked by internal-network isolation,
not by deep packet inspection. A transparent proxy for all protocols is
deferred to P2.

### llm_provider_lock restricts agent.llm() only, not agent.http()

The `llm_provider_lock.allowed_endpoints` field restricts the LLM route path in the
gateway config — it applies only to `agent.llm()` calls (which route through the
harness LLM RPC and then the gateway's LLM route). An agent can call a non-approved
LLM provider via `agent.http("POST", "https://api.openai.com/...")` and it passes
egress if the domain is in the egress allowlist. This is defense-in-depth, not a
primary security control — egress policy is the primary restriction. To fully lock
LLM calls to a provider, also remove other LLM provider domains from the egress list.

## Runtime and harness

### Install requires --allow-unlocked-deps without uv.lock

When installing an agent bundle that has no `uv.lock` file, `agentpaas install --yes`
fails with "missing uv.lock requires --allow-unlocked-deps in non-interactive mode".
Pass `--allow-unlocked-deps` to allow the rebuild without locked dependencies.

### LLM integration uses a dedicated RPC over shared gateway egress

LLM calls (`agent.llm()`) route through the gateway as credentialed HTTP
egress to the provider's chat-completions endpoint (B15-T02, Option B).
The SDK uses the harness LLM RPC and provider adapters, while network policy
and credential application share the governed egress boundary. The provider,
model, and credential binding are configured in `agent.yaml`.
Pre-deployment validation via `agentpaas secret test <name>` verifies the
credential works before runtime use.

### Trigger server uses API-key auth for --expose

The trigger API supports API-key authentication via
`AGENTPAAS_TRIGGER_API_KEY` (B15-T04). In default (loopback) mode, no key
is required — any process on localhost can invoke an agent. When
`--expose` is used, API-key auth is mandatory. mTLS is deferred to P2.

## Supply chain and signing

### Cosign integration test is opt-in

The real cosign signing integration test is guarded by
`//go:build integration` and `AGENTPAAS_PACK_REAL_TOOLS=1`. CI does not run
it by default. Local manual runs with Docker + cosign + registry are
required to exercise real signing.

## Agent sharing

### TOFU (Trust On First Use) identity model

Publisher identity uses TOFU — the first time you install an agent from a
publisher, you verify their fingerprint out-of-band. There is no centralized
certificate authority. If a publisher's key is stolen, the attacker can sign
bundles with the same fingerprint. Report stolen keys to your team immediately.

### Rebuilt images differ from publisher's

When a receiver installs a bundle without `--prefer-image`, the Docker image
is rebuilt locally from source. The image digest will differ from the
publisher's (different build environment, layers, timestamps). The source
digest, lock signature, and policy digest are verified — but the image
itself is not byte-identical. Use `--prefer-image` with a cosign-signed
image for identical reproduction.

### No revocation (P2)

There is no revocation mechanism in P1. A stolen publisher key can sign
valid bundles until the receiver manually removes trust via
`agentpaas trust remove <fingerprint>`. Revocation infrastructure is P2.

### Plugin consent gate is client-side

The Hermes plugin's consent enforcement (no auto-approve, terminal-only
install) is a client-side policy. A user could bypass it by running
`agentpaas install` directly in their terminal without Hermes. This is by
design — the user owns their machine. The plugin prevents LLM-mediated
auto-approval, not user-mediated manual approval.

## Audit integrity

### Hash chain record deletion detection

Truncating the last N records from a JSONL audit file leaves a valid prefix
chain. Post-export tampering (deletion) cannot be detected on a second
machine without an external anchor. The audit checkpoint signing key is now
encrypted at rest (B15-T05 MC3, AES-256-GCM), and signed checkpoint export
provides tamper evidence when anchored externally. Runtime daemon chain is
authoritative during operation. Full external anchoring (transparency log
for checkpoints) is P2.

## Daemon lifecycle

### ReconcileAfterCrash does not clean gateways or networks

If the daemon crashes, orphaned gateway containers and Docker networks may
remain. `ReconcileAfterCrash` removes orphaned agent containers but not
gateway sidecars or per-run networks. Manual `docker ps` / `docker network ls`
cleanup may be needed. P2 extends reconciliation.

### maxConcurrentRuns and Docker resource multiplier

The daemon allows at most **3** concurrent agent runs (`maxConcurrentRuns`).
Each run provisions **two** containers (agent + gateway sidecar) and **two**
Docker networks (internal-only + egress). At the default limit that is up to
**6 containers** and **6 networks** while three runs are active.

On memory- or CPU-constrained machines (small Colima VMs, Docker Desktop with
low resource limits), avoid overlapping runs: start the next `agentpaas run` only
after the previous run finishes, or keep fewer than three runs active at once.
v0.3 plans receiver-local per-deployment `max_concurrent_runs` admission
inside the runtime-wide capacity, defaulting to one. Changing or distributing
the runtime-wide capacity remains later work.

## Production hardening (B15-T05)

### CAP_NET_ADMIN: capset drop, not init container (P2)

The agent container's iptables egress firewall is **defense-in-depth** — the
**primary** egress control is Docker network topology isolation (internal-only
network, no default route to the internet). The firewall applies additional
`OUTPUT DROP` rules to the container's own network stack and requires the
harness binary (PID 1, root) to program rules. After programming,
`DropNetAdminCapability()` removes CAP_NET_ADMIN from the process's effective,
permitted, and inheritable sets before the Python worker starts. Docker
`inspect` still shows NET_ADMIN in CapAdd, but the runtime process cannot use
it. If iptables is unavailable or any rule fails, the harness logs a warning
and continues — topology isolation remains the hard boundary. The full
init-container pattern (separate firewall-init container, `--net=container:`
namespace sharing) is P2. Verified by Docker integration test (B15-T05 MC5).

### RFC1918 tightened to gateway /16 (fail-closed, defense-in-depth)

The agent container firewall allows only the specific Docker bridge /16
subnet (derived from gateway IP), not all of RFC1918. If
`AGENTPAAS_GATEWAY_SUBNET` is unset (e.g. gateway IP discovery fails), no
broad allow is added — the firewall fails closed, relying on the specific
gateway IP allow + default OUTPUT DROP only. This is defense-in-depth; the
primary isolation is network topology.

### Rekor transparency log retry for production signing

Production image signing retries up to 3 times (2s/4s backoff) on transient
Rekor/transparency-log errors. Local registry refs skip tlog entirely.

### Checkpoint key encrypted at rest

The ECDSA P-256 audit checkpoint signing key is stored encrypted
(AES-256-GCM, passphrase via PBKDF2-HMAC-SHA256 100K iterations). Passphrase
sourced from macOS Keychain (preferred) or a 0600 passphrase file. Legacy
unencrypted DER keys are migrated on next regeneration.

## Observability

### Integer overflow in Stats() for very high CPU

`DockerRuntime.Stats()` casts uint64 CPU counters to int64. Very long-running
containers with extremely high cumulative CPU usage can overflow the delta
calculation. `computeCPUPercent` clamps to zero on negative deltas. P2 uses
overflow-safe uint64 arithmetic.

## Platform support

### No Linux support (macOS only)

P1 targets macOS with Docker Desktop or Colima. Linux-native `dockerd` support,
certified seccomp/AppArmor profiles, and Linux install paths are P2.

## Release gate

### Golden Loop is the current lifecycle gate

The v0.2.3 Golden Loop covers the shipped build, modify, provenance, export,
receive, inspect, install, and run lifecycle. It does not prove Durable Routed
Run.
Before v0.5.0 ships, B41 requires the Golden Loop to include a real
long-running multi-turn worker, cross-container MCP, native sequential
handoff, parent/child fan-out and collation, model recovery, one checkpoint
continuation, shared workflow LLM spend, fencing, live local/cloud
conformance, immutable deployment/alias lifecycle, Hermes-absent CLI/API
invocation, idempotency/concurrency rejection, operator
cancel/pause/resume/restart, limit amendment, and a fresh-install proof that
authors through Hermes but invokes independently.

## Demo recordings

Video or asciinema is not a current release guarantee. The authoritative
evidence is the automated/manual Golden Loop and its sanitized release
records. Any future recording must use the same fault labels and must not imply
correctness certification or measured savings.

## Additional P1 honesty statements

From the PRD threat model (§3.3):

- Container hardening, not a kernel 0-day sandbox (no gVisor/Kata in P1).
- Outbound DLP is fingerprint-based, not semantic.
- Red-team suite is a release smoke gate, not full adversarial research.
- Local mode trusts the developer's machine — we protect against the agent,
  not against the user.

## Related docs

- [How enforcement works](how-enforcement-works.md)
- [Threat model](threat-model.md)
- [Audit export and verification](audit-export.md)
