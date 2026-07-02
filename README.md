# AgentPaaS

**AgentPaaS is an open-source, governed runtime for running AI agents safely on
your own machine. It enforces policy on every action an agent takes — so only
the actions you prescribe are carried out, securely, without being tampered
with — and it packages agents as signed, verifiable artifacts you can share
without worrying about supply-chain attacks.**

Local-first. No cloud. No telemetry. No signup.

---

## Why?

AI-generated agents are powerful, but running them is dangerous. An agent is
code that can call tools, reach the network, and touch your credentials. Today,
the moment you hand an AI-written agent a live API key and let it call out to
the internet, you have no boundary between "what the agent was supposed to do"
and "what it actually does."

AgentPaaS exists to close that gap. It is the guardrail layer between an agent
and the rest of your system.

- **Prescribed actions only.** You write a `policy.yaml` that declares exactly
  which domains, ports, credentials, and tools an agent may use. Everything else
  is denied by default. The agent cannot deviate.
- **Secure execution.** Every agent run is isolated in a hardened container with
  no direct network route, no root, a read-only root filesystem, and every Linux
  capability dropped. Credentials are brokered at the gateway — the agent never
  sees raw secrets.
- **Tamper-proof.** Every action — every allowed or denied egress, every
  credential use, every tool call — is recorded in a hash-chained, signed audit
  trail. Tampering breaks the chain and is immediately detectable.
- **Sharable without supply-chain risk.** Agents are packed as signed images
  with an SBOM and vulnerability scan. Recipients can verify a package's
  integrity and provenance before running it, the same way you can verify the
  `agentpaas` binary you install.

If you are building, sharing, or accepting AI agents from any source — a coding
assistant, a teammate, a marketplace — AgentPaaS is the runtime that makes them
safe to actually run.

---

## How It Works

Each agent run gets two containers on isolated Docker networks:

```
┌─────────────────────────────────────────────────────┐
│  Docker Host                                        │
│                                                     │
│  ┌──────────┐       ┌───────────┐                   │
│  │  Agent   │───────│  Gateway  │──────── Internet  │
│  │ (no net) │ proxy │ (dual-homed)│                 │
│  └──────────┘       └───────────┘                   │
│   internal net        egress net                    │
│   (no internet)       (internet)                    │
└─────────────────────────────────────────────────────┘
```

1. **Agent container** — attached only to an internal bridge. No route to the
   internet. DNS resolves only through the gateway stub. Outbound HTTP/HTTPS is
   forced through the gateway via `HTTP_PROXY`.
2. **Gateway sidecar** — dual-homed on the internal bridge and a dedicated
   egress network with internet access. It compiles `policy.yaml` into
   enforcement rules. Brokered credentials are injected here; raw secrets never
   reach the agent.

Egress, ingress, MCP tool calls, and credential use all pass through the
gateway. Denials are recorded in the signed audit chain.

Deep dive: [docs/how-enforcement-works.md](docs/how-enforcement-works.md)

---

## Security Measures

AgentPaaS is built defense-in-depth. Every layer below is implemented and
verified in P1.

### Network containment
- **Default-deny egress.** No domain, port, or host is reachable unless
  `policy.yaml` explicitly allows it.
- **No direct internet route.** The agent container sits on an internal-only
  network. All traffic must traverse the gateway.
- **DNS stub.** The agent's DNS resolves only through the gateway, preventing
  DNS-based exfiltration.
- **Domain fronting defense.** The gateway cross-checks SNI / Host header /
  DNS answer; mismatches are denied and audited.

### Credential safety
- **Brokered credentials.** Secrets live in the OS keychain (macOS Keychain),
  never in the container image or environment. The gateway injects them
  server-side at the moment of an allowed call.
- **No raw secrets to the agent.** Agent code never sees key material.
- **Credential revocation.** The broker denies future requests for a credential
  the moment you revoke it, and denies credentialed redirects.
- **Pre-deployment validation.** `agentpaas secret test <name>` makes a trivial
  authenticated call to the target service before you ever pack the agent, so a
  bad key fails fast and clear.

### Container hardening
- **Non-root.** Agent runs as UID 64000.
- **Read-only rootfs**, no shell, no new privileges.
- **All capabilities dropped.** `CAP_NET_ADMIN` is removed from the agent
  process via a PID-1 capset-drop, verified end-to-end with a Docker test that
  proves the agent cannot flush or modify iptables.
