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

> What's the weather in Folsom?

Hermes invokes the agent through the trigger API. The agent fetches
real weather data from wttr.in through the gateway (egress enforced),
calls the LLM to summarize it (credential brokered), and returns the
forecast.

### Step 5: Verify the audit trail

Every allowed and denied call is logged: timestamp, agent identity,
destination, credential used, and policy decision. View the full trail:

```bash
agentpaas audit query
```

Filter to a specific run:

```bash
agentpaas audit query --run-id <run-id>
```

The audit trail is **tamper-evident** — every record is hash-chained to
the previous one, and each record gets a signed checkpoint. Verify the
integrity of the entire chain at any time:

```bash
agentpaas audit verify
# Output: "Audit chain valid: N records, N checkpoints"
# Exit code 0 = chain intact, non-zero = tampering detected
```

This works during daemon operation and after shutdown. Run it whenever
you need cryptographic proof that the audit log has not been modified.

You can also export a signed audit bundle for verification on a second
machine:

```bash
agentpaas export-audit --output ~/audit-export.json
```

## Sharing Agents

AgentPaaS supports signed agent bundles for secure sharing between users.

### Sender: Export and Share

```bash
# Initialize your publisher identity (first time only)
agentpaas identity init
# You'll be prompted for a publisher name (GitHub-style slug, 1-39 chars)

# Verify your identity and get your fingerprint
agentpaas identity show
# Output:
#   Name:        your-name
#   Fingerprint: abcd 1234 ... (64 hex chars)

# Export an agent as a shareable bundle
agentpaas export ~/weather-agent --output ~/weather-agent/weather-agent.agentpaas --yes
# Produces: weather-agent.agentpaas

# Verify the bundle before sharing
agentpaas bundle inspect weather-agent.agentpaas
# Shows: 9 integrity checks, policy summary, provenance chain, SBOM

# View the agent's provenance chain
agentpaas provenance show weather-agent
# Shows every pack/fork event with publisher signature

# Share TWO things with the receiver:
# 1. The .agentpaas bundle file (via any file-sharing method)
# 2. Your full 64-character fingerprint (via a SEPARATE channel — phone,
#    Signal, etc.) so they can verify authenticity
```

### Receiver: Inspect, Verify, Install

```bash
# Step 1: Inspect before installing — see what the agent can access
agentpaas bundle inspect weather-agent.agentpaas
```

The inspect output shows 9 integrity checks, each must PASS:

| Check | What it verifies |
|---|---|
| manifest_parse | Bundle manifest is valid |
| manifest_signature | Manifest is signed by the publisher |
| publisher_match | Manifest and lock publisher match |
| lock_provenance | Lock file and provenance chain are verified |
| content_sha256 | All content digests match the manifest |
| policy_digest | Policy digest matches the lock |
| sbom_digest | SBOM digest matches the lock |
| source_digest | Source code digest matches the manifest and lock |
| image_digest | Image digest matches (or "no image" for source-only bundles) |

The policy summary shows every egress domain the agent can reach and
every credential it declares. Review these carefully — this is your
chance to see exactly what the agent can do before you trust it.

```bash
# Step 2: Verify the publisher fingerprint
# Compare the fingerprint shown by `bundle inspect` against what the
# sender told you over a SEPARATE channel. Do NOT skip this step.
# If they don't match, do NOT install — the bundle may be forged.

# Step 3: Install (interactive — confirms fingerprint, policy, credentials)
agentpaas install weather-agent.agentpaas
# You'll be prompted to:
#   - Type the last 8 characters of the fingerprint to confirm
#   - Approve the policy
#   - Confirm any missing locked dependencies

# For non-interactive / automated install:
agentpaas install weather-agent.agentpaas \
  --yes \
  --confirm-fingerprint "<full-64-char-hex-fingerprint>" \
  --accept-policy "<policy-digest>" \
  --allow-unlocked-deps
# Get the policy digest from: agentpaas bundle inspect --json | jq .policy_digest
# NOTE: --confirm-fingerprint requires the FULL 64-character hex, not last 8

# Step 4: Verify the install
agentpaas installed list
# Shows: weather-agent@<pub8> — installed agents with publisher reference

# Step 5: Map credentials (if the agent declares any)
agentpaas installed map-credential weather-agent@<pub8> \
  --credential-id openrouter-key \
  --secret-name openrouter-key
# Maps the bundle's credential ID to a secret in YOUR Keychain.
# The agent's original credentials don't travel with the bundle.

# Step 6: Run the installed agent
agentpaas run weather-agent@<pub8>

# Step 7: Invoke it
agentpaas trigger invoke weather-agent@<pub8> --payload '{"city":"Folsom"}' --wait

# Step 8: Verify your own audit trail (same as sender)
agentpaas audit verify
# The receiver's audit chain is independently verifiable — proving
# what the agent did on YOUR machine, with YOUR credentials
```

