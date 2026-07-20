# Block 24 — Fork, Modify, Redistribute: Provenance Chains

**Status:** COMPLETE — shipped in v0.2.x; historical execution record
**Date:** 2026-07-06
**Target version:** v0.2.0
**Depends on:** B23 (installed agents, install manifests, parent-bundle
ref). B21 T05 provenance library is the verification substrate.
**Normative spec:** `docs/execution/planning/phase2-sharing-prd-v1.md`
(implements D5; threat rows A4, A5).

## Why B24 exists

Sharing without forking is a demo; forking with lineage is a distribution
model. B24 lets a receiver turn an installed agent into an editable
project, modify it, and re-export under their OWN identity with a signed
provenance chain and an honest policy delta. Downstream receivers see
every hand the agent passed through and exactly how the policy changed at
each hop.

## Scope boundaries

IN: `agentpaas fork`, lineage record in forked projects, provenance-entry
append at pack/export of forked projects, policy-delta computation,
chain rendering already shipped in B21/B22/B23 (extended for deltas),
multi-hop chain verification hardening.

OUT: upstream update notifications ("parent released 1.3.0"), merge
tooling, revocation (B26).

## Design notes (binding)

- Fork source of truth is the INSTALLED, VERIFIED state dir (B23 T04),
  not the original bundle file (which may be gone). `parent-bundle.ref`
  supplies the parent bundle digest for the chain entry.
- A forked project is a normal Phase 1 project + `lineage.json`. Everything
  (pack, run, policy edit, Hermes iteration) works identically; lineage
  only matters again at pack/export time.
- The chain entry for a fork is created at PACK time of the forked project
  (pack already appends the `created` entry for originals; forked projects
  append `forked` instead). Export just carries the lock. This keeps one
  signing site (pack) — no separate export-time signing path to audit.
- Policy delta in the chain entry is computed at pack time from
  `lineage.json`'s embedded parent policy bytes vs the project's current
  policy. It is signer-claimed by construction (PRD D5/A4 honesty rule):
  downstream verifiers can recompute it ONLY if they hold the parent
  artifacts; renderers must label it accordingly (B21 T05 already carries
  `ChainSemantics`).

## Task list

### T01 — P0: `agentpaas fork <installed-ref> <target-dir>`

**Spec:**
- Preconditions: ref resolves to an installed agent; its state passes
  B20 T03 verification NOW (never fork tampered state); target dir absent
  or empty.
- Materializes: `source/**` → project root; `policy.yaml` (exact bytes);
  `agent.yaml` untouched (name/version stay until the forker edits —
  fork does not auto-rename; see pitfall 3); plus `lineage.json`:

```json
{
  "version": 1,
  "parent": {
    "agent_name": "weather-agent",
    "agent_version": "1.2.0",
    "publisher_fingerprint": "<parent lock.publisher.fingerprint>",
    "publisher_name": "maria",
    "lock_digest": "<B21 LockDigest of parent lock>",
    "bundle_digest": "<from parent-bundle.ref>",
    "policy_digest": "<parent lock.policy_digest>",
    "policy_yaml_b64": "<parent policy bytes, for delta computation>",
    "provenance": [ ...full parent provenance array, verbatim... ]
  },
  "forked_at": "..."
}
```

- `lineage.json` is advisory input to pack, not itself signed; its
  contents become signed the moment pack embeds them into the new lock's
  provenance entry. Deleting it turns the project into an origin
  (pack emits `created`) — that is allowed but pack prints
  "lineage.json absent: packing as original work" when the dir previously
  had one is NOT detectable; instead, fork writes a `.agentpaas-lineage`
  marker line into agent.yaml comments? No — keep it simple: deletion is
  permitted and documented as "claiming original authorship", an
  unavoidable property of any DVCS-style model (docs state it; the parent
  can always prove priority with their own signed artifact).
- Prints next steps: edit → `agentpaas pack` → `agentpaas export`.
- Audits `agent_forked` (parent ref, parent lock digest, target dir).

**How an agent tests it:**
- Fork installed weather agent → project packs and runs unmodified
  (byte-equal source ⇒ same build_input_digest).
- lineage.json matches the golden schema; parent provenance array
  byte-equal to installed lock's.
- Fork of tampered installed state → refused before any file write.
- Non-empty target dir → refused.

### T02 — P0: pack-time `forked` provenance entry + chain append

