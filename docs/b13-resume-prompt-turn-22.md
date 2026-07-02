Continue AgentPaaS Block 13. 22 turns used. Pick up at BUG 7d Step 5 (fix test agent SDK bundling) then e2e verification.

CONTEXT (load these first):
- Load skills: agentpaas-40-turn-rhythm, agentpaas-owa-build-orchestration
- Read checkpoint: docs/b13-checkpoint-turn-22.md
- Read architecture trace: docs/b13-deploy-e2e-checkpoint-v5.md (§"FULL ARCHITECTURE TRACE" and §"E2E test plan for BUG 7d verification")
- Read the B13 block spec in agentpaas-execution-plan-v1.md (search "BLOCK 13")

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: merge of fix/b13-audit-ingest (BUG 7d Step 4)
- Steps 1-4 DONE: harness audit appender, egress audit events, ContainerSpec.Binds, mount audit volume in Run handler, ingest harness audit JSONL on Stop.
- Step 5 BLOCKED: test agent at /tmp/agentpaas-e2e-agent/ created but SDK not bundled.
- All daemon + runtime tests passing. Build + lint clean.
- Session limit: 40 turns. Micro-chunks only. Merge after each. Checkpoint every 2-3 chunks. Exit prompt mandatory at turn 38.

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API. Plans, dispatches, reviews, merges. Does NOT edit code.
- Worker: grok-composer-2.5-fast via Grok CLI ($0). Dispatch on git worktree via tmux.
- Adversary: grok-4.3 via agentpaas-adversary profile ($0).
- Verifier: GLM-5.2 via agentpaas-verifier profile, ONCE at block-end.

CRITICAL FROM THIS SESSION:
1. The agentpaas_sdk is a Python RPC client package (python/agentpaas_sdk/) that the test agent imports to call agent.http(), agent.llm(), agent.mcp(). These send JSON over a Unix socket to the Go harness (PID 1). The harness makes the actual HTTP call and writes audit events.
2. The distroless container (gcr.io/distroless/python3-debian12) has NO pip. The SDK cannot be pip-installed at runtime. It must be bundled into the agent project dir so the build copies it into /app/.
3. The harness Python worker finds the SDK via pythonPackagePath() which walks from cwd (/app in container) looking for python/agentpaas_sdk/. The fix: copy python/agentpaas_sdk/ into the test agent project dir before packing.
4. lookupRun now returns 3 values (containerID, netID, auditDir). All call sites updated.
5. ingestHarnessAudit() in Stop handler reads {AuditDir}/harness-audit.jsonl and appends to daemon audit chain. Errors don't fail Stop.

IMMEDIATE NEXT ACTION:
Fix the test agent, then run e2e verification:
```sh
# 1. Bundle the SDK into the test agent project
mkdir -p /tmp/agentpaas-e2e-agent/python
cp -r ~/projects/agentpaas/python/agentpaas_sdk /tmp/agentpaas-e2e-agent/python/agentpaas_sdk

# 2. Remove agentpaas_sdk from requirements.txt (no pip in distroless)
# requirements.txt should just be: "# Add Python dependencies here.\n"

# 3. Verify main.py is correct — should call agent.http("GET", "https://example.com") in try/except

# 4. Build all binaries
cd ~/projects/agentpaas
go build -o bin/agentpaas ./cmd/agentpaas
go build -o bin/agentpaasd ./cmd/agentpaasd
go build -o bin/agentpaas-harness ./cmd/harness
GOOS=linux GOARCH=arm64 go build -o bin/agentpaas-harness-linux ./cmd/harness

# 5. Start daemon with e2e home
AGENTPAAS_HOME=/tmp/agentpaas-e2e-home bin/agentpaasd &

# 6. Pack weather-agent
bin/agentpaas pack /tmp/agentpaas-e2e-agent

# 7. Run weather-agent
bin/agentpaas run weather-agent

# 8. Wait for container to exit (agent finishes quickly), then stop
bin/agentpaas stop <runID>

# 9. Query audit
bin/agentpaas audit --run-id <runID>

# 10. VERIFY: audit records contain egress_denied with destination https://example.com
```

THEN (in order, each is a micro-chunk):
1. B13-T06: prompt-change immutable redeploy — est 4-5 turns
2. B13-T07: demo matrix fixtures — est 3-4 turns
3. B13-T08: /agentpaas slash commands — est 4-5 turns
4. B13-T09: bundled SKILL.md — est 3-4 turns
5. make block13-gate implementation — 2-3 turns
6. Block-end verifier + b13-block-end.md — 3-4 turns

If you reach turn 35 before finishing all, checkpoint and write exit prompt for the remainder.

Start at: Fix the test agent SDK bundling (3 commands above), then build + e2e verify.
