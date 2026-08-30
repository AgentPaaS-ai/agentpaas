# Known Limitations

AgentPaaS is a local-first governed runtime for macOS with secure agent
sharing. This document records accepted trade-offs and capability gaps in the
current release. A listed gap must not be mistaken for a shipped,
operator-ready feature merely because a design or specification exists.

For the full security posture, see [threat-model.md](threat-model.md).
For workarounds and authoring guidance, see
[policy-reference.md](policy-reference.md) and
[how-enforcement-works.md](how-enforcement-works.md).

## Cloud composition vs local run

Cloud workflow envelopes are the multi-step path (linear, fan-out join-all, choice, phone-call). Local `agentpaas run` of a multi-stage composition remains fail-closed.

A single agent does not need a workflow. Native HITL, join-any, for-each, wait/delay, spawn deeper than 1, and standalone agent-to-agent calls are not shipped.

## Cloud data-plane assurance debt

AgentPaaS Cloud runs on a Cloudflare-only data plane. On the default tier,
egress and topology are enforced by AgentPaaS control-plane carrier code
(per-instance egress plus the gateway boundary), proven correct per release
by review and adversary testing — not by the substrate. A carrier bug is a
potential bypass; there is no kernel/substrate backstop on the routing
decision as there is in the local sidecar model. This is the accepted
assurance debt of the Cloudflare-only plane. Substrate-enforced isolation
(kernel-enforced network policy, independent of our code) is reserved for
the high-assurance Kubernetes tier (paid, on-demand). Cloud scope limits:
HTTP/S egress only (no non-HTTP protocol governance), and no external
agent-to-agent federation. See threat-model.md §3.4.

## Long-running and routed runs

The current shipped path has one configured provider/model/credential target.
Remaining gaps not yet closed:

- Immutable deployment versions, audited aliases, atomic promotion/rollback,
  deactivation, or exact-version pinning for independent cron/API invocation.
- Durable invocation idempotency and receiver-local per-deployment top-level
  workflow concurrency admission suitable for external schedulers.
- One authoritative time model. Several fixed wall-clock layers currently
  conflict. A single accumulated workflow-active-time model is planned that
  freezes only in fully paused states.
- Policy-derived worker CPU/process limits. The worker bootstrap currently
  applies a fixed 30-second CPU rlimit and zero child-process allowance.
- First-class conversation/session state. Repeated `agent.llm()` calls work,
  but the worker must explicitly carry prior context.
- Logical route selection across an approved local/cloud model pool.
- Automatic cross-model fallback.
- A shared hard USD LLM spend limit across workflow stages, parents, children,
  calls, and worker attempts.
- `agent.progress(...)`, semantic checkpoints, or safe worker resume.
- Explicit worker attempts, leases, fencing, or one bounded continuation.
- Operator cancel, safe-boundary pause/resume, provenance-linked restart, or
  active-time accounting that freezes only when fully paused.
- A scoped append-only way to raise current active-time, attempt-lease, or LLM
  spend ceilings before terminal exhaustion.
- Deterministic repeated-action/no-progress recovery guardrails.
- Complete cached-input/cache-write/subscription/local cost representation.
- A production-wired AgentPaaS-container MCP registry/router. The current
  harness path can validate an allowlist but fails closed with a typed
  not-enabled error (`agentpaas_mcp_service_not_enabled`) when no router is
  installed. Explicit test mode (`AGENTPAAS_TEST_FAKE_MCP=1`) keeps a
  synthetic result for fixtures that opt in, mirroring
  `AGENTPAAS_TEST_FAKE_LLM`.
- Full multi-image packed pipeline stages with real handoff schemas.
- Parent/child spawn and join. The runtime remains fail-closed with
  `agentpaas_child_spawn_not_enabled` until spawn/join is implemented.

Model timeout, quota, authentication, context, or subscription failures can
therefore fail the worker. These gaps close across cumulative releases; no
intermediate state should be presented as shipped long-running routed-run
support.

## Network enforcement

### HTTP_PROXY only (no transparent proxy for non-HTTP)

Outbound policy enforcement routes agent HTTP/HTTPS traffic through the
gateway via `HTTP_PROXY` / `HTTPS_PROXY` environment variables. Non-HTTP
protocols (raw TCP, UDP, ICMP) are blocked by internal-network isolation,
not by deep packet inspection. A transparent proxy for all protocols is
deferred.

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
egress to the provider's chat-completions endpoint. The SDK uses the harness
LLM RPC and provider adapters, while network policy and credential application
share the governed egress boundary. The provider, model, and credential
binding are configured in `agent.yaml`. Pre-deployment validation via
`agentpaas secret test <name>` verifies the credential works before runtime
use.

### Trigger server uses API-key auth for --expose

The trigger API supports API-key authentication via
`AGENTPAAS_TRIGGER_API_KEY`. In default (loopback) mode, no key is required —
any process on localhost can invoke an agent. When `--expose` is used,
API-key auth is mandatory. mTLS is deferred.

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

### No revocation

There is no revocation mechanism in the current release. A stolen publisher
key can sign valid bundles until the receiver manually removes trust via
`agentpaas trust remove <fingerprint>`. Revocation infrastructure is planned.

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
machine without an external anchor. The audit checkpoint signing key is
encrypted at rest (AES-256-GCM), and signed checkpoint export provides tamper
evidence when anchored externally. The runtime daemon chain is authoritative
during operation. Full external anchoring (transparency log for checkpoints)
is planned.

## Daemon lifecycle

### ReconcileAfterCrash does not clean gateways or networks

If the daemon crashes, orphaned gateway containers and Docker networks may
remain. `ReconcileAfterCrash` removes orphaned agent containers but not
gateway sidecars or per-run networks. Manual `docker ps` / `docker network ls`
cleanup may be needed.

### maxConcurrentRuns and Docker resource multiplier

The daemon allows at most **3** concurrent agent runs (`maxConcurrentRuns`).
Each run provisions **two** containers (agent + gateway sidecar) and **two**
Docker networks (internal-only + egress). At the default limit that is up to
**6 containers** and **6 networks** while three runs are active.

On memory- or CPU-constrained machines (small Colima VMs, Docker Desktop with
low resource limits), avoid overlapping runs: start the next `agentpaas run` only
after the previous run finishes, or keep fewer than three runs active at once.

## Production hardening

### CAP_NET_ADMIN: capset drop, not init container

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
namespace sharing) is planned.

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
calculation. `computeCPUPercent` clamps to zero on negative deltas.
Overflow-safe uint64 arithmetic is planned.

## Platform support

### No Linux support (macOS only)

The current release targets macOS with Docker Desktop or Colima.
Linux-native `dockerd` support, certified seccomp/AppArmor profiles, and
Linux install paths are planned.

## Release gate

The Golden Loop covers the shipped build, modify, provenance, export,
receive, inspect, install, and run lifecycle. It does not yet prove
long-running routed runs.

## Demo recordings

Video or asciinema is not a current release guarantee. The authoritative
evidence is the automated/manual Golden Loop and its sanitized release
records. Any future recording must not imply correctness certification or
measured savings.

## Additional honesty statements

- Container hardening, not a kernel 0-day sandbox (no gVisor/Kata in the
  current release).
- Outbound DLP is fingerprint-based, not semantic.
- The red-team suite is a release smoke gate, not full adversarial research.
- Local mode trusts the developer's machine — we protect against the agent,
  not against the user.

## Related docs

- [How enforcement works](how-enforcement-works.md)
- [Threat model](threat-model.md)
- [Audit export and verification](audit-export.md)
