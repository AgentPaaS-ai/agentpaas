# Block 31 — Local Package Registry and Promotion (Reduced)

**Status:** EXECUTION-READY SPEC — reduced per architecture audit Fix 3
(2026-07-19) and founder answers on registry consumers
**Date:** 2026-07-19 (revised)
**Target release:** v0.3.0
**Depends on:** B30 complete; `make block30-gate` green
**Must complete before:** B32–B35 and Hermes integration
**Normative product decisions:** `Agentpaas-pitch.md`, decisions D40–D43 as
narrowed by this revision

## Why this block exists, reduced

The founder confirmed the registry consumers: Hermes during authoring, and
an authored orchestrator agent that delegates to an already-registered,
permitted sub-agent. The orchestrator queries the daemon beforehand, then
pins the chosen package identities into the signed workflow. Runtime
capability-schema matching is explicitly not a v0.5 requirement.

Therefore B31 ships the local package registry as a read API over the
existing B23 installed-agent store and B26 deployment store, plus one
promotion bit. It does not ship the six-state lifecycle, attestation
records, Agent Card import/export, or capability-schema resolver. Those are
post-v0.5 work driven by multi-publisher demand.

## Outcome

At block completion:

- One daemon query answers: which packages are installed, verified,
  promoted, and ready to be invoked, with their exact digests, aliases,
  deployment status, and declared capabilities (metadata, not matched).
- A package is promoted by an explicit authorized operation; un-promoted
  installed packages are visible but not delegable by an orchestrator.
- An orchestrator at authoring time lists promoted packages, selects worker
  /verifier/testing roles by name and declared capability metadata, and pins
  the exact package digests into the signed workflow via B26 admission.
- A runtime orchestrator receives only logical package identities; it never
  receives container addresses, substrate endpoints, or raw ports.

## What is NOT in this block (deferred to post-v0.5)

- Six-state lifecycle (`draft`/`tested`/`approved`/`deployed`/`deprecated`/
  `deactivated`). Replaced by one boolean: `promoted`.
- Test attestation records (fixture digest, result digest, signer,
  freshness expiry). v0.3 has no third-party packages to attest.
- A2A Agent Card generation and import quarantine. No external partners.
- Capability-schema resolution (input/output JSON schema intersection,
  deterministic ranked candidates). Deferred by founder decision; name and
  declared-metadata lookup suffice through v0.5.
- A separate transactional catalog store. The B23 installed-agent store and
  B26 deployment store are the data layer; this block adds a read API and
  one field.

## Registry data model

The registry entry is a joined view, not a new store:

```text
package: name@pub8, version, publisher fingerprint, package/policy digests
install: install mode, local image digest, installed-at, credential ids
deployment: deployment id, status (ACTIVE|INACTIVE), aliases, generation
promoted: boolean + promoted-at + promoted-by
capabilities: declared capability ids and descriptions from the signed
  package manifest (stored verbatim; not schema-matched in v0.3)
```

`promoted` defaults to false. A locally built and installed package is
runnable directly by its owner (existing B23/B26 behavior is unchanged);
promotion is the additional gate that makes a package eligible for
orchestrator delegation and for naming in another package's signed
workflow allowlist.

## Task sequence

|| Task | Name | Primary result |
|---|---|---|
| T01 | Registry read API and promoted flag | `promoted` field on installed state; daemon/CLI `registry list` and `registry show` joining installed + deployment + alias data |
| T02 | Promotion operation and delegation gate | Authorized `registry promote` / `registry demote`; workflow validation rejects allowlist entries naming un-promoted packages; audit events |

## T01 — Registry read API and promoted flag

### Required work

1. Add `promoted`, `promoted_at`, `promoted_by` to the installed-agent
   state (B23 install manifest), defaulting to false. Migration: existing
   installed agents read as `promoted: false`.
2. Add daemon + CLI read surface:
   - `agentpaas registry list [--json]` — every installed package with
     deployment status, aliases, digests, and promoted flag.
   - `agentpaas registry show <name|alias> [--json]` — full entry including
     declared capability metadata from the package manifest.
3. Declare capability metadata as additive optional fields in the package
   manifest (stored verbatim). No schema validation against other packages
   in v0.3.
4. Deterministic output ordering (name, then version). Bounded output.

### Tests to write first

- Empty registry, installed-not-promoted, installed-promoted entries.
- Migration: pre-registry installed state reads as not promoted.
- Name and alias resolution; ambiguous name lists candidates.
- Digests in output match the installed lock and deployment records.
- CLI/daemon parity; JSON schema golden.

### Exit gate

Hermes (or any CLI caller) can enumerate what is ready to invoke and read
exact digests without touching internal store paths.

## T02 — Promotion operation and delegation gate

### Required work

1. `agentpaas registry promote <ref>` and `agentpaas registry demote
   <ref>` — authorized local operations, idempotent, audited
   (`package_promoted`, `package_demoted` with fingerprint, digest, actor).
2. Workflow validation at pack/deploy time: a `workflow.yaml` service
   binding, pipeline stage, or child allowlist entry that names an
   un-promoted package fails validation with an actionable error ("promote
   it first: `agentpaas registry promote <ref>`").
3. Promotion does not grant runtime authority by itself: the signed
   workflow still pins exact digests at B26 admission, and policy
   intersection still applies per B32–B35.
4. Demotion prevents future workflow validation against the package but
   does not alter already-signed workflows (they are immutable).

### Tests to write first

- Promote/demote idempotency and audit events.
- Workflow naming un-promoted package fails before resource creation.
- Workflow naming promoted package pins exact digest at admission.
- Demote after workflow signed: existing workflow unaffected; new
  validation against the package fails.
- Locally owned packages still run without promotion (B23 regression).

### Exit gate

An orchestrator can only name promoted packages in a signed workflow, and
the pinned digest — never the registry entry — governs what actually runs.

## Exit gate

`make block31-gate` includes the B30 gate plus registry read/promotion
unit, race, migration, and workflow-validation tests. It is **NO-GO** if an
orchestrator can delegate to an un-promoted package, if a raw endpoint
reaches worker code, or if registry reads expose credential values.

## Handoff to B32–B35

B32 consumes `registry show` for communication-edge identity display. B33
consumes it for MCP service binding validation. B35 consumes it for
orchestrator role selection at authoring time. All three pin exact digests
at B26 admission; none depends on a lifecycle state machine, attestation
records, or capability-schema matching.

## Deferred (post-v0.5, demand-driven)

If a second publisher or a managed multi-package tenant appears, revisit:
lifecycle states beyond the boolean, test attestations, Agent Card
import/export, capability-schema resolution, and a dedicated catalog store.
D40–D43 record this narrowing.
