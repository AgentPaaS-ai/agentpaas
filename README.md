# AgentPaaS

**Run AI agents securely. Even if the agent is compromised, it can't leak your data.**

You just built an agent using an LLM. Maybe you downloaded a skill from
the internet. Maybe the model itself was instructed to be malicious. How
do you know it won't phone home with your API keys, your files, your
PII?

AgentPaaS runs every agent inside a locked-down container with a
default-deny network policy. The agent can only talk to the exact
endpoints you approve — nothing else. Credentials are brokered through a
gateway sidecar and never visible to the agent code. Every call is
logged to a tamper-evident audit trail.

If a prompt-injected agent tries to exfiltrate data to an unknown
server, the gateway blocks it. You see the denial in the audit log. Your
secrets stay safe.

## How It Works

```
┌─────────────────────────────────────────────────────────┐
│                    YOUR MACHINE                          │
│                                                          │
│  ┌─────────────────────────────────────────┐            │
│  │         INTERNAL-ONLY DOCKER NETWORK     │            │
│  │         (no route to the internet)       │            │
│  │                                          │            │
│  │  ┌──────────────┐    ┌───────────────┐  │            │
│  │  │   AGENT      │    │   GATEWAY     │  │            │
│  │  │  CONTAINER   │    │   SIDECAR     │  │            │
│  │  │              │    │               │  │            │
│  │  │  · Python    │    │  · Policy     │  │            │
│  │  │    agent     │───▶│    enforcer   │  │            │
│  │  │  · No shell  │    │  · Credential │  │            │
│  │  │  · Non-root  │    │    broker     │  │            │
│  │  │  · Read-only │    │  · DNS stub   │  │            │
│  │  │    rootfs    │    │               │  │            │
│  │  │  · No caps   │    │       │       │  │            │
│  │  └──────────────┘    └───────┼───────┘  │            │
│  │                              │          │            │
│  └──────────────────────────────┼──────────┘            │
│                                 │                        │
│                          ONLY ALLOWED                    │
│                          EGRESS PATH                     │
│                                 │                        │
│                                 ▼                        │
│                    ┌─────────────────────┐               │
│                    │   APPROVED APIs     │               │
│                    │  (api.x.ai, etc.)   │               │
│                    └─────────────────────┘               │
│                                                          │
│  ┌──────────────────────────────────────────┐           │
│  │              DAEMON (agentpaasd)           │           │
│  │  · Tamper-evident audit trail              │           │
│  │  · Signed checkpoints                      │           │
│  │  · Hash-chained JSONL + SQLite index       │           │
│  └──────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────┘
```

The agent container has no direct internet route. All traffic goes
through the gateway sidecar, which enforces your policy. The gateway is
the only path out.

## Security Features

| Feature | What it stops |
|---|---|
| **Default-deny egress** | Agent can't call any endpoint you didn't explicitly approve |
| **Container isolation** | Non-root (UID 64000), read-only rootfs, no shell, all capabilities dropped, seccomp profile |
| **Credential brokering** | Secrets never reach agent code — injected by gateway at request time |
| **Internal-only network** | No route to internet except through gateway; DNS stub only resolves approved domains |
| **Tamper-evident audit** | Hash-chained JSONL + signed checkpoints — deletions detected on verification |
| **Signed audit export** | Portable signed bundle, verifiable on a second machine |
| **Signed images** | Every agent image cosign-signed with per-agent identity key |
| **SBOM on every artifact** | Software bill of materials surfaced at pack time |
| **Domain fronting block** | Gateway cross-checks SNI/Host/DNS — mismatch = deny |
| **DNS exfiltration block** | DNS only resolves via gateway stub — raw-IP dialing blocked |
| **Pack-time secret scan** | Scans build context for leaked secrets before image creation |
| **Budget enforcement** | Token/wall-clock/iteration limits kill runaway agents |
| **Locked dependency installs** | `uv` lockfiles only — no typosquatted package injection |

### Verified by red-team smoke

Six attack fixtures through the real pack → run → gateway → audit pipeline:

