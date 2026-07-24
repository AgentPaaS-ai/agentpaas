# OWA Record — B33 Preflight Gate Fix

**Date:** 2026-07-23
**Commit:** ee27507 (merged 5950bf0)
**Branch:** fix/b33-preflight-gate → main

## Problem
`make block32-gate` failed on main after v0.3.0 ship commits:

1. `TestDoc_ReadmeStatesTopologyIsPrimary` — README lost exact phrases after humanize (5af97cf / later network docs).
2. `TestCreateAgentLock_DelegationSnapshot` — BUG-040 snapshot wired `lock.PublicKeyFingerprint` as `CallerPackageDigest` and `agentName` as tenant; test expects ImageDigest + `"default"`.

## Fix (ap-worker)
- README.md: restored exact PRIMARY topology + defense-in-depth strings.
- internal/pack/lock.go: tenant `"default"`; callerPackageDigest = `cfg.BuildResult.ImageDigest`.

## Evidence
```
go test ./internal/harness/ -run TestDoc_Readme -count=1  PASS
go test ./internal/pack/ -run TestCreateAgentLock_DelegationSnapshot -count=1  PASS
go test ./internal/pack/ ./internal/delegation/ -count=1  PASS
```
Full `make block32-gate` re-run recorded in /tmp/b32-gate-after-preflight.log.

## Next
B33-T01 MCP gap characterization.
