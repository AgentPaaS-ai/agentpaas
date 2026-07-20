# Block 23 — Verified Install, Consent, Credential Mapping, Run Integration

**Status:** COMPLETE — shipped in v0.2.x; historical execution record
**Date:** 2026-07-06
**Target version:** v0.2.0
**Depends on:** B22 (bundle format), B20 T03 (runtime verification),
B20 T07 (fail-closed missing credentials).
**Normative spec:** `docs/execution/planning/phase2-sharing-prd-v1.md`
(implements D2 TOFU flow, D3, D4, D6, D7; threat rows A1-A3, A6, A10).

## Why B23 exists

This is the receiver half of the wedge: verify who built it, see exactly
what it can do, consent explicitly, bind your own credentials, run under
your own enforcement. Everything B21/B22 built becomes user-visible here.
The consent card is the product moment; the TOFU trust flow is the
security moment; the name@pub8 threading is the engineering grind.

## Scope boundaries

IN: `agentpaas install`, TOFU + key-conflict handling, consent card,
credential mapping, install manifest, installed-agent state layout,
rebuild-from-source path, prebuilt-image path, name@pub8 threading through
run/trigger/cron/logs/audit, update/downgrade semantics,
`installed list/remove`.

OUT: fork (B24), plugin tools (B25), policy-narrowing overlay and
revocation (B26).

## Install flow (normative order)

```
open bundle → Verify() (B22)            FAIL → tamper error, exit, no state
→ trust check (TOFU / pinned / conflict) FAIL/decline → exit, no state
→ consent card + policy approval         decline → exit, no state
→ credential mapping (interactive/flags)
→ materialize state + install manifest
→ build or load image
→ B20 T03-equivalent post-install verify
→ registered; runnable as name@pub8
```

Nothing is written to `~/.agentpaas/state` before consent passes. Trust
pinning (a consented act) is written to the trust store at the moment the
user approves the fingerprint, before the policy consent step.

## Task list

### T01 — P0: trust resolution + TOFU consent

**Spec:**
- After Verify() passes, resolve `lock.publisher.fingerprint` against the
  B21 trust store:
  - **pinned + same key:** show "publisher: <alias> (pinned <date>)".
  - **unknown (TOFU):** display full fingerprint (display form) with
    instruction: "Verify this fingerprint with the sender over another
    channel before continuing." TTY requires typing the LAST 8 hex chars
    of the fingerprint (not y/n — forces reading it). Non-TTY requires
    `--confirm-fingerprint <full-fp>` matching exactly. On approval: pin
    to trust store (source `tofu`), audit `publisher_trusted`.
  - **alias/name match, different fingerprint:** SSH-style hard warning
    (`PUBLISHER KEY CHANGED — someone may be impersonating <alias>`),
    audit `publisher_key_conflict`, and REFUSE. Recovery is explicit:
    `agentpaas trust remove` then reinstall (documented in the error).
    No inline override flag.
- `--confirm-fingerprint` with a non-matching value fails with exit 2 and
  is audited (`publisher_confirm_mismatch`).

**How an agent tests it:**
- Unit table: pinned/unknown/conflict paths produce the exact prompts,
  audit events, and store mutations above.
- Non-TTY without flag → exit with instruction to run inspect first.
- Wrong typed suffix (TTY sim) → re-prompt twice then abort; nothing
  pinned.
- Conflict path: pin key A as alias `maria`, install bundle signed by key
  B claiming name `maria` → refusal + conflict audit; trust store
  unchanged.

### T02 — P0: consent card + policy approval

**Spec:**
- Rendered after trust resolution, from VERIFIED bundle contents only:
  publisher line, provenance (B21 FormatProvenance incl. deltas), full
  policy summary + lints (same renderer as B22 inspect T03 — shared code,
  not copied), credential requirements, install mode (prebuilt image vs
  rebuild), SBOM count, and the fixed D3 disclaimer line.
- Approval binds to the POLICY DIGEST: TTY prompt "Approve this policy?
  [type 'approve']"; non-TTY requires `--accept-policy <policy-digest>`
  matching `lock.policy_digest` exactly. Mismatch = exit 2 (stale digest
  from an older inspect = correct refusal).
- Accepted digest recorded in the install manifest
  (`accepted_policy_digest`); audit `install_policy_approved` (agent,
  publisher fp, policy digest).
