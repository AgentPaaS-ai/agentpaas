# Plan: Close cosign signing coverage gap (D1) + verify D3

## Context

Analysis report flagged 3 defects in `writeCosignSigningKey` → `SignImage`
(`internal/pack/lock.go`). Orchestrator ran the ground-truth repro on
2026-06-24 and confirmed:

- **A1-A5 VERIFIED WORKING** with real cosign v3.1.1 on macOS/arm64.
  `import-key-pair` produces `signing-key.key`, format
  `-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----` (scrypt+nacl, empty
  passphrase round-trips because both import and sign set `COSIGN_PASSWORD=`).
- **D2 DOES NOT MANIFEST.** macOS `EvalSymlinks` → `/private/var/folders/...`
  is NOT in the protected list (`/etc /usr /bin /sbin` only), and
  `rejectSymlinkComponents` walks the RESOLVED path (a real dir, not a
  symlink), so `validateSecurePath` ACCEPTS it. The analysis speculated D2;
  repro disproves it.

So the production code is functionally correct. **The real defect is D1:
zero real-cosign test coverage.** Green status is structurally decoupled from
cosign correctness (pitfall #12). The fix is TESTS, not production code —
unless D3 turns up a real bug.

## Outstanding unknown: D3 (tlog/Rekor suppression)

Whether `noTlogSigningConfigJSON` actually suppresses Rekor upload in
cosign v3.1.1 `sign` is unverified. The worker's first job is to prove it
by signing a real image against localhost:5001 and confirming no network
hang / Rekor call. If it hangs/errors, that IS a production bug to fix.

## Subtasks

### T1 (worker): Real-cosign integration test + D3 verification
- Create isolated worktree `fix/b13-cosign-real-test` at
  `/tmp/agentpaas-b13-cosign-realtest`.
- Write `internal/pack/lock_sign_real_test.go` with `//go:build integration`.
  Skip-if-absent (cosign, docker). Guard with
  `AGENTPAAS_PACK_REAL_TOOLS=1` to stay out of default `go test`.
- The test MUST exercise the REAL contract end-to-end:
  1. start localhost:5001 registry (reuse B13 docker helper if present,
     else `docker run -d -p 5001:5001 registry:2`).
  2. generate P256 PKCS8 key (`crypto/ecdsa` + `x509.MarshalPKCS8PrivateKey`,
     NOT openssl — keep it hermetic).
  3. call `writeCosignSigningKey` (export a test helper or call via
     `CreateAgentLock` path) → assert `signing-key.key` EXISTS and is
     loadable (`cosign public-key --key <key>` returns a pubkey).
  4. build/push a tiny image by digest to localhost:5001, call `SignImage`,
     assert return value is `cosign://<ref>`.
  5. `cosign verify --key <pub> --allow-insecure-registry <ref>` → MUST
     pass (round-trip proof). This is the closing assertion that proves
     the signature is real and usable, not fabricated.
  6. D3 check: capture `cosign sign` stderr/stdout; assert no Rekor URL /
     no 30s hang. If it hangs or errors on Rekor → STOP, report as a
     production bug (don't paper over it).
- Add negative-path unit tests (build-tagged or env-gated) for:
  cosign exit 1 with stderr (error wraps stderr); cosign produces NO .key
  file (chmod error surfaces). These pin behavior the fake hides.
- Run gate: `go build ./...`, `go test -tags=integration
  -run TestSignImage_Real ./internal/pack/` with
  AGENTPAAS_PACK_REAL_TOOLS=1, plus `go test ./internal/pack/` (default,
  no tag) for the negative-path units. `go vet ./internal/pack/`.
- Commit to branch.

### T2 (worker): Make the fake honest + macOS symlink regression test
- `fakeCosignScript`: emit a real cosign-format encrypted key body (or a
  sentinel that makes the sign branch fail loudly if the contract drifts)
  instead of `[REDACTED PRIVATE KEY]` + unconditional `exit 0` on the sign
  branch. Keep unit tests green.
- Add `TestValidateSecurePath_macOS_var_folders` (unit, no build tag): create
  a temp file under the real `$TMPDIR` (which is `/var/folders` on macOS),
  run `validateSecurePath(resolved, true)`, assert it ACCEPTS. Pins D2
  non-manifestation as a regression guard.

### T3 (orchestrator): Adversary + verify
- Adversary: try to break T1/T2 tests (mutation: deliberately break a flag,
  confirm a test goes RED — V5 from the analysis).
- Block-end verify: real cosign integration test actually ran (count
  invocations / assert `cosign version` executed), signature verifies, macOS
  path accepted. Mutation check passes.

## Verification criteria (done = ALL true)
1. `go test -tags=integration -run SignImage_Real ./internal/pack/` with
   AGENTPAAS_PACK_REAL_TOOLS=1 runs REAL cosign and PASSES on macOS/arm64.
2. Round-trip: signature from `SignImage` verifies with
   `cosign verify --key <pub>`.
3. D3 proven: no Rekor hang/error during sign (or, if it fails, production
   code fixed and the fix documented).
4. Default `go test ./internal/pack/` still green (no regressions, fakes honest).
5. Mutation check: break a flag → at least one test RED.
6. Zero carried-forward debt (pitfall #13): every speculation from the
   analysis is resolved (A1-A5 proven, D2 disproven+regression test, D3 proven).

## NOT doing
- Reimplementing cosign in Go (keep thin wrapper).
- Production code changes UNLESS D3 surfaces a real bug.
- Per-subtask CI/PR (local-first; push once at block end).
