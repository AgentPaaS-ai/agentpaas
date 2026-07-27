# B34 close — BUG-036…043 spotcheck (2026-07-26)

**Repo:** main after b34-close merges  
**Method:** code path inspection + targeted unit tests (not full nuclear e2e redo)

## Summary

| Bug | Severity | Doc status after | Code evidence | Spotcheck |
|-----|----------|------------------|---------------|-----------|
| 036 silent identity init | P2 | FIXED | Plugin/SKILL forbid silent init; tell user to run `identity init` | SKILL.md + tools guidance present |
| 037 install PATH discovery | P2 | FIXED (partial UX) | Plugin `cli_not_found` + doctor path resolution | doctor/cli_not_found paths in plugin |
| 038 fingerprint spaces | P2 | FIXED | `strings.Fields` strip in `resolveTOFUInteractive` | `internal/install/trust.go:274` |
| 039 uv.lock UX | P3 | FIXED | requirements.txt → note, no prompt | `TestMaterializeRequirementsTxtNoPrompt` PASS |
| 040 delegation trust | P1 | FIXED | snapshot path env + harness load | daemon injects `AGENTPAAS_DELEGATION_SNAPSHOT_PATH`; harness builds `DelegationTrustState` |
| 041 plugin flag drift | P3 | FIXED | export uses positional path | `tools.py` `cmd.append(resolved)` not `--project-dir` |
| 042 skill collision | P3 | FIXED (profile hygiene) | single primary SKILL + skills/setup | collision was ap-testing profile; structure cleaned in fix branch |
| 043 durable start | P0 | FIXED (code) | `startDurableRun` from InvokeDeployment | unit InvokeDeployment suite PASS; live multi-turn soak still residual |

## Commands run

```text
go test ./internal/install/ -count=1 -run 'Unlocked|Requirements|UVLock|Materialize'
go test ./internal/daemon/ -count=1 -run 'InvokeDeployment'
go test ./internal/harness/ -count=1   # package ok
rg -n 'Join\(strings.Fields' internal/install/trust.go
rg -n 'startDurableRun' internal/daemon/
rg -n 'AGENTPAAS_DELEGATION_SNAPSHOT_PATH' internal/daemon/
```

## Disposition policy

- Records flipped FIXED/CLOSED with this date and fix merge SHAs where known.
- Remaining **product** residual called out only for 043 live soak + multi-image pipeline operator path (see current-state.md).
- No open P0/P1 **code** defects from this list block B35 start after GO.

## Fix SHAs (on main lineage)

- UX batch: `2b6c3ce` Merge fix/b32-ux-bugs (036/037/038/041/042)
- BUG-039: `91aa146` / `fcc05c7`
- BUG-040: `f86117f` / `f74aed7` / related
- BUG-043: `3b0995b` / `81ff5db`
