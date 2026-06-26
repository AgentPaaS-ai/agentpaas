# Task: Fix 14A-T08 production bug — cosign verify --offline deprecated

You are on branch `feat/b14a-t08`. 

## Problem

The worker discovered that production code in `internal/pack/lock.go` line 638 uses:
```go
cmd := exec.CommandContext(cmdCtx, "cosign", "verify", "--offline", "--key", pubFile, imageRef)
```

The `--offline` flag is deprecated in cosign v3.1.1 and still expects transparency log entries. Since our signing suppresses Rekor upload (via `noTlogSigningConfigJSON`), verification with `--offline` may fail because it looks for tlog entries that don't exist.

The integration test confirmed that `--insecure-ignore-tlog` is the correct flag for verifying signatures where Rekor upload was suppressed.

## Fix

In `internal/pack/lock.go`, replace `--offline` with `--insecure-ignore-tlog`:

```go
// Before:
cmd := exec.CommandContext(cmdCtx, "cosign", "verify", "--offline", "--key", pubFile, imageRef)

// After:
cmd := exec.CommandContext(cmdCtx, "cosign", "verify", "--insecure-ignore-tlog", "--key", pubFile, imageRef)
```

Also add `--allow-insecure-registry` since we're using localhost:5001:
```go
cmd := exec.CommandContext(cmdCtx, "cosign", "verify", "--insecure-ignore-tlog", "--allow-insecure-registry", "--key", pubFile, imageRef)
```

Wait — check the production code context first. The `verifyImageSignature` function is called by `VerifyAgentLock`. It needs to work for both local registry (localhost:5001) and potentially real registries. Add `--allow-insecure-registry` because local-first mode always uses localhost:5001.

## Verification

```bash
cd /Users/pms88/projects/agentpaas
go test ./internal/pack/... -race
go build ./...
AGENTPAAS_PACK_REAL_TOOLS=1 go test -tags=integration -run TestSignImage_RealCosign ./internal/pack/ -v -timeout 5m
```

## Commit message
```
fix(14a-t08): replace deprecated --offline with --insecure-ignore-tlog in verifyImageSignature

cosign v3.1.1 deprecated --offline. Since signing suppresses Rekor upload
(noTlogSigningConfigJSON), verify must use --insecure-ignore-tlog to skip
tlog entry checks. Also add --allow-insecure-registry for localhost:5001.
```
