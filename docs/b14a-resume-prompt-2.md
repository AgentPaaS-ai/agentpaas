Continue AgentPaaS Block 14A — Security Remediation.

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read execution plan: agentpaas-execution-plan-v1.md search "14A" (tasks T01-T08)
- Read B13 security audit skill: agentpaas-b13-security-audit (gap descriptions)
- Read T05 adversary log: /tmp/b14a-t05-adversary-prompt.md (adversary findings for hash chain)

STATE:
- Repo: ~/projects/agentpaas, on main, 10 commits ahead of origin/main (NOT pushed yet)
- Last commit on main: 4b0ccb9 Merge feat/b14a-t04: T04 thread-safe confirmation state (14A)
- Branch feat/b14a-t05 exists with hash-chained audit (commit ba0d54b), NOT merged — adversary HIGH finding needs fix first
- Working tree has untracked file: docs/b14a0-resume-prompt-2.md (old resume prompt, can delete)
- All feat/b14a-t0{1,2,3,4} branches are merged and can be deleted

COMPLETED 14A TASKS (all merged to main):
- T01 (plugin path allow-list, GAP-1): commit 3f6bb07 + fix 0a7d029. _validate_project_path() in tools.py, 16 tests. Adversary: HOME + PROJECT_ROOT override → fixed via pwd.getpwuid().
- T02 (AGENTPAAS_CLI binary verification, GAP-2): commit f1b0401 + fix 56babd6. Path allow-list + --version check. 10 tests + 3 adversary tests. Adversary: HOME override for ~/.local/bin → fixed via pwd.getpwuid().
- T03 (subprocess output cap + timeout, GAP-3): commit de4e03b. AGENTPAAS_CLI_TIMEOUT env var, stdout 50KB cap, stderr 10KB cap. 10 tests.
- T04 (thread-safe confirmation state, GAP-5): commit da31c0d. _ConfirmationState class with threading.Lock. 7 tests (concurrent replay: 10 threads, exactly 1 succeeds). Adversary: 1 MEDIUM (unbounded set growth — acceptable P1).
- T09 (slash commands + SKILL.md): Already resolved from B13. No work needed.

IN PROGRESS — T05 (hash-chained harness audit, GAP-6):
- Branch: feat/b14a-t05, commit ba0d54b
- FileAuditAppender maintains SHA-256 hash chain (prev_hash + record_hash per record)
- Daemon verifyHarnessChain() validates harness chain before ingestion
- On tamper: logs harness_audit_chain_broken event, refuses ingestion
- 8 tests pass with -race, build passes, 0 lint issues
- Exported ComputeRecordHash() on AuditRecord for cross-package use

ADVERSARY FINDINGS FOR T05 (1 HIGH, 2 MEDIUM):
1. HIGH — verifyHarnessChain() does NOT check genesis record has prev_hash=="". An attacker
   who tampers with the JSONL post-container could set a non-empty prev_hash on the first
   record and the verification would pass. FIX: add `if records[0].PrevHash != ""` check at
   the start of the loop (i==0 case). Add test TestVerifyHarnessChain_GenesisNonEmptyPrevHash.
2. MEDIUM — record deletion (truncation of last records) is undetectable. No length/anchor
   comparison. Acceptable for P1 (the daemon chain is authoritative, not the harness chain).
3. MEDIUM — NewFileAuditAppender doesn't seed prevHash from existing file on re-open.
   Low risk (per-run new file), but contract gap. Can defer to P2.

NEXT ACTIONS (in order):

1. CLEANUP GIT TREE
   - Delete merged feature branches: git branch -D feat/b14a-t01 feat/b14a-t02 feat/b14a-t03 feat/b14a-t04
   - Remove untracked old resume prompt: rm docs/b14a0-resume-prompt-2.md
   - Push main to GitHub: git push origin main

2. FIX T05 ADVERSARY HIGH FINDING
   - Checkout feat/b14a-t05
   - Dispatch Grok worker to add genesis prev_hash=="" check in verifyHarnessChain()
   - Add test: TestVerifyHarnessChain_GenesisNonEmptyPrevHash
   - Verify: go test ./internal/daemon/... -run TestVerifyHarnessChain -v -race
   - Merge feat/b14a-t05 to main: git checkout main && git merge --no-ff feat/b14a-t05
   - Delete branch: git branch -D feat/b14a-t05
   - Push to GitHub

