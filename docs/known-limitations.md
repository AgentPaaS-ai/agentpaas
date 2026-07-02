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

### LLM integration routes through gateway egress (no special-casing)

LLM calls (`agent.llm()`) route through the gateway as credentialed HTTP
egress to the provider's chat-completions endpoint (B15-T02, Option B).
There is no dedicated LLM code path — it is sugar over
`agent.http_with_credential`. The provider, model, and credential binding
are configured in `agent.yaml`. Pre-deployment validation via
`agentpaas secret test <name>` verifies the credential works before baking
into a container.

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
low resource limits), avoid overlapping runs: start the next `agent run` only
after the previous run finishes, or keep fewer than three runs active at once.
Configurable concurrency limits are planned for P2.

## Production hardening (B15-T05)

### CAP_NET_ADMIN: capset drop, not init container (P2)

The agent container's iptables egress firewall requires the harness binary
(PID 1, root) to program rules. After programming, `DropNetAdminCapability()`
removes CAP_NET_ADMIN from the process's effective, permitted, and inheritable
sets before the Python worker starts. Docker `inspect` still shows NET_ADMIN
in CapAdd, but the runtime process cannot use it. The full init-container
pattern (separate firewall-init container, `--net=container:` namespace
sharing) is P2. Verified by Docker integration test (B15-T05 MC5).

### RFC1918 tightened to gateway /16 (fail-closed)

The agent container firewall allows only the specific Docker bridge /16
subnet (derived from gateway IP), not all of RFC1918. If
`AGENTPAAS_GATEWAY_SUBNET` is unset (e.g. gateway IP discovery fails), no
broad allow is added — the firewall fails closed, relying on the specific
gateway IP allow + default OUTPUT DROP only.

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

### Volunteer gate (14C) not yet run

The Block 14C release gate requires two volunteers (not the maintainer) to
reach a governed agent in under 15 minutes following the README/quickstart,
with evidence committed under `docs/release/volunteer-evidence/`. This manual
gate has not been completed for v0.1.0 until volunteer evidence is collected.

## Demo recordings

### No demo video or asciinema (B15 scope)

End-to-end demo video and asciinema recordings are not part of the P1 doc set.
They are planned for Block 15 (manual testing) and will be produced during the
assisted use-case assessment. This is a known documentation gap, not a code defect.

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