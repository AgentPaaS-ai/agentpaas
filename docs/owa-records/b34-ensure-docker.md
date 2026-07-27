# B34 Ensure Docker — Never Skip

**Date:** 2026-07-27
**Branch:** feat/b34-ensure-docker
**Base:** main

## Summary

`block34-gate` now **always** requires Docker. No more `t.Skip`. If Docker
is not running, `scripts/ensure-docker.sh` auto-starts Colima on macOS
(installs via brew if needed).

## Changes

### 1. `scripts/ensure-docker.sh`

- Checks `docker info` → exit 0 if running
- If `colima` installed → `colima start --cpu 4 --memory 8`
- If `brew` installed → `brew install colima docker lima` then start
- Waits up to 120s for Docker to become ready
- Exports `DOCKER_HOST` for Colima socket
- Exits 1 with clear error if Docker cannot be made available

### 2. `internal/workflow/pipeline/docker_e2e_require.go`

Shared helper `requireDockerE2E(t)` that **never calls `t.Skip`**.
If Docker is not running, shells out to `scripts/ensure-docker.sh`.
On failure, calls `t.Fatalf`.

### 3. Test files updated

- `multiagent_docker_e2e_test.go` — replaced `t.Skip` with `requireDockerE2E(t)`
- `stage_docker_e2e_test.go` — replaced all 4 `t.Skip` calls with `requireDockerE2E(t)`
- Removed unused `"os"` imports from both files

### 4. Makefile

- New `ensure-docker` target
- `block34-gate` now ends with `$(MAKE) block34-docker-gate` (Docker mandatory)
- `block34-multiagent-gate`: depends `ensure-docker`
- `block34-docker-gate`: depends `ensure-docker block34-multiagent-gate`
- `block34-full-gate`: depends `ensure-docker block34-gate block34-docker-gate`
- `block345-docker-gate`: depends `ensure-docker block34-multiagent-gate`
- Updated `gates` help text

## Acceptance

```bash
# Docker already up
make ensure-docker && make block34-gate   # must run multiagent + isolation e2e

# Docker down
colima stop; make ensure-docker          # must bring Docker back

# No Skip in test output
go test ./internal/workflow/pipeline/ -count=1 -run 'TestB34MultiAgentE2E|DockerE2E' -timeout 20m
# must NOT print SKIP
```

Grep for `t.Skip` in `internal/workflow/pipeline/*docker_e2e*` → zero matches.