3. T06: Pre-flight daemon socket check (GAP-8, LOW)
   - Worker prompt already written at /tmp/b14a-t06-prompt.md
   - Dispatch Grok worker
   - 7 tests in test_socket_check.py
   - Merge to main, push

4. T07: Sanitizer improvements (GAP-4, MEDIUM)
   - Add base64 + hex decode steps to _decode_evidence_text() in sanitizer.py
   - Narrow false-positive pattern (line 40: "do not|don't|must|should|always|never") to only
     match when followed by a security-relevant verb (disable, enable, allow, deny, etc.)
   - Add YAML-structure injection detection (next_action: outside JSON)
   - Adversary test: base64-encoded "disable policy" → detected
   - Dispatch Grok worker, review, merge

5. T08: cosign tlog fix + real integration test (SHORTCUT-6)
   - Read plan: docs/plans/b13-cosign-coverage-fix.md
   - Create internal/pack/lock_sign_real_test.go with //go:build integration
   - Guard with AGENTPAAS_PACK_REAL_TOOLS=1
   - Test signs real image against localhost:5001, verifies with cosign verify --key <pub>
   - Fix --tlog-upload=false → correct cosign v3.x flag
   - Mutation check: break a flag → test goes RED
   - Dispatch Grok worker, review, merge

6. 14A GATE + VERIFIER
   - Implement make block14a-gate target (currently stubs with "not implemented" error)
   - Gate should run: go build, golangci-lint, go test -race ./internal/... ,
     python3 plugin tests (135+ tests), cosign integration test (if AGENTPAAS_PACK_REAL_TOOLS=1)
   - Run verifier (agentpaas-verifier profile) for block-end review
   - Write checkpoint: docs/b14a-checkpoint-1.md
   - Write block-end risk analysis: docs/b14a-risk-analysis.md
   - Push everything to GitHub

7. Close GitHub issues for 14A tasks (create issues if they don't exist yet)

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API. Plans, dispatches, reviews, merges. Does NOT edit code.
- Worker: grok-composer-2.5-fast via Grok CLI ($0). Dispatch on specific tasks. Use print mode (-p) for one-shot.
- Adversary: grok-4.3 via agentpaas-adversary profile. Security/correctness review.
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only.

GROK WORKER DISPATCH PATTERN (print mode, preferred):
```bash
grok --no-auto-update -m grok-composer-2.5-fast \
  -p "$(cat /tmp/b14a-tNN-prompt.md)" \
  --always-approve --no-memory --max-turns 50 \
  --cwd /Users/pms88/projects/agentpaas
```

ADVERSARY DISPATCH PATTERN:
```bash
cd /Users/pms88/projects/agentpaas
hermes -p agentpaas-adversary chat -q "$(cat /tmp/b14a-tNN-adversary-prompt.md)" -Q --toolsets terminal,file,search
```

CRITICAL FROM THIS SESSION:
1. The pwd.getpwuid() pattern is the standard fix for HOME env override attacks — use it everywhere $HOME is trusted for security boundaries.
2. The --version substring check ("agentpaas" in output) is a P1 limitation — it's defense-in-depth, not cryptographic verification. Acceptable per build-rhythm skill.
3. T05 adversary proved the hash chain design is sound but the genesis check was missed — always test edge cases (first record, empty input, etc.) in adversary prompts.
4. 152 Python plugin tests pass, all Go tests pass with -race, 0 lint issues on main.
5. All 14A work is on local main only — NOT pushed to GitHub yet. Must push before next session.

TEST COUNTS (on main after T04 merge):
- Python plugin tests: 152 (was 109 at start of 14A)
- Go harness tests: 4 new (file_appender chain tests)
- Go daemon tests: 4 new (chain verification tests)
- Go audit tests: no regressions

REMAINING MICRO-CHUNKS (in order):
1. Fix T05 adversary HIGH (genesis prev_hash check) — 1 worker dispatch
2. Merge T05, push to GitHub
3. T06 pre-flight socket check — 1 worker dispatch (prompt at /tmp/b14a-t06-prompt.md)
4. T07 sanitizer improvements — 1 worker dispatch
5. T08 cosign integration test — 1-2 worker dispatches
6. Implement make block14a-gate target
7. Run verifier
8. Write checkpoint + risk analysis
9. Push + close issues

Start at: Cleanup git tree (delete merged branches, remove untracked file), push main to GitHub, then fix T05 adversary HIGH finding.
