# Task: Fix 14A-T08 Adversary HIGH findings

You are on branch `feat/b14a-t08`.

## Finding 2 (HIGH): --allow-insecure-registry unconditional

In `internal/pack/lock.go`, both `SignImage` (line ~167) and `verifyImageSignature` (line ~638) hardcode `--allow-insecure-registry` for all registries. This should be conditional on the image ref being localhost.

### Fix

Add a helper function:
```go
// isLocalRegistryRef returns true if the image reference points to a local registry.
func isLocalRegistryRef(imageRef string) bool {
    return strings.Contains(imageRef, "localhost:") ||
        strings.Contains(imageRef, "127.0.0.1:")
}
```

In `SignImage`, change:
```go
// Before:
"--allow-insecure-registry",
// After:
// only allow insecure registry for localhost
```

Actually, for SignImage, the `--allow-insecure-registry` is needed for localhost:5001. Make it conditional:
```go
signArgs := []string{"sign", "--key", keyPath, "--signing-config", signingConfigPath, "--yes"}
if isLocalRegistryRef(imageRef) {
    signArgs = append(signArgs, "--allow-insecure-registry")
}
signArgs = append(signArgs, imageRef)
cmd := exec.CommandContext(cmdCtx, "cosign", signArgs...)
```

Same for `verifyImageSignature`:
```go
verifyArgs := []string{"verify", "--insecure-ignore-tlog"}
if isLocalRegistryRef(imageRef) {
    verifyArgs = append(verifyArgs, "--allow-insecure-registry")
}
verifyArgs = append(verifyArgs, "--key", pubFile, imageRef)
cmd := exec.CommandContext(cmdCtx, "cosign", verifyArgs...)
```

## Finding 8 (HIGH): Integration test doesn't call verifyImageSignature

The integration test `TestSignImage_RealCosign` calls SignImage and a manual `cosign verify`, but never calls the production `verifyImageSignature` function. This means the production verify path is not regression-protected.

### Fix

In `internal/pack/lock_sign_real_test.go`, after the round-trip verify section, add:

```go
// Also exercise the production verifyImageSignature function
// to ensure the verify path is regression-protected.
// We need the public key PEM for this.
pubKeyPEM, err := publicKeyPEM(&privateKey.PublicKey)
if err != nil {
    t.Fatalf("publicKeyPEM: %v", err)
}
if err := verifyImageSignature(string(pubKeyPEM), imageRef); err != nil {
    t.Fatalf("verifyImageSignature() = %v, want nil", err)
}
```

Note: `verifyImageSignature` takes `packageAID` (which is the public key PEM string) and `imageRef`. Check the function signature in lock.go to make sure the arguments match. The `publicKeyPEM` function may need to be called differently — check how `CreateAgentLock` calls it.

Also: `verifyImageSignature` is unexported, but since the test is in `package pack`, it can call it directly.

## Verification

```bash
cd /Users/pms88/projects/agentpaas
go test ./internal/pack/... -race
go build ./...
AGENTPAAS_PACK_REAL_TOOLS=1 go test -tags=integration -run TestSignImage_RealCosign ./internal/pack/ -v -timeout 5m
make block14a-gate
```

All must pass.

## Commit message
```
fix(14a-t08): adversary fixes — conditional --allow-insecure-registry, verifyImageSignature in integration test

- --allow-insecure-registry now only added for localhost/127.0.0.1 refs
- Integration test now calls production verifyImageSignature for regression
  protection of the verify path
```
