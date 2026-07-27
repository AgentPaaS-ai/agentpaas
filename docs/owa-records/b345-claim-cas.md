# B34.5 — ClaimNextReady CAS steal fix

**Branch:** `fix/b345-claim-cas`
**Commit:** `50f2f2f`
**Date:** 2026-07-26

## Bug

`ClaimNextReady` on CAS conflict blindly retried `UpdateNode(..., nodeGen+1)` and
`+2` while still forcing status LAUNCHING. Two controllers both starting with
gen=1: first wins gen→2; second's +1 retry **overwrites** the first claim.

## Fix

In `internal/workflow/pipeline/controller.go` `ClaimNextReady` CAS path:

1. On CAS conflict, re-read node from store via `GetNode`
2. If status is not READY → another controller won → return `nil, nil` (no claim)
3. If still READY → stale gen map (restart scenario) → retry with gen+1/+2
   (safe because no other controller has claimed it)

The status guard (#2) is the key: it prevents the blind overwrite while still
allowing the restart path where a fresh controller needs to discover the actual
store generation.

## Test results

```
TestAdversary_B34_TwoControllersSharedStore_NoDoubleClaim: 10/10 PASS
Full pipeline suite: PASS
Adversary suite: PASS
make lint: 0 issues
go build ./...: PASS
```
