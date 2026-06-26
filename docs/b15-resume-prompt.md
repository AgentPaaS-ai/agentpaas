Continue AgentPaaS Block 15 — Manual Testing and Release.

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b14-final-checkpoint.md (Block 14 final verification complete)
- Read: docs/b14e-checkpoint-2.md (B14E status — all 24 risk items resolved)
- Read: docs/b14e-risk-analysis.md (residual P2 items)
- Read: agentpaas-execution-plan-v1.md Block 15 (search "BLOCK 15")

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: 1583814 "test: verify checkpoint key 0600 perms + fix CapAdd Docker normalization"
- BLOCK 14 FULLY COMPLETE: all code done, all tests green, all CI workflows green
- All 24 risk register items resolved (R1-R21 + R22-R24 from B14D CI work)
- 3 CI workflows green: ci.yml, block-gates.yml, release-verify.yml
- Docker e2e test (pack→run→invoke→stop→audit) passes with real Docker/colima
- Two final tests added: checkpoint key 0600 permissions + CapAdd Docker normalization

B15 SCOPE (from execution plan):
1. Volunteer clean-machine test: 2 users follow README on their own macOS, reach running governed agent in <15 min
2. Demo video/asciinema recordings (R21 — deferred from B14C)
3. v0.1.0 tag + goreleaser release
4. cosign verify-blob on real release artifacts
5. Offline bundle creation + verification

CRITICAL FROM B14 FINAL VERIFICATION:
1. All code work is done — B15 is manual testing + release, no new code expected
2. The trigger server supports AGENTPAAS_TRIGGER_API_KEY for auth (R18)
3. The audit chain has signed checkpoints (R2+R3) — verify during e2e
4. The egress firewall (R17) adds iptables rules — test that agents can reach allowed destinations
5. R1 conditional tlog means production images require Rekor — test with a real registry if possible
6. Checkpoint key file is written with 0600 permissions (verified by test)
7. Docker API normalizes capability names to CAP_ prefix (NET_ADMIN → CAP_NET_ADMIN)

PRE-B15 CHECKS (ALL PASSED):
1. ✅ Full e2e: AGENTPAAS_DOCKER_TESTS=1 TestE2E_PackRunInvokeStopAudit — PASS (19s)
2. ✅ make block14-gate — all 4 sub-segments pass
3. ✅ make lint — 0 issues
4. ✅ make test — 21/21 packages pass
5. ✅ make race — 21/21 packages pass with -race
6. ✅ Python plugin tests — 167 tests pass
7. ✅ GitHub CI — all 3 workflows green

Start at: tag v0.1.0 and trigger goreleaser release (release.yml workflow runs on tag push).
