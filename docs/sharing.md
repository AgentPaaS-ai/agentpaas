# Sharing agents

This guide walks through publishing a signed `.agentpaas` bundle and
installing one on another machine. Signatures prove **identity** and
**integrity** only — not safety. See [trust-model.md](trust-model.md)
for the full trust model and [bundle-format.md](bundle-format.md) for
the on-disk format.

When you inspect a bundle or approve an install, AgentPaaS shows this
fixed disclaimer (PRD D3):

> A valid signature proves who signed this and that it is unmodified.
> It does not mean the agent is safe. Review the policy below.

---

## Publisher walkthrough

### 1. Create a publisher identity

Every bundle is signed with a publisher key. Create one identity per
publisher (person or team):

```bash
agentpaas identity init --name my-publisher
```

Show the fingerprint you will share out-of-band with receivers:

```bash
agentpaas identity show
```

Optional encrypted backup (passphrase-protected):

```bash
agentpaas identity export --out publisher-backup.enc
agentpaas identity import publisher-backup.enc
```

### 2. Build and pack the agent

From the agent project directory (with `agent.yaml` and `policy.yaml`):

```bash
agentpaas daemon start
agentpaas pack ./my-agent
```

`pack` builds the locked image and writes `agent.lock` under the project.
Review pack output for SBOM and policy warnings before exporting.

### 3. Export a bundle

Export produces a single `.agentpaas` file for distribution:

```bash
agentpaas export ./my-agent -o my-agent.agentpaas --yes
```

Use `--with-image` to embed a prebuilt OCI layout (receivers on the same
CPU architecture can use `--prefer-image` at install time). Without
`--with-image`, receivers rebuild the image locally from pinned source.

### 4. Send the bundle and verify the fingerprint

Send `my-agent.agentpaas` over your usual channel (Slack, email, USB, …).
**Also** communicate the publisher fingerprint from `agentpaas identity show`
on a **different** channel — call, in-person, or a team registry. Receivers
must compare that value to what `agentpaas bundle inspect` shows before they
trust the key.

---

## Receiver walkthrough

### 1. Inspect offline (no install, no daemon)

Review integrity, publisher, policy, and provenance before any install
state is written:

```bash
agentpaas bundle inspect weather.agentpaas
```

If verification fails (tamper), the command exits with an error and does
not show the full policy summary. Fix: obtain a fresh copy from the publisher.

When verification passes, read the publisher fingerprint in the output and
compare it to the value the sender gave you out-of-band. Only continue if
they match.

Provenance only (same data as inspect section 4):

```bash
agentpaas provenance show weather.agentpaas
```

### 2. Install: trust, consent, credentials, materialize

The install pipeline runs in this order:

1. Open bundle and verify signatures and digests.
2. Resolve publisher against the trust store (TOFU, pinned, or key conflict).
3. Show the consent card; you approve the policy (D3 disclaimer applies).
4. Map declared policy credentials to your local secret names.
5. Materialize install state and build or load the image.

A single `agentpaas install <bundle>` command will orchestrate these steps
when wired in the CLI; until then the daemon and library enforce the same
order — no state under `~/.agentpaas/state` is created before policy consent
passes.

**Trust store (optional pre-pin):**

```bash
agentpaas trust list
agentpaas trust add PUBLISHER_FP --key publisher.pem --alias my-publisher
```

On first install from an unknown publisher, TOFU prompts you to confirm the
fingerprint (TTY: type the last 8 hex chars; non-TTY: `--confirm-fingerprint`).

**After a successful install:**

```bash
agentpaas installed list
agentpaas installed alias AGENT_REF my-alias
```

If you deferred credential mapping at install time:

```bash
agentpaas secret add OPENROUTER_KEY
agentpaas installed map-credential AGENT_REF api-token=OPENROUTER_KEY
```

### 3. Run the installed agent

Targets use `name@pub8` (first 8 hex chars of the publisher fingerprint),
a display alias, or a bare name when unambiguous:

```bash
agentpaas daemon start
agentpaas run weather-agent@a1b2c3d4
agentpaas run my-alias
```

Audit and logs use the same ref. Phase 1 agents on this machine keep
working with bare names when no shared install collides.

### 4. Remove install state

Removing an install deletes materialized state; the trust pin remains.

```bash
agentpaas installed remove weather-agent@a1b2c3d4
agentpaas provenance show weather-agent@a1b2c3d4
```

(`provenance show` on an installed ref reads the locked copy under state.)

---

## Updates and downgrades

Re-installing a newer bundle from the same publisher and agent name:

| Situation | Behavior |
|-----------|----------|
| Same `policy_digest` as the installed copy | Abbreviated consent card; policy re-approval still recorded. |
| Changed policy | Full diff; you must explicitly re-approve the new policy. |
| Lower `agent_version` than installed | Refused unless you pass `--allow-downgrade` (audited). |

Downgrade without the flag:

```text
install refused: version downgrade requires --allow-downgrade
```

---

## Troubleshooting

| Problem | Cause | Fix |
|---------|-------|-----|
| Bundle tamper | File modified in transit | Re-download from the publisher; `bundle inspect` fails verification before any consent or state writes. |
| Key conflict (`PUBLISHER KEY CHANGED`) | Publisher rotated keys or possible impersonation | `agentpaas trust remove <publisher> --yes`, verify fingerprint out-of-band, reinstall. No inline override. |
| Unmapped credential | Mapping deferred at install | `agentpaas installed map-credential <ref> <declared>=<local>` then run again. |
| Platform mismatch | e.g. amd64 bundle on arm64 host with `--prefer-image` | Reinstall without `--prefer-image` to rebuild locally for your platform. |

---

## Related commands

| Command | Purpose |
|---------|---------|
| `agentpaas bundle inspect <file>` | Offline security review |
| `agentpaas trust list` / `show` / `add` / `remove` | Publisher key pins |
| `agentpaas installed list` / `remove` / `alias` / `map-credential` | Installed shared agents |
| `agentpaas provenance show <ref-or-bundle>` | Provenance chain report |
| `agentpaas secret list` | Local secret names (values never shown) |

Daemon required: `pack`, `export`, `run`, and install materialization that
builds images.