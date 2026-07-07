# Trust Model

This document describes what AgentPaaS publisher signatures prove,
what they do not prove, and how receivers should establish trust.
It covers the Phase 2 sharing feature set (B21–B25).

For the overall security threat model, see
[threat-model.md](threat-model.md). For known limitations, see
[known-limitations.md](known-limitations.md).

---

## 1. What a publisher signature proves

A publisher signature on a lock file proves exactly one thing:

> The holder of the private key corresponding to the displayed
> fingerprint signed these bytes.

Technically: the signature is an ECDSA P-256 signature over the
canonical map of the lock file (every field except the signature
fields themselves). Verifying the signature confirms that:

- The lock bytes were produced by someone who controlled the private
  key at signing time.
- The bytes have not been modified since signing (integrity).
- The publisher block embedded in the lock is self-consistent (the
  PEM-encoded public key's fingerprint matches the stored fingerprint
  field).

Provenance entries are individually signed by their respective
publishers, so a verified provenance chain confirms that each
intermediate publisher signed a claim about their parent artifact.
The last-signer rule (the final provenance entry's publisher must
match the lock's publisher) ties the chain to the artifact.

---

## 2. What a publisher signature does NOT prove

A valid signature establishes cryptographic integrity from a key
holder. It does NOT establish any of the following:

- **Safety.** The signed agent may contain harmful behaviour,
  exfiltrate data, or request overly broad credentials. The signature
  says nothing about what the code does — only that the key holder
  produced it and it has not been tampered with in transit.

- **Authorship of intent.** The key holder may have signed a bundle
  they did not write, did not understand, or were socially engineered
  into signing. A signature proves key possession, not competence or
  intent.

- **Absence of malice.** A publisher with a known-good reputation can
  ship a malicious agent. Trust in the person behind the key is a
  social decision; the signature provides the cryptographic identity
  to anchor that decision on, not a substitute for it.

- **Key custody.** The publisher's private key may be compromised (see
  adversary A11 in the threat model). AgentPaaS v0.2.0 does not ship
  a revocation mechanism; this is scoped for B26.

- **Binary-level tamper-proofing.** The signature covers the lock file
  and provenance entries. It does not cover the container image layers
  independently — those are verified through digest chaining during
  install (B23). A signed lock with a tampered image payload fails at
  the digest-verification stage, not at signature verification.

---

## 3. TOFU and out-of-band fingerprint verification

AgentPaaS uses **Trust-On-First-Use (TOFU)** for publisher keys.

### The TOFU flow

1. A receiver obtains a bundle (e.g., Slack, AirDrop, email).
2. The receiver runs `agentpaas install <bundle>`.
3. AgentPaaS sees a publisher fingerprint it has never encountered
   before. It displays the consent card with the fingerprint in
   display form (`a1b2 c3d4 e5f6 7890 abcd ef12 3456 7890`).