- **seccomp** default profile, `pids` / memory / cpu caps.

### Tamper-evident audit
- **Hash-chained JSONL.** Every audit event links to the previous via a
  cryptographic hash. Tampering breaks the chain.
- **Checkpoint signatures.** The daemon signs periodic checkpoints with an
  ECDSA P-256 key that is itself **encrypted at rest** (AES-256-GCM,
  PBKDF2-HMAC-SHA256, 100K iterations; passphrase from env → Keychain → file).
- **Signed export.** `agentpaas audit export` produces a signed manifest;
  `agentpaas audit verify` checks it independently.
- **Local head anchor.** The current chain head can be anchored to your local
  keychain so rollback is detectable.

### Supply-chain integrity (for your agents and for AgentPaaS itself)
- **Per-agent SBOM** on every `agentpaas pack`.
- **Dependency locking.** Python installs use locked `uv` installs only — no
  silent resolution of typosquatted packages.
- **Vulnerability scan.** `osv-scanner` advisories surface in `agentpaas pack`
  output.
- **Signed packages.** Agent packages are signed with a per-agent identity key.
- **Signed releases.** AgentPaaS's own releases are cosign-signed (keyless,
  via GitHub Actions OIDC) with a published SBOM, so you can verify the binary
  you install.

### Trigger API hardening
- Idempotency keys, constant-time key compare, rate limiting, lockout + audit
  on repeated 401s.

Full threat model: [docs/threat-model.md](docs/threat-model.md)

---

## Quick Start

