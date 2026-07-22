# AgentPaaS

**Run AI agents so that even a compromised one can't leak your data.**

You built an agent with an LLM. Or you installed a skill off the internet.
Or the model itself was told to be malicious. How do you know it won't
ship your API keys, files, or PII somewhere you never approved?

AgentPaaS runs each agent in a locked-down container with a default-deny
network. It can only reach the endpoints you list. Credentials go through
a gateway sidecar and never sit in the agent process. Every call lands in
a tamper-evident audit trail.

If a prompt-injected agent tries an unknown host, the gateway blocks it,
the denial shows up in the audit log, and your secrets stay put.

## How it works

```
┌─────────────────────────────────────────────────────────┐
│                    YOUR MACHINE                         │
│                                                         │
│  ┌─────────────────────────────────────────┐            │
│  │         INTERNAL-ONLY DOCKER NETWORK    │            │
│  │         (no route to the internet)      │            │
│  │                                         │            │
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
│                                 │                       │
│                          ONLY ALLOWED                   │
│                          EGRESS PATH                    │
│                                 │                       │
│                                 ▼                       │
│                    ┌─────────────────────┐              │
│                    │   APPROVED APIs     │              │
│                    │  (api.x.ai, etc.)   │              │
│                    └─────────────────────┘              │
│                                                         │
│  ┌──────────────────────────────────────────┐           │
│  │              DAEMON (agentpaasd)         │           │
│  │  · Tamper-evident audit trail            │           │
│  │  · Signed checkpoints                    │           │
│  │  · Hash-chained JSONL + SQLite index     │           │
│  └──────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────┘
```

The agent container has no direct internet path. Traffic goes through the
gateway sidecar, which applies your policy. That sidecar is the only way
out.

Main control is network topology: the agent sits on a Docker internal-only
network with no default route to the internet. An iptables egress firewall
inside the agent container is extra depth. It drops unexpected OUTPUT
traffic on the container's own stack.

## Security features

Five that matter first. Full list: [docs/security-features.md](docs/security-features.md).

| Feature | What it stops |
|---|---|
| Default-deny egress | Any host you did not put on the allow list |
| Credential brokering | Secrets never enter agent code; the gateway injects them per request |
| Container isolation | Non-root (UID 64000), read-only rootfs, no shell, stripped capabilities, seccomp |
| Tamper-evident audit | Hash-chained log + signed checkpoints; edits and inserts fail verify |
| Signed bundles | Shareable `.agentpaas` packages with publisher identity and provenance |

Red-team smoke (six attack fixtures on the real pack → run → gateway path)
is in the [security features doc](docs/security-features.md#red-team-smoke).

## Prerequisites

- [Hermes Agent](https://github.com/NousResearch/hermes-agent) — install first;
  AgentPaaS runs through Hermes
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) or
  [Colima](https://github.com/abiosoft/colima)

  ```bash
  brew install colima docker
  colima start
  ```

- An LLM API key (OpenRouter, OpenAI, xAI, Anthropic, …). Store it with
  `agentpaas secret add`. It never goes into the Hermes conversation.
- macOS (Apple Silicon or Intel)

## Install

Install with Homebrew. Do not build from source for normal use. Even with
the repo cloned, `make build-all` produces dev binaries (`0.3.0-dev`,
unknown commit, no version stamp). The brew cask ships versioned binaries
and the Linux harness. A new user should not need Go, `make`, or the
source tree.

### 1. Install Hermes

```bash
brew install nousresearch/tap/hermes-agent
```

### 2. Install Docker if needed

```bash
brew install colima docker
colima start
```

### 3. Install AgentPaaS

```bash
brew tap AgentPaaS-ai/homebrew-tap
brew install agentpaas
xattr -cr /opt/homebrew/bin/agentpaas /opt/homebrew/bin/agentpaasd /opt/homebrew/bin/agentpaas-harness-linux
agentpaas daemon start
agentpaas doctor
agentpaas version
```

The brew cask is not notarized. Run `xattr -cr` before any `agentpaas`
command or macOS will kill the binaries (exit 137). Once after install is
enough.

`agentpaas doctor` checks Docker, the daemon, keychain, and the harness.
If something fails, it says what to fix.

## Quickstart: build and run a governed agent

Do this inside Hermes:

```bash
hermes
```

### Step 1: Install the AgentPaaS plugin

Tell Hermes:

> Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas

Use `hermes plugins install` from GitHub. Do not use `make install-plugin`
from a local clone. That skips after-install work (skill pointer, toolset
registration), and a normal user does not have the source repo.

When Hermes asks you to restart:

```
/quit
hermes
```

### Step 2: Store your API key

Keys never go through the Hermes chat. They go into macOS Keychain. In a
separate terminal:

```bash
agentpaas secret add openrouter-key
# paste your API key when prompted
```

Then tell Hermes you are done. It checks that the label exists, never the
value.

### Step 3: Build an agent

Tell Hermes:

> Build a weather agent that takes a city name as input, fetches real weather data from wttr.in, uses an LLM to summarize the conditions, and returns a short forecast.

Hermes asks a few short questions (provider, model, hostnames), writes the
agent, builds an egress policy for `wttr.in` and your LLM host, packs a
signed image, and runs it under governance. Pack fails if any external
hostname or credential is missing from the policy, so a broken or open
runtime never ships.

### Step 4: Invoke the agent

Tell Hermes:

> What's the weather in Folsom?

Hermes calls the agent through the trigger API. The agent hits wttr.in
through the gateway, calls the LLM with a brokered credential, and returns
the forecast.

### Step 5: Read and verify the audit chain

Every allow and deny is written as it happens: time, agent identity,
destination, credential used (by id, not value), and the policy decision.

```bash
# Recent events
agentpaas audit query

# One run
agentpaas audit query --run-id <run-id>
```

How the chain works:

1. Each record is a line of JSON (JSONL) with a sequence number.
2. Record N stores `prev_hash` = hash of record N-1.
3. Record N also stores its own `record_hash` over canonical JSON.
4. After each write, the daemon signs a checkpoint over the latest hash.
5. `agentpaas audit verify` walks the chain, recomputes every hash, and
   checks the checkpoints. Exit 0 means intact. Non-zero means something
   changed, moved, or was inserted.

```bash
agentpaas audit verify
# Audit chain valid: N records, N checkpoints
```

Hand a signed export to someone else (or yourself on another box):

```bash
agentpaas audit export --output ~/audit-export.json
```

Full field list and second-machine verify flow:
[docs/audit-export.md](docs/audit-export.md).

## Sharing agents

Signed bundles for handing an agent to someone else.

### Sender: export and share

```bash
# First time only: publisher identity
agentpaas identity init
# Publisher name: GitHub-style slug, 1-39 chars

agentpaas identity show
#   Name:        your-name
#   Fingerprint: abcd 1234 ... (64 hex chars)

agentpaas export ~/weather-agent --output ~/weather-agent/weather-agent.agentpaas --yes

agentpaas bundle inspect weather-agent.agentpaas
# 9 integrity checks, policy summary, provenance, SBOM

agentpaas provenance show weather-agent
# Every pack/fork event with publisher signature

# Share two things:
# 1. The .agentpaas file (any file share)
# 2. Your full 64-char fingerprint on a separate channel (phone, Signal, etc.)
```

### Receiver: inspect, verify, install

```bash
agentpaas bundle inspect weather-agent.agentpaas
```

Inspect runs nine checks. All should PASS:

| Check | What it verifies |
|---|---|
| manifest_parse | Bundle manifest is valid |
| manifest_signature | Manifest signed by the publisher |
| publisher_match | Manifest and lock publisher match |
| lock_provenance | Lock file and provenance chain verified |
| content_sha256 | Content digests match the manifest |
| policy_digest | Policy digest matches the lock |
| sbom_digest | SBOM digest matches the lock |
| source_digest | Source digest matches manifest and lock |
| image_digest | Image digest matches (or "no image" for source-only) |

The policy summary lists every egress domain and every declared credential.
Read that before you trust the agent.

```bash
# Compare the fingerprint from `bundle inspect` with what the sender
# told you on a separate channel. Skip this and you may install a forge.

agentpaas install weather-agent.agentpaas
# Prompts:
#   - type last 8 chars of the fingerprint
#   - approve the policy
#   - confirm any missing locked deps

# Non-interactive:
agentpaas install weather-agent.agentpaas \
  --yes \
  --confirm-fingerprint "<full-64-char-hex-fingerprint>" \
  --accept-policy "<policy-digest>" \
  --allow-unlocked-deps
# policy digest: agentpaas bundle inspect --json | jq .policy_digest
# --confirm-fingerprint wants the full 64 hex chars, not the last 8

agentpaas installed list
# weather-agent@<pub8>

agentpaas installed map-credential weather-agent@<pub8> \
  --credential-id openrouter-key \
  --secret-name openrouter-key
# Maps the bundle credential id to a secret in YOUR Keychain.
# Original secrets do not travel with the bundle.

agentpaas run weather-agent@<pub8>

agentpaas trigger invoke weather-agent@<pub8> --payload '{"city":"Folsom"}' --wait

agentpaas audit verify
# Your machine, your credentials, your independent audit chain
```

### Fork and redistribute

```bash
agentpaas fork weather-agent@<pub8> ~/my-weather-agent

cd ~/my-weather-agent && agentpaas pack . && agentpaas export . --output ~/my-weather-agent/my-weather-agent.agentpaas --yes
# Provenance chain example:
#   1. created  weather-agent 0.1.0  by original-publisher
#   2. forked   weather-agent 0.1.0  by your-name  (policy delta: +egress)
```

Full guide: [docs/sharing.md](docs/sharing.md).

## Documentation

- [Security features (full)](docs/security-features.md)
- [Sharing guide](docs/sharing.md)
- [Trust model](docs/trust-model.md)
- [Manual testing guide](docs/manual-testing.md)
- [Quickstart](docs/quickstart.md)
- [Policy reference](docs/policy-reference.md)
- [Secrets guide](docs/secrets.md)
- [Enforcement topology](docs/how-enforcement-works.md)
- [Threat model](docs/threat-model.md)
- [Bundle format](docs/bundle-format.md)
- [Audit export](docs/audit-export.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Golden loop test](docs/execution/reference/e2e-test-plan.md)
- [Hermes plugin setup](integrations/hermes-plugin/SKILL.md)
- [Known limitations](docs/known-limitations.md)
- [Changelog](CHANGELOG.md)

## Tech stack

Most of AgentPaaS is Go: the CLI, the daemon, and the Linux harness.

APIs are protobuf over gRPC. Policy and agent config are plain YAML.
Agents themselves are usually Python, wired in through
`python/agentpaas_sdk`, with deps pinned by `uv` lockfiles at pack time.

Runtime is Docker (Desktop or Colima on a Mac). The agent lands on an
internal-only network. The only way out is a dual-homed
[agentgateway](https://github.com/agentgateway/agentgateway) sidecar we
vendor and configure for that run.

Secrets live in the macOS Keychain. The gateway injects them at request
time; the agent process never sees them. Images and shareable bundles can
be cosign-signed, and every pack ships an SBOM.

Audit is a hash-chained JSONL log, indexed in SQLite, with signed
checkpoints on each write.

Day to day you drive it from Hermes: pack, run, trigger, audit. Everything
above runs on your machine. No phone-home control plane on this path.

## Repository layout

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

See [CHANGELOG.md](CHANGELOG.md).

## License

MIT — see [LICENSE](LICENSE).
