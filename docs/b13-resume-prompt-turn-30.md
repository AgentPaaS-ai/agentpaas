Continue AgentPaaS Block 13. ~30 turns used. Pick up at BUG 7d Step 5 e2e verification.

CONTEXT (load these first):
- Load skills: agentpaas-40-turn-rhythm, agentpaas-owa-build-orchestration
- Read checkpoint: docs/b13-checkpoint-turn-22.md
- Read architecture trace: docs/b13-deploy-e2e-checkpoint-v5.md (§"FULL ARCHITECTURE TRACE" and §"E2E test plan for BUG 7d verification")
- Read the B13 block spec in agentpaas-execution-plan-v1.md (search "BLOCK 13")

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: abb6362 (audit dir 0700→0777)
- UNCOMMITTED: control_handlers.go has an additional os.Chmod(hostAuditDir, 0o777) after MkdirAll — this is the real fix, not yet built/tested/committed
- Steps 1-4 DONE + committed: harness audit appender, egress audit events, ContainerSpec.Binds, mount audit volume, ingest harness audit on Stop
- Step 5 SDK bundling DONE: python/agentpaas_sdk/ copied into /tmp/agentpaas-e2e-agent/python/, removed from requirements.txt
- All daemon + runtime tests passing. Build clean (before the chmod patch).

CRITICAL FROM THIS SESSION:
1. os.MkdirAll(path, 0o777) does NOT yield 0777 — macOS umask 022 masks it to 0755. The container runs as UID 64000 (set in docker.go:116, NOT 65532 as previously assumed). UID 64000 maps to "other" on the host, so 0755 denies write. The fix is an explicit os.Chmod(hostAuditDir, 0o777) AFTER MkdirAll. This patch is already applied but NOT built/tested/e2e-verified.
2. The pack correctly bundles python/agentpaas_sdk/ into the image. Verified via debug test (collectBuildFiles returns all 9 files) and docker run inspection. The image at sha256:e61823ec has /app/python/agentpaas_sdk/.
3. The container exits cleanly (harness starts, Python worker connects) but the harness audit appender fails with "open /audit/harness-audit.jsonl: permission denied" because of the umask issue above.
4. The container uses ReadonlyRootfs: true with tmpfs on /tmp. The audit volume is the only writable bind mount.
5. Docker container cleanup: old exited containers accumulate from prior runs. Clean with: docker ps -aq --filter "ancestor=localhost:5001/agentpaas/weather-agent" | while read c; do docker rm -f "$c"; done
6. Colima must be running (colima start). Local registry must be running (docker start agentpaas-registry or docker run -d -p 5001:5000 --name agentpaas-registry registry:2).
7. lookupRun returns 3 values (containerID, netID, auditDir). All call sites updated.
8. ingestHarnessAudit() in Stop handler reads {AuditDir}/harness-audit.jsonl and appends to daemon audit chain. Errors don't fail Stop.

IMMEDIATE NEXT ACTION:
Build, test, and e2e-verify the chmod fix:
```sh
# 1. Build + test the chmod patch
cd ~/projects/agentpaas
go build ./...
go test ./internal/daemon/... -count=1

# 2. If tests pass, commit
git add internal/daemon/control_handlers.go
git commit -m "fix(b13): explicit chmod 0777 for audit dir to defeat umask

os.MkdirAll(path, 0o777) yields 0755 due to umask 022. Container UID
64000 maps to 'other' on host, so 0755 denies write. Add explicit
os.Chmod after MkdirAll to force 0777."

# 3. Rebuild all binaries
go build -o bin/agentpaas ./cmd/agent
go build -o bin/agentpaasd ./cmd/agentpaasd
go build -o bin/agentpaas-harness ./cmd/harness
GOOS=linux GOARCH=arm64 go build -o bin/agentpaas-harness-linux ./cmd/harness

# 4. Full e2e (clean slate)
pkill -f agentpaasd; sleep 1
rm -rf /tmp/agentpaas-e2e-home
mkdir -p /tmp/agentpaas-e2e-home
docker ps -aq --filter "ancestor=localhost:5001/agentpaas/weather-agent" | while read c; do docker rm -f "$c"; done

# Start daemon (background)
AGENTPAAS_HOME=/tmp/agentpaas-e2e-home bin/agentpaasd > /tmp/agentpaas-e2e-home/daemon.log 2>&1 &

# Pack + run
sleep 2
AGENTPAAS_HOME=/tmp/agentpaas-e2e-home bin/agentpaas pack /tmp/agentpaas-e2e-agent
AGENTPAAS_HOME=/tmp/agentpaas-e2e-home bin/agentpaas run weather-agent
# Note the runID from output

# Wait for container to start, check no permission denied in logs
sleep 15
docker ps -q --filter "ancestor=localhost:5001/agentpaas/weather-agent" | head -1 | xargs -I{} docker logs {}

# Stop + query audit
AGENTPAAS_HOME=/tmp/agentpaas-e2e-home bin/agentpaas stop <runID>
AGENTPAAS_HOME=/tmp/agentpaas-e2e-home bin/agentpaas audit query --run-id <runID> --json

# VERIFY: audit records contain egress_denied with destination https://example.com
# Also check harness-audit.jsonl was written:
cat /tmp/agentpaas-e2e-home/state/runs/<runID>/harness-audit/harness-audit.jsonl
```

THEN (in order, each is a micro-chunk):
1. B13-T06: prompt-change immutable redeploy — est 4-5 turns
2. B13-T07: demo matrix fixtures — est 3-4 turns
3. B13-T08: /agentpaas slash commands — est 4-5 turns
4. B13-T09: bundled SKILL.md — est 3-4 turns
5. make block13-gate implementation — 2-3 turns
6. Block-end verifier + b13-block-end.md — 3-4 turns

If you reach turn 35 before finishing all, checkpoint and write exit prompt for the remainder.

Start at: Build + test the chmod patch, then commit, then full e2e verify.
