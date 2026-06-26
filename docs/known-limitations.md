# Known Limitations (P1)

AgentPaaS v0.1.0 is a local-first governed runtime for macOS. These are
accepted P1 limitations documented during adversary review. They are
intentional trade-offs, not bugs.

For the full security posture, see [threat-model.md](threat-model.md).
For workarounds and authoring guidance, see
[policy-reference.md](policy-reference.md) and
[how-enforcement-works.md](how-enforcement-works.md).

## Network enforcement

### HTTP_PROXY only (no transparent proxy for non-HTTP)

Outbound policy enforcement routes agent HTTP/HTTPS traffic through the
gateway via `HTTP_PROXY` / `HTTPS_PROXY` environment variables. Non-HTTP
protocols (raw TCP, UDP, ICMP) are blocked by internal-network isolation,
not by deep packet inspection. A transparent proxy for all protocols is
deferred to P2.

## Runtime and harness

### No real LLM integration

The harness `agent.llm()` call returns a hardcoded stub response
(`"agentpaas fake llm response"`). Budget tracking and audit plumbing work,
but no real LLM provider API is called. Agents requiring live LLM access
need direct integration work beyond P1.

### Trigger server has no authentication

The local trigger API is loopback-only in P1. There is no API key or mTLS
on trigger endpoints. Any process on localhost can invoke an agent. P2 adds
authentication when `--expose` is supported.

## Supply chain and signing

### Cosign integration test is opt-in

The real cosign signing integration test is guarded by
`//go:build integration` and `AGENTPAAS_PACK_REAL_TOOLS=1`. CI does not run
it by default. Local manual runs with Docker + cosign + registry are
required to exercise real signing.

## Audit integrity

### Hash chain record deletion is undetectable

Truncating the last N records from a JSONL audit file leaves a valid prefix
chain. Without an external signed checkpoint anchor, post-export tampering
(deletion) cannot be detected on a second machine. The daemon chain is
authoritative during runtime; signed checkpoint export is P2.

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
low resource limits), avoid overlapping runs: start the next `agent run` only
after the previous run finishes, or keep fewer than three runs active at once.
Configurable concurrency limits are planned for P2.

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

### Volunteer gate (14C) not yet run

The Block 14C release gate requires two volunteers (not the maintainer) to
reach a governed agent in under 15 minutes following the README/quickstart,
with evidence committed under `docs/release/volunteer-evidence/`. This manual
gate has not been completed for v0.1.0 until volunteer evidence is collected.

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