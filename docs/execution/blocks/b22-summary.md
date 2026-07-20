# Block 22 — Bundle Format, Export, and Inspect

**Status:** COMPLETE — shipped in v0.2.x; historical execution record
**Date:** 2026-07-06
**Target version:** v0.2.0
**Depends on:** B21 (publisher identity, lock v2, provenance library).
**Normative spec:** `docs/execution/planning/phase2-sharing-prd-v1.md`
(implements D1, D8, D9, PRD 7.2; threat rows A1, A7, A8).

## Why B22 exists

The `.agentpaas` bundle is the distribution unit for Phase 2: one
deterministic, signed, self-describing file a publisher can AirDrop or
Slack to a receiver. B22 ships the container format, the fail-closed
export pipeline, the hardened extractor, and an offline `bundle inspect`
that a security reviewer can run with no daemon and no trust decision.
Install (B23) consumes what this block produces.

## Scope boundaries

IN: bundle tar.gz format + manifest schema, `agentpaas export`,
`agentpaas bundle inspect`, safe extraction library, export secret-scan
gate, optional `--with-image` OCI payload, format doc.

OUT: install/consent/trust decisions (B23), fork (B24), plugin (B25),
registry push/pull (B26).

## Task list

### T01 — P0: deterministic bundle writer + safe reader library

**Spec:**
- New `internal/bundle/` package.
- Writer `Write(cfg BundleConfig, out io.Writer) (*BundleResult, error)`:
  - tar entries sorted lexicographically; mtime = SOURCE_DATE_EPOCH;
    uid/gid 0, uname/gname empty; mode 0644 files / 0755 dirs; gzip with
    zero MTime and no Name header. Reuse the pack pipeline's determinism
    conventions (sorted tar order, SourceDateEpoch).
  - Layout per PRD 7.2: `manifest.json`, `agent.lock`, `policy.yaml`,
    `sbom.spdx.json`, `source/**`, optional `image/**` (OCI layout).
  - `manifest.json` written LAST in construction order but FIRST in tar
    order; per-file sha256 for lock/policy/sbom; source digest =
    `build_input_digest` from the lock; `manifest_signature` by publisher
    key over canonical map minus the signature field (same
    canonicalization helper as B21 T04).
  - `BundleResult{ BundleDigest, Path, FileCount, TotalBytes }` where
    BundleDigest = sha256 of the final file bytes.
- Reader `Open(path) (*Bundle, error)` — hardened:
  - Reject: absolute paths, any `..` segment, symlinks/hardlinks, device
    or FIFO entries, duplicate paths, paths outside `source/`/`image/`/
    the four top-level files, entry names with control chars.
  - Caps (constants, tested): max entries 10_000; max single file 256MB;
    max total uncompressed 2GB; max manifest/lock/policy/sbom 10MB each.
    Exceeding any cap = typed error, partial extraction cleaned up.
  - Two-phase: (1) stream-scan headers + extract the four metadata files
    to memory, (2) extract `source/`/`image/` only on demand to a caller
    -supplied directory (install-time), never implicitly.
- `Verify(b *Bundle) (*VerifyReport, error)` — offline, no trust store:
  1. manifest parses, `bundle_schema_version` supported;
  2. manifest_signature verifies against manifest.publisher key;
  3. manifest.publisher equals lock.publisher (fingerprint + PEM);
  4. lock verifies (both signatures, B21 rules) and provenance verifies
     (B21 T05);
  5. per-file sha256s match manifest;
  6. policy.yaml canonical digest == lock.policy_digest;
  7. sbom sha256 matches AND lock.sbom_digest;
  8. recomputed source digest over extracted `source/` ==
     lock.build_input_digest (uses the same canonical build-context digest
     routine as `internal/pack/build.go` — refactor to share, do not
     copy);
  9. if image present: OCI index digest == manifest.contents.image.digest
     and == lock.image_digest, platform recorded.
  Report lists each check with pass/fail; ANY fail → `Verified=false` and
  install (B23) must refuse before consent.

**How an agent tests it:**
- Determinism golden: same input written twice → byte-identical file;
  pinned golden BundleDigest fixture fails on any format drift.
- Round-trip: write → open → verify all-pass; extracted source bytes equal
  input.
- Tamper matrix (each flips exactly one thing, each must fail at the named
  check): manifest byte, manifest signature, lock byte, policy byte,
  policy/lock digest mismatch, sbom byte, one source file byte, added
  source file, removed source file, image blob byte, publisher mismatch
  between manifest and lock.
- Zip-slip suite: crafted tars with `../x`, `/etc/x`, symlink to `/`,
  hardlink, duplicate entry, 20k entries, 3GB expansion bomb — all
  rejected with typed errors, nothing written outside target dir (assert
  via temp-dir walk).

### T02 — P0: export pipeline + secret-leak gate

**Spec:**
- `agentpaas export [project-dir] [-o file] [--with-image]
  [--include <glob>]...` (CLI + daemon handler; daemon owns Keychain
  signing).
- Preconditions (fail closed, in order):
  1. project packed and deployed lock is schema v2 WITH publisher block —
     else "run `agentpaas identity init` then `agentpaas pack`";
  2. deployed artifacts pass B20 T03 verification (never export what you
     would refuse to run);
  3. working source digest == lock.build_input_digest — else "source
     changed since pack; repack before export".
- Fileset: exactly the pack build-context fileset (same walker as
  build_input_digest) plus explicit `--include` additions, which are
  digest-recorded in the manifest as `extra_files` and NOT part of
  source digest verification but ARE sha256-pinned individually.
