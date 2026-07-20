# Pre-B31 Crypto / Signing Security Review

**Branch:** `sweep/p1c-crypto-review`  
**Base commit reviewed:** `88e470c7105111baa2d2db940d92e52a17097839`  
**Scope:** READ-ONLY analysis of cryptographic operations, signature verification, key handling, and canonical form computation.  
**Packages / files:**

| Area | Paths |
|------|--------|
| Agent lock / signatures | `internal/pack/lock.go`, `internal/pack/provenance.go`, `internal/pack/verify.go` |
| Bundle verification | `internal/bundle/verify.go`, `internal/bundle/writer.go` (signing helpers) |
| Trust store | `internal/trust/store.go`, `internal/trust/fingerprint.go` |
| Identity / keys | `internal/identity/*` (ca, filestore, keychain, publisher, spiffe, issuer) |
| Secrets | `internal/secrets/*` (keychain, store, broker) |
| Checkpoint digests (B30 F8) | `internal/routedrun/progress.go` |

**Review dimensions:** timing-safe compares, key leakage, canonicalization bypass, TOCTOU, durable digest verify-on-read, replay, KDF/entropy, error leakage, races.

---

## Executive summary

AgentPaaS’s core signing model is coherent and generally well engineered for a local-first P1 product:

- Lock, publisher, provenance entry, and bundle manifest signatures all use **ECDSA P-256 over SHA-256 of explicitly constructed canonical JSON maps** (Go `encoding/json` map key sort).
- Pack path **requires a publisher identity** and builds a signed provenance chain.
- Bundle offline verify cross-checks manifest signature, lock/publisher/provenance signatures, and content digests (lock/policy/sbom/source/image).
- Progress journals and control journals use **HMAC-SHA256 with `hmac.Equal`**.
- File keystore uses **AES-256-GCM + PBKDF2-HMAC-SHA256**, permission gating, and target-file symlink refusal.
- Secrets broker redacts credential values from `String`/`GoString`/`Format` and audit paths are tested against leakage.
- ID generation uses **`crypto/rand` (128 bits)**.

No **CRITICAL** issue was found that is a clear remote crypto break (forged publisher signature without key, or silent acceptance of tampered signed lock/bundle under the documented verification APIs).

Several **HIGH** and **MEDIUM** issues remain, primarily around:

1. **Incomplete checkpoint integrity binding** after B30 F8 (identity/sequence fields outside the digest).
2. **Private key exposure surfaces** during cosign import and macOS keychain `security(1)` argv.
3. **Missing timeouts** on identity keychain and cosign key import.
4. **Custom RFC 6979 ECDSA** implementation in bundle signing (correctness / maintenance risk).
5. **Trust PEM path not enforcing P-256**, fingerprint format inconsistency, and FileKeyStore parent-component symlink gap.

**B31 block recommendation:** Do **not** hard-block B31 on a CRITICAL crypto break. Treat **C-01 (checkpoint digest field coverage)** and **C-02 (AID private key on disk / cosign import)** as the top pre-merge hardening items if B31 touches resume or pack signing.

---

## Findings table

