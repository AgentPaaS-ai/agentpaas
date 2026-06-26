Continue AgentPaaS Block 14B — Real-time Egress Timeline.

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read execution plan: agentpaas-execution-plan-v1.md search "14B" (tasks T01-T05)
- Read B14A risk analysis: docs/b14a-risk-analysis.md (10 P1 backlog items)
- Read checkpoint: docs/b14a-checkpoint-1.md

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: 69aba9b docs: B14A risk analysis + checkpoint
- B14A COMPLETE: all T01-T08 merged, verifier passed, gate passing
- 166 Python plugin tests + all Go tests pass with -race
- 10 P1 backlog items documented in risk analysis (accepted adversary findings)
- GitHub issues #159-#163 open for 14B tasks (T01-T05)

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Worker: grok-composer-2.5-fast via Grok CLI ($0)
- Adversary: grok-4.3 via agentpaas-adversary profile
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only

GROK WORKER DISPATCH PATTERN (print mode, preferred):
```bash
grok --no-auto-update -m grok-composer-2.5-fast \
  -p "$(cat /tmp/b14b-tNN-prompt.md)" \
  --always-approve --no-memory --max-turns 50 \
  --cwd /Users/pms88/projects/agentpaas
```

ADVERSARY DISPATCH PATTERN:
```bash
cd /Users/pms88/projects/agentpaas
hermes -p agentpaas-adversary chat -q "$(cat /tmp/b14b-tNN-adversary-prompt.md)" -Q --toolsets terminal,file,search
```

CRITICAL FROM B14A SESSION:
1. The pwd.getpwuid() pattern is standard for HOME env override resistance — use everywhere $HOME is trusted.
2. T05 adversary proved hash chain design is sound — always test edge cases (genesis, empty input) in adversary prompts.
3. T08 found production bug: cosign verify --offline deprecated in v3.1.1 → use --insecure-ignore-tlog.
4. T08 D3 verified: noTlogSigningConfigJSON DOES suppress Rekor upload.
5. --allow-insecure-registry now conditional on localhost/127.0.0.1 refs.
6. verifyImageSignature now called in integration test for regression protection.
7. block14a-gate implemented and passing.
8. Verifier verdict: BLOCK COMPLETE for 14A.

P1 BACKLOG (from B14A risk analysis — do NOT re-litigate, just be aware):
1. Unbounded confirmation set growth (T04 MEDIUM)
2. Hash chain record deletion undetectable (T05 MEDIUM)
3. NewFileAuditAppender prevHash seeding on re-open (T05 MEDIUM)
4. Cosign integration test in CI (T08 shortcut)
5. Registry container cleanup helper (T08 MEDIUM)
6. D3 tlog suppression check strengthening (T08 MEDIUM)
7. Port 5001 conflict handling (T08 MEDIUM)
8. Fake cosign verify flag validation (T08 MEDIUM)
9. --insecure-ignore-tlog conditional or Rekor-enabled mode (T08 HIGH → P1 design)
10. Signed checkpoint mechanism for hash chain (T05 MEDIUM + T08 HIGH)

POST-BLOCK-14 ACTION ITEM (user request):
After all of Block 14 (14A + 14B + 14C) is complete, go through entire AgentPaaS project — review each MEDIUM or LOW adversary callout so user can decide accept vs fix.

REMAINING B14B MICRO-CHUNKS (from GitHub issues #159-#163):
1. T01 (#159): Gateway container in Run handler (CRITICAL — core product)
2. T02 (#160): Policy enforcement at runtime (wire policy.yaml → gateway)
3. T03 (#161): Real-time egress visibility (dashboard timeline during run)
4. T04 (#162): DockerRuntime Stats implementation (dashboard resource monitoring)
5. T05 (#163): Trigger server startup in local-first mode

Start at: Read the execution plan for Block 14B, check git status, start T01 (gateway container in Run handler — this is the CRITICAL core product gap).
