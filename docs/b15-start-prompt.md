Continue AgentPaaS Block 15 — P1 Completion Items (Pre-Release Gap Closure).

START HERE: 15-T01 (Credential Onboarding). This is the first task — 15-T02
(LLM integration) depends on it.

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: agentpaas-execution-plan-v1.md — search "## BLOCK 15" for full task specs
- Read: docs/b15-e2e-test-plan.md — lifecycle use cases LC-01..LC-05 that these tasks enable
- Read: docs/b14-final-checkpoint.md (B14 complete, all 24 risk items resolved)

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: fc4e2d2 "plan: renumber B15 tasks"
- BLOCK 14 FULLY COMPLETE: all code done, tests green, 3 CI workflows green
- B15 plan finalized: 8 tasks, numbered in build order T01→T08
- B16 (manual testing) runs AFTER B15 — do not start B16 until B15 gate passes

B15 TASKS (build order = task number):
1. 15-T01: Credential onboarding (secret add/list/remove/rotate/test CLI)
2. 15-T02: LLM provider integration (unified gateway egress, depends on T01)
3. 15-T03: Policy authoring (policy init, templates, pack-time validation)
4. 15-T04: Trigger/cron/event surface (exposes existing B9 backend via CLI + plugin)
5. 15-T05: Production hardening (init container, encrypted key, Rekor retry)
6. 15-T06: Release binary (v0.1.0 tag, goreleaser, brew install)
7. 15-T07: Clean-machine docs (prerequisites, README quickstart)
8. 15-T08: Egress regression gate (already passing, just wire the gate target)

15-T01 SCOPE (what to build now):
- `agentpaas secret add <name>` — stores credential in macOS Keychain
- `agentpaas secret list` — lists secrets by label, never by value
- `agentpaas secret remove <name>` — removes a credential
- `agentpaas secret rotate <name>` — atomic add+remove
- `agentpaas secret test <name>` — pre-deployment validation: makes a trivial
  authenticated call to the target service (LLM: "say OK"; third-party: GET
  /health or equivalent), OUTSIDE the container, before pack/run. Fail fast
  with clear error if key is wrong, provider unreachable, or egress policy
  doesn't allow destination. Required by 15-T02 (test-before-baking-in).
- Keychain service name convention documented and enforced
- Hermes plugin: secret onboarding skill (guide user through adding credentials)

EXISTING INFRASTRUCTURE (already built, reuse don't rebuild):
- internal/secrets/broker.go — Broker.RequestCredential() does credential
  injection via headers, revocation, deny-credentialed-redirect, audit events
- internal/secrets/keychain.go — macOS Keychain integration
- internal/secrets/store.go — secret store with last-used tracking
- internal/secrets/lease.go — credential leases
- internal/cli/control.go — existing `secret set/list/rm` commands (line 573+),
  but NO `test` or `rotate` commands yet. Use these as the starting point.
- integrations/hermes-plugin/tools.py — existing plugin tools, add secret tools here
- policy.yaml already supports credentials[] with brokered access

15-T01 VERIFICATION:
1. `agentpaas secret add openweather-api-key` stores a key in Keychain
2. `agentpaas secret list` shows it by label, value never printed
3. `agentpaas secret test openweather-api-key` validates it (trivial HTTP call)
4. `agentpaas secret rotate openweather-api-key` replaces it atomically
5. `agentpaas secret remove openweather-api-key` deletes it
6. Secret value never appears in: container env, logs, audit trail, CLI output
7. Unit tests for all 5 commands + integration test with real Keychain

BUILD DISCIPLINE:
- Micro-chunks: one CLI command at a time, test, commit, checkpoint every 2-3
- Write resume prompt docs/b15-checkpoint-<seq>.md after each checkpoint
- Run `make test` and `make lint` before every commit
- Follow agentpaas-build-rhythm skill for the OWA pattern
- Do NOT start 15-T02 until 15-T01 is complete and tested

PRE-B15 CHECKS (all passed, do not re-run unless debugging):
- make test — 21/21 packages pass
- make race — 21/21 with -race
- make lint — 0 issues
- make block14-gate — all 4 sub-segments pass
- Python plugin tests — 167 tests pass
- GitHub CI — 3 workflows green
- Docker e2e (AGENTPAAS_DOCKER_TESTS=1) — PASS

Start with `agentpaas secret add` first. Read internal/cli/control.go lines
573-620 to see the existing `secret set/list/rm` commands, then add `test`
and `rotate`, then wire the Hermes plugin tools.
