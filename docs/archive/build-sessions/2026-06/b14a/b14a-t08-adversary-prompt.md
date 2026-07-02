# Adversary Review: 14A-T08 Cosign Integration Test + Production Fix

You are reviewing security-sensitive changes on branch `feat/b14a-t08` in /Users/pms88/projects/agentpaas.

Run `git diff main` to see all changes.

## What was changed

1. `internal/pack/lock_sign_real_test.go` (NEW): Real cosign integration test with `//go:build integration` tag. Tests sign+verify round-trip against localhost:5001 registry. Verifies D3 (Rekor suppression). Guarded by AGENTPAAS_PACK_REAL_TOOLS=1.

2. `internal/pack/lock_test.go`: `fakeCosignScript()` made honest — writes sentinel key body, validates required flags on sign branch.

3. `internal/pack/lock.go`: Production fix — `verifyImageSignature` changed from `--offline` to `--insecure-ignore-tlog --allow-insecure-registry`.

## Review checklist

1. **`--insecure-ignore-tlog` security implications**: By ignoring the transparency log in verify, we lose append-only audit trail verification. Is this acceptable for local-first P1 mode? Could this hide a signature substitution attack?

2. **`--allow-insecure-registry`**: This disables TLS verification for the registry connection. In production (non-localhost), this would be a vulnerability. Is it scoped correctly?

3. **Test isolation**: Does the integration test properly clean up Docker containers, temp files, and registry state? Are there resource leak risks?

4. **D3 verification completeness**: The test asserts no "rekor"/"tlog" in cosign sign output. But what if cosign prints to stdout what looks like normal output? Is the check too loose or too strict?

5. **Honest fake completeness**: Does the updated fakeCosignScript verify ALL required flags that production code uses? Specifically: `--key`, `--signing-config`, `--allow-insecure-registry`, `--yes`. Does it handle the `verify` subcommand?

6. **Key material handling**: Does the test properly handle the P256 private key? Is it written to disk securely (0600)? Is it cleaned up after the test?

7. **Registry startup race**: Does the test handle the case where port 5001 is already in use? Does it wait properly for the registry to be ready?

8. **Mutation check**: Can you verify that breaking the `--insecure-ignore-tlog` flag in production code would cause the integration test to fail? (Don't actually break it — just reason about whether the test would catch it.)

Report findings as:
```
FINDING N: [severity] [title]
Description: ...
Impact: ...
Recommendation: ...
```
