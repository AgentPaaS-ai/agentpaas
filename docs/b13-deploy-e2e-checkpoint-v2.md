# B13 Deploy E2E — Session Checkpoint #2

**Date:** 2026-06-24
**Branch:** main (local-first, not pushed)
**Last commit:** bd0eb96 (Merge fix/b13-pack-sign)
**Goal:** Complete B13-T05 through T09 + block13-gate. Stop after Block 13.

## PROGRESS THIS SESSION

### Completed
- Committed uncommitted cosign fix from last session (a9db1ba)
- Built all 3 binaries (agentpaas, agentpaasd, harness)
- Ran e2e flow, discovered and fixed 3 pack/sign bugs via worker dispatch
- Merged fix/b13-pack-sign (bd0eb96)

### Bugs Found and Fixed (merged to main)

**Batch 1 (commit e4f0343, merged bd0eb96) — fixed by Grok worker:**
1. **cosign v3 `--tlog-upload` deprecated** → Use signing-config JSON
   (`{"mediaType":"application/vnd.dev.sigstore.signingconfig.v0.2+json","rekorTlogConfig":{},"tsaConfig":{}}`)
   with `--signing-config` flag. Old `--use-signing-config=false --tlog-upload=false` rejected by cosign v3.1.1.
2. **cosign sign requires registry access** → Pack handler now pushes built image
   to local registry (`localhost:5001`) before signing. New `internal/pack/registry.go`
   manages registry container lifecycle and image push. Port 5001 (not 5000 — macOS AirPlay conflict).
3. **syft/cosign don't inherit DOCKER_HOST** → `dockerclient.ResolvedDockerHost()` propagates
   colima socket to child processes in GenerateSBOM and SignImage.

## ACTIVE BUGS (next worker dispatch)

**BUG 4: cosign can't read EC PRIVATE KEY — needs PKCS8**
- File: `internal/pack/lock.go`, `privateKeyFromMaterial` (line ~441)
- Error: `unsupported pem type: EC PRIVATE KEY`
- Cause: `privateKeyFromMaterial` returns raw SEC1 PEM bytes when material is `[]byte`.
  cosign needs PKCS8. Fix: re-marshal as PKCS8 after parsing.

**BUG 5: Run handler constructs wrong image ref — missing registry prefix**
- File: `internal/daemon/control_handlers.go`, `Run` (line ~174)
- Current: `agentpaas/%s@sha256:%s` (no registry)
- Should be: `localhost:5001/agentpaas/%s@sha256:%s` (image is in local registry)
- Fix: add `pack.LocalImageRef()` helper, use in Run handler.

## HOW TO REPRODUCE THE E2E FLOW

```bash
export TMPDIR=/tmp
export DOCKER_HOST="unix:///Users/pms88/.colima/default/docker.sock"
cd ~/projects/agentpaas

# Build
go build -o bin/agentpaas ./cmd/agent
go build -o bin/agentpaasd ./cmd/agentpaasd
go build -o bin/agentpaas-harness ./cmd/harness

# Clean state
pkill -f agentpaasd; rm -rf /tmp/agentpaas-e2e-home && mkdir -p /tmp/agentpaas-e2e-home
./bin/agentpaas --home /tmp/agentpaas-e2e-home daemon start
sleep 2

# E2E steps
./bin/agentpaas --home /tmp/agentpaas-e2e-home validate /tmp/agentpaas-e2e-agent  # ✓ PASSES
./bin/agentpaas --home /tmp/agentpaas-e2e-home pack /tmp/agentpaas-e2e-agent      # ✗ BUG 4
./bin/agentpaas --home /tmp/agentpaas-e2e-home run weather-agent                   # ✗ BUG 5 (after 4 fixed)
curl http://localhost:8090/                                                        # dashboard
```

## REMAINING WORK

1. Fix BUG 4 + BUG 5 (worker dispatch in progress)
2. Complete B13-T05 e2e (detect→validate→pack→run→dashboard DENIED)
3. B13-T06: prompt-change immutable redeploy
4. B13-T07: demo matrix fixtures
5. B13-T08: /agentpaas slash commands
6. B13-T09: bundled SKILL.md + plugin.yaml
7. `make block13-gate` implementation
8. Block-end verifier + b13-block-end.md

## KEY FACTS

- Dashboard on port **8090** (not 8080)
- Local registry on port **5001** (not 5000 — macOS AirPlay)
- Docker via colima: `DOCKER_HOST=unix:///Users/pms88/.colima/default/docker.sock`
- cosign v3.1.1 installed — needs `--signing-config` not `--tlog-upload`
- syft v1.45.1 installed
- Test agent: /tmp/agentpaas-e2e-agent/ (weather-agent, python, deny-all egress)
- Worker dispatch: Grok CLI on worktree at /private/tmp/agentpaas-b13-*
- TMPDIR must be overridden (/tmp) — stale from previous session

## OWA MODEL ALLOCATION

- Orchestrator: z-ai/glm-5.2 — plans, reviews, merges, does NOT edit code
- Worker: grok-composer-2.5-fast via Grok CLI ($0) — dispatched on worktree
- Adversary: grok-4.3 via agentpaas-adversary ($0)
- Verifier: GLM-5.2 via agentpaas-verifier, ONCE at block-end
- Local-first mode: merge locally, push once at block end