4. The receiver must **verify the fingerprint out-of-band**:
   - Read it aloud to the publisher during a call.
   - Compare it against a known-good value from a trusted channel
     (publisher's website, signed email, in-person).
   - Check it against a team wiki or internal registry.
5. Only after confirming the fingerprint matches does the receiver
   approve the install. AgentPaaS records the fingerprint in the
   trust store at `~/.agentpaas/trust/publishers.json`.

### Why TOFU matters

TOFU without out-of-band verification is **not trust**. If a receiver
clicks through the consent card without checking the fingerprint, a
man-in-the-middle or impersonator who substituted their own key passes
the check. The UX is designed to make the fingerprint prominent and
actionable precisely to encourage verification.

### Subsequent encounters

Once a fingerprint is pinned in the trust store, subsequent bundles
from the same publisher are automatically recognized. AgentPaaS
verifies the signature against the stored public key without
re-prompting for fingerprint confirmation.

If the publisher rotates their key (see section 4), the old
fingerprint is no longer valid. AgentPaaS **hard-fails** rather than
silently accepting a new key for a known publisher.

---

## 4. Key rotation consequences

Publisher key rotation (`agentpaas identity init --force-rotate`)
generates a new ECDSA P-256 keypair with a new fingerprint.

### What changes for the publisher

- All new bundles are signed with the new key.
- Old bundles still verify against the old public key (if the receiver
  has it pinned).
- The publisher must communicate the new fingerprint to all existing
  receivers out-of-band.

### What happens for receivers

- **Receivers who pinned the old key will hard-fail** when they
  attempt to install a bundle signed with the new key. The error
  message indicates a key change for a known publisher and directs the
  receiver to verify the new fingerprint out-of-band.
- There is no automatic key transition or trust delegation in v0.2.0.
  Key rotation is a **trust reset**: receivers must re-verify the new
  fingerprint just as they did on first use.
- Revocation-list support (where a publisher can declare a key
  compromised and receivers can automatically reject it) is planned
  for B26.

### Best practice

Publishers should avoid unnecessary key rotation. Treat the publisher
keypair like a long-lived identity credential. Export an encrypted
backup (`agentpaas identity export`) and store it securely offline.

---

## 5. Provenance chains and forks

Forked and re-exported bundles carry a **provenance chain**: an ordered
list of signed entries in the lock, each from a publisher who claims how
their artifact relates to a parent (digest + policy delta summary). Install
and consent UX surface the full chain when present.

### What a chain proves

- **Per-hop parent claims:** Each entry is signed by that hop's publisher
  and claims the identity and integrity of the parent lock (parent publisher
  fingerprint and parent lock digest at fork/pack time).
- **Final artifact integrity:** The lock you hold is signed by the **tail**
  publisher; signature verification ties the chain's last entry to that
  publisher's key.
- **Policy deltas (signer-claimed):** Each hop can record a summary of how
  policy changed relative to the claimed parent. These summaries are
  assertions by the signer — not independently verified unless you hold the
  parent artifacts and compare.

### What a chain does NOT prove

- **Parent content:** The chain does not embed parent source, `policy.yaml`
  bytes, or full parent locks — only digests and delta summaries.
- **Absence of deleted lineage:** A publisher may delete `lineage.json`
  before packing and ship a bundle whose chain starts with `created` and
  omits any parent reference. That claims original authorship. This is an
  unavoidable property of any DVCS-style redistribution model; the parent
  can always prove priority with their own signed artifact and earlier
  chain entries they published.
- **Safety of any hop:** Each signer attests to their own packaging step,
  not that the agent is safe to run. Review policy on every install (D3).

### Tail-anchor trust rule

When you install a forked or re-shared bundle, **anchor your trust decision
on the final signer** — the publisher whose key signed the lock you are
installing (the person who sent you this file).

Earlier signers are **lineage claims**: they describe history and parent
relationships, but your cryptographic trust relationship for this install is
with the tail publisher. Consent copy states this explicitly: you are
trusting the tail publisher; earlier signers are lineage claims, not a
substitute for reviewing the tail signature and policy.

### Chain cap (32 entries)

Chains are limited to **32 entries** to bound lock size and receiver work
(every hop's public key PEM and signature rides in the lock — on the order
of ~15KB at the cap). Beyond the cap, `pack` fails with advice to publish
as a new original with attribution rather than appending another hop.

### Locally verified hops

If you have the **parent publisher pinned** in your trust store **and** an
installed copy of the parent agent whose lock digest matches the digest
claimed in the next chain entry, that hop may be labeled **(locally
verified)** on the consent card.

This is a convenience when your machine already holds matching evidence —
no network fetch and no registry lookup. When you lack the parent install or
pin, verification is silent; the hop still appears as a signer-claimed
lineage step.

---

## 6. D3 language rules

All AgentPaaS documentation, CLI output, consent cards, and plugin
messages must follow these rules when describing signatures and
provenance:

| Never say | Always say |
|-----------|------------|
| "verified safe" or "safe to run" | "from `<publisher>`, unmodified since signing" |
| "trusted publisher means" (or any conflation of signing with trustworthiness) | Describe what the signature proves (section 1) separately from what it does not (section 2) |
| "guaranteed safe" or "secure" alone (without qualification) | "cryptographically signed" or "integrity-verified" |

The word **`signed`** must always co-occur with provenance wording
that clarifies what the signature establishes (integrity + key
possession) and does not establish (safety, intent). Examples:

- "Signed by `parvez` (`a1b2 c3d4 …`), unmodified since signing."
- "Provenance chain signed by 2 publishers; last signer `maria`."
- "Lock file from `parvez`, cryptographically signed and unmodified."

These rules exist because **signing is not a safety guarantee**, and
any language that implies otherwise trains users to click through a
security boundary without understanding what they are consenting to.

---

## 7. Trust store file

The trust store lives at `~/.agentpaas/trust/publishers.json` (mode
0600, directory 0700). Each entry records:

- **Fingerprint** (64 hex characters): the canonical identity.
- **Alias** (optional local label): convenience, not identity.
- **Public key PEM**: used to verify future signatures.
- **First-seen** timestamp.
- **Last-used** timestamp.
- **Source**: how the key was added (`install-tofu`, `trust add`, etc.).

Fingerprints are the identity. Aliases are local conveniences and
collisions between aliases are warnings, not errors. Trust decisions
must never be keyed on alias.