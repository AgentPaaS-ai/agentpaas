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
agentpaas doctor
```

`agentpaas doctor` checks Docker, daemon connectivity, and keychain access.

## 60-Second Quickstart

```bash
# Start the daemon
agentpaas daemon start

# Create a governed agent
agentpaas init my-agent
cd my-agent

# Pack it (builds container image)
agentpaas pack

# Apply a policy
agentpaas policy apply policy.yaml

# Run it (governed execution)
agentpaas run my-agent

# Check the audit trail
agentpaas audit query --run-id <run-id>

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

Deep dive: [docs/how-enforcement-works.md](docs/how-enforcement-works.md)

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
- [Policy reference](docs/policy-reference.md)
- [Secrets guide](docs/secrets.md)
- [Enforcement topology](docs/how-enforcement-works.md)
- [Threat model](docs/threat-model.md)
- [Audit verification](docs/audit-export.md)
- [Hermes plugin setup](integrations/hermes-plugin/SKILL.md)

## Hermes Plugin (Developer Setup)

### Option A: Install from GitHub (for users)

If you're using AgentPaaS (not developing it), install the plugin directly
from GitHub. The plugin lives in a **subdirectory** of the repo, so you must
point at it explicitly:

```bash
hermes plugins install https://github.com/AgentPaaS-ai/agentpaas/tree/main/integrations/hermes-plugin --enable
```

Then add the plugin's toolset to your profile and restart Hermes:

```bash
python3 /path/to/agentpaas/scripts/ensure-toolset.py <profile-name>
# OR if you have the repo cloned locally:
make install-plugin HERMES_PROFILE=<profile-name>
```

**Restart Hermes** (run `/quit`, then relaunch) — plugins and toolsets load
at startup, not mid-session. After relaunching:

```bash
hermes -p <profile-name> tools list | grep agentpaas
hermes -p <profile-name> skills list | grep -i agentpaas
```

### Option B: Local symlink (for development)

If you're developing AgentPaaS and have the repo cloned, use `make install-plugin`
from the repo root. This symlinks the plugin, enables it, AND adds the `agentpaas`
toolset to `platform_toolsets.cli` — all in one step:

```bash
make install-plugin                          # installs into 'agentpaas' profile
make install-plugin HERMES_PROFILE=myprof    # custom profile
```

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