- Reinstall/update of the same (publisher, name):
  - same policy digest → abbreviated card ("policy unchanged since last
    approval") + single confirm;
  - different digest → full card PLUS a structural policy diff (egress/
    credentials/MCP added/removed, computed locally from old vs new
    policy bytes stored in state — NOT the signer-claimed delta) and full
    re-approval;
  - version decrease per (publisher, name) → refuse without
    `--allow-downgrade` (A6), audited.

**How an agent tests it:**
- Golden card render (fixture bundle, deterministic).
- Non-TTY: correct digest proceeds; wrong/absent digest exit 2.
- Update matrix: same-policy update = short path; changed-policy update =
  diff shown (assert added domain appears) + re-approval required;
  downgrade refused; `--allow-downgrade` + explicit approvals proceeds and
  audits `install_downgrade_allowed`.
- Assert zero writes under `~/.agentpaas/state` on every decline path
  (temp-dir walk before/after).

### T03 — P0: credential mapping + broker integration

**Spec:**
- For each credential ID in the signed policy: TTY flow lists Keychain
  secret NAMES (labels only, existing SecretList), lets receiver map
  declared-id → local name or defer; non-TTY uses repeated
  `--map-credential <declared>=<local>`.
- Unmapped-but-declared credentials: install completes with a WARN, but
  run fails closed per B20 T07 with "map credential <id>:
  `agentpaas installed map-credential <ref> <declared>=<local>`" (add this
  subcommand).
- `credential_map` lives in the LOCAL install manifest (unsigned, PRD D4:
  renames only, never scope changes). Broker resolution order at run:
  declared id → map → local Keychain name → value. Destination/method
  validation still runs against the SIGNED policy rule for the declared
  id — mapping cannot widen scope by construction; add an explicit test
  proving a mapped credential is still route-scoped (B20 T04 semantics).
- Secret VALUES never appear in install flow output, manifest, or audit.

**How an agent tests it:**
- Map at install → credentialed run succeeds via gateway injection with
  the receiver's own secret (sentinel distinct from any publisher value).
- Defer at install → run fails closed pre-Docker with the actionable
  message; `map-credential` then run succeeds.
- Route-scope: mapped credential for rule A cannot be used for rule B
  (reuse B20 T04 adversary fixture against an installed agent).
- Grep install stdout/stderr/manifest/audit for sentinel → absent.

### T04 — P0: state materialization + image acquisition

**Spec:**
- State root: `~/.agentpaas/state/agents/<name>@<pub8>/` containing the
  verified `agent.lock`, exact `policy.yaml` bytes, `sbom.spdx.json`,
  `source/`, `install-manifest.json` (PRD 7.4), plus `parent-bundle.ref`
  (bundle digest + original file path) for B24.
- Image paths:
  - **rebuild (default):** run the pack build against the verified
    `source/` with locked deps (uv.lock required; absent → hard WARN
    prompt, recorded `deps_unlocked_rebuild: true` in manifest, A10).
    Resulting local image digest recorded as `local_image_digest`;
    `install_mode: local-rebuild`. The SIGNED lock keeps the publisher's
    image digest; runtime verification for rebuilt installs checks
    source digest + policy digest + LOCAL image digest from the manifest
    (see pitfall 1).
  - **prebuilt (`--prefer-image`, only when bundle has image/):** load
    OCI layout into Docker; loaded digest MUST equal lock.image_digest,
    else fail closed; platform mismatch (amd64 bundle on arm64) → refuse
    with "reinstall without --prefer-image to rebuild".
- Post-install verification: immediately run the B20 T03 check suite
  against the materialized state; failure rolls back the entire install
  (temp-dir + rename atomicity) and exits with a bug-report message.
- `agentpaas installed list` (name@pub8, alias, version, publisher,
  installed-at, mode) and `agentpaas installed remove <ref>` (removes
  state + containers; trust pin retained).

**How an agent tests it:**
- Rebuild path e2e on weather-agent bundle: install → run → real invoke
  response; manifest fields correct.
- Prebuilt path: digest-matching image loads and runs; digest-mismatched
  image (tampered blob that somehow passed — construct by editing after
  extraction) fails closed; wrong-platform refusal message.
- Missing uv.lock fixture → warn prompt, manifest flag set.
- Post-install tamper (mutate materialized policy.yaml between install
  and run) → run fails via B20 T03; mutate DURING install via injected
  hook → rollback leaves no state dir.
- list/remove round-trip; remove kills running containers first.

### T05 — P0: name@pub8 threading through the daemon

