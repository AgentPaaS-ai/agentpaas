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

**The PRIMARY egress control is network topology isolation:** the agent
container is on a Docker internal-only network with no default route to
the internet. An additional iptables egress firewall runs inside the agent
container as **defense-in-depth** — it applies `OUTPUT DROP` rules to the
container's own network stack. This firewall may be unavailable in some
container environments (e.g., when `iptables` is not installed or
`CAP_NET_ADMIN` is absent), and the harness continues without it. Topology
isolation remains the hard boundary regardless.

## Security Features

| Feature | What it stops |
|---|---|
| **Default-deny egress** | Agent can't call any endpoint you didn't explicitly approve |
| **Container isolation** | Non-root (UID 64000), read-only rootfs, no shell, all capabilities dropped except NET_ADMIN when egress firewall is enabled (defense-in-depth), seccomp profile |
| **Credential brokering** | Secrets never reach agent code — injected by gateway at request time |
| **Internal-only network** | No route to internet except through gateway; DNS stub only resolves approved domains |
| **Tamper-evident audit** | Hash-chained JSONL + signed checkpoints — in-chain tampering (modification, reordering, insertion) detected on verification. Tail truncation (removing last N records) leaves a valid prefix chain and requires external checkpoint anchoring (P2). |
| **Signed audit export** | Portable signed bundle, verifiable on a second machine |
| **Signed images** | Every agent image cosign-signed with per-agent identity key |
| **SBOM on every artifact** | Software bill of materials surfaced at pack time |
| **Domain fronting block** | Gateway cross-checks SNI/Host/DNS — mismatch = deny |
| **DNS exfiltration block** | DNS only resolves via gateway stub — raw-IP dialing blocked |
| **Pack-time secret scan** | Scans build context for leaked secrets before image creation |
| **Budget enforcement** | Token/wall-clock/iteration limits kill runaway agents |
| **Locked dependency installs** | `uv` lockfiles only — no typosquatted package injection |
| **Publisher identity** | ECDSA P-256 keypair per publisher; bundles signed with publisher key; fingerprint verification via TOFU |
| **Signed bundles** | `.agentpaas` file: deterministic tar.gz with lock, policy, SBOM, source; cosign-signed image optional |
| **Provenance chains** | Every pack/fork appends a signed entry; chain verifies end-to-end; 32-entry cap prevents bloat |
| **Policy deltas** | Forked bundles show exactly what egress/credentials/MCP tools changed at each hop |
| **Tamper-evident install** | Post-install verification: lock signature, image digest, policy digest, source digest — all checked |
| **Consent card** | Receiver sees publisher, policy summary, provenance chain, egress lints before approving install |
| **Fork & redistribute** | `agentpaas fork <ref> <dir>` creates editable project; re-pack appends `forked` provenance entry |

### Verified by red-team smoke

Six attack fixtures through the real pack → run → gateway → audit pipeline:

| Attack | Result |
|---|---|
| Network exfiltration | Blocked — gateway policy enforcement (primary: topology isolation) |
| DNS exfiltration | Blocked — internal network isolation (primary: topology isolation) |
| File system access | Restricted — container UID 64000, bind mounts |
| Privilege escalation | Blocked — non-root container |
| Process escape | Mitigated — Docker isolation, no new privs |
| Secret leakage | Prevented — Keychain broker, no env passthrough |

## Prerequisites

