# AgentPaaS

**AgentPaaS is a governed, local-first runtime for packaging, running, and
observing AI agents on your Mac.** Every action is policy-checked and audited.
No cloud dependency. No telemetry.

- **Governed** — policy enforcement on every egress, credential, and tool call
- **Local-first** — daemon, gateway, and dashboard run entirely on your machine
- **Observable** — dashboard, signed audit trail, and OTel traces
- **Verifiable** — signed audit exports, cosign-signed releases, and SBOMs

## Prerequisites

- macOS (darwin/arm64 or darwin/amd64)
- [Homebrew](https://brew.sh)
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) or
  [Colima](https://github.com/abiosoft/colima)

See [docs/known-limitations.md](docs/known-limitations.md) before installing.

## Quick Install

```bash
brew install agentpaas/tap/agentpaas
agent doctor
```

`agent doctor` checks Docker, daemon connectivity, and keychain access.

## 60-Second Quickstart

```bash
# Start the daemon
agent daemon start

# Create a governed agent
agent init my-agent
cd my-agent

# Pack it (builds container image)
agent pack

# Apply a policy
agent policy apply --file policy.yaml

# Run it (governed execution)
agent run my-agent

# Check the audit trail
agent audit list --run <run-id>

# View in dashboard
open http://localhost:8090
```

More detail: [docs/quickstart.md](docs/quickstart.md)

## How Enforcement Works

Each agent run gets two containers on isolated Docker networks:

1. **Agent container** — on an internal-only bridge. No route to the internet.
   DNS points only at the gateway stub. Outbound HTTP/HTTPS is routed via
   `HTTP_PROXY` to the gateway.
2. **Gateway sidecar** — dual-homed on the internal bridge and a dedicated
   egress network. Compiles `policy.yaml` into agentgateway rules. Brokered
   credentials are injected here; raw secrets never reach the agent.

Ingress, egress, MCP tool calls, and credential use all pass through the
gateway. Denials are recorded in the signed audit chain.

Deep dive: [docs/enforcement.md](docs/enforcement.md)

## Containment (P1 Red-Team Smoke)

Verified by `make redteam-smoke` — six fixtures through the real pack → run →
gateway → audit pipeline:

| Attack Vector         | Status     | How                              |
|-----------------------|------------|----------------------------------|
| Network exfiltration  | Blocked    | Gateway policy enforcement       |
| DNS exfiltration      | Blocked    | Internal network isolation       |
| File system access    | Restricted | Container UID 64000, bind mounts |
| Privilege escalation  | Blocked    | Non-root container               |
| Process escape        | Mitigated  | Docker isolation, no new privs   |
| Secret leakage        | Prevented  | Keychain broker, no env passthrough |

## Zero Telemetry

**Zero telemetry, zero phone-home by default.**

AgentPaaS does not send analytics, update checks, crash reports, or usage
pings. Future telemetry, if any, will be a separate explicit opt-in.

Privacy details: [docs/privacy.md](docs/privacy.md)

## Known Limitations

Full list: [docs/known-limitations.md](docs/known-limitations.md)

P1 accepted limitations:

- macOS only (Docker Desktop or Colima); Linux `dockerd` is P2
- Container hardening, not a kernel 0-day sandbox (no gVisor/Kata in P1)
- HTTP/HTTPS egress enforcement via gateway proxy; raw TCP/UDP bypass is blocked
  by network isolation, not deep inspection
- Outbound DLP is fingerprint-based, not semantic
- Red-team suite is a release smoke gate, not full adversarial research
- Local mode trusts the developer's machine — we protect against the agent, not
  the user

## Documentation

- [Quickstart](docs/quickstart.md)
- [Policy reference](docs/policy.md)
- [Secrets guide](docs/secrets.md)
- [Enforcement topology](docs/enforcement.md)
- [Threat model](docs/threat-model.md)
- [Audit verification](docs/audit-verification.md)
- [Hermes plugin setup](integrations/hermes-plugin/SKILL.md)

## Repository Layout

```text
agentpaas/
├── agentpaas-prd-v4-master.md
├── agentpaas-execution-plan-v1.md
├── cmd/                  # agent CLI, agentpaasd daemon, harness
├── internal/             # runtime, policy, secrets, audit, pack, ...
├── api/                  # control + trigger protobuf APIs
├── web/dashboard/        # embedded operator dashboard
├── python/agentpaas_sdk/ # Python SDK for agent code
├── integrations/hermes-plugin/
├── test/e2e/             # end-to-end tests
├── test/redteam/         # P1 adversarial smoke fixtures
├── third_party/agentgateway/
├── demo/                 # governed-weather, secret-saas, repair-loop
├── docs/
│   ├── agentpaas-subtask-decomposition-v1.md
│   └── archive/
│       ├── checkpoints/
│       ├── landing-page-legacy/
│       └── prd-history/
└── landing-page/
    ├── README.md
    └── index.html
```

## Archive Policy

Superseded PRDs, checkpoints, and unused landing-page generation assets are
kept under `docs/archive/` for provenance. They are not active implementation
inputs unless a task explicitly references them.

## Landing Page

The static landing page is in `landing-page/`. See
[landing-page/README.md](landing-page/README.md) for deploy notes.

## Development

Active planning documents:

- [agentpaas-prd-v4-master.md](agentpaas-prd-v4-master.md) — product and
  technical definition
- [agentpaas-execution-plan-v1.md](agentpaas-execution-plan-v1.md) — block
  by block implementation plan and gates
- [docs/agentpaas-subtask-decomposition-v1.md](docs/agentpaas-subtask-decomposition-v1.md)
  — coding-agent sized issue breakdown

Build from source: `make build`. Run gates: `make test`, `make redteam-smoke`.

## License

MIT — see [LICENSE](LICENSE).