**Spec:**
- Extend `resolveRunTarget` (B18-003 site) and every agent-name keyed
  surface to accept B21 T06 refs and aliases: Run, ListRuns, StreamLogs,
  CancelRun, TriggerInvoke, CronCreate/List/Remove, AuditQuery filters,
  dashboard resource manager, container labels
  (`agentpaas.agent-ref=<name@pub8>`).
- Resolution: exact `name@pub8` → installed agent; alias → unique alias in
  install manifests; bare name → locally-packed agent first, and if it
  uniquely matches exactly one installed agent, resolve with an
  "ambiguous soon" info line; multiple matches → error listing candidates.
- Aliases: `--alias` at install, unique across installed agents, stored
  in manifest; `installed alias <ref> <alias>` to change.
- Every run/log/audit surface that prints an installed agent prints the
  publisher (D7): e.g. `weather@a1b2c3d4 (maria)`.

**How an agent tests it:**
- Matrix: run/trigger/cron/logs/audit each invoked by full ref, alias, and
  ambiguous bare name (two installs of same agent name from two
  publishers) — correct resolution or correct candidate-listing error.
- Local Phase 1 agents by bare name: zero behavior change (golden suite).
- Cron persisted with full ref survives daemon restart and fires the
  right agent (two same-named agents installed).

### T06 — P1: `agentpaas provenance show <installed-ref>`

**Spec:**
- Renders B21 T05 report from the installed lock; `--json` stable schema.
  Also accepts a bundle file path (delegates to inspect's section 4).

**How an agent tests it:**
- Golden output for installed weather agent (1-entry chain); exit 1 +
  no render on tampered installed lock.

### T07 — P1: sharing user guide

**Spec:**
- `docs/sharing.md`: publisher walkthrough (identity init → pack →
  export → send + read fingerprint aloud), receiver walkthrough
  (inspect → install → verify fingerprint → approve policy → map
  credentials → run), update/downgrade behavior, troubleshooting table
  (tamper, key conflict, unmapped credential, platform mismatch). D3
  language throughout.

**How an agent tests it:**
- Link checker; grep gate for forbidden safety-claim phrases; every
  command in the doc executes against a fixture bundle in a scripted
  smoke test.

## Success gate

1. Build + tests clean.
2. Two-home-dir E2E (publisher home exports; receiver home with separate
   trust store and Keychain namespace installs): TOFU verify → consent →
   map credential → run → real invoke output → audit shows publisher
   events. Under 10 minutes manual, scripted variant in CI-gated Docker
   suite.
3. Single-byte bundle tamper → refusal BEFORE consent card; zero state
   writes on every refusal/decline path.
4. Key-conflict impersonation fixture refused with conflict audit.
5. Changed-policy update forces re-approval with correct diff; downgrade
   refused by default.
6. Receiver's sentinel secret never visible to agent code (rerun B20 T01/
   T02 adversary fixtures against an INSTALLED agent).
7. Phase 1 golden dataset unchanged.

## Pitfalls

- **Rebuilt-image digest vs signed lock.** The receiver's rebuilt image
  digest will NOT equal lock.image_digest (Docker builds are not
  bit-reproducible cross-machine). Runtime verification for installed
  agents must check the local manifest's `local_image_digest` for the
  image while keeping source/policy checks against the SIGNED lock. Get
  this split wrong in one direction and installs never run; wrong in the
  other and image tamper goes undetected. Dedicated tests both ways.
- **Consent bypass via daemon API.** Install must be a daemon operation
  with the approval inputs as request fields; the daemon enforces
  digest-match server-side so a buggy/hostile client cannot skip checks.
  D6's human gate is client-side policy (documented limitation), but
  digest binding is server-side.
- **Trust store writes before full consent.** Pin at fingerprint-approval
  time is intentional (the user verified the key), but policy decline
  after that must leave the pin — document, don't "clean up" the pin.
- **Alias/name collision UX.** Two publishers, same agent name is the
  COMMON case among coworkers. Never silently pick one; test it.
- **Container label cardinality.** `@` in Docker names is invalid;
  labels carry the ref, container NAMES keep the existing
  `agentpaas-agent-<id>` scheme. Do not put `name@pub8` into DNS-ish
  identifiers.
- **Keychain namespace in tests.** Receiver-home simulation needs
  `AGENTPAAS_HOME`-style override for trust/state; Keychain service name
  already derives from home hash. Verify the override reaches
  `internal/secrets/keychain.go`; if not, add test-only plumbing (PRD
  pitfall 6).