| Attack | Result |
|---|---|
| Network exfiltration | Blocked — gateway policy enforcement |
| DNS exfiltration | Blocked — internal network isolation |
| File system access | Restricted — container UID 64000, bind mounts |
| Privilege escalation | Blocked — non-root container |
| Process escape | Mitigated — Docker isolation, no new privs |
| Secret leakage | Prevented — Keychain broker, no env passthrough |

## Prerequisites

- **[Hermes Agent](https://github.com/NousResearch/hermes-agent)** — every
  AgentPaaS experience runs through Hermes. Install it first.
- **An LLM API key** — e.g. from [OpenRouter](https://openrouter.ai),
  OpenAI, xAI, or Anthropic. You'll provide this when building an agent.
- macOS (Apple Silicon or Intel)
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) or
  [Colima](https://github.com/abiosoft/colima)

## Install

### 1. Install Hermes

If you don't have Hermes yet:

```bash
brew install nousresearch/tap/hermes-agent
```

### 2. Install AgentPaaS

```bash
brew install agentpaas/tap/agentpaas
agentpaas doctor
```

`agentpaas doctor` checks Docker, daemon connectivity, and keychain access.

## Quickstart: Build and Run a Governed Agent

Everything below happens inside Hermes. Launch a Hermes session:

```bash
hermes
```

### Step 1: Install the AgentPaaS plugin

Tell Hermes:

> Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas

Hermes installs the plugin, registers the toolset, and creates the skill
pointer. Restart Hermes when it tells you to:

```
/quit
hermes
```

### Step 2: Build an agent

Tell Hermes:

> Build an agent that takes a question as input and uses an LLM to answer it.

Hermes will ask which LLM provider and model you want to use, then ask
for your API key (stored in macOS Keychain — never baked into the image).
It then writes the agent code, creates an egress policy allowing only the
LLM provider's domain, packs it into a signed container image, and runs
it under governance.

### Step 3: Invoke the agent

Tell Hermes:

> Ask the agent: "What is the capital of France?"

Hermes invokes the agent through the trigger API. The agent calls the
LLM through the gateway (credential brokered, egress enforced), and
returns the answer.

### Step 4: Check the audit trail

Tell Hermes:

> Show me the audit trail for the last run

Or from the terminal:

```bash
agentpaas audit query
```

Every allowed and denied call is logged: timestamp, agent identity,
destination, credential used, and policy decision.

## What's Next: Share Agents Securely

The next feature we're building: **securely package and transfer an
agent to a friend or co-worker.**

Today, when you build and test an agent on your machine, it stays there.
Soon, you'll be able to export the signed agent artifact (image +
lockfile + SBOM) and hand it to someone else. They run:

```bash
agentpaas verify agent.lock   # verify signature + SBOM
agentpaas run my-agent         # run it under the same governance
```

They get the same security guarantees: default-deny egress, brokered
credentials, tamper-evident audit — no matter who built the agent.

See [docs/roadmap.md](docs/roadmap.md) for the full task list.

## Documentation

- [Quickstart](docs/quickstart.md)
- [Policy reference](docs/policy-reference.md)
- [Secrets guide](docs/secrets.md)
- [Enforcement topology](how-enforcement-works.md)
- [Threat model](threat-model.md)
- [Audit export](audit-export.md)
- [Hermes plugin setup](integrations/hermes-plugin/SKILL.md)
- [Known limitations](docs/known-limitations.md)
- [Roadmap](docs/roadmap.md)

## Repository Layout

```text
agentpaas/
├── cmd/                  # agent CLI, agentpaasd daemon, harness
├── internal/             # runtime, policy, secrets, audit, pack, llm, ...
├── api/                  # control + trigger protobuf APIs
├── web/dashboard/        # embedded operator dashboard
├── python/agentpaas_sdk/ # Python SDK for agent code
├── integrations/hermes-plugin/
├── test/e2e/             # end-to-end tests
├── test/redteam/         # P1 adversarial smoke fixtures
├── third_party/agentgateway/
├── docs/
└── landing-page/
```

## License

MIT — see [LICENSE](LICENSE).
