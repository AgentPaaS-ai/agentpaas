Continue AgentPaaS Block 16 — P1 Completion Items (Pre-Release Gap Closure).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b14-final-checkpoint.md (Block 14 final verification complete)
- Read: docs/b14e-checkpoint-2.md (B14E status — all 24 risk items resolved)
- Read: docs/b14e-risk-analysis.md (residual P2 items)
- Read: agentpaas-execution-plan-v1.md Block 16 (search "BLOCK 16")

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: d87bcb4 "fix(docs): correct broken README links"
- BLOCK 14 FULLY COMPLETE: all code done, all tests green, all CI workflows green
- All 24 risk register items resolved (R1-R21 + R22-R24 from B14D CI work)
- 3 CI workflows green: ci.yml, block-gates.yml, release-verify.yml
- Docker e2e test (pack→run→invoke→stop→audit) passes with real Docker/colima
- Two final tests added: checkpoint key 0600 permissions + CapAdd Docker normalization
- Broken docs links fixed, lychee R24 gate passing with 0 errors

B16 SCOPE (from execution plan — P1 items, must close before v0.1.0):

Build order: 16-T05 → 16-T01 → 16-T04 → 16-T07 → 16-T02 → 16-T03 → 16-T06

1. 16-T05: Credential onboarding (`secret add/list/remove/rotate` CLI commands)
2. 16-T01: LLM provider integration (Hermes selects provider at agent design time,
   installs API key in Keychain, gateway proxies LLM calls, budget + audit)
3. 16-T04: Policy authoring via Hermes (`policy init`, default templates,
   validation at pack time, Hermes plugin skill for Q&A-based generation)
4. 16-T07: Production hardening (init container for NET_ADMIN removal, tighten
   RFC1918, Rekor retry, checkpoint key encryption, capset verification test)
5. 16-T02: Release binary (v0.1.0 tag, goreleaser, brew install, cosign verify)
6. 16-T03: Clean-machine prerequisites docs (agentpaas doctor, README quickstart)
7. 16-T06: HTTP/HTTPS egress regression gate (already passing, just needs gate target)

P2 ITEMS (tracked, not blocking):
- 16-P2-01: Linux support (systemd, libsecret, deb/rpm)
- 16-P2-02: Dashboard / observability (policy diff, cost tracking, visual timeline)
- 16-P2-03: Multi-agent orchestration (chaining, shared state, scheduled runs)
- 16-P2-04: Non-HTTP egress deep inspection (transparent proxy, DNS, DLP)

CRITICAL FROM B14 FINAL VERIFICATION:
1. The e2e test (pack→run→invoke→stop→audit) passes — infrastructure is solid
2. The trigger server supports AGENTPAAS_TRIGGER_API_KEY for auth (R18)
3. The audit chain has signed checkpoints (R2+R3) — verify during e2e
4. The egress firewall (R17) adds iptables rules — agents can reach allowed destinations
5. R1 conditional tlog means production images require Rekor
6. Checkpoint key file is written with 0600 permissions (verified by test)
7. Docker API normalizes capability names to CAP_ prefix (NET_ADMIN → CAP_NET_ADMIN)
8. agent.llm() returns fake response — this is the #1 gap to close

PRE-B16 CHECKS (ALL PASSED):
1. ✅ Full e2e: AGENTPAAS_DOCKER_TESTS=1 TestE2E_PackRunInvokeStopAudit — PASS (19s)
2. ✅ make block14-gate — all 4 sub-segments pass
3. ✅ make lint — 0 issues
4. ✅ make test — 21/21 packages pass
5. ✅ make race — 21/21 packages pass with -race
6. ✅ Python plugin tests — 167 tests pass
7. ✅ GitHub CI — all 3 workflows green

Start at: 16-T05 (credential onboarding CLI commands). This is the foundation
that 16-T01 (LLM integration) depends on.
