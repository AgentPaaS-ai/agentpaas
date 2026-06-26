# Post-Build Audit Checklist

Run this AFTER the block-end Verifier passes and BEFORE declaring the
block complete. An agent's closure comment is a claim, not evidence.

## Audit Steps

### 1. Remote Commit Verification
```
# Are fix commits actually on the remote, not just local?
git log origin/main --oneline | grep <fix-commit-sha>
# If a fix commit is missing from origin/main, the agent committed
# locally and forgot to push. Push it before closing.
```

### 2. Remote CI Verification
```
# Is CI actually green on the remote main branch?
<your-ci-status-check> <repo> --branch main
# Do NOT trust "CI green" in a closure comment. Check the actual status.
```

### 3. E2E Test Re-Run (Local)
```
# CI often skips Docker-gated or slow e2e tests. Run them locally on
# the merged code to confirm they pass.
<your-e2e-test-command> --docker --parallel-batches
# If CI skips e2e and you don't re-run locally, you have no evidence
# that the end-to-end flow works on the merged code.
```

### 4. Security Suppression Validation
```
# For every CVE ignore, lint exception, or security suppression:
#   - Is the reasoning factually accurate?
#   - "No fix available" is WRONG if a fix exists in a later version.
#   - "Daemon-side CVE, patched in runtime" must be verified against
#     the actual runtime version you're running.
<your-security-scanner> --suppressions-list
# Read each suppression's annotation. Verify the claim.
```

### 5. CI Workflow Correctness
```
# Does the CI workflow actually run the current block's gate?
# Or is it still running a stale first-block gate?
cat .github/workflows/<your-ci-file>.yml | grep <current-block-gate>
# If the workflow doesn't reference the current block's gate, CI is
# providing false assurance — it's running an old gate that doesn't
# cover the new code.
```

### 6. Issue Closure (if using issue tracking)
```
# Every block issue must be closed with a closure comment documenting:
#   - What was tested (test counts, adversary counts, gate results)
#   - What was checked in (files, commit SHAs)
#   - Adversary breaks resolved (severity, fix, verification)
#   - CI verification status (remote, not self-reported)
#   - Reference to the OWA record file
<your-issue-tool> list --label block-<N> --state open
# Must return EMPTY. If not, close the remaining issues.
```

### 7. Stale Branch Cleanup
```
# Stale local branches accumulate across blocks.
git branch | grep -v main | xargs git branch -D
# Do this AFTER confirming all merged work is on origin/main.
```

### 8. Pruning Restore
```
# Re-enable toolsets and skills disabled during the build.
# Move skills back from the holding directory.
# Re-add MCP servers removed during pruning.
# See scripts/post-build-restore-template.sh
```

## Audit Outcome

If any audit step FAILS:
- Do NOT declare the block complete.
- Dispatch a fix Worker for the failing item.
- Re-run the audit step.

If all audit steps PASS:
- Block is declared complete.
- Post the block summary (what was built, what was tested, total cost).
- Advance to the next block.
