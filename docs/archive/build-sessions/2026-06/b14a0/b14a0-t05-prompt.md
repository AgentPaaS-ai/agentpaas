# Worker Task: 14A0-T05 — Rename stubControlServer + Fix Stale CLI doc.go

## Context
AgentPaaS repo at /Users/pms88/projects/agentpaas, on main branch.
The production daemon's control server type is named `stubControlServer` — misleading since it's the real production server, not a stub.

## Task 1: Global Rename stubControlServer → controlServer

Rename the type `stubControlServer` to `controlServer` across ALL Go files in the daemon package.

Files affected (73 occurrences across 9 files):
- `internal/daemon/control_handlers.go` (26 occurrences)
- `internal/daemon/operator_handlers.go` (17 occurrences)
- `internal/daemon/control_handlers_test.go` (12 occurrences)
- `internal/daemon/operator_path_boundary_b11t06_test.go` (4 occurrences)
- `internal/daemon/stub_handlers.go` (4 occurrences)
- `internal/daemon/operator_handlers_b11t03_test.go` (5 occurrences)
- `internal/daemon/operator_handlers_b11t04_test.go` (2 occurrences)
- `internal/daemon/server.go` (2 occurrences)
- `internal/daemon/adversary_test.go` (1 occurrence)

Use `gofmt -r 'stubControlServer -> controlServer'` or sed across all .go files in `internal/daemon/`.

NOTE: The file `stub_handlers.go` contains the type definition and methods. Do NOT rename the file itself (that would be a larger change). Only rename the type.

## Task 2: Fix Stale CLI doc.go

In `internal/cli/doc.go`, remove " (not yet implemented)" from these commands that ARE implemented:
- pack (line 15)
- run (line 16)
- stop (line 17)
- logs (line 18)
- policy (line 19)
- secrets (line 20)
- audit (line 21)
- validate (line 22)
- summarize (line 23)
- explain-failure (line 24)
- explain-denial (line 25)
- recommend-patch (line 26)
- timeline (line 27)
- next-action (line 28)

LEAVE these as "not yet implemented" (they are actually stubs):
- daemon install (line 12)
- daemon uninstall (line 13)
- doctor (line 14 — leave as "v0 stub")

## Verification

After making changes:
1. Run `cd /Users/pms88/projects/agentpaas && go build ./...` — must compile
2. Run `go test ./internal/daemon/... -count=1 -race -timeout 120s` — must pass
3. Run `go test ./internal/cli/... -count=1 -timeout 60s` — must pass
4. Run `golangci-lint run ./internal/daemon/... ./internal/cli/...` — must be clean (if golangci-lint is available)

## Rules
- Do NOT make any logic changes — this is a pure mechanical rename + doc fix
- Do NOT rename the file `stub_handlers.go` — only the type inside it
- Commit with message: `refactor(14a0-t05): rename stubControlServer → controlServer + fix stale doc.go`