**Spec:**
- Extend B21 T04 pack integration: when `lineage.json` exists and parses:
  1. verify the embedded parent provenance array structurally + all entry
     signatures (B21 T05 rules, minus last-signer-owns-lock check which
     applies to the parent's own lock, using
     `lineage.parent.publisher_fingerprint` as the expected tail signer);
     invalid lineage = pack FAILS closed ("lineage.json corrupt; re-fork
     or delete to pack as original");
  2. require publisher identity (fork packs cannot be publisher-less if
     lineage exists — else the chain tail rule breaks); error directs to
     `identity init`;
  3. new lock provenance = parent array verbatim + new entry
     `{action: forked, publisher_*: forker identity, agent_name/version:
     current agent.yaml, parent_lock_digest/bundle_digest/policy_digest:
     from lineage, policy_delta: T03 output, timestamp, entry_signature}`;
  4. both lock signatures (AID + publisher) as per B21.
- Self-fork (forker == parent publisher) is legal and renders as a normal
  hop.
- Chain length cap: 32 entries (constant); beyond → pack error advising
  to publish as original with attribution (prevents chain-bloat DoS on
  receivers, PRD A5-adjacent).

**How an agent tests it:**
- Fork → edit main.py → pack → lock has 2 entries; entry[1] verifies;
  parent entries byte-preserved; B21 VerifyProvenance passes.
- Two-hop: publisher A creates, B forks, C forks B's install → 3-entry
  chain verifies end-to-end; golden render matches PRD example shape.
- Corrupt one parent entry signature in lineage.json → pack fails closed.
- Fork-pack without identity → actionable error.
- 33-entry synthetic lineage → cap error.
- Tamper AFTER pack (edit provenance in lock) → B21 verification fails
  (regression re-assert, adversary-style).

### T03 — P0: policy delta computation

**Spec:**
- `internal/policy/delta.go`: `ComputeDelta(parentYAML, childYAML []byte)
  (*PolicyDelta, error)` over CANONICAL policy forms (reuse
  canonical.go): egress rules added/removed (domain+ports+methods as the
  identity key), credentials added/removed (id+type+destination), MCP
  servers/tools added/removed, ingress changes, hooks changes. Modified
  rule = removed(old)+added(new) — simple and honest.
- Deterministic ordering (sorted) so entry signatures are stable.
- Empty delta serializes as `null` (not `{}`) to keep created/forked
  entries visually distinct in JSON.
- Renderer additions (shared with B22 inspect / B23 consent card):
  additions rendered prominently (`+egress api.slack.com:443`), removals
  as `-`, always suffixed `(signer-claimed)` unless the verifier
  recomputed it locally (B23 update-diff path computes locally and says
  so).

**How an agent tests it:**
- Table: add egress, remove egress, change ports on same domain, add
  credential, add MCP tool, no-change → expected delta structs; no-change
  → null.
- Determinism: shuffled-input YAML orderings produce identical delta JSON.
- Delta appears in packed fork's chain entry and in B22 inspect output of
  the forked bundle (golden).

### T04 — P1: downstream install UX for forked bundles

**Spec:**
- B23 consent card for a multi-hop bundle: chain section shows every hop
  with deltas; the TRUST decision anchors on the FINAL signer (the person
  who sent it to you) per PRD A4 — card states: "You are trusting <tail
  publisher>. Earlier signers are lineage claims."
- Lint (extends B22 T03 lints): if any hop's claimed delta adds egress
  domains, surface `⚠ chain adds egress vs original: <list>` on the card.
- If the receiver ALSO has the parent publisher pinned and a parent
  artifact installed with matching lock digest, mark that hop
  `(locally verified)` instead of `(signer-claimed)` — cheap when
  available, silent when not.

**How an agent tests it:**
- Install 3-hop fixture: card golden includes tail-anchor sentence and
  per-hop deltas; egress-adding hop fires the lint.
- Receiver with parent installed: hop marked locally verified (fixture
  arranges matching digests); without parent: signer-claimed suffix.

### T05 — P1: docs — fork & lineage guide

**Spec:**
- Extend `docs/sharing.md` with fork walkthrough; extend
  `docs/trust-model.md` with chain semantics: what a chain proves (each
  signer's claim + final artifact integrity), what it does not (parent
  content, absence of deleted lineage), the tail-anchor trust rule, chain
  cap, and the "deleting lineage.json claims original authorship" note.

**How an agent tests it:**
- Link checker + forbidden-phrase grep gate; commands in doc smoke-tested
  against fixtures.

## Success gate

1. Build + tests clean; Phase 1 + B21-B23 suites green.
2. Three-persona E2E across three home dirs: A exports; B installs,
   forks, adds an egress domain, exports; C installs B's bundle — C's
   consent card shows the 3-hop chain, B as trust anchor, and the
   +egress delta lint; C runs it with C's own credential mapping.
3. Full forged-chain adversary matrix fails closed: forged middle entry,
   truncated chain, reordered entries, replaced tail, delta mutation
   after signing, lineage.json corruption at fork-pack time.
4. Policy delta is deterministic and matches a hand-computed fixture.
5. Self-fork and 2x same-publisher hops render correctly.

## Pitfalls

- **Chain grows the lock; lock is inside the bundle AND state.** 32-hop
  cap bounds it, but every provenance PEM (~450B) rides along; at cap
  that is ~15KB per lock — fine, but do not also embed parent POLICY
  bytes in the lock (they live only in lineage.json / delta summary).
- **Version semantics across forks.** agent.version is publisher-local;
  B23's downgrade check keys on (publisher fingerprint, name) so B's
  1.0.0 fork of A's 1.2.0 is NOT a downgrade. Test this explicitly —
  it is an easy off-by-identity bug.
- **Fork does not rename.** Two agents named `weather-agent` from A and B
  is the expected end state; B23 T05 disambiguation carries the load.
  Fork SHOULD print a hint suggesting the forker bump version and
  optionally rename to reduce human confusion.
- **lineage.json is attacker-visible input to pack.** Treat it as
  untrusted: size cap (1MB), strict JSON, signature verification of every
  embedded entry BEFORE embedding, reject unknown fields. It is the one
  new parse surface in this block.
- **Do not implement "verified against parent" by fetching anything.**
  Locally-verified hop marking uses only local installed state; no
  network, no registry assumptions (that is B26).
- **Renderer consistency.** Inspect, consent card, and provenance show
  must share one rendering package or the three will drift; B22 already
  mandates sharing — enforce via a single golden fixture set used by all
  three test suites.