### Fork and Redistribute

```bash
# Fork creates an editable project from an installed agent
agentpaas fork weather-agent@<pub8> ~/my-weather-agent

# After modifying, repack and re-export — provenance chain tracks all changes
cd ~/my-weather-agent && agentpaas pack . && agentpaas export . --output ~/my-weather-agent/my-weather-agent.agentpaas --yes
# The new bundle's provenance chain shows:
#   1. created  weather-agent 0.1.0  by original-publisher
#   2. forked   weather-agent 0.1.0  by your-name  (policy delta: +egress)
# Receivers can see the full chain and verify each hop
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
- [Audit export](docs/audit-export.md) — Export signed audit trail for verification on a second machine
- [Golden loop test](docs/execution/golden-loop-test.md) — 12-phase release gate test
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

### v0.2.3 (July 2026)

**Audit checkpoint fix, redirect blocking, export integrity, and golden loop release gate.**

What's new since v0.2.2:

- **Audit checkpoint verification** (`agentpaas audit verify`): every audit
  record now gets an immediate signed checkpoint (cadence lowered from 25 to
  1). `audit verify` passes during daemon operation AND after shutdown —
  proving the full hash-chained JSONL + checkpoint trail is tamper-evident.
- **Golden loop test** (12 phases): release gate test covering the full
  lifecycle — clean install, build, modify, provenance, export, teardown,
  receive, inspect, install, run. Every public release must pass it.
- **Consolidated defect log** (`docs/defects.md`): single source of truth for
  all 35 tracked bugs with root cause, reproduce steps, fix, and post-fix
  verification.

Bug fixes since v0.2.2:

- Bug 032: Export source_digest mismatch caused by `.agentpaas.tmp` file
  contaminating the source digest. Fixed: `.agentpaas.tmp` and
  `audit-export.json` added to default ignore patterns.
- Bug 033: Gateway no longer follows HTTP redirects. `CheckRedirect` returns
  `ErrUseLastResponse` on both http.Client instances. Redirect responses
  returned to agent with `redirect_url` field. Audited as `egress_denied`.
- Bug 034: TLS handshake error on redirect URLs resolved by Bug 033 fix.
- Bug 031: SKILL.md now explicitly requires egress hostname confirmation for
  agent modification, not just creation.
- Bug 018: SKILL.md now explicitly instructs agent to use `domain` field name
  (not `host` or `hostname`) in policy.yaml.
- Bug 027: Install error message for missing `uv.lock` now explains the issue
  and provides the exact command to fix it.
- Bug 028: Added `--limit` as alias for `--page-size` on `audit query`.
- Bug 035: Audit checkpoint verification failed because
  `DefaultCheckpointCadence` was 25 but typical sessions produced <25 records,
  so `ShouldCheckpoint` never fired. Fixed: cadence lowered to 1.

### v0.2.2 (July 2026)

**After-install prerequisite check and wall clock budget fix.**

- **After-install Step 0 prerequisite check**: `hermes plugins install` now
  detects missing prerequisites (colima, docker, agentpaas binaries) and
  installs them via brew before proceeding. Includes anti-clone guard.
- Bug 030: Wall clock budget increased from 30s to 120s. The first
  `agent.llm()` call to an LLM provider can take 10-30s (cold connection,
  model loading). The 30s default caused `wall_clock_budget_exceeded` kills
  before the LLM responded.

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
