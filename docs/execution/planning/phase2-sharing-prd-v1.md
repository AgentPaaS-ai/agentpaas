# Phase 2 PRD v1 — Secure Agent Sharing & Distribution

**Status:** HISTORICAL NORMATIVE BASELINE FOR SHIPPED B21–B25 ONLY; all old
B26/B27 extension plans below are superseded by the current v0.3–v0.5 B26–B41 train
**Last reconciled:** 2026-07-18
**Author:** Thinker session (planning only)
**Prerequisites:** B20 complete (all 11 success gates), B19 P0 complete (gateway budgets/rate limits + retest)
**Target version line:** v0.2.0 ("Share")

---

## 1. One-line goal

Let a builder export an agent as a single signed file, hand it to a friend
or coworker, and let the receiver verify who built it, see exactly what it
is allowed to do, run it under their own credentials and policy enforcement,
and fork/modify/redistribute it under their own identity — all open source,
all offline-capable, all audited.

## 2. Wedge extension

Phase 1 wedge: "AI-generated agent code becomes a signed, sandboxed,
policy-controlled, audited workload in one command."

Phase 2 wedge: "That workload becomes a shareable app. Anyone who receives
it knows who built it, knows it was not tampered with, and sees exactly
what it can touch before it runs. Modify it, and the lineage travels with
it."

This is the app-store trust model without an app store: signatures,
provenance, and policy transparency replace centralized review. Security
and governance are the product, distribution is the feature.

## 3. Personas

- **Publisher (P):** Existing Phase 1 persona. Builds agents with Hermes on
  a Mac. Wants to share a working agent with a coworker without a "clone my
  repo and pray" workflow. Technical enough to read a fingerprint aloud.
- **Receiver (R):** Mac + Hermes user, possibly less technical than P. Gets
  a file over Slack/AirDrop/email. Wants: "is this really from P, what will
  it do, what do I need to provide (API keys), how do I run it." Must reach
  first successful run in under 10 minutes.
- **Modifier (M):** A receiver who forks. Changes the prompt, adds an egress
  domain, redistributes. Downstream receivers must see that M changed P's
  agent and what changed in the policy.
- **Security reviewer (S):** Coworker of R at a company. Never runs the
  agent. Uses `bundle inspect` and `provenance show` output to approve or
  reject. Cares about egress list, credential requests, MCP tools, SBOM,
  and lineage.

## 4. User stories

1. As P, I run `agentpaas export` and get `weather-agent-1.2.0.agentpaas`,
   a single file I can AirDrop. Export refuses if any secret material would
   be included.
2. As R, I run `agentpaas install weather-agent-1.2.0.agentpaas`, see a
   consent card (publisher fingerprint, provenance, full policy, required
   credentials), verify the fingerprint out-of-band with P, approve, map my
   own Keychain secrets to the declared credential IDs, and run it.
3. As R, if a single byte of the bundle was modified in transit, install
   fails closed with a clear tamper message before any consent prompt.
4. As R, I ask Hermes "install the bundle Sarah sent me" and Hermes walks
   me through inspection and credential mapping, but the trust approval
   itself happens in my terminal, typed by me.
5. As M, I run `agentpaas fork weather-agent@a1b2c3d4 ./my-weather` and get
   an editable project with lineage recorded. When I export, my bundle
   carries a signed provenance chain: created-by-P, forked-by-M, plus a
   policy delta ("M added egress: api.slack.com:443").
6. As S, I run `agentpaas bundle inspect file.agentpaas` on a machine with
   no daemon running and get a full report without installing anything.
7. As P, I update the agent to 1.3.0 and re-share. R's reinstall shows an
   abbreviated card if the policy digest is unchanged, and a full re-consent
   card with policy diff if it changed. Downgrades warn.

## 5. Non-goals (v0.2.0)

- No hosted registry, no discovery/search, no ratings. Distribution rail is
  a file. OCI-registry push/pull and GitHub-release install were deferred from
  v0.2; the current private component catalog is specified in B31.
- No payment, licensing enforcement, or DRM. MIT open source; bundles are
  inspectable by design.
- No Linux receivers. Mac-only (darwin/arm64 primary, amd64 via rebuild).
- No key escrow/recovery service. Key backup is a local encrypted export.
- No automatic updates or update polling.
- No policy-narrowing overlay at install (receiver accepts policy or
  forks). Narrowing overlay was deferred from v0.2.
- No revocation infrastructure in v0.2.0; a future spec must re-authorize it.

## 6. Locked design decisions