- **[Hermes Agent](https://github.com/NousResearch/hermes-agent)** — every
  AgentPaaS experience runs through Hermes. Install it first.
- **[Docker Desktop](https://www.docker.com/products/docker-desktop/) or
  [Colima](https://github.com/abiosoft/colima)** — agents run in containers.
  Start Colima after installing: `colima start`
- **An LLM API key** — e.g. from [OpenRouter](https://openrouter.ai),
  OpenAI, xAI, or Anthropic. You'll store this in macOS Keychain via
  `agentpaas secret add` — it never enters the Hermes conversation.
- macOS (Apple Silicon or Intel)

## Install

### 1. Install Hermes

If you don't have Hermes yet:

```bash
brew install nousresearch/tap/hermes-agent
```

### 2. Install Docker (if you don't have it)

```bash
brew install colima
colima start
```

### 3. Install AgentPaaS

```bash
brew install agentpaas-ai/tap/agentpaas
xattr -cr /opt/homebrew/bin/agentpaas
agentpaas doctor
```

**Important:** The brew cask is not notarized. The `xattr -cr` command
clears the macOS quarantine attribute so the binary can run. You only
need to do this once after install.

`agentpaas doctor` verifies Docker, the daemon, keychain, and the harness
binary are all ready. If the daemon isn't running, it will tell you to
start it:
```bash
agentpaas daemon start
```

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

### Step 2: Store your API key

API keys are never passed through the Hermes conversation — they go
directly into macOS Keychain via the terminal. Run this in a separate
terminal:

```bash
agentpaas secret add openrouter-key
# paste your API key when prompted
```

Then tell Hermes you're done. Hermes verifies the key exists (by label,
never by value) and proceeds.

### Step 3: Build an agent

Tell Hermes:

> Build an agent that takes a question as input and uses an LLM to answer it.
> Use OpenRouter with the deepseek/deepseek-v4-flash model.
> My OpenRouter key is stored as "openrouter-key".

Hermes writes the agent code, creates an egress policy allowing only the
LLM provider's domain, packs it into a signed container image, and runs
it under governance. The pack step enforces that the configured provider's
domain is in the egress policy — if it's missing, the build fails before
the agent can ship with a broken runtime.

### Step 4: Invoke the agent

Tell Hermes:

> Ask the agent: "What is the capital of France?"

Hermes invokes the agent through the trigger API. The agent calls the
LLM through the gateway (credential brokered, egress enforced), and
returns the answer.

### Step 5: Check the audit trail

Tell Hermes:

> Show me the audit trail for the last run

Or from the terminal:

```bash
agentpaas audit query
```

Every allowed and denied call is logged: timestamp, agent identity,
destination, credential used, and policy decision.

## Share Agents Securely (v0.2.0)

AgentPaaS v0.2.0 adds secure agent sharing with signed bundles,
publisher identity, provenance chains, and fork/redistribute:

```bash
# Publisher: create identity, build, pack, export
agentpaas identity init --name my-publisher
agentpaas pack ./my-agent
agentpaas export ./my-agent -o my-agent.agentpaas --yes

# Receiver: inspect offline, install with consent
agentpaas bundle inspect my-agent.agentpaas
agentpaas install my-agent.agentpaas

# Fork an installed agent, modify, re-export under your identity
agentpaas fork weather-agent@a1b2c3d4 ./my-fork
# edit the forked project...
agentpaas pack ./my-fork
agentpaas export ./my-fork -o my-fork.agentpaas --yes
```

Each bundle carries a signed provenance chain. Forks append a `forked`
entry with a policy delta showing exactly what changed at each hop.
Receivers see the full chain, trust anchors on the final signer, and
can locally verify hops when they hold parent artifacts.

See [sharing.md](docs/sharing.md) for the full walkthrough and
[trust-model.md](docs/trust-model.md) for chain semantics.

See [docs/known-limitations.md](docs/known-limitations.md) for current development status and [open issues](https://github.com/AgentPaaS-ai/agentpaas/issues) for upcoming work.

## Documentation

- [Sharing guide](docs/sharing.md) — Export, install, fork, and redistribute agents
- [Trust model](docs/trust-model.md) — What signatures prove, TOFU, provenance chains
- [Manual testing guide](docs/manual-testing.md) — Step-by-step lifecycle tests (T1-T10)
- [Quickstart](docs/quickstart.md)
- [Policy reference](docs/policy-reference.md)
- [Secrets guide](docs/secrets.md)
- [Enforcement topology](docs/how-enforcement-works.md)
- [Threat model](docs/threat-model.md)
- [Bundle format](docs/bundle-format.md)
- [Audit export](docs/audit-export.md)
- [Hermes plugin setup](integrations/hermes-plugin/SKILL.md)
- [Known limitations](docs/known-limitations.md)

## Troubleshooting

### `agentpaas` binary won't run after brew install

The brew cask is not notarized. Clear the quarantine attribute:
```bash
xattr -cr /opt/homebrew/bin/agentpaas
```

### Daemon won't start (checkpoint key error)

After binary upgrades or clean state resets:
```bash
rm -f ~/.agentpaas/state/audit-checkpoint-key.der
agentpaas daemon start
```

### No `agentpaas_*` tools visible in Hermes

The toolset isn't registered. Run the ensure-toolset script:
```bash
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/ensure-toolset.py <profile>
```
Then restart Hermes: `/quit` then `hermes -p <profile>`

### Pack fails: "agentpaas-sdk was not found"

The SDK is bundled automatically — do NOT list `agentpaas-sdk` in
requirements.txt. Only list your agent's own dependencies.

### Agent returns "agentpaas fake llm response"

The LLM is not configured. Either set the `llm:` section in agent.yaml
or tell Hermes to configure it.

### Agent fails: "credential is not declared"

Credentials must be declared in policy.yaml, not just stored in
Keychain:
```yaml
credentials:
  - id: my-api-key
    type: header
    header: Authorization
```

See the [manual testing guide](docs/manual-testing.md) for more.

## Repository Layout

```text
agentpaas/
├── cmd/                  # agent CLI, agentpaasd daemon, harness
├── internal/             # runtime, policy, secrets, audit, pack, llm, ...
├── api/                  # control + trigger protobuf APIs
├── web/dashboard/        # operator dashboard (not yet enabled)
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