| ID | Severity | Location | Description | Recommendation |
|----|----------|----------|-------------|----------------|
| C-01 | **HIGH** | `internal/routedrun/progress.go:54-83`, `108-140`, `171-216` | Checkpoint `ComputeDigest` / `VerifyDigest` omit binding fields (`attempt_id`, `run_id`, `sequence`, `lease_id`, `checkpoint_id`, `artifact_meta_digest`, etc.). FS/local attacker can retarget or reorder identity of a content-valid checkpoint. | Include all security-relevant identity + sequence + artifact meta fields in canonical digest; fail closed on missing fields. |
| C-02 | **HIGH** | `internal/pack/lock.go:1302-1337`, `584-597` | Pack exports AID private key to temp files and runs `cosign import-key-pair` with `COSIGN_PASSWORD=` (empty). Unencrypted/empty-password signing material lands on disk for the pack window. | Prefer in-memory / FD-based signing; always encrypt temp keys; shred on cleanup; never empty password; scope temp dir 0700 + immediate wipe. |
| C-03 | **HIGH** | `internal/identity/keychain.go:86-104`, `200-210` | Identity Keychain store invokes `security` with **no timeout** and passes key material via **argv (`-w`)**. Hang/GUI lock risk; brief process-list exposure (documented P1 risk). | Mirror `secrets.KeychainStore` timeouts; move to Security.framework/CGo or helper reading secret from pipe; never put key bytes on argv. |
| C-04 | **MEDIUM** | `internal/bundle/writer.go:396-452` | Custom RFC 6979 deterministic ECDSA implementation for manifest signatures. Hand-rolled scalar arithmetic / k generation is a high-maintenance footgun; no low-S normalization. | Prefer a reviewed library (e.g. filippo.io/nistec + RFC6979 helper) or accept non-deterministic signatures + separate determinism strategy; add Wycheproof vectors; enforce low-S. |
| C-05 | **MEDIUM** | `internal/identity/filestore.go:183-191`, `270-276` | Symlink defense checks only the keystore **file**, not parent directory components. Parent symlink swap can redirect read/write. | Walk every path component with `Lstat` (pack’s `rejectSymlinkComponents` pattern). |
| C-06 | **MEDIUM** | `internal/trust/store.go:396-431` | `parsePublicKeyPEM` accepts any ECDSA curve (`Curve != nil` only). Pack path requires P-256; trust pin path does not. | Reject non-P-256 keys at pin/parse time; align with `pack.PublicKeyFromPEM`. |
| C-07 | **MEDIUM** | `internal/pack/lock.go:1316-1321` | `cosign import-key-pair` uses bare `exec.Command` (**no `CommandContext` / timeout**). Hung cosign can stall pack indefinitely while key material sits on disk. | Use `exec.CommandContext` with `externalSignatureTimeout`; cleanup temp keys on timeout. |
| C-08 | **MEDIUM** | `internal/routedrun/progress.go:449-458`, `internal/supervisor/hmac.go:27-31` | HMAC canonicalization ignores `json.Marshal` errors (`b, _ := json.Marshal(...)`). Failure yields MAC over empty/partial input. | Check marshal errors and fail closed. |
| C-09 | **MEDIUM** | `internal/routedrun/progress.go:405-427` + daemon journal key mount | Progress journal HMAC key is provisioned to the run environment. HMAC authenticates “holder of key” (harness/container), not honest agent semantics. Malicious agent code that can read the key can mint valid resume checkpoints. | Document clearly; consider harness-only key (not mounted into untrusted agent FS), or dual-sign daemon attestation of checkpoints. |
| C-10 | **MEDIUM** | `internal/pack/verify.go:49-52`, `86-102` | Build verify interpolates `entryFile` into a Python one-liner via `fmt.Sprintf` without escaping; harness freshness uses **MD5**. | Pass entry path via env/argv list; switch freshness hash to SHA-256. |
| C-11 | **MEDIUM** | `internal/pack/provenance.go:42-146`, `14` | Provenance chain verifies per-entry signatures and structural rules, but parent digests are **claims** only (explicit `chainSemantics`). Fork history is not independently re-validated against parent artifacts at verify time. | Keep claim model, but surface in verify UX; optional “deep verify” mode that fetches/checks parent lock/bundle digests. |
| C-12 | **MEDIUM** | `internal/identity/ca.go:336-353` vs `internal/pack/lock.go:993-1003` / `publisher.go:79-88` | Fingerprint encoding split: colon-separated (CA/AID helper) vs bare 64-hex (pack/publisher/trust). Comparison footguns across packages. | Standardize on one storage form (bare hex) + one display form; never mix in equality checks. |
| C-13 | **MEDIUM** | `internal/identity/ca.go:159-161` | `ttl <= 0` silently defaults to `time.Hour` instead of erroring. | Reject non-positive TTL explicitly. |
| C-14 | **MEDIUM** | `internal/identity/publisher.go:263-277` | `LoadPublisherSigningKey` returns raw `*ecdsa.PrivateKey` to callers (bundle write path). Expands private-key lifetime outside KeyStore. | Prefer `Sign`/`SignAsPublisher` callback; if load is required, document + zeroize responsibility at call sites. |
| C-15 | **LOW** | `internal/routedrun/progress.go:80`, `internal/pack/verify.go:102`, `internal/bundle/verify.go:132`, `internal/supervisor/hmac.go:87` | Digest / MD5 compares use `==` / `!=` rather than `subtle.ConstantTimeCompare` / `hmac.Equal`. Not secret MAC keys (except where HMAC already uses `hmac.Equal`). | Use constant-time helpers for hex digests adjacent to auth paths for defense-in-depth. |
| C-16 | **LOW** | `internal/identity/filestore.go:55-56`, `95-99` | PBKDF2 100k iterations is acceptable but dated; passphrase retained in struct for process lifetime; no explicit key zeroization after decrypt/sign. | Consider Argon2id/scrypt; wipe derived keys and plaintext key PEMs after use where practical. |
| C-17 | **LOW** | `internal/identity/spiffe.go:34-38` | `ValidateURIComponent` only rejects `..` and `/` — not `\`, NUL, spaces, or overly long components. | Tighten charset (e.g. `[a-zA-Z0-9._-]`), reject NUL/control, max length. |
| C-18 | **LOW** | `internal/pack/lock.go:847-869` | `VerifyLockfileSignature` trusts embedded `package_aid` (self-signed AID). Correct only when paired with publisher/trust checks; v1 / nil-publisher paths remain local-trust. | Keep; ensure all install/run entry points call publisher+trust, not AID-only verify. |
| C-19 | **LOW** | `internal/bundle/writer.go:72-78` | Bundle write accepts pre-set `ManifestSignature` without re-sign. Caller misuse could ship stale signature. | Only accept pre-set sig in tests; production path always re-sign after digest fill. |
| C-20 | **LOW** | `internal/secrets/keychain.go:70`, `internal/identity/keychain.go:210` | Secret/key JSON blobs on `security -w` argv (process listing). Secrets package has timeouts; identity does not (see C-03). | Same as C-03; already partially documented as P1 accepted risk. |

---

## Detailed findings

### C-01 HIGH — Checkpoint digest does not bind identity / sequence fields

**Code**

```54:83:internal/routedrun/progress.go
func (cp *SemanticCheckpoint) ComputeDigest() string {
	canonical := map[string]any{
		"phase":                 cp.Phase,
		"completed_work":        cp.CompletedWork,
		"remaining_work":        cp.RemainingWork,
		"last_committed_action": cp.LastCommittedAction,
		"safe_to_resume":        cp.SafeToResume,
		"artifact_references":   cp.ArtifactRefs,
	}
	b, _ := json.Marshal(canonical)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
```

Read paths call `VerifyDigest()` (`GetCheckpoint` L171-174, `GetLatestCheckpoint` L213-216). Save path auto-computes or checks caller digest (L130-140) — good F8 pattern for **content** fields only.

**Attack scenario**

An attacker (or buggy writer) with write access to the checkpoint store can:

1. Take a valid `safe_to_resume` checkpoint for attempt A.
2. Change `attempt_id` / `run_id` / `lease_id` / `sequence` / `checkpoint_id` / `artifact_meta_digest` without touching digested fields.
3. Leave `checkpoint_digest` unchanged — `VerifyDigest` still passes.
4. Cause `GetLatestCheckpoint(attemptB)` to accept foreign progress, or inflate `sequence` so an older semantic state wins.

This weakens B30 F8’s “durable state integrity on read-back” against local tampering.

**Recommended fix**

- Expand canonical map to include at least: `schema_version`, `checkpoint_id`, `attempt_id`, `run_id`, `workflow_id`, `node_id`, `lease_id`, `sequence`, `artifact_meta_digest`, and ideally `created_at` (or exclude time and forbid mutation another way).
- Use `hmac.Equal` / `subtle.ConstantTimeCompare` on hex digests.
- Do not ignore `json.Marshal` errors.
- Add adversary tests: retarget `attempt_id`, bump `sequence`, swap `artifact_meta_digest`.

---

### C-02 HIGH — AID private key materialized on disk for cosign

**Code**

```584:597:internal/pack/lock.go
keyMaterial, err := cfg.KeyStore.Load(cfg.KeyID)
// ...
privateKey, privateKeyPEM, err := privateKeyFromMaterial(keyMaterial)
// ...
keyFile, cleanup, err := writeCosignSigningKey(privateKeyPEM)
defer cleanup()
```

```1302:1337:internal/pack/lock.go
func writeCosignSigningKey(pkcs8PEM []byte) (string, func(), error) {
	// writes src.pem 0600, runs:
	// cosign import-key-pair --key srcPath ... with COSIGN_PASSWORD=
	// returns path to signing-key.key
}
```

**Attack scenario**

During `pack`, the package identity private key exists as files under a temp directory, imported with an **empty cosign password**. Concurrent local malware, misconfigured shared temp, crash leaving files, or pack failure before `cleanup` increases exposure beyond KeyStore boundaries.

**Recommended fix**

- Encrypt cosign key with a random ephemeral password held only in memory; pass via env carefully or use cosign’s newer keyless/API paths if available.
- `defer` wipe (`RemoveAll`) on all error paths (mostly present) + best-effort overwrite before unlink.
- Avoid writing PEM if cosign can sign via PKCS11/API; at minimum keep window tiny and directory `0700`.
- Never log `privateKeyPEM` / command lines containing paths is already mostly OK — keep it that way.

---

### C-03 HIGH — Identity keychain: no timeout + argv secret

**Code**

```86:104:internal/identity/keychain.go
func (k *KeychainKeyStore) securityCall(args ...string) (string, error) {
	cmd := exec.Command("security", args...)
	out, err := cmd.CombinedOutput()
	// no context / timeout
}
```

```200:210:internal/identity/keychain.go
// NOTE: password data is passed via argv (-w <value>) ...
// P1 accepted risk ... P2 will replace ...
if _, err := k.securityCall("add-generic-password", "-a", string(id), "-s", k.service, "-w", string(data)); err != nil {
```

Contrast: `internal/secrets/keychain.go:226-238` uses `context.WithTimeout` (10s) and classifies lock/timeout errors.

**Attack / impact scenario**

- Locked keychain or GUI prompt: identity operations hang the daemon/CLI indefinitely.
- Local same-user process snapshot during Create can observe base64 key material in argv.

**Recommended fix**

- Port secrets timeout + error taxonomy to identity keychain.
- Eliminate argv secrets (Security framework).

---

### C-04 MEDIUM — Custom deterministic ECDSA (bundle manifests)

**Code:** `internal/bundle/writer.go:396-452` (`deterministicECDSASignASN1`)

Implemented for reproducible bundles (RFC 6979-style HMAC_DRBG + manual `r,s`). Verification uses stdlib `ecdsa.VerifyASN1` (`internal/bundle/verify.go:239`), which is good, but **signing** correctness depends on this custom code.

**Risks**

- Biased `k` → private key recovery (classic ECDSA failure mode).
- No low-S normalization → rare dual-signature encoding differences (usually still verifies).
- Future curve/param edits are easy to get wrong.

**Recommended fix**

- Add extensive vectors (RFC 6979 A.2.5 P-256 SHA-256).
- Consider external maintained implementation.
- Document why lock/provenance signing stays non-deterministic (`ecdsa.SignASN1` + `rand.Reader`) while manifests are deterministic.

---

### C-05 MEDIUM — FileKeyStore symlink check is target-only

**Code:** `internal/identity/filestore.go:183-191`, `270-276`

Only `Lstat(f.filePath)` is checked. If a parent directory component is replaced with a symlink between checks, or the store directory itself is a symlink, reads/writes follow the link. Pack’s lock path validation explicitly learned this lesson (`rejectSymlinkComponents`, comment at `lock.go:1439-1441`).

**Recommended fix**

Apply component-wise symlink rejection on `dir` + `filePath` for load and save.

---

### C-06 MEDIUM — Trust store accepts non-P-256 ECDSA PEMs

**Code**

```424:431:internal/trust/store.go
pub, ok := parsed.(*ecdsa.PublicKey)
if !ok {
	return nil, fmt.Errorf("public key is %T, not ECDSA", parsed)
}
if pub.Curve == nil {
	return nil, errors.New("public key has no curve")
}
return pub, nil
```

`pack.PublicKeyFromPEM` rejects non-P-256 (`lock.go:987-989`). An operator could pin a P-384 key that later fails lock/bundle verify, or create inconsistent trust entries.

**Recommended fix**

`if pub.Curve != elliptic.P256() { return error }`.

---

### C-07 MEDIUM — Cosign import without timeout

**Code:** `writeCosignSigningKey` → `exec.Command("cosign", "import-key-pair", ...)` without context (`lock.go:1316-1321`). Sign/verify paths correctly use `externalSignatureTimeout` (30s).

**Impact:** Hang + prolonged private key on disk (amplifies C-02).

---

### C-08 MEDIUM — Ignored JSON marshal errors in MAC/digest canonicalization

Examples:

- `progress.go:67` `b, _ := json.Marshal(canonical)`
- `progress.go:453` `canonical, _ := json.Marshal(rec)`
- `supervisor/hmac.go:30,53,76`

If marshal ever fails, verification may MAC empty input and fail closed (good) or, worse, produce a stable empty digest used on both sides in buggy callers. Fail closed with explicit error.

---

### C-09 MEDIUM — Journal HMAC trust boundary

Daemon generates 32-byte key via `crypto/rand`, writes `0o600`, mounts into the run (`control_handlers.go` ~740-788). Tailer verifies with `hmac.Equal` (`progress.go:458`) and enforces monotonic sequence (L388-393) — solid against **external** journal injection.

It does **not** stop a compromised agent/harness process that can read `/agentpaas/journal-key` from forging `safe_to_resume` checkpoints. Align docs/threat model: HMAC = container-origin authenticity, not semantic honesty.

---

### C-10 MEDIUM — Build verify: string-injected Python + MD5 freshness

```49:52:internal/pack/verify.go
auditScript := fmt.Sprintf(`... os.path.isfile('/app/%s') ...`, entryFile)
```

```86:102:internal/pack/verify.go
// MD5 host vs image harness
result.HarnessFresh = hostMD5 == imageMD5
```

Prefer argv/env for paths; SHA-256 for freshness. MD5 collisions are not a practical harness-swap here but the primitive choice is unnecessary.

---

### C-11 MEDIUM — Provenance is signed claims, not a fully re-validated artifact chain

Documented at `provenance.go:14` and enforced structurally (created/forked rules, last signer = lock publisher, per-entry ECDSA). Parent lock/bundle/policy digests are not re-fetched or matched to durable parent objects during `VerifyProvenance`. Acceptable if UX makes “claim chain” explicit; dangerous if operators believe cryptographic ancestry of binaries is proven end-to-end from the lock alone.

---

### C-12 MEDIUM — Fingerprint format inconsistency

| Helper | Format |
|--------|--------|
| `pack.PublicKeyFingerprint` / `identity.PublisherFingerprint` / `trust.computeFingerprint` | bare lowercase hex |
| `identity.publicKeyFingerprint` / `FingerprintPublicKey` | `aa:bb:cc:...` |

Cross-package string equality without normalization will false-negative (safe fail-closed) or confuse operators. Trust already has `NormalizeFingerprint` for display/input — reuse everywhere.

---

### C-13 MEDIUM — Non-positive workload cert TTL coerced to 1h

```159:161:internal/identity/ca.go
if ttl <= 0 {
	ttl = time.Hour
}
```

Callers requesting “no cert” or zero TTL get a long-lived cert. Prefer hard error (adversary expectation: TTL=0 rejected).

---

### C-14 MEDIUM — Publisher private key escape hatch

`LoadPublisherSigningKey` (`publisher.go:263-277`) intentionally loads PEM private key for bundle signing. Increases leakage surface vs `SignAsPublisher`. Prefer signing callback; audit all call sites for log/persist.

---

### C-15 LOW — Non-constant-time digest compares

HMAC paths correctly use `hmac.Equal`. Hex digest equality for checkpoints, SBOM, content digests, result digests uses string `==`. Timing side channels on public hashes are low practical risk; still inconsistent with project norms (`dashboard`/`trigger` use `subtle.ConstantTimeCompare`).

---

### C-16 LOW — FileKeyStore KDF / memory hygiene

PBKDF2-HMAC-SHA256 @ 100k, 32-byte salt, AES-GCM — sound baseline. Improvements: memory-hard KDF, wipe passphrase/derived key on `Close`, avoid retaining passphrase string forever.

---

### C-17 LOW — SPIFFE component validation gaps

`ValidateURIComponent` (`spiffe.go:34-38`) only blocks `..` and `/`. Tighten charset for agentName/version/runID.

---

### C-18 LOW — AID lock signature is self-referential

`VerifyLockfileSignature` verifies with embedded `package_aid` (`lock.go:853-867`). Anyone can create a self-signed lock. Security relies on **publisher signature + trust pin** (pack requires publisher at `lock.go:642-658`). Ensure no install/run path accepts AID-only verification for third-party bundles.

Note: `PublicKeyFingerprint` is inside the signed canonical map, so casual fingerprint edits break the signature; intentional AID PEM vs fingerprint mismatch by the signer is still possible and should be explicitly cross-checked for display integrity.

---

### C-19 LOW — Pre-supplied manifest signature

`bundle/writer.go:72-78` can skip re-signing if `ManifestSignature` already set. Safe only if digests were filled before that signature was created. Production should always sign after digest population.

---

### C-20 LOW — `security -w` argv exposure (secrets + identity)

Documented P1 accepted risk for identity; secrets path similar. Track for P2 Security.framework work.

---

## Positive findings (things done well)

1. **Explicit canonical maps for lock signing** (`lockCanonicalMap`, `canonicalJSON` excluding both signature fields) — avoids JSON key-order and “sign the whole struct with omitempty surprises” classes of bugs (`lock.go:775-782`, `1056-1134`).

2. **Dual signature design** — AID integrity + publisher attribution; publisher fingerprint checked against PEM on verify (`lock.go:819-828`); provenance entries signed independently then bound into lock canonicalization with `entry_signature` included (`lock.go:1102-1120`).

3. **Publisher required at pack** — fail closed without identity (`lock.go:642-658`).

4. **Bundle verify depth** — nine checks including content SHA-256, policy canonical digest, source/build-input binding, optional image index digest (`bundle/verify.go:20-213`).

5. **HMAC verification hygiene** — `hmac.Equal` in progress tailer, supervisor events, control journal, webhooks; empty key rejected in supervisor helpers.

6. **B30 F8 direction is correct** — save-time digest check/auto-compute + read-time `VerifyDigest` (even though field coverage is incomplete; see C-01).

7. **FileKeyStore crypto** — AES-256-GCM, random salt/nonce per save, PBKDF2, 0600/0400 enforcement, wrong passphrase → `ErrWrongPassphrase`, O_EXCL create race mitigation (`filestore.go`).

8. **Key ID validation** — charset/length/`.`/`..` rejection (`keystore.go:27-46`).

9. **SPIFFE case sensitivity** — raw `spiffe://` prefix check before `url.Parse` (`spiffe.go:75-78`).

10. **Workload cert time validity** — not-before / not-after checked; CA chain verify with client auth EKU (`spiffe.go:148-159`, `ca.go:233-260`).

11. **Trust store durability** — corrupt JSON hard-fails; flock + atomic rename + dir fsync; PEM/fingerprint self-consistency on Pin (`trust/store.go`).

12. **Secrets broker redaction** — `CredentialInjection` custom `String`/`GoString`/`Format`; tests assert no secret leakage in errors/audit.

13. **Path safety on lock I/O** — absolute clean paths, protected prefixes, component symlink rejection (`lock.go:1416-1495`).

14. **Entropy** — `crypto/rand` for IDs (16 bytes), journal keys (32 bytes), ECDSA keygen, salts/nonces.

15. **Cosign local vs remote policy** — local registry skips tlog; non-local keeps Rekor defaults (`buildCosignSignArgs` / `buildCosignVerifyArgs`).

16. **Artifact hashing TOCTOU** — open FD + pre/post size/mtime check (`routedrun/artifacts.go:298-325`).

---

## Canonicalization notes (no bypass found under stated rules)

| Artifact | Canonical form | Signature | Notes |
|----------|----------------|-----------|-------|
| Agent lock | `lockCanonicalMap` → `json.Marshal` (sorted map keys) | ECDSA P-256 ASN.1 over SHA-256 | Signatures excluded when signing; provenance entry sigs included |
| Provenance entry | Explicit map w/o `entry_signature` | ECDSA over SHA-256 | PEM bytes are signed as-is (whitespace matters) |
| Bundle manifest | `manifestCanonicalJSON` | ECDSA (deterministic signer) over SHA-256 | Content digests bound in manifest |
| Policy digest | Parse → validate → `json.Marshal(policy)` | Stored in lock | Not raw YAML bytes — intentional canonical policy |
| Progress journal line | `json.Marshal` struct with HMAC cleared | HMAC-SHA256 hex | Struct field order stable |
| Checkpoint digest | Subset map JSON | SHA-256 hex (not a MAC) | See C-01 |

No practical canonicalization bypass was identified that yields a **different semantic object** verifying under the same signature without the private key, given Go’s map-key sorting and the explicit field allowlists. Residual risks are **omitted fields** (checkpoint) and **claim-only parent digests** (provenance), not JSON ambiguity.

---

## Timing-attack summary

| Compare | Mechanism | Verdict |
|---------|-----------|---------|
| Progress/control/webhook HMAC | `hmac.Equal` | Good |
| API tokens / dashboard keys | `subtle.ConstantTimeCompare` | Good (outside core scope but consistent) |
| ECDSA verify | Go stdlib | Acceptable |
| Checkpoint / content / result hex digests | string `==` | Low risk; harden optionally (C-15) |
| MD5 harness freshness | string `==` | N/A crypto auth |

---

## Severity totals

| Severity | Count |
|----------|------:|
| CRITICAL | 0 |
| HIGH | 3 |
| MEDIUM | 11 |
| LOW | 6 |
| **Total** | **20** |

---

## Top 3 most urgent findings

1. **C-01 HIGH** — Expand checkpoint digest canonical fields so F8 integrity binds attempt/run/sequence/lease/artifact meta (resume safety).
2. **C-02 HIGH** — Stop leaving AID private keys on disk with empty cosign passwords during pack.
3. **C-03 HIGH** — Identity keychain timeouts + remove argv key material (align with secrets keychain).

---

## CRITICAL issues that should block B31

**None identified.**

Suggested non-blocking but strongly recommended before/with B31 if those surfaces are touched:

- If B31 touches **resume / checkpoints**: fix **C-01** in the same release train.
- If B31 touches **pack / cosign / AID**: fix **C-02** and **C-07**.
- If B31 touches **identity keystore**: fix **C-03** and **C-05**.

---

## Out of scope / explicitly not claimed

- Full audit chain cryptography (hash-chained JSONL) beyond HMAC/digest patterns shared with supervisor.
- Host compromise / same-user malware as a complete adversary (threat model: protect against the agent, not the user — `docs/threat-model.md`).
- Stolen publisher key revocation (documented out of scope until later blocks).
- Cosign/Rekor ecosystem correctness beyond local invocation wrapping.

---

## Suggested follow-up test ideas (for fix workers / adversary rounds)

1. Checkpoint: mutate `attempt_id` / `sequence` only → must fail `VerifyDigest`.
2. FileKeyStore: parent dir symlink → must refuse load/save.
3. Trust pin: P-384 PEM → must error.
4. Cosign import: cancel/timeout path must delete temp key dir.
5. `IssueWorkloadCert` TTL=0 → error.
6. Cross-check `package_aid` fingerprint vs `public_key_fingerprint` in `VerifyLockfileSignature` / `VerifyAgentLock`.
7. Bundle deterministic ECDSA: RFC 6979 P-256 test vectors.

---

*End of pre-B31 crypto security review.*