**D1 — Source-first distribution.** The signed unit of sharing is
source + policy.yaml + agent.lock (+ SBOM). A prebuilt OCI image is an
optional bundle payload (`--with-image`). Default receiver path: verify
source against the signed `build_input_digest`, rebuild locally with locked
deps, run. Rationale: cross-machine image bit-reproducibility is not
achievable with Docker builds; arch mismatch (arm64/amd64) breaks
image-only sharing; source transparency is the governance story ("read what
you run"). Rejected: image-only bundles (opaque, arch-fragile, unauditable).

**D2 — Local publisher identity, TOFU trust, Keychain-backed.** Publisher
identity is a long-lived ECDSA P-256 keypair in the macOS Keychain (reuses
`internal/identity` keystore, new key type `publisher_identity`), separate
from per-agent package AIDs. Receivers pin publisher keys in a local trust
store on first use and verify the fingerprint out-of-band (read it over
Slack/phone, Signal safety-number model). Key changes for a known publisher
hard-fail with an SSH-style warning. Rejected for v0.2.0: Sigstore keyless
(OIDC dependency, online-only, wrong for offline friend-sharing; revisit in
a separately approved public-distribution spec).

**D3 — Signature proves provenance, never endorsement.** All UX copy says
"from <publisher>, unmodified since signing." Never "verified safe." The
consent card (policy + credentials + provenance + SBOM) is the safety
review surface. This must be enforced in CLI strings, plugin strings, and
docs, and checked in B25 docs review.

**D4 — Policy travels signed and is approved explicitly at install.**
`agent.lock.policy_digest` binds the policy; B20 T03 runtime verification
already enforces digest match at run. Install adds the human approval
layer: the receiver approves the exact policy digest. Any policy change on
update forces re-consent with a diff. Receivers who want a different policy
fork and re-sign. Local unsigned state is limited to credential-name
mapping, which never widens capability.

**D5 — Fork requires re-signing under the modifier's identity.** Provenance
is an append-only array in the lock; each entry is signed by that entry's
publisher key and carries the parent lock digest. Chain semantics are
honest: each entry is a signed claim by that signer about its parent. The
chain proves lineage claims and end-artifact integrity; it does not prove
intermediate artifact contents unless intermediate bundles are available.
Docs must state this.

**D6 — Trust approval is human-gated in the terminal.** The Hermes plugin
install tool can inspect, explain, and prepare, but cannot approve trust or
accept a policy. The user types the confirmation in their terminal
(interactive prompt) or supplies explicit `--confirm-fingerprint` +
`--accept-policy` values obtained from `bundle inspect`. Mirrors the B17
secret-ingestion lesson. Known limitation: the daemon cannot prove a human
typed the flags; the plugin-side rule is the control and is documented.

**D7 — Namespacing by publisher.** Installed agents are addressed as
`<agent-name>@<pub8>` where pub8 = first 8 hex chars of the publisher
fingerprint. Receivers may set a local alias. Bare names remain valid only
for locally-packed agents. Every UI surface that shows a shared agent shows
its publisher. Rationale: no registry means no global namespace; names are
only meaningful per-publisher; this kills typosquatting confusion at the
root.

**D8 — Bundle is a deterministic tar.gz with extension `.agentpaas`.**
Sorted entries, fixed mtimes (SOURCE_DATE_EPOCH), uid/gid 0, gzip without
timestamp. Bundle digest = SHA-256 of the file. No new compression deps.

**D9 — Export is fail-closed on secret material.** gitleaks re-scan of the
exact export fileset, denylist for `.env`-like files, `.git/` excluded by
default (history can hold secrets), full included-file manifest displayed
before writing. A sentinel-secret red-team test gates the release.

**D10 — Reuse, do not reinvent.** Lock signing, canonical JSON, Keychain
keystore, gitleaks scan, audit chain, and B20 runtime verification are the
substrate. Phase 2 adds schema v2 fields, a bundle container, a trust
store, and install/fork flows. No new crypto primitives.

## 7. Canonical schemas (normative for B21–B25; later additions use current specs)

### 7.1 agent.lock schema v2 (extends v1, `schema_version: 2`)

New fields on `AgentLock`:

```json
"publisher": {
  "name": "parvez",
  "fingerprint": "<sha256-hex-of-DER-SPKI>",
  "public_key_pem": "-----BEGIN PUBLIC KEY-----...",
  "signed_at": "2026-07-06T00:00:00Z"
},
"publisher_signature": "<base64 ECDSA over canonical lock map minus both signature fields>",
"provenance": [
  {
    "action": "created",
    "publisher_fingerprint": "<fp>",
    "publisher_name": "parvez",
    "publisher_public_key_pem": "...",
    "agent_name": "weather-agent",
    "agent_version": "1.2.0",
    "parent_lock_digest": "",
    "parent_bundle_digest": "",
    "parent_policy_digest": "",
    "policy_delta": null,
    "timestamp": "...",
    "entry_signature": "<base64, by this entry's publisher key>"
  }
]
```

Rules:
- v1 locks continue to run locally (B20 compatibility rules apply). Export
  requires v2. Install requires v2.
- `publisher_signature` and package-AID `lockfile_signature` are both
  present in v2; both must verify.
- Provenance entries are append-only; each entry's `entry_signature` covers
  the canonical JSON of the entry minus the signature field. Entry [0] must
  have `action: created` and empty parent digests. Entry [n>0] must have
  `action: forked` and a non-empty `parent_lock_digest`.
- `policy_delta` (optional, forked entries): `{ "egress_added": [...],
  "egress_removed": [...], "credentials_added": [...],
  "credentials_removed": [...], "mcp_tools_added": [...],
  "mcp_tools_removed": [...] }`. Marked "publisher-claimed" in receiver UX
  unless the parent bundle is locally available for recomputation.

### 7.2 Bundle layout (`bundle_schema_version: 1`)

```
weather-agent-1.2.0.agentpaas   (deterministic tar.gz)
├── manifest.json               signed bundle manifest
├── agent.lock                  signed lock v2
├── policy.yaml                 exact bytes matching lock.policy_digest
├── sbom.spdx.json
├── source/                     agent.yaml, main.py, requirements.txt,
│                               uv.lock, ... (exact fileset of
│                               build_input_digest)
└── image/                      OPTIONAL OCI image layout (index.json,
                                oci-layout, blobs/) when --with-image
```

`manifest.json`:

```json
{
  "bundle_schema_version": 1,
  "format": "agentpaas-bundle",
  "created_at": "...",
  "publisher": { ...same as lock.publisher... },
  "agent_name": "weather-agent",
  "agent_version": "1.2.0",
  "contents": {
    "agent_lock":  {"path": "agent.lock",     "sha256": "..."},
    "policy":      {"path": "policy.yaml",    "sha256": "..."},
    "sbom":        {"path": "sbom.spdx.json", "sha256": "..."},
    "source":      {"path": "source/", "digest": "<build_input_digest>",
                    "file_count": 6, "total_bytes": 14210},
    "image":       {"path": "image/", "digest": "sha256:...",
                    "platform": "linux/arm64"}
  },
  "manifest_signature": "<base64, publisher key, canonical map minus sig>"
}
```

`contents.image` is null when the image is not included.

### 7.3 Trust store — `~/.agentpaas/trust/publishers.json` (0600)

```json
{
  "version": 1,
  "publishers": [
    {
      "fingerprint": "<64-hex>",
      "public_key_pem": "...",
      "alias": "parvez",
      "first_seen": "...",
      "last_used": "...",
      "source": "tofu | manual",
      "status": "trusted"
    }
  ]
}
```

Every mutation appends a daemon audit record (`publisher_trusted`,
`publisher_removed`, `publisher_key_conflict`).

### 7.4 Install manifest — per installed agent

Location: `~/.agentpaas/state/agents/<name>@<pub8>/install-manifest.json`

```json
{
  "version": 1,
  "bundle_digest": "sha256:...",
  "lock_digest": "sha256:...",
  "publisher_fingerprint": "...",
  "accepted_policy_digest": "...",
  "installed_at": "...",
  "install_mode": "prebuilt-image | local-rebuild",
  "local_image_digest": "sha256:... (when rebuilt)",
  "credential_map": { "openrouter-key": "my-openrouter-key" },
  "alias": "weather"
}
```

`credential_map` maps credential IDs declared in the signed policy to local
Keychain secret names. It can only rename, never add scope. The broker
resolves through this map at run time.

## 8. CLI surface added in Phase 2

```
agentpaas identity init                    create publisher identity (Keychain)
agentpaas identity show                    name + fingerprint (display-grouped)
agentpaas identity export --out <file>    encrypted backup (passphrase)
agentpaas identity import <file>          restore

agentpaas trust add <fp> [--alias <n>]    pin a publisher manually
agentpaas trust list | show <fp> | remove <fp>

agentpaas export [project-dir] [-o <file>] [--with-image] [--include <glob>...]
agentpaas bundle inspect <file> [--json]  offline, no trust decision, no install

agentpaas install <file>
    [--alias <name>]
    [--map-credential <declared>=<local> ...]
    [--confirm-fingerprint <fp>]           non-interactive trust approval
    [--accept-policy <policy-digest>]      non-interactive policy approval
    [--allow-downgrade]
agentpaas installed list | remove <name@pub8>

agentpaas fork <name@pub8> <target-dir>
agentpaas provenance show <name@pub8 | bundle-file> [--json]
```

`run`, `trigger invoke`, `cron add`, `logs`, `audit` accept `<name@pub8>`
and aliases everywhere a local agent name is accepted today.

## 9. Threat model delta

| # | Adversary | Attack | Control | Block |
|---|-----------|--------|---------|-------|
| A1 | Man-in-the-middle / storage tamper | Modify source, policy, lock, or image in the bundle | Per-file SHA-256 in signed manifest; signed lock; digest chain into B20 T03 runtime verification | B22/B23 |
| A2 | Impersonator | "This bundle is from Parvez" with attacker key | TOFU pinning + out-of-band fingerprint verification; key-change hard fail for known publishers | B21/B23 |
| A3 | Malicious publisher | Signed bundle with hostile policy (exfil egress, broad credentials) | Consent card shows full policy + credential requests; D3 language; policy lints (warn on wildcards, raw IPs, non-TLS ports, >N domains) | B23 |
| A4 | Malicious modifier in chain | Fork adds exfil egress, hopes receiver trusts original author | Provenance chain shows every signer; policy delta highlighted; final publisher is the trust anchor in UX | B24 |
| A5 | Forged lineage | Fabricated provenance entries or truncated chain | Per-entry signatures, parent digest linkage, structural rules (entry 0 = created) | B24 |
| A6 | Rollback attacker | Re-share an old vulnerable version | Monotonic version check per (publisher, name); `--allow-downgrade` explicit | B23 |
| A7 | Bundle-as-exploit | zip-slip paths, symlinks, absolute paths, decompression bombs | Hardened extractor: reject `..`, symlinks, absolute paths; per-file and total size caps; entry-count cap | B22 |
| A8 | Accidental self-leak | Publisher bundles `.env`, keys, git history | Export fail-closed scan + denylist + `.git` exclusion + file manifest preview | B22 |
| A9 | Confused-deputy LLM | Hermes tricked into auto-installing/auto-trusting | Human-gated consent (D6); plugin tool cannot pass approval flags | B25 |
| A10 | Dependency drift on rebuild | Receiver rebuild resolves different deps than publisher build | uv.lock required in bundle for rebuild path; absent lock = hard warning + recorded in install manifest | B23 |
| A11 | Stolen publisher key | Attacker signs as publisher | Out of scope v0.2.0; future revocation requires a separately approved spec; docs state the limitation | Future |

## 10. Phase 2 security claims (each maps to a red-team gate in B25)

1. **Integrity:** any single-byte modification of any bundle payload causes
   install to fail before the consent card, with nothing written to state.
2. **Provenance:** a bundle that verifies was signed by the holder of the
   displayed fingerprint's private key, and its source/policy/SBOM match
   what that key signed.
3. **Policy transparency:** the policy shown on the consent card is
   byte-identical (digest-verified) to the policy enforced at run time.
4. **No secret export:** no Keychain value, lease file, or `.env` content
   can appear in an exported bundle.
5. **No credential sharing:** installing and running a shared agent never
   transfers publisher credentials; receivers bind their own secrets.
6. **Lineage integrity:** forged, reordered, or truncated provenance chains
   are rejected; a verified chain of length 3 renders correctly.
7. **Human consent:** no install completes through the Hermes plugin
   without explicit human terminal confirmation.

## 11. Historical roadmap and block map

Sequencing after current state:

- v0.1.2 = B20 Security Claim Closure (in flight, prerequisite)
- v0.1.3 = B19 P0 subset (gateway budgets/rate limits + B18 retest);
  B19 P1/P2 items may interleave later, they do not block Phase 2
- v0.2.0 = Phase 2 "Share": B21 → B22 → B23 → B24 → B25
- v0.2.3 = B21–B25 sharing plus release hardening, shipped.
- The B26/B27 rows from this PRD are deleted as active plans. Current B26 is
  durable deployment/run/workflow state and current B27 is progress/checkpoint/
  artifact protocol; both are implemented. The current release train continues
  B28–B41, closing stable v0.3 at B32, v0.4 at B35, and v0.5 at B41.

| Block | Name | Depends on | Ships |
|-------|------|-----------|-------|
| B21 | Publisher identity, trust store, provenance schema | B20 | identity CLI, lock v2, trust store, audit events |
| B22 | Bundle format, export, inspect | B21 | `.agentpaas` format, export/inspect CLI, safe extractor |
| B23 | Verified install, consent, credential mapping, run integration | B22, B20 T03/T07 | install/installed CLI, TOFU flow, namespaced run |
| B24 | Fork, modify, redistribute, provenance chain | B23 | fork CLI, chain append/verify, policy diff |
| B25 | Hermes sharing UX, two-persona E2E, red-team gate, v0.2.0 release | B24 | plugin tools, SKILL flows, S1-S10 test cards, release |

Dependency notes:
- B21 and B22 are backend-only and can start the moment B20 gates pass;
  they do not touch B19 surfaces.
- B23 depends hard on B20 T03 (runtime lock/policy verification) and
  B20 T07 (fail-closed missing credentials); do not start B23 before those
  gates are green.
- B24 T04 (update semantics) depends on B23 T07.
- Later Hermes, catalog, MCP, A2A, and artifact behavior is governed by
  `Agentpaas-pitch.md` decisions D34–D65 and current B28–B41, not this sharing
  PRD.

## 12. Success metrics

- Receiver time-to-first-run from receiving a bundle: under 10 minutes,
  measured in B25 S-cards with a fresh home dir.
- Tamper detection: 100% of the B25 tamper matrix fails closed.
- Secret export: 0 sentinel leaks across the B25 red-team suite.
- Design-partner signal (post-release): of 10 real sharing attempts among
  design partners, at least 3 receivers run a shared agent without
  publisher hand-holding beyond fingerprint verification.

## 13. Kill / de-scope conditions (stated honestly)

- If fewer than 3 of 10 design-partner sharing attempts result in a receiver
  actually running the agent, sharing is not the wedge; freeze the deferred
  distribution backlog and refocus on single-machine governance depth. The
  low-cost Codex/conformance/Approval Pack proof may still complete because it
  validates platform portability and security evidence without hosted rails.
- If consent cards are universally rubber-stamped (observed in partner
  sessions), the governance value claim weakens; response is policy lints
  and diff-only re-consent, not more ceremony.
- If TOFU fingerprint verification proves too heavy for the friend-sharing
  audience, do not weaken it silently; that is the trigger to prioritize
  a future approved Sigstore-keyless design instead.

## 14. Global pitfalls (all blocks; blocks repeat their own)

1. **Never conflate signing with safety** in any string, doc, or plugin
   message. Grep gate in B25.
2. **Keychain prompts:** signing at export and key access at install can
   trigger macOS Keychain UI. All flows must survive "user must unlock
   keychain" and give actionable errors when denied. Reuse the existing
   `ErrKeychainLocked` handling patterns.
3. **agent name keyed state:** daemon state, trigger targets, cron
   schedules, audit queries, and `resolveRunTarget` all key on agent name
   today (see B18-003). Namespaced names (`name@pub8`) must be threaded
   through every one of these surfaces or shared agents will half-work.
4. **The lock stores policy as sidecar bytes** (`PolicyYAML` excluded from
   the canonical signature map, digest included). Bundle must carry the
   exact deployed `policy.yaml` bytes; re-serialization would change the
   digest and brick install.
5. **Determinism regressions:** bundle digests are only stable if tar
   ordering, mtimes, and gzip headers stay fixed. Golden-fixture tests must
   pin the byte-level digest, not just structural equality.
6. **Two-home-dir testing:** Keychain service names derive from
   `sha256(homeDir)`, which makes a second `AGENTPAAS_HOME`/`HOME` a clean
   receiver simulation. If home-dir override is not currently supported
   end-to-end, B25 adds a test-only override; never ship a default that
   weakens single-home assumptions.
7. **Do not break Phase 1 UX.** Local pack→run without any identity setup
   must keep working. `identity init` is required only for export.
8. **v1 lock migration:** repack-required errors must name the fix
   (`agentpaas pack` regenerates v2) exactly like B20 T03 legacy handling.

## 15. Doc/index synchronization

- `docs/execution/README.md` indexes the current B21–B41 dependency status.
- `docs/roadmap.md` marks B21–B25 shipped, B26/B27 implemented, and B28 next.
- Public docs added during Phase 2: `docs/sharing.md` (user guide),
  `docs/trust-model.md` (what signatures prove and do not prove),
  `docs/bundle-format.md` (format spec for third-party tooling).
