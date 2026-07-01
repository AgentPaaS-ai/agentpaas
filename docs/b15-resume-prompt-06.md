Continue AgentPaaS Block 15 — P1 Completion Items (Pre-Release Gap Closure).

START HERE: 15-T05 MC5 (CAP_NET_ADMIN verification test).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b15-t05-decisions.md — MC4 architecture decision (Option B chosen)
- Read: docs/b15-checkpoint-04.md — T04 complete (trigger/cron surface)
- Read: agentpaas-execution-plan-v1.md — search "## BLOCK 15" then "15-T05"
- Read: internal/harness/firewall_caps_linux.go — the capset drop code under test

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: c066cc9 (merge MC3)
- BLOCK 15-T05 MC1 DONE (3b5c5ea): tightened RFC1918 allow to gateway /16 subnet
- BLOCK 15-T05 MC2 DONE (2084a59): Rekor retry fallback (3 attempts, 2s/4s backoff)
- BLOCK 15-T05 MC3 DONE (dfea120): checkpoint key encrypted at rest (AES-256-GCM)
- BLOCK 15-T05 MC4 DECIDED: Option B — keep PID 1 capset-drop, no init container
  (see docs/b15-t05-decisions.md). MC4 collapses into MC5.
- make block15-gate: PASS (T01+T02+T03+T04)
- make test: all Go packages pass
- make lint: 0 issues
- 3 stale git worktrees at /tmp/b15-t05-mc{1,2,3} — clean with:
  git worktree remove /tmp/b15-t05-mc1 /tmp/b15-t05-mc2 /tmp/b15-t05-mc3

MC4 ARCHITECTURE DECISION (already made — Option B):
The full init container pattern (Option A) is P2. For P1, we keep the existing
PID 1 capset-drop approach (DropNetAdminCapability in firewall_caps_linux.go)
which already removes CAP_NET_ADMIN from the agent process before the Python
worker starts. MC4 is therefore just MC5 — the verification test that proves
this works end-to-end in a real Docker container.

Full rationale in docs/b15-t05-decisions.md.

REMAINING MICRO-CHUNKS:
1. MC5: CAP_NET_ADMIN verification test
   - Docker integration test (AGENTPAAS_DOCKER_TESTS=1 guard) proving the
     agent process (UID 64000) cannot run `iptables -F` after the harness
     binary calls DropNetAdminCapability()
   - Assert iptables -F returns permission denied (non-zero exit code)
   - Assert iptables OUTPUT DROP policy persists (rules not flushed)
   - Also add a unit test for DropNetAdminCapability on linux builds
     (internal/harness/firewall_caps_linux_test.go — verify capset clears
     the bit; can mock the syscall or test the logic path)
   - Test location: internal/runtime/ or internal/harness/
   - Pattern: see agentpaas-build-rhythm skill "In-process e2e test pattern"
     and "Docker test cleanup races" pitfalls

2. MC6: Gate + docs
   - Update block15-gate Makefile target to add T05 section
   - Write docs/b15-checkpoint-05.md
   - Write docs/b15-risk-analysis.md (include MC4 Option B decision, all
     adversary findings, P1 backlog items)
   - Write docs/b15-resume-prompt-07.md

3. Adversary review (MANDATORY — T05 is security-critical)
   - Dispatch grok-4.3 via agentpaas-adversary profile on all T05 changes
   - Review MC1 (RFC1918 tightening), MC2 (Rekor retry), MC3 (key encryption),
     MC5 (capset verification)
   - Fix all HIGH findings immediately; document MEDIUM/LOW in risk analysis
   - User must approve all MEDIUM/LOW accept decisions

4. Verifier (block-end)
   - Run GLM-5.2 via agentpaas-verifier profile
   - Verify block15-gate passes with T05 section

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Worker: grok-composer-2.5-fast via Grok CLI ($0, subscription)
  (if grok stalls >5min at 0% CPU, kill and redispatch via delegate_task
   with hermes config set delegation.model deepseek/deepseek-v4-pro)
- Adversary: grok-4.3 via agentpaas-adversary profile
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only

CRITICAL FROM THIS SESSION:
1. MC1-MC3 all dispatched to grok workers sequentially via git worktree +
   --cwd pattern. Each completed in 1-3 minutes. Zero stalls. Sequential
   dispatch is reliable for shared-repo changes.
2. Grok startup takes 30-60s. The "reply OK" health check needs timeout=120,
   not 30.
3. Checkpoint key encryption reuses proven crypto from
   internal/identity/filestore.go (AES-256-GCM + PBKDF2-HMAC-SHA256, 100K
   iterations). Passphrase resolves from: env var → macOS Keychain (via
   `security` CLI) → passphrase file (0600). See internal/audit/passphrase.go.
4. Legacy unencrypted DER keys read transparently (migration on next regen).
5. Rekor retry only fires for production refs (isLocalRegistryRef=false).
   Pattern matching in isRetryableSignError covers rekor/tlog/5xx/timeout.
6. gatewaySubnetFromIP() derives /16 from gateway IP. Passed as
   AGENTPAAS_GATEWAY_SUBNET env var.
7. Pre-existing gofmt issues in internal/audit/ (16 files) — NOT from T05.
   Do NOT fix in T05 scope.
8. The capset drop (DropNetAdminCapability) clears CAP_NET_ADMIN from
   effective+permitted+inheritable sets via unix.Capset. The stub for
   non-Linux builds is in firewall_caps_other.go (if it doesn't exist,
   MC5 worker should create it — //go:build !linux, no-op function).
9. Plugin tool count: 29. T05 adds no plugin tools (internal hardening).

T05 SCOPE STATUS:
- R17 init container pattern: DECIDED Option B (capset drop, no init container)
- R17 tighten RFC1918 allow: DONE (MC1)
- R1 Rekor retry fallback: DONE (MC2)
- Checkpoint key encryption at rest: DONE (MC3)
- CAP_NET_ADMIN capset verification: MC5 (THIS SESSION)

After T05: T06 (release binary), T07 (clean-machine docs), T08 (done).
Then block15-gate passes fully → Block 16 (manual testing).

BUILD DISCIPLINE:
- Micro-chunks: one change at a time, test, commit, checkpoint
- Write resume prompt after checkpoint
- Run make test and make lint before every commit
- T05 is security-critical — adversary review MANDATORY before declaring done
- Orchestrator NEVER edits code directly — all code via grok worker dispatch
- Orchestrator CAN write docs directly (OWA exception for .md files)
- Sequential dispatch (not parallel) for same-repo changes

Start with: MC5 (CAP_NET_ADMIN verification test). Dispatch a grok worker
with the test requirements. The test must be a Docker integration test
(AGENTPAAS_DOCKER_TESTS=1) that starts a real container, verifies
DropNetAdminCapability ran, and confirms iptables -F fails as UID 64000.
