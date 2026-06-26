# Task: 14A-T08 — Cosign real integration test + fake honesty (SHORTCUT-6)

## Branch
Create branch `feat/b14a-t08` from `main`.

## Context

The plan is at `docs/plans/b13-cosign-coverage-fix.md`. Read it first.

The existing test `TestSignImage` in `internal/pack/lock_test.go` (line 183) uses `fakeCosignScript()` which is dishonest — it writes `[REDACTED PRIVATE KEY]` instead of a real cosign key body, and always `exit 0` on sign. This means cosign signing correctness is never actually tested.

There is also an unverified concern (D3): does `noTlogSigningConfigJSON` actually suppress Rekor upload in cosign v3.x? If it hangs or errors, that's a production bug.

## What to implement

### 1. Create `internal/pack/lock_sign_real_test.go`

Create a new test file with `//go:build integration` build tag:

```go
//go:build integration

package pack

import (
    // needed imports
)

func TestSignImage_RealCosign(t *testing.T) {
    if os.Getenv("AGENTPAAS_PACK_REAL_TOOLS") != "1" {
        t.Skip("set AGENTPAAS_PACK_REAL_TOOLS=1 to run real cosign integration test")
    }
    if _, err := exec.LookPath("cosign"); err != nil {
        t.Skip("cosign not available")
    }

    // 1. Start local registry: docker run -d -p 5001:5001 registry:2
    //    (or reuse if already running on :5001)
    //    Wait for it to be ready (curl localhost:5001/v2/)

    // 2. Generate P256 PKCS8 key:
    //    Use crypto/ecdsa + x509.MarshalPKCS8PrivateKey
    //    Write to temp file as PEM

    // 3. Call writeCosignSigningKey (it's unexported, so test in-package)
    //    OR call SignImage directly with the key
    //    Assert signing-key.key exists and is loadable

    // 4. Build a tiny image and push to localhost:5001 by digest
    //    Can use a simple Dockerfile: FROM scratch, ADD a dummy file
    //    OR use crane/oras to push a dummy manifest
    //    Get the image digest-pinned ref: localhost:5001/test:latest@sha256:...

    // 5. Call SignImage(ctx, imageRef, keyPath)
    //    Assert return value is "cosign://" + imageRef
    //    Assert no error

    // 6. D3 check: capture cosign sign output, verify no Rekor URL in output
    //    (no "rekor" or "tlog" in stderr/stdout, no 30s hang)

    // 7. Round-trip: cosign verify --key <pub> --allow-insecure-registry <ref>
    //    MUST pass (signature is real and usable)

    // 8. Cleanup: stop registry container, remove temp files
}
```

Key implementation details:
- Use `context.WithTimeout(ctx, 2*time.Minute)` for the whole test
- Start registry: `exec.Command("docker", "run", "-d", "-p", "5001:5001", "registry:2")`
- Wait for registry: poll `http://localhost:5001/v2/` until 200 or timeout
- Generate key: `ecdsa.GenerateKey(elliptic.P256(), rand.Reader)` → `x509.MarshalPKCS8PrivateKey` → PEM encode
- Build tiny image: use `docker build` with a minimal Dockerfile, then `docker push localhost:5001/test:latest`, then get digest with `docker inspect --format='{{.Id}}'`
- To get a digest-pinned ref, use `localhost:5001/test@sha256:<digest>`
- For verify: `exec.Command("cosign", "verify", "--key", pubKeyPath, "--allow-insecure-registry", imageRef)`
- Set `COSIGN_PASSWORD=` in env for all cosign commands
- Write the public key to a temp file for the verify step

### 2. Make fakeCosignScript honest

In `internal/pack/lock_test.go`, update `fakeCosignScript()`:

- Instead of writing `[REDACTED PRIVATE KEY]`, write a real-looking cosign encrypted key body (or at minimum, a sentinel that makes the sign branch fail if the contract drifts)
- The sign branch should NOT just `exit 0` unconditionally — it should verify that `--key`, `--signing-config`, and `--yes` flags are present, and exit 1 if the key file doesn't exist

