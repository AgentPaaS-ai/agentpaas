# Quickstart — 15-Minute Path to a Governed Agent

This guide walks from zero to a running, policy-enforced agent on macOS.
For accepted P1 limitations, see [known-limitations.md](known-limitations.md).

## Prerequisites

- macOS (Apple Silicon or Intel)
- [Homebrew](https://brew.sh)
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) or
  [Colima](https://github.com/abiosoft/colima)

## Step 1: Install

```bash
brew install agentpaas/tap/agentpaas
```

## Step 2: Verify

```bash
agent doctor
```

`agent doctor` checks Docker, daemon connectivity, keychain access, and
network isolation. All checks must pass before continuing.

## Step 3: Start the daemon

```bash
agent daemon start
```

The daemon (`agentpaasd`) runs locally under `~/.agentpaas`. It manages
agent runs, policy compilation, and the audit chain.

## Step 4: Create your first agent

```bash
agent init weather-agent
cd weather-agent
```

This scaffolds `agent.yaml`, `main.py`, `requirements.txt`, and
`.agentpaasignore`.

## Step 5: Write the agent

Replace `main.py` with a simple weather agent using the Python SDK:

```python
from agentpaas_sdk import agent


@agent.on_invoke
def invoke(payload):
    city = payload.get("city", "New York")
    try:
        resp = agent.http(
            "GET",
            f"https://api.weather.gov/locations/{city}",
        )
        return {"city": city, "status": "ok", "data": resp.get("body", "")[:200]}
    except Exception as e:
        return {"city": city, "status": "denied", "error": str(e)}
```

Update `agent.yaml` so the entry point matches:

```yaml
name: weather-agent
version: 0.1.0
runtime: python3.12
entry: main:invoke
```

## Step 6: Apply a policy

Create `policy.yaml` allowing the weather API:

```yaml
version: "1"
agent:
  name: weather-agent
  description: Fetches weather data through the governed gateway
egress:
  - domain: api.weather.gov
    ports: [443]
credentials: []
ingress: []
```

Apply it:

```bash
agent policy apply --file policy.yaml
```

The policy compiles into agentgateway `frontendPolicies.networkAuthorization`
CEL rules. See [how-enforcement-works.md](how-enforcement-works.md) and
[policy-reference.md](policy-reference.md) for details.

## Step 7: Pack the agent

```bash
agent pack
```

`agent pack` builds a container image, runs secret scanning, generates an
SBOM, and signs the package with a per-agent identity key.

## Step 8: Run it

```bash
agent run weather-agent
```

Each run creates two containers: an agent on an internal-only network and a
dual-homed gateway sidecar. Outbound HTTP is routed through the gateway via
`HTTP_PROXY`.

## Step 9: View the dashboard

```bash
open http://localhost:8090
```

The dashboard shows run status, egress decisions, policy denials, and audit
events in real time.

## Step 10: Check the audit trail

```bash
agent audit list
```

Filter by run ID:

```bash
agent audit list --run <run-id>
```

Export and verify on another machine: [audit-export.md](audit-export.md).

## Next steps

- [How enforcement works](how-enforcement-works.md) — gateway topology deep dive
- [Policy reference](policy-reference.md) — full `policy.yaml` format
- [Threat model](threat-model.md) — STRIDE-condensed security model
- [Known limitations](known-limitations.md) — P1 accepted trade-offs