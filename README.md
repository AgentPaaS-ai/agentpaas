# AgentPaaS

**Secure PaaS for agents, applications, MCP servers, and agentic integration workflows.**

AgentPaaS.ai gives teams one end-to-end path to build, deploy, run, govern, and audit workloads with Agent Security at the foundation. The proof is isolated containers, default-deny egress, gateway-brokered credentials, signed bundles, and tamper-evident audit.

Start with the open-source CLI locally, then deploy the governed package to AgentPaaS Cloud. You can keep building in Hermes or your own environment.

[Start a free 30-day trial](https://agentpaas.ai/#contact) · [Read the documentation](https://docs.agentpaas.ai/) · [Install](#install)

Building an agent is easy now. The hard part is trust. One bad outbound call can expose API keys, files, or PII. AgentPaaS provides the end-to-end platform path that lets teams build useful workloads while keeping policy, secrets, isolation, provenance, and audit at the platform layer.

When an agent tries an unknown host, the call is blocked, you see the denial
in the log, and your secrets are never leaked. When you hand an agent to a
coworker or a friend, you ship a signed bundle with your publisher
fingerprint. They inspect policy and provenance before install, map
credentials to their own Keychain, and keep their own audit trail. If someone
forks that agent, changes the code or policy, and re-shares it, the bundle
records each hop: who published the original, who forked it, and what egress
or credentials they added. Receivers can verify the provenance chain before
accepting and running the agent.

**In 0.4:** governed MCP and tool deployments, linear workflows, fan-out, choice, and phone-call envelopes on AgentPaaS Cloud. A single agent does not need a workflow: pack, deploy, invoke. Local multi-stage run stays fail-closed; the cloud envelope is the multi-step path.

## How it works

```
┌────────────────────────────────────────────────────┐
│                   YOUR MACHINE                     │
│                                                    │
│  ┌──────────────────────────────────────┐          │      ┌──────────────────┐
│  │   INTERNAL-ONLY DOCKER NETWORK       │          │      │  APPROVED APIs   │
│  │   (no direct internet route)         │          │      │ (api.x.ai, etc.) │
│  │                                      │          │      │   (internet)     │
│  │  ┌────────────┐    ┌──────────────┐  │  only    │      │                  │
│  │  │   AGENT    │    │   GATEWAY    │  │  allowed │      │                  │
│  │  │  CONTAINER │◄──►│   SIDECAR    │◄─┼──────────┼─────►│                  │
│  │  │            │    │              │  │  egress/ │      │                  │
│  │  │ · Python   │    │ · Policy     │  │  ingress │      └──────────────────┘
│  │  │ · No shell │    │ · Credential │  │          │
│  │  │ · Non-root │    │   broker     │  │          │
│  │  │ · Read-only│    │ · DNS stub   │  │          │
│  │  │ · No caps  │    │              │  │          │
│  │  └────────────┘    └──────────────┘  │          │
│  └──────────────────────────────────────┘          │
│                                                    │
│  ┌──────────────────────────────────────┐          │
│  │           DAEMON (agentpaasd)        │          │
│  │  · Tamper-evident audit trail        │          │
│  │  · Signed checkpoints                │          │
│  │  · Hash-chained JSONL + SQLite index │          │
│  └──────────────────────────────────────┘          │
└────────────────────────────────────────────────────┘
```

The agent container has no direct path to the internet. Every packet goes
through the gateway sidecar, which applies your policy. That is the only way
out.

The PRIMARY egress control is network topology isolation: the agent
lives on a Docker internal-only network with no default route off the box.
An iptables egress firewall inside the container is defense-in-depth —
it drops unexpected outbound traffic on the container's own stack.

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

Install [Hermes](https://hermes-agent.nousresearch.com/docs) first.
AgentPaaS runs through Hermes.

If you use a dedicated Hermes testing profile, `google/gemini-3.8-flash`
is a good default. Other models work.

macOS (Apple Silicon or Intel).

## Install

Open Hermes and paste:

```
Install AgentPaaS from github https://github.com/AgentPaaS-ai/agentpaas/tree/main/install
```

That one line is the full product: brew CLI, Docker or Colima, the Hermes
plugin, install completion, and doctor 7/7. Do not run
`hermes plugins install` or python install scripts yourself.

If Hermes asks you to reopen:

```
/quit
hermes
```

Testing profile: `hermes -p ap-testing`.

## Quickstart

### Build a weather agent

In Hermes, paste:

```
Build a weather agent that uses an LLM, and responds with a friendly demeanour
```

When asked for a publisher identity, run this in your own terminal:

```
agentpaas identity init --name <yourname>
```

Keep the placeholder `<yourname>`. Type your chosen publisher slug. Do not
use your Mac account name, `$USER`, `whoami`, or your home folder name.
Identity creation is terminal-gated.

API keys never go in chat. In your own terminal run `agentpaas secret add`,
then tell Hermes you are done.

### Lineage and audits

Paste:

```
Show me lineage and audits
```

A packed agent has four digests:

- Image digest: the container that ran
- Policy digest: the signed allow-list
- Build input digest: packed source
- SBOM digest: pack-time bill of materials

A simple weather agent SBOM is OS (Debian slim) plus the AgentPaaS harness
Go modules, not a pip supply chain, unless the agent declared pip deps.

The audit order is the proof: weather host (wttr.in) first, then the LLM.
That is a real fetch, then a summarize.

### Run in AgentPaaS Cloud

Paste:

```
Make it run in the agentpaas cloud
```

When asked to log in, run `agentpaas cloud login` in your own terminal.
Use the same browser as your claim.

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

Inspect runs nine offline integrity checks on the bundle: does the
manifest parse and verify under the publisher key, do lock/provenance
signatures hold, and do the digests for policy, SBOM, source, and image
match what the lock claims. All of them should PASS before you install.
The full checklist is in [docs/bundle-format.md](docs/bundle-format.md#5-verification-checklist-9-checks).

Inspect also prints a policy summary (egress domains and declared
credentials). Read that before you trust the agent.

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
