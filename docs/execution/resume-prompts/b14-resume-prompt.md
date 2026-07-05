Continue AgentPaaS Block 15 — Manual Testing and Release.

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b14e-checkpoint-2.md (final B14E status)
- Read: docs/b14e-risk-analysis.md (residual P2 items)
- Read: agentpaas-execution-plan-v1.md Block 15 (search "BLOCK 15")

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: 24ec2ce "docs: update B14D risk register — all 24 items resolved"
- B14E COMPLETE: all 20 remaining risk items resolved, all tests green, block14-gate passes
- Risk register: 24/24 items resolved (see execution plan §14D)
- 3 CI workflows green: ci.yml, block-gates.yml, release-verify.yml

B15 SCOPE (from execution plan):
1. Volunteer clean-machine test: 2 users follow README on their own macOS, reach running governed agent in <15 min
2. Demo video/asciinema recordings (R21 — deferred from B14C)
3. v0.1.0 tag + goreleaser release
4. cosign verify-blob on real release artifacts
5. Offline bundle creation + verification

CRITICAL FROM B14E SESSION:
1. All code work is done — B15 is manual testing + release, no new code expected
2. The trigger server now supports AGENTPAAS_TRIGGER_API_KEY for auth (R18)
3. The audit chain now has signed checkpoints (R2+R3) — verify during e2e
4. The egress firewall (R17) adds iptables rules — test that agents can still reach allowed destinations
5. R1 conditional tlog means production images require Rekor — test with a real registry if possible

PRE-B15 CHECKS:
1. Run full e2e: AGENTPAAS_DOCKER_TESTS=1 make block14a0-gate (pack→run→invoke→stop→audit)
2. Verify checkpoint creation: check state/audit.jsonl.checkpoints exists after a run
3. Verify egress firewall: run an agent and confirm it can reach allowed domains through the gateway
4. Push to GitHub to trigger all 3 CI workflows

Start at: read docs/b14e-checkpoint-2.md, then run the pre-B15 e2e checks.
