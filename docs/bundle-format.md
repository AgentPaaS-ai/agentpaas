# .agentpaas Bundle Format

**Version:** 1 (`bundle_schema_version: 1`)

The `.agentpaas` bundle is the distribution unit for AgentPaaS Phase 2:
one deterministic, signed, self-describing file a publisher can send to a
receiver. This document specifies the format, manifest schema, determinism
rules, extraction hardening requirements, verification checklist, caps table,
and schema-version evolution policy.

## 1. Bundle Layout

```
weather-agent-1.2.0.agentpaas   (deterministic tar.gz)
├── manifest.json               signed bundle manifest (FIRST in tar order)
├── agent.lock                  signed lock v2
├── policy.yaml                 exact bytes matching lock.policy_digest
├── sbom.spdx.json              SPDX JSON SBOM
├── source/                     agent.yaml, main.py, requirements.txt,
│                               uv.lock, ... (exact fileset of
│                               build_input_digest)
└── image/                      OPTIONAL OCI image layout (index.json,
                                oci-layout, blobs/) when --with-image
```

### File ordering

`manifest.json` is written LAST in construction order (it records digests
of all other entries) but FIRST in tar order. This allows a reader to
extract the manifest first and then verify each subsequent entry against
the manifest's recorded digests.

## 2. Manifest Schema

```json
{
  "bundle_schema_version": 1,
  "format": "agentpaas-bundle",
  "created_at": "2026-07-06T00:00:00Z",
  "publisher": {
    "name": "parvez",
    "fingerprint": "<64-hex sha256 of DER-encoded SPKI>",
    "public_key_pem": "-----BEGIN PUBLIC KEY-----...",
    "signed_at": "2026-07-06T00:00:00Z"
  },
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
  "extra_files": [
    {"path": "extra/README.md", "sha256": "...", "bytes": 1024}
  ],
  "manifest_signature": "<base64 ECDSA P-256, publisher key, canonical map minus this field>"
}
```

- `contents.image` is `null` when the image is not included.
- `extra_files` is absent when no `--include` files were added.
- `manifest_signature` covers the canonical JSON of the manifest with the
  `manifest_signature` field removed. Canonicalization uses sorted keys,
  matching the `lockCanonicalMap` approach from `internal/pack/lock.go`.

## 3. Determinism Rules

| Property | Value |
|----------|-------|
| Tar entry order | Lexicographically sorted by path |
| File mtime | `SOURCE_DATE_EPOCH` (default: Unix epoch 0) |
| uid/gid | 0 |
| uname/gname | empty string |
| File mode | 0644 for files, 0755 for directories |
| gzip MTime | 0 (zeroed) |
| gzip OS byte | 255 (0xff) |
| gzip Name header | absent |

**gzip determinism is critical.** The Go `gzip.Writer` embeds a MTime field
and an OS byte in the gzip header. Both must be zeroed explicitly, or two
runs of the same input will produce different bytes. The golden-digest test
in `internal/bundle/determinism_test.go` catches exactly this.

The bundle digest (`BundleDigest`) is the SHA-256 of the final `.agentpaas`
file bytes (after gzip compression).

## 4. Extraction Hardening Requirements (for third-party readers)

A compliant `.agentpaas` reader MUST enforce the following:

### 4.1 Path validation

Reject any tar entry that is:
- An absolute path (leading `/`)
- Contains any `..` path component
- A symlink or hardlink
- A device file or FIFO
- A duplicate path (same path seen twice)
- Outside `source/`, `image/`, or the four top-level metadata files
  (`manifest.json`, `agent.lock`, `policy.yaml`, `sbom.spdx.json`)
- Contains control characters in the entry name

### 4.2 Size caps

| Cap | Value | Constant |
|-----|-------|----------|
| Max entries | 10,000 | `MaxEntries` |
| Max single file | 256 MB | `MaxSingleFileSize` |
| Max total uncompressed | 2 GB | `MaxTotalUncompressed` |
| Max metadata file (manifest/lock/policy/sbom) | 10 MB each | `MaxMetadataFileSize` |

Exceeding any cap produces a typed error. Any partial extraction must be
cleaned up — no files should remain outside the target directory.

### 4.3 Two-phase extraction

1. **Phase 1:** Stream-scan tar headers and extract the four metadata files
   (`manifest.json`, `agent.lock`, `policy.yaml`, `sbom.spdx.json`) to memory.
   Do not write to disk.
2. **Phase 2:** Extract `source/` and `image/` only on demand to a
   caller-supplied directory (at install time). Never extract implicitly.

## 5. Verification Checklist (9 checks)

`bundle.Verify()` performs the following checks offline, with no daemon,
no trust store, and no network access:

| # | Check ID | Description |
|---|----------|-------------|
| 1 | `manifest_parse` | Manifest parses as JSON; `bundle_schema_version` is supported; publisher fields are present |
| 2 | `manifest_signature` | `manifest_signature` verifies against `manifest.publisher.public_key_pem` |
| 3 | `publisher_match` | `manifest.publisher` equals `lock.publisher` (fingerprint + PEM) |
| 4 | `lock_provenance` | Lock verifies (both `lockfile_signature` and `publisher_signature` per B21 rules); provenance chain verifies (B21 T05) |
| 5 | `content_sha256` | Per-file SHA-256s for `agent.lock`, `policy.yaml`, `sbom.spdx.json` match manifest |
| 6 | `policy_digest` | `policy.yaml` canonical digest == `lock.policy_digest` |
| 7 | `sbom_digest` | SBOM SHA-256 matches manifest AND `lock.sbom_digest` |
| 8 | `source_digest` | Recomputed source digest over extracted `source/` == `lock.build_input_digest` (uses the same `ComputeBuildInputDigest` routine as `internal/pack/build.go`) |
| 9 | `image_digest` | If image present: OCI index digest == `manifest.contents.image.digest` AND `lock.image_digest`; platform recorded. Skipped (pass) if no image. |

Any check failure sets `Verified = false`. Install (B23) must refuse a
bundle that does not pass all checks.

## 6. Extra Files (`--include`)

Files added via `agentpaas export --include <glob>` are:
- NOT part of the source digest — they bypass `build_input_digest`
- Individually SHA-256-pinned in the manifest's `extra_files` array
- Listed under a separate "extra files (not part of build)" heading in
  `bundle inspect` output
- Subject to the same secret-leak gate as all other files

Reviewers must be aware that extra files are not covered by the source
digest verification story and should review them independently.

## 7. Schema-Version Evolution Policy

- **Additive fields** (new optional fields in manifest, lock, or contents):
  minor change, no `bundle_schema_version` bump. Old readers ignore
  unknown fields.
- **Layout changes** (new top-level files, changed tar ordering, changed
  compression): bump `bundle_schema_version`. Old readers reject unknown
  versions with a clear error.
- **Field removal or semantic change**: bump `bundle_schema_version`.

## 8. D3 Language Rules

All user-facing copy about bundles must state:

> A valid signature proves who signed this and that it is unmodified.
> It does not mean the agent is safe. Review the policy below.

Never use language like "verified safe" or "trusted." The consent card
(policy + credentials + provenance + SBOM) is the safety review surface.
See `docs/trust-model.md` for the full trust model.

## 9. Large Bundle Warning

Caps protect readers from decompression bombs, but export should warn
above 50 MB pre-compression. Friend-sharing bundles should be small
(source + policy + lock + SBOM, no image).

## Related

- [Trust Model](trust-model.md)
- [Agent Lock Schema](../internal/pack/lock.go) (source code)
- [Bundle Package](../internal/bundle/) (source code)
- [Export Pipeline](../internal/export/) (source code)
