# B13 Deploy E2E — Session Checkpoint

**Date:** 2026-06-24
**Branch:** main
**Last commit:** f5244cb
**Goal:** Complete B13-T05 through T09 + block13-gate. Stop after Block 13.

## WHERE WE ARE

The OWA loop for T01-T04 is DONE and merged. We are now running the B13-T05
deploy e2e flow (detect -> init -> validate -> pack -> run -> dashboard DENIED
probe) to find and fix ALL remaining bugs before completing T05-T09.

## BUGS FOUND AND FIXED (committed to main)

1. **[COMMITTED f2054d3]** Docker endpoint resolution: Docker SDK's
   client.FromEnv doesn't read Docker context store (colima/Desktop). New
   `internal/dockerclient` package centralizes resolution.

2. **[COMMITTED f5244cb]** Harness binary not found: daemon Pack handler
   never set BuildConfig.HarnessPath. Added resolveHarnessBinary() in
   control_handlers.go (checks next-to-daemon-binary, then PATH).

3. **[COMMITTED f5244cb]** Private key parse failure: identity keystore
   stores keys as SEC1 (MarshalECPrivateKey) but pack/lock.go only tried
   PKCS8. parsePrivateKeyPEM now falls back to ParseECPrivateKey.

## BUG FOUND — NOT YET FIXED (this is where to resume)

4. **BUG #3 (ACTIVE BLOCKER): cosign --tlog-upload=false deprecated.**
   - File: `internal/pack/lock.go` line 152
   - Code: `cmd := exec.CommandContext(cmdCtx, "cosign", "sign", "--key", keyPath, "--tlog-upload=false", "--yes", imageRef)`
   - Error: `Flag --tlog-upload has been deprecated, prefer using a --signing-config file`
   - cosign v3.1.1 is installed. The `--tlog-upload=false` flag is no longer
     supported. Must use `--signing-config` with no transparency log, OR use
     `--no-tlog` (check cosign sign --help), OR use `--yes` with a signing
     config.
   - **To fix:** Run `cosign sign --help` to find the correct flag in v3.x.
     Likely `--tlog-upload=false` should become just removing the flag with
     `--signing-config` approach, or there may be a `--no-upload=true` or
     similar. Check what the B8 pack tests do — they may already handle this.

## THE 9 PREVIOUS FIXES (already merged to main via merge commit)

All 9 fixes from the worktree branch `fix/b13-deploy-blockers` are merged:
- Centralize Docker endpoint resolution (dockerclient package)
- Implement 10 unimplemented control RPC handlers (Pack/Run/Stop/Logs/etc.)
- Wire dashboard server into daemon startup (port 8090, NOT 8080)
- Redirect daemon stdout/stderr to log file (CLI hang fix)
- Close goroutine leaks on daemon start/stop paths
- Dedupe AGENTPAAS_HOME/SOCKET env vars for daemon subprocess
- Add missing harness binary main entry point (cmd/harness/main.go)
- Resolve CLI relative paths to absolute before daemon send
- Dashboard server.go errors import + grace period + test stability

## HOW TO REPRODUCE THE E2E FLOW

```bash
# Build all binaries (agent CLI, daemon, harness)
cd ~/projects/agentpaas
go build -o bin/agentpaas ./cmd/agent
go build -o bin/agentpaasd ./cmd/agentpaasd
go build -o bin/agentpaas-harness ./cmd/harness

# Clean daemon state and start fresh
pkill -f agentpaasd; rm -rf /tmp/agentpaas-e2e-home && mkdir -p /tmp/agentpaas-e2e-home
./bin/agentpaas --home /tmp/agentpaas-e2e-home daemon start
sleep 1
./bin/agentpaas --home /tmp/agentpaas-e2e-home daemon status

# Test agent project is at /tmp/agentpaas-e2e-agent/ (already created)
# agent.yaml: name=weather-agent, runtime=python, entrypoint=main.py
# policy.yaml: version "1", deny all egress (no rules = deny default)
# main.py: simple script that prints JSON

# Steps to run:
./bin/agentpaas --home /tmp/agentpaas-e2e-home validate /tmp/agentpaas-e2e-agent
./bin/agentpaas --home /tmp/agentpaas-e2e-home pack /tmp/agentpaas-e2e-agent
# ^^ Currently fails at BUG #3 (cosign --tlog-upload=false)

# After pack works, continue with:
./bin/agentpaas --home /tmp/agentpaas-e2e-home run weather-agent
./bin/agentpaas --home /tmp/agentpaas-e2e-home logs <run_id>
curl http://localhost:8090/  # dashboard
# Check dashboard shows DENIED probe
```

## REMAINING WORK AFTER BUG #3 IS FIXED

After pack succeeds, continue the e2e:
- run the agent
- check logs
- verify dashboard DENIED probe (policy denies all egress)
- fix any further bugs found in run/logs/dashboard path

Then proceed through the remaining subtasks:
- B13-T05: Complete deploy e2e (this is what we're doing)
- B13-T06: Prompt-change immutable redeploy
- B13-T07: Demo matrix fixtures (3 demos)
- B13-T08: /agentpaas slash commands
- B13-T09: Bundled SKILL.md + plugin.yaml requires_env
- Implement `make block13-gate` (currently stub that exits 1)
- Run block-end verifier
- Write docs/owa-records/b13-block-end.md

## OWA MODEL ALLOCATION

- Orchestrator: z-ai/glm-5.2 (this model) — plans, reviews, merges, does NOT edit code
- Worker: grok-composer-2.5-fast via Grok CLI ($0) — dispatched on worktree
- Adversary: grok-4.3 via agentpaas-adversary ($0)
- Verifier: GLM-5.2 via agentpaas-verifier, ONCE at block-end
- Local-first mode: merge locally, push once at block end

## KEY FACTS

- Dashboard is on port **8090** not 8080 (memory was wrong)
- `validate` CLI subcommand DOES support --json (resume prompt note was stale)
- Policy YAML schema: `version`, `agent.name`, `agent.description`, `egress: []`
  (NOT kubernetes-style apiVersion/kind/spec)
- AgentPaaS CLI binary is `bin/agentpaas` (from cmd/agent)
- Daemon binary is `bin/agentpaasd` (from cmd/agentpaasd)
- Harness binary is `bin/agentpaas-harness` (from cmd/harness)
- Docker runs via colima context (not default socket)
- Test agent project: /tmp/agentpaas-e2e-agent/