Example honest fake:
```sh
#!/bin/sh
if [ "$1" = "import-key-pair" ]; then
  prefix=""
  shift
  while [ $# -gt 0 ]; do
    case "$1" in
      -o|--output-key-prefix) prefix="$2"; shift 2 ;;
      -k|--key) shift 2 ;;
      -y|--yes) shift ;;
      *) shift ;;
    esac
  done
  if [ -z "$prefix" ]; then prefix="import-cosign"; fi
  # Write a sentinel key body that looks like cosign's format
  printf '%s\n' '-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----' \
    'fake-sentinel-key-body' \
    '-----END ENCRYPTED SIGSTORE PRIVATE KEY-----' > "${prefix}.key"
  printf '%s\n' '-----BEGIN PUBLIC KEY-----' 'fake' '-----END PUBLIC KEY-----' > "${prefix}.pub"
  exit 0
fi
if [ "$1" = "sign" ]; then
  # Verify required flags are present
  has_key=0; has_config=0; has_yes=0
  for arg in "$@"; do
    case "$arg" in
      --key) has_key=1 ;;
      --signing-config) has_config=1 ;;
      --yes) has_yes=1 ;;
    esac
  done
  if [ $has_key -eq 0 ] || [ $has_config -eq 0 ] || [ $has_yes -eq 0 ]; then
    echo "fake-cosign: missing required flag" >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "verify" ]; then
  exit 0
fi
exit 0
```

### 3. Add macOS symlink regression test

In `internal/pack/lock_test.go`, add a unit test (no build tag):

```go
func TestValidateSecurePath_macOSVarFolders(t *testing.T) {
    // On macOS, $TMPDIR resolves to /var/folders/... which is a symlink
    // to /private/var/folders/... — this should be ACCEPTED, not rejected.
    // This is a regression test for D2 (which was disproven).
    tmpFile, err := os.CreateTemp("", "agentpaas-symlink-test-*")
    if err != nil {
        t.Fatalf("CreateTemp: %v", err)
    }
    defer os.Remove(tmpFile.Name())
    tmpFile.Close()

    resolved, err := filepath.EvalSymlinks(tmpFile.Name())
    if err != nil {
        t.Fatalf("EvalSymlinks: %v", err)
    }
    // This should pass — the resolved path under /private/var/folders
    // is not in the protected list (/etc /usr /bin /sbin)
    if err := validateSecurePath(resolved, true); err != nil {
        t.Fatalf("validateSecurePath(%q) = %v, want nil", resolved, err)
    }
}
```

## Verification

```bash
cd /Users/pms88/projects/agentpaas

# Default tests (no integration tag) — must still pass
go test ./internal/pack/... -v -race

# Integration test (requires Docker + cosign)
AGENTPAAS_PACK_REAL_TOOLS=1 go test -tags=integration -run TestSignImage_RealCosign ./internal/pack/ -v -timeout 5m

# Build
go build ./...

# Lint
golangci-lint run ./internal/pack/...
```

## Important Notes
- The integration test will be SLOW (needs Docker, registry startup, image build). That's expected.
- If cosign is not installed, the integration test should skip, not fail.
- If Docker is not running, the integration test should skip.
- The default `go test ./internal/pack/` (without -tags=integration) must still pass with the honest fake.
- The `--signing-config` flag approach is the current production code. If the D3 check reveals that `noTlogSigningConfigJSON` does NOT suppress Rekor in cosign v3.x, STOP and report — do not paper over it.

## Commit message
```
feat(14a-t08): real cosign integration test + honest fake + macOS symlink regression test (SHORTCUT-6)

- lock_sign_real_test.go: real cosign sign+verify round-trip against
  localhost:5001, D3 tlog suppression verification
- lock_test.go: fakeCosignScript now writes sentinel key body and
  validates required flags on sign
- TestValidateSecurePath_macOSVarFolders: regression test for D2
  (macOS /var/folders symlink accepted)
- Guarded by //go:build integration + AGENTPAAS_PACK_REAL_TOOLS=1
```