- Secret gate (D9), all mandatory:
  1. gitleaks scan over the exact export fileset (reuse
     `internal/pack/scan.go`), fail closed on findings;
  2. denylist filenames regardless of scan: `.env*`, `*.pem`, `*.key`,
     `*credentials*`, `id_rsa*`, `*.p12`, `.netrc`, `auth.json`;
  3. `.git/`, `.hermes/`, `__pycache__/`, `.venv/` always excluded;
  4. print the full included-file manifest (path + bytes) before writing;
     TTY prompts "Export N files? [y/N]"; `--yes` for non-TTY.
- `--with-image`: export the locked image (by lock.image_digest, never by
  tag) to an OCI layout under `image/`. Refuse with a clear error if the
  local image digest is missing.
- Output default `<name>-<version>.agentpaas` in cwd; prints BundleDigest
  and publisher fingerprint on success. Audits `bundle_exported`
  (agent, version, bundle digest, with_image, file count).

**How an agent tests it:**
- E2E: init → identity init → pack → export → Verify() all-pass.
- Sentinel red-team: project containing `.env` with
  `SENTINEL_EXPORT_SECRET`, a `notes.txt` embedding the sentinel, and a
  Keychain-stored secret used by policy → export (a) fails on scan; after
  removing files, (b) succeeds and untarred bundle grep for sentinel is
  empty; (c) keychain value never present (it is never in the fileset —
  test asserts anyway).
- Dirty-source: edit main.py after pack → export fails with repack
  message.
- No-identity and v1-lock projects → actionable failures, no partial file
  left on disk (temp + rename).
- `--with-image` round-trip: image digest in bundle == lock.image_digest;
  missing local image → typed failure.

### T03 — P0: `agentpaas bundle inspect`

**Spec:**
- `agentpaas bundle inspect <file> [--json]` — fully offline, read-only,
  no daemon, no trust store, no state writes. This is persona S's tool.
- Output sections:
  1. header: file, size, bundle digest, schema versions;
  2. integrity: Verify() report table (check-by-check PASS/FAIL);
  3. publisher: name + display fingerprint, with the fixed D3 line:
     "A valid signature proves who signed this and that it is unmodified.
     It does not mean the agent is safe. Review the policy below.";
  4. provenance: B21 `FormatProvenance` rendering incl. signer-claimed
     policy deltas;
  5. policy summary: every egress domain:port+methods, every credential
     id/type/header/destination, MCP servers+tools, ingress, hooks —
     rendered from the bundle's policy.yaml, no elision;
  6. policy lints (warnings, PRD A3): wildcard domains, raw-IP/CIDR
     egress, non-443 ports, >8 egress domains, credential bound to
     wildcard destination;
  7. requirements: credentials the receiver must map, llm provider,
     image included or rebuild-required, platform;
  8. SBOM: package count + top-level deps.
- `--json` emits the full structured report (stable schema; B25 plugin
  consumes it).
- Tampered bundle: sections 1-2 print with FAILs, exit code 1, sections
  3+ suppressed (never render attacker-controlled metadata as if
  authenticated).

**How an agent tests it:**
- Golden text + golden JSON for a fixture bundle (1-entry and 3-entry
  provenance variants).
- Every lint fires on a crafted lint-bait policy; clean policy fires none.
- Tampered fixtures: exit 1, FAIL lines present, policy/provenance
  sections absent.
- Runs with daemon stopped (test kills daemon first).

### T04 — P1: bundle format public doc

**Spec:**
- `docs/bundle-format.md`: layout, manifest schema, determinism rules,
  extraction hardening requirements for third-party readers, verification
  checklist (the 9 checks), caps table, schema-version evolution policy
  (additive fields minor; layout changes bump `bundle_schema_version`).
- States D3 language rules and links `docs/trust-model.md` (B21 T07).

**How an agent tests it:**
- Link checker passes; doc's check list count matches Verify()
  implementation (scripted grep of check IDs in code vs doc).

## Success gate

1. `go build ./... && go test ./internal/... -count=1` clean.
2. Byte-determinism golden holds; format drift breaks the build.
3. Full tamper matrix (11 cases) and zip-slip suite (7 cases) fail closed
   with zero filesystem residue.
4. Sentinel secret cannot be exported through any tested path.
5. `bundle inspect` renders complete policy + provenance for valid
   bundles, refuses authenticated-looking output for tampered ones, works
   offline.
6. E2E: pack → export → inspect on the weather agent, under 60s, bundle
   under 5MB without image.
7. No Phase 1 regression (existing suites green; export absent = nothing
   changes).

## Pitfalls

- **Share the digest walker.** Verify() check 8 must use the SAME
  build-context digest code as pack, or bundles verify on the publisher
  machine and fail on receivers (path separators, hidden files, walker
  ordering). Refactor to a shared function with its own tests.
- **policy.yaml exact bytes.** Copy the deployed sidecar bytes; never
  re-marshal (PRD pitfall 4 — digest would change).
- **gzip determinism.** `gzip.Writer` embeds MTime and OS byte; zero both.
  The golden-digest test exists to catch exactly this.
- **Never render tampered metadata as trusted.** Inspect must gate
  sections 3+ on Verify(); showing a forged publisher name above a small
  FAIL line is a phishing UI.
- **Temp-file hygiene.** Export and extraction both write via temp dir +
  rename; every error path removes temps (test with injected failures).
- **`--include` is a footgun.** Extra files bypass the source-digest story;
  they are pinned in the manifest but reviewers must see them — inspect
  lists them under a separate "extra files (not part of build)" heading.
- **Large repos.** Caps protect readers, but export should warn above
  50MB pre-compression; friend-sharing bundles should be small.
