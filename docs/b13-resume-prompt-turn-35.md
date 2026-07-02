Continue AgentPaaS Block 13. ~35 turns used. Pick up at BUG 7d e2e — invoke mechanism needed.

CONTEXT (load these first):
- Load skills: agentpaas-40-turn-rhythm, agentpaas-owa-build-orchestration
- Read checkpoint: docs/b13-checkpoint-turn-22.md
- Read the e2e recipe: skill agentpaas-40-turn-rhythm → references/e2e-verification-recipe.md
- Read the B13 block spec in agentpaas-execution-plan-v1.md (search "BLOCK 13")

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: 528bc5b (AGENTPAAS_AGENT_PATH fix)
- All daemon + runtime tests passing. Build clean. Lint clean (0 issues).
- 3 commits done this session:
  - 90515a5: chmod 0777 audit dir + lowercase docker error strings (ST1005)
  - 528bc5b: AGENTPAAS_AGENT_PATH=/app/main.py in container env

COMPLETED THIS SESSION:
1. Built + tested + committed the chmod 0777 fix for audit dir
2. Fixed ST1005 lint violations in dockerclient.go (lowercase error strings)
3. Discovered + fixed AGENTPAAS_AGENT_PATH bug (harness default /agent/main.py is wrong; pack copies to /app/)
4. Verified both fixes work in a live container:
   - No "permission denied" on audit file
   - Container log shows agent=/app/main.py (correct)
   - Harness audit JSONL file created and writable in bind-mounted /audit

CRITICAL FROM THIS SESSION:
1. **Colima virtiofs only mounts /Users/pms88.** /tmp on the host is NOT the same as /tmp inside the colima VM. Bind mounts from /tmp silently point to an empty VM-local path. E2E HOME MUST LIVE UNDER ~/ (e.g. ~/agentpaas-e2e-home), NOT /tmp.
2. **os.MkdirAll(path, 0o777) + os.Chmod(path, 0o777) is confirmed working.** Host audit dir shows drwxrwxrwx. Container UID 64000 can write.
3. **The harness defaults AGENTPAAS_AGENT_PATH to /agent/main.py (cmd/harness/main.go:18).** The pack Dockerfile copies project files to /app/ (internal/pack/build.go:516). The daemon must pass AGENTPAAS_AGENT_PATH=/app/main.py explicitly. Done.
4. **THE BLOCKER: harness audit events are not written because no invoke is sent.** The harness (cmd/harness/main.go) is an RPC server listening on 127.0.0.1:8080. The daemon's Run handler (control_handlers.go:206) starts the container but does NOT send an invoke RPC. There is no `agent invoke` CLI command. The trigger server (gRPC :7718 / REST :7717, internal/trigger/server.go) handles invocations but isn't started in local-first mode.
5. The SDK's agent.http() requires self._rpc to be set (python/agentpaas_sdk/agent.py:62), which happens in runner.py:33 AFTER import. So egress calls at import time fail with "SDK RPC is not connected". Egress must happen inside an invoke handler.
6. The test agent at /tmp/agentpaas-e2e-agent/main.py currently registers an @agent.on_invoke handler that calls agent.http("GET", "https://example.com"). The SDK is bundled at /tmp/agentpaas-e2e-agent/python/agentpaas_sdk/.

IMMEDIATE NEXT ACTION — solve the invoke gap (pick ONE approach):
A. Add a minimal `agent invoke <run-id> [--payload '{}']` CLI command that sends an invoke RPC to the harness inside the running container (via docker exec or container network).
B. Start the trigger server alongside the daemon and invoke via gRPC (:7718).
C. Write a Go integration test that starts the daemon, packs, runs, sends an invoke to the harness RPC, stops, and queries audit — all in-process.

Approach A is simplest for e2e. Check internal/trigger/server.go for the Invoke RPC proto, and internal/harness/rpc_server.go for the harness-side invoke handler. The harness listens on 127.0.0.1:8080 INSIDE the container — the daemon or CLI needs to reach it (docker exec, or the container's internal network IP).

E2E COMMANDS (after fixing invoke):
```sh
# Build + start daemon
cd ~/projects/agentpaas
go build -o bin/agentpaas ./cmd/agent && go build -o bin/agentpaasd ./cmd/agentpaasd
pkill -f agentpaasd; sleep 1
rm -rf ~/agentpaas-e2e-home && mkdir -p ~/agentpaas-e2e-home
AGENTPAAS_HOME=$HOME/agentpaas-e2e-home bin/agentpaasd > ~/agentpaas-e2e-home/daemon.log 2>&1 &

# Pack + run
sleep 2
AGENTPAAS_HOME=$HOME/agentpaas-e2e-home bin/agentpaas pack /tmp/agentpaas-e2e-agent
AGENTPAAS_HOME=$HOME/agentpaas-e2e-home bin/agentpaas run weather-agent
# Note the runID

# Invoke (once implemented)
AGENTPAAS_HOME=$HOME/agentpaas-e2e-home bin/agentpaas invoke <runID>

# Stop + query audit
AGENTPAAS_HOME=$HOME/agentpaas-e2e-home bin/agentpaas stop <runID>
AGENTPAAS_HOME=$HOME/agentpaas-e2e-home bin/agentpaas audit query --run-id <runID> --json
# VERIFY: audit records contain egress_denied with destination https://example.com
cat ~/agentpaas-e2e-home/state/runs/<runID>/harness-audit/harness-audit.jsonl
```

THEN (in order, each is a micro-chunk):
1. B13-T06: prompt-change immutable redeploy — est 4-5 turns
2. B13-T07: demo matrix fixtures — est 3-4 turns
3. B13-T08: /agentpaas slash commands — est 4-5 turns
4. B13-T09: bundled SKILL.md — est 3-4 turns
5. make block13-gate implementation — 2-3 turns
6. Block-end verifier + b13-block-end.md — 3-4 turns

If you reach turn 35 before finishing all, checkpoint and write exit prompt for the remainder.

Start at: Solve the invoke gap so the harness receives an invoke RPC and the agent's agent.http() call fires, producing an egress_denied audit event.
