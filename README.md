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
  Install and start Colima:
  ```bash
  brew install colima docker
  colima start
  ```
- **An LLM API key** — e.g. from [OpenRouter](https://openrouter.ai),
  OpenAI, xAI, or Anthropic. You'll store this in macOS Keychain via
  `agentpaas secret add` — it never enters the Hermes conversation.
- macOS (Apple Silicon or Intel)

## Install

**IMPORTANT: Install via Homebrew. Do NOT build from source.** Even if
you have the repo cloned, `make build-all` produces dev binaries
(`0.1.0-dev`, unknown commit, no version stamp). The brew cask ships
proper versioned binaries with the Linux harness bundled. A new user
should never need Go, `make`, or the source repo.

### 1. Install Hermes

If you don't have Hermes yet:

```bash
brew install nousresearch/tap/hermes-agent
```

### 2. Install Docker (if you don't have it)

```bash
brew install colima docker
colima start
```

### 3. Install AgentPaaS

```bash
brew install agentpaas-ai/tap/agentpaas
xattr -cr /opt/homebrew/bin/agentpaas /opt/homebrew/bin/agentpaasd /opt/homebrew/bin/agentpaas-harness-linux
agentpaas daemon start
agentpaas doctor
```

**Important:** The brew cask is not notarized. Run `xattr -cr` BEFORE any
`agentpaas` command — the binaries will be killed by macOS (exit 137)
if you skip this step. You only need to do this once after install.

`agentpaas doctor` verifies Docker, the daemon, keychain, and the harness
binary are all ready. If any check fails, it will tell you what to fix.

## Quickstart: Build and Run a Governed Agent

Everything below happens inside Hermes. Launch a Hermes session:

```bash
hermes
```

### Step 1: Install the AgentPaaS plugin

Tell Hermes:

> Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas

**IMPORTANT:** Use `hermes plugins install` from GitHub. Do NOT use
`make install-plugin` from a local clone — that bypasses the after-install
flow (skill pointer creation, toolset registration) and a real user does
not have the source repo.

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

> Build a weather agent that takes a city name as input, fetches real weather data from wttr.in, uses an LLM to summarize the conditions, and returns a short forecast.

Hermes asks you a few short questions (which LLM provider, which model,
confirm the hostnames), writes the agent code, creates an egress policy
allowing only `wttr.in` and your LLM provider's domain, packs it into a
signed container image, and runs it under governance. The pack step
enforces that every external hostname and credential is declared in the
policy — if anything is missing, the build fails before the agent can
ship with a broken or insecure runtime.

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

## Sharing Agents

AgentPaaS supports signed agent bundles for secure sharing between users.

### Export (Share)

```bash
# Initialize your publisher identity (first time only)
agentpaas identity init

# Export an agent as a shareable bundle
agentpaas export --project-dir ~/weather-agent

# Result: weather-agent.agentpaas bundle + your publisher fingerprint
# Read your fingerprint to the receiver over a separate channel
```

### Install (Receive)

```bash
# Inspect before installing — see policy, credentials, provenance
agentpaas bundle inspect weather-agent.agentpaas

# Install (interactive — confirms fingerprint, policy, credentials)
agentpaas install weather-agent.agentpaas

# Run the installed agent
agentpaas run weather-agent@&lt;pub8&gt;
```

### Fork and Redistribute

```bash
# Fork creates an editable project from an installed agent
agentpaas fork weather-agent@&lt;pub8&gt; ~/my-weather-agent

# After modifying, repack and re-export — provenance chain tracks all changes
cd ~/my-weather-agent && agentpaas pack . && agentpaas export --project-dir .
```

See [docs/sharing.md](docs/sharing.md) for the full guide.

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

### `agentpaas` binary killed (exit 137) after brew install

The brew cask is not notarized. macOS kills the binary with exit code 137.
Clear quarantine on ALL THREE binaries before running any agentpaas command:
```bash
xattr -cr /opt/homebrew/bin/agentpaas /opt/homebrew/bin/agentpaasd /opt/homebrew/bin/agentpaas-harness-linux
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

## Changelog

### v0.2.0 + v0.2.1 (July 2026)

**Agent sharing, provenance chains, fork/redistribute, and release hardening.**

What's new since v0.1.1:

- **Publisher identity** (`agentpaas identity init/show/export`): ECDSA P-256
  keypair per publisher, stored in macOS Keychain. Every pack signs the lockfile
  with both the agent identity key and the publisher key.
- **Signed bundles** (`.agentpaas`): deterministic tar.gz containing lock,
  policy, SBOM, source, and optional cosign-signed image. Offline
  `bundle inspect` verifies all signatures and digests without a running daemon.
- **Provenance chains**: every pack appends a signed `created` entry; every
  fork appends a signed `forked` entry with a policy delta. Chains verify
  end-to-end. 32-entry cap prevents chain bloat.
- **Fork & redistribute** (`agentpaas fork <ref> <dir>`): creates an editable
  project from an installed agent with `lineage.json` capturing parent
  metadata. Re-packing appends a `forked` provenance entry. Tampering
  lineage.json fails pack closed.
- **Bundle install with consent card** (`agentpaas install`): TOFU trust flow,
  policy approval, per-hop locally-verified/signer-claimed markings,
  tail-anchor trust sentence, chain egress lints.
- **Credential mapping** (`agentpaas installed map-credential`): map declared
  credential IDs to local secrets. Raw values never appear in manifests or
  audit.
- **Gateway policy enforcement** (B19): token budgets, rate limiting, LLM
  provider locking, ingress auth, guardrails, transformations, timeouts/retry,
  cost tracking, MCP tool access control — all compiled into per-run
  agentgateway configs and enforced at runtime.
- **Security claim closure** (B20): credential zero-visibility, guaranteed
  audit ingestion on every terminal path, fail-closed on invalid
  input/missing credentials/fake LLM, README-claim red-team release gate.
- **Two-persona sharing E2E** (B25): S1-S10 test cards covering identity
  onboarding, export, inspect-before-trust, TOFU install, tamper/impersonation
  detection, fork chains, credential invisibility.
- **Pack verification checklist**: post-build verification (S30-002 through
  S30-010) catches divergent harness binaries, missing signatures, stale
  digests.
- **Doctor skopeo check**: `agentpaas doctor` now includes a 12th check using
  skopeo to verify remote image digests.

Bug fixes since v0.1.1: gateway port field crash (#001), HTTP response status
field (#013), stale harness credentials (#016), onboarding skip (#017),
gateway-native rate limiting (#021), budget enforcement wiring (#019),
HTTP_PROXY regression for non-LLM egress, CLI run timeout (30s→90s),
install builder SDK resolution, divergent harness binary detection.

**v0.2.1-specific**: harness-linux binary now included in the brew cask
(was missing — required Go toolchain to build manually). Install section
now shows `colima docker` as a single brew install. `agentpaas daemon start`
added to the install flow before `doctor`.

### v0.1.1 (July 2026)

Initial public release. Local-first governed runtime with default-deny egress,
credential brokering, tamper-evident audit, SBOM, pack-time secret scan, and
budget enforcement.

## License

MIT — see [LICENSE](LICENSE).