### Prerequisites
- macOS 13+ (Apple Silicon or Intel)
- [Homebrew](https://brew.sh) 4+
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) or
  [Colima](https://github.com/abiosoft/colima)

### Install

```bash
brew tap AgentPaaS-ai/tap https://github.com/AgentPaaS-ai/homebrew-tap
brew trust AgentPaaS-ai/tap      # Homebrew 6.0+ requires trusting third-party taps
brew install agentpaas
agentpaas doctor                  # verify your environment is ready
```

`agentpaas doctor` runs six checks: CLI version, Docker CLI, Docker daemon,
macOS Keychain access, linux harness presence, and home-directory writability.
All must pass before you continue.

### 60-second tour

```bash
# Start the daemon
agentpaas daemon start

# Scaffold a governed agent
agentpaas init my-agent
cd my-agent

# Apply a policy declaring exactly what this agent may do
agentpaas policy apply --file policy.yaml

# Build the agent into a signed, SBOM'd container image
agentpaas pack

# Run it — isolated, policy-enforced, audited
agentpaas run my-agent

# Inspect the signed audit trail for that run
agentpaas audit list --run <run-id>

# Operator dashboard (audit, logs, timeline, policy diff)
open http://localhost:8090
```

Step-by-step (writing your first weather agent, applying a policy, packing,
running): [docs/quickstart.md](docs/quickstart.md)

---

## CLI Reference

| Command | Purpose |
|---|---|
| `agentpaas daemon start/stop/status/restart` | Manage the local control daemon |
| `agentpaas doctor` | First-run environment diagnostics |
| `agentpaas init <name>` | Scaffold a new agent project |
| `agentpaas validate` | Validate an agent project directory structure |
| `agentpaas policy init` | Scaffold a `policy.yaml` from a named template |
| `agentpaas policy apply --file <f>` | Apply (or validate) a policy |
| `agentpaas policy show` | Show the active policy for a run or project |
| `agentpaas policy explain` | Explain why a destination was denied |
| `agentpaas pack` | Build a signed, SBOM'd agent image |
| `agentpaas run <name>` | Start a governed agent run |
| `agentpaas stop <run-id>` | Terminate a running agent |
| `agentpaas secret add/list/remove/rotate/test` | Manage credentials in the OS keychain |
| `agentpaas cron add/list/remove` | Schedule agent invocations |
| `agentpaas trigger invoke` | Invoke an agent via the trigger REST API |
| `agentpaas audit list/export/verify` | Query, export, and verify the signed audit trail |
| `agentpaas logs` | Follow or query agent logs |
| `agentpaas timeline` | Chronological event timeline for a run |
| `agentpaas summarize` | Generate a summary of a completed run |
| `agentpaas explain-denial` | Explain why a destination was denied |
| `agentpaas explain-failure` | Analyze a failed run and return root cause |
| `agentpaas recommend-patch` | Suggest a policy patch for a desired behavior |
| `agentpaas next-action` | Recommend the next action based on context |
| `agentpaas confirm` / `confirmations` | Approve pending trust-boundary changes |
| `agentpaas version` | Print CLI version |

Add `--help` to any command for details. Add `--json` for machine-readable output.

---

## Containment (P1 Red-Team Smoke)

Verified by `make redteam-smoke` — fixtures run through the real
pack → run → gateway → audit pipeline:

| Attack Vector | Status | How |
|---|---|---|
| Network exfiltration | Blocked | Gateway policy enforcement |
| DNS exfiltration | Blocked | Internal network isolation |
| File system access | Restricted | Container UID 64000, bind mounts |
| Privilege escalation | Blocked | Non-root container, caps dropped |
| Process escape | Mitigated | Docker isolation, no new privs |
| Secret leakage | Prevented | Keychain broker, no env passthrough |
| iptables tampering | Blocked | CAP_NET_ADMIN dropped from agent PID 1 |

---

## Zero Telemetry

**Zero telemetry, zero phone-home by default.** AgentPaaS sends no analytics, no
update checks, no crash reports, no usage pings. Future telemetry, if any, will
be a separate explicit opt-in. Details: [docs/privacy.md](docs/privacy.md)

---

## Accepted P1 Limitations (honesty = trust)

- macOS only (Docker Desktop or Colima). Linux `dockerd` support is planned.
- Container hardening, not a kernel 0-day sandbox (no gVisor/Kata yet).
- HTTP/HTTPS egress is inspected and policy-checked; raw TCP/UDP is blocked by
  network isolation rather than deep packet inspection.
- Outbound data-loss prevention is fingerprint-based, not semantic.
- The red-team suite is a release smoke gate, not a full adversarial corpus.
- Local mode trusts the developer's machine — we protect against the agent, not
  against you.

Full list: [docs/known-limitations.md](docs/known-limitations.md)

---

## Documentation

- [Quickstart](docs/quickstart.md) — zero to governed agent in 15 minutes
- [How enforcement works](docs/how-enforcement-works.md) — topology and policy compilation
- [Policy reference](docs/policy-reference.md) — `policy.yaml` fields and examples
- [Secrets guide](docs/secrets.md) — credential onboarding and the keychain broker
- [Credential onboarding](docs/credential-onboarding.md) — the `secret` commands in depth
- [Threat model](docs/threat-model.md) — STRIDE threat table and controls
- [Audit verification](docs/audit-export.md) — signed export and verification
- [Known limitations](docs/known-limitations.md) — P1 scope and future work
- [Privacy](docs/privacy.md) — zero-telemetry commitment
- [Hermes plugin](integrations/hermes-plugin/SKILL.md) — integrate with Hermes Agent

---

## Build from Source

Requires Go 1.26+ and Docker.

```bash
git clone https://github.com/AgentPaaS-ai/agentpaas.git
cd agentpaas
make build          # builds agentpaas, agentpaasd, agentpaas-harness into ./bin/
make test           # full test suite
make race           # race detector
make lint           # golangci-lint
make redteam-smoke  # adversarial smoke gate
```

---

## Repository Layout

```
agentpaas/
├── cmd/                  # agentpaas CLI, agentpaasd daemon, harness
├── internal/             # runtime, policy, secrets, audit, pack, gateway, ...
├── api/                  # control + trigger protobuf APIs
├── web/dashboard/        # embedded operator dashboard
├── python/agentpaas_sdk/ # Python SDK for agent code
├── integrations/hermes-plugin/
├── test/e2e/             # end-to-end tests
├── test/redteam/         # adversarial smoke fixtures
├── third_party/agentgateway/
├── demo/                 # governed-weather, secret-saas, repair-loop
└── docs/
```

---

## Contributing

Contributions are welcome. Please open an issue first to discuss significant
changes. Security vulnerabilities must be reported privately via
[GitHub Security Advisories](https://github.com/AgentPaaS-ai/agentpaas/security/advisories/new)
— not through public issues. See [SECURITY.md](SECURITY.md).

---

## License

[MIT](LICENSE)
