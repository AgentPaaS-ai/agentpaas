Continue AgentPaaS Block 15 — P1 Completion Items (Pre-Release Gap Closure).

START HERE: Block 15 is code-complete. T01-T05, T08 done + verified. T06
(goreleaser) config fixed. T07 (docs) updated. Remaining: block-end verifier,
merge to main, then Block 16 (manual testing).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b15-checkpoint-05.md — T05 complete, all MCs verified
- Read: docs/b15-risk-analysis.md — full risk analysis, adversary findings
- Read: docs/b15-t05-decisions.md — MC4 Option B architecture decision
- Read: agentpaas-execution-plan-v1.md — search "## BLOCK 15"

STATE:
- Repo: ~/projects/agentpaas, on branch feat/b15-t05-mc5-capset-verify
- HEAD: a814658 (go mod tidy)
- make build: PASS
- make lint: 0 issues
- make block15-gate: PASS (T01+T02+T03+T04+T05+T08)
- goreleaser check: PASS (0 deprecation warnings)
- goreleaser release --snapshot: PASS (builds + archives + SBOMs + checksums)
- MC5 Docker integration test: PASS (AGENTPAAS_DOCKER_TESTS=1)

BLOCK 15 COMPLETION STATUS:
- T01 (credential onboarding): DONE (prior sessions)
- T02 (LLM integration): DONE (prior sessions)
- T03 (policy authoring): DONE (prior sessions)
- T04 (trigger/cron surface): DONE (prior sessions)
- T05 (production hardening): DONE (MC1-MC6 all complete this session)
  - MC1: RFC1918 tightened to gateway /16 (3b5c5ea)
  - MC2: Rekor retry fallback (2084a59) + adversary fix (c8df2c2)
  - MC3: checkpoint key encrypted at rest (dfea120)
  - MC4: Option B decision (cfa7785) — capset drop, not init container
  - MC5: CAP_NET_ADMIN verification Docker test (ca29c68 + 37288e0 fix)
  - MC6: gate wiring + docs (5c248d2, 007f5d9, 53f00fc)
- T06 (release binary): config fixed (024dfd5). Tag push = actual release.
- T07 (clean-machine docs): stale known-limitations.md updated (007f5d9)
- T08 (egress regression gate): wired (a9bf858 + a570d24)

ADVERSARY FINDINGS (T05):
- HIGH: 500-substring false retry in Rekor error classification (FIXED: c8df2c2)
- All other T05 claims CONFIRMED-SAFE (MC1 gateway subnet, MC3 crypto, MC5 capset)
- Full report in docs/b15-risk-analysis.md

REMAINING STEPS:
1. Block-end verifier (MANDATORY before merge)
   - Run GLM-5.2 via agentpaas-verifier profile
   - Fresh context, verify block15-gate passes on merged code
   - Cross-subtask integration review (T01-T05 + T08)
   - Report: VERIFY PASS or VERIFY FAIL with evidence

2. Merge to main
   - git checkout main
   - git merge feat/b15-t05-mc5-capset-verify (no-ff, preserve commits)
   - git push origin main
   - Verify CI passes on main

3. Tag v0.1.0 (triggers release pipeline)
   - git tag v0.1.0
   - git push origin v0.1.0
   - release.yml builds darwin/amd64 + darwin/arm64 binaries, signs with cosign,
     generates SBOMs, publishes to homebrew-tap

4. Block 16 (manual use-case assessment)
   - Only starts after block15-gate passes on main
   - 2-user <15 min test on fresh macOS

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only
- Workers: grok-composer-2.5-fast via Grok CLI ($0, subscription)
  (fallback: deepseek-v4-pro via openrouter if grok stalls)

CRITICAL FROM THIS SESSION:
1. MC5 test had a real bug — iptables -L (read) requires CAP_NET_ADMIN just
   like iptables -F (write). Original test wrongly asserted agent could read
   rules after capset drop. Fixed by removing agent-side read assertion.
2. Rekor retry "500" pattern was too broad — matched "5001" in error messages.
   Tightened to "HTTP 500", "status 500", "500 " (with trailing space).
3. goreleaser v2.16 deprecates archives.format (→ formats list) and brews
   (soft-deprecated). Config migrated, snapshot release verified.
4. go mod tidy promotes golang.org/x/sys to direct dep (capset test imports
   golang.org/x/sys/unix). This is correct, not a regression.
5. All adversary break-tests committed: internal/pack/adversary_t05_break_test.go
6. Plugin tool count: 29 (unchanged — T05 is internal hardening)

BUILD DISCIPLINE:
- All code changes went through workers (grok or deepseek-v4-pro)
- Orchestrator wrote docs (.md) and Makefile changes directly (OWA exception)
- Every micro-chunk committed individually with clear scope
- Gate verified after each merge
- Adversary review MANDATORY for security-critical T05 changes — DONE

This session's commits (feat/b15-t05-mc5-capset-verify):
  a814658 chore: go mod tidy — golang.org/x/sys promoted to direct dep
  a570d24 fix: T08 gate regex — match 'Firewall' tests properly
  53f00fc docs: B15 risk analysis
  c8df2c2 fix(security): tighten Rekor retry patterns (MC2 adversary fix)
  a9bf858 merge: B15-T08 — wire egress regression into block15-gate
  024dfd5 fix(release): migrate goreleaser config to v2.16
  007f5d9 docs: B15-T05 checkpoint-05 + update stale known-limitations
  5c248d2 merge: B15-T05 MC6 — wire T05 into block15-gate
  37288e0 fix(test): MC5 capset verify — iptables -L requires NET_ADMIN
  ca29c68 test(harness): CAP_NET_ADMIN capset verification (MC5)
  (prior session commits for MC1-MC4 below this)
