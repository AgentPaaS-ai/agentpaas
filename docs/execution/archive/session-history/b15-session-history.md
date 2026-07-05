# Block 15 — Session History

# B15 Session Checkpoint — 01

**Date:** 2026-06-30
**Branch:** main (commit after MC6 merge)
**Goal:** 15-T01 Credential Onboarding — complete all 5 secret CLI commands + plugin tools + docs

## Completed This Session
- MC1: secret add/remove with aliases (commit 0633252) — renamed set→add, rm→remove
- MC2: secret rotate command (commit 0212fe0) — atomic credential replacement via stdin
- MC3: secret test command + provider adapters (commit 4c72682) — OpenAI/Anthropic/xAI validation
- MC4: Hermes plugin 5 secret tools (commit c6def5c) — _run_cli_with_stdin, 11 Python tests
- MC5: credential onboarding docs + Hermes skill (commit b09ea0a)
- MC6: block15-gate Makefile update + this checkpoint

## Key Facts
- Provider adapters use package-level endpoint vars (openAIEndpoint, anthropicEndpoint, xaiEndpoint)
  with SetTestEndpoints() for test override
- secret test auto-detects provider from name: "openai"/"gpt"→openai, "anthropic"/"claude"→anthropic, "xai"/"grok"→xiai
- _run_cli_with_stdin helper added to plugin tools.py for stdin-based secret value passing
- Secret values NEVER appear in: CLI stdout/stderr, audit trail, container env, process args
- Grok CLI stalled twice (xAI API congestion, 0% CPU 5+ min). Killed and redispatched to
  deepseek-v4-pro via delegate_task. Both completed in <7 min each. The build-rhythm skill
  already documents this pattern.
- MC2 subagent did both MC2+MC3 work in one commit (rotate + test command). MC3 subagent
  created the provider adapter files. Reconciled via cherry-pick.

## Test Counts
- Go: 21 packages all pass (make test, make race, make lint — 0 issues)
- Python: 178 tests pass (was 167, +11 new secret tool tests)
- block15-gate: PASS (Go secrets+cli tests + Python plugin tests)

## Next Session Start
- Immediate next action: start 15-T02 (LLM provider integration via unified gateway egress)
- File to read first: agentpaas-execution-plan-v1.md, search "15-T02"
- Block: B15, Subtask: T02, Micro-chunk: LLM provider adapter + agent.llm() sugar
- 15-T02 depends on 15-T01 (this task) — secret add + secret test are the foundation

## 15-T01 Verification Status
1. `agentpaas secret add <name>` — DONE, stores in Keychain
2. `agentpaas secret list` — DONE, shows labels only, never values
3. `agentpaas secret test <name>` — DONE, validates via provider HTTP call
4. `agentpaas secret rotate <name>` — DONE, atomic replace
5. `agentpaas secret remove <name>` — DONE, deletes from Keychain
6. Secret value never in container env, logs, audit, CLI output — VERIFIED by tests
7. Unit tests for all 5 commands + provider adapter tests — DONE (21+ Go tests, 11 Python tests)
8. Hermes plugin: 5 secret tools — DONE (MC4)
9. Keychain service name convention documented — DONE (MC5)
10. Hermes secret onboarding skill — DONE (MC5)

---

Continue AgentPaaS Block 15 — P1 Completion Items (Pre-Release Gap Closure).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b14-final-checkpoint.md (Block 14 final verification complete)
- Read: docs/b14e-checkpoint-2.md (B14E status — all 24 risk items resolved)
- Read: docs/b14e-risk-analysis.md (residual P2 items)
- Read: agentpaas-execution-plan-v1.md Block 15 (search "BLOCK 15")

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: d87bcb4 "fix(docs): correct broken README links"
- BLOCK 14 FULLY COMPLETE: all code done, all tests green, all CI workflows green
- All 24 risk register items resolved (R1-R21 + R22-R24 from B14D CI work)
- 3 CI workflows green: ci.yml, block-gates.yml, release-verify.yml
- Docker e2e test (pack→run→invoke→stop→audit) passes with real Docker/colima
- Two final tests added: checkpoint key 0600 permissions + CapAdd Docker normalization
- Broken docs links fixed, lychee R24 gate passing with 0 errors

B15 SCOPE (from execution plan — P1 items, must close before v0.1.0):

Build order: 15-T01 → 15-T02 → 15-T03 → 15-T04 → 15-T05 → 15-T06 → 15-T07 → 15-T08

1. 15-T01: Credential onboarding (`secret add/list/remove/rotate/test` CLI commands)
2. 15-T02: LLM provider integration (unified gateway egress, interactive provider
   selection, `secret test` pre-deployment validation, agent.llm = sugar over
   http_with_credential, deprecate fake handleLLM RPC)
3. 15-T03: Policy authoring via Hermes (`policy init`, default templates,
   validation at pack time, Hermes plugin skill for Q&A-based generation)
4. 15-T04: Trigger/cron/event surface (CLI + plugin tools for invoke, cron
   add/list/remove, event publish/subscribe — exposes existing B9 backend)
5. 15-T05: Production hardening (init container for NET_ADMIN removal, tighten
   RFC1918, Rekor retry, checkpoint key encryption, capset verification test)
6. 15-T06: Release binary (v0.1.0 tag, goreleaser, brew install, cosign verify)
7. 15-T07: Clean-machine prerequisites docs (agentpaas doctor, README quickstart)
8. 15-T08: HTTP/HTTPS egress regression gate (already passing, just needs gate target)

P2 ITEMS (tracked, not blocking):
- 15-P2-01: Linux support (systemd, libsecret, deb/rpm)
- 15-P2-02: Dashboard / observability (policy diff, cost tracking, visual timeline)
- 15-P2-03: Multi-agent orchestration (chaining, shared state, scheduled runs)
- 15-P2-04: Non-HTTP egress deep inspection (transparent proxy, DNS, DLP)

CRITICAL FROM B14 FINAL VERIFICATION:
1. The e2e test (pack→run→invoke→stop→audit) passes — infrastructure is solid
2. The trigger server supports AGENTPAAS_TRIGGER_API_KEY for auth (R18)
3. The audit chain has signed checkpoints (R2+R3) — verify during e2e
4. The egress firewall (R17) adds iptables rules — agents can reach allowed destinations
5. R1 conditional tlog means production images require Rekor
6. Checkpoint key file is written with 0600 permissions (verified by test)
7. Docker API normalizes capability names to CAP_ prefix (NET_ADMIN → CAP_NET_ADMIN)
8. agent.llm() returns fake response — this is the #1 gap to close

PRE-B15 CHECKS (ALL PASSED):
1. ✅ Full e2e: AGENTPAAS_DOCKER_TESTS=1 TestE2E_PackRunInvokeStopAudit — PASS (19s)
2. ✅ make block14-gate — all 4 sub-segments pass
3. ✅ make lint — 0 issues
4. ✅ make test — 21/21 packages pass
5. ✅ make race — 21/21 packages pass with -race
6. ✅ Python plugin tests — 167 tests pass
7. ✅ GitHub CI — all 3 workflows green

Start at: 15-T01 (credential onboarding CLI commands). This is the foundation
that 15-T02 (LLM integration) depends on.

---

# B15 Session Checkpoint — 02

**Date:** 2026-06-30
**Branch:** main (commit daf6172 — merge of feat/b15-t02-mc6-gate)
**Goal:** 15-T02 LLM Provider Integration — complete all 6 micro-chunks

## Completed This Session
- MC1: agent.yaml llm schema — provider, model, credential binding fields (commit 34082a3)
- MC2: LLM provider adapter package internal/llm/ — openai/anthropic/xai adapters (commit 34082a3)
- MC3: Real LLM calls via provider adapter in handleLLM + SDK model param (commit 0d0126a)
- MC4: Deprecate fake handleLLM — thin wrapper over real adapter (commit 0d0126a)
- MC5: buildInvokePayload credential resolution + LLM config population (commit 6551234)
- MC6: Integration test + lint fixes + block15-gate T02 update (commit 2e382ad)
- MC6 (plugin): agentpaas_llm_configure plugin tool — interactive LLM provider selection (commit ea79fa2)
- Merge to main (commit daf6172)

## Key Facts
- internal/llm/ package: provider adapters for OpenAI, Anthropic, xAI
  - Maps provider name → endpoint + auth header + request/response format
  - SetTestEndpoints() for test override (same pattern as secret provider adapters)
- agent.yaml llm schema: llm.provider, llm.model, credential binding
- handleLLM harness RPC: now makes real LLM calls via provider adapter (was fake stub)
- buildInvokePayload: resolves LLM credential from secrets broker, populates llm config in invoke payload
- agentpaas_llm_configure plugin tool (24th plugin tool): interactive provider selection,
  asks user which LLM, writes agent.yaml llm.provider + llm.model
- Integration test: real LLM call through gateway, audited (rpc_server_llm_integration_test.go)
- llm-configuration.md skill added to hermes-plugin skills

## Test Counts
- Go: 21 packages all pass (make test, make race, make lint — 0 issues)
- Python: 190 tests pass (was 178, +12 new LLM configure tool tests)
- block15-gate: PASS (T01 secrets+cli + T02 llm+harness+daemon+pack + 190 Python plugin tests)

## Next Session Start
- Immediate next action: start 15-T03 (Policy Authoring via Hermes)
- File to read first: agentpaas-execution-plan-v1.md, search "15-T03"
- Block: B15, Subtask: T03 — policy authoring (default templates, validation, plugin skill)
- 15-T03 does NOT depend on T02 — it's about policy.yaml scaffolding and validation
- T03 scope: agentpaas policy init command, default templates (allow-http, allow-llm, allow-mcp,
  deny-all), policy validation at pack time, Hermes plugin policy generation skill

## 15-T02 Verification Status
1. agent.yaml llm schema (provider, model, credential binding) — DONE (MC1)
2. LLM provider adapter (openai/anthropic/xai) — DONE (MC2)
3. agent.llm() / handleLLM real calls via adapter — DONE (MC3+MC4)
4. buildInvokePayload credential resolution — DONE (MC5)
5. Hermes plugin interactive provider selection tool — DONE (MC6 plugin)
6. Integration test (real LLM call, audited) — DONE (MC6)
7. block15-gate T02 section — DONE, PASS
8. Go tests + lint — DONE, 0 issues
9. Python plugin tests — DONE, 190 pass

---

Continue AgentPaaS Block 15 — P1 Completion Items (Pre-Release Gap Closure).

START HERE: 15-T02 (LLM Provider Integration via Unified Gateway Egress).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b15-checkpoint-01.md — T01 complete, all 5 secret commands + plugin tools working
- Read: agentpaas-execution-plan-v1.md — search "## BLOCK 15" then "15-T02" for full task specs
- Read: docs/b15-e2e-test-plan.md — lifecycle use cases LC-01..LC-05 that these tasks enable

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: ed7ce6f "chore: update block15-gate for T01 + checkpoint"
- BLOCK 15-T01 FULLY COMPLETE: secret add/list/remove/rotate/test + provider adapters + plugin tools + docs
- make block15-gate: PASS (Go secrets+cli tests + 178 Python plugin tests)
- make test: 21/21 packages pass
- make lint: 0 issues
- 15-T02 depends on 15-T01 (secret add + secret test are the foundation)

15-T02 DESIGN (Option B — Unified Gateway Egress):
LLM calls are NOT special. They route through the gateway as credentialed HTTP
egress, exactly like any third-party API call. The existing secrets broker,
gateway proxy, audit chain, and policy engine already handle credentialed HTTP
egress (B7 adversary-tested, B14 risk-closed). agent.llm() becomes thin sugar
over agent.http_with_credential to the provider's chat-completions endpoint.

Four pillars:
1. Interactive provider selection — plugin ASKS the user which LLM, writes
   agent.yaml llm.provider + llm.model. User decision, not Hermes-decided.
2. Pre-deployment credential validation — agentpaas secret test <name> (DONE in T01)
3. Unified LLM routing — agent.llm = sugar over http_with_credential.
   Deprecate the fake handleLLM harness RPC.
4. agent.yaml schema — llm.provider, llm.model, credential binding.

MICRO-CHUNKS (suggested):
1. MC1: agent.yaml schema — add llm.provider, llm.model, credential binding fields
2. MC2: LLM provider adapter — maps provider→endpoint+auth header+request/response format
3. MC3: agent.llm() SDK method → sugar over http_with_credential
4. MC4: Deprecate/convert fake handleLLM harness RPC to thin wrapper
5. MC5: Hermes plugin — interactive provider selection tool
6. MC6: Integration test (real LLM call through gateway, audited) + checkpoint

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Worker: deepseek-v4-pro via delegate_task (Grok CLI stalled twice last session
  due to xAI API congestion — use deepseek directly to avoid the stall)
- Adversary: grok-4.3 via agentpaas-adversary profile (for security-sensitive changes)
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only

CRITICAL FROM LAST SESSION:
1. Grok CLI stalls on xAI API congestion (0% CPU 5+ min). Use deepseek-v4-pro
   via delegate_task as the primary worker dispatch method.
2. Provider adapters use package-level endpoint vars with SetTestEndpoints()
   for test override. Follow this pattern for LLM provider adapters.
3. Secret values NEVER in CLI output, audit trail, container env, or process args.
4. block15-gate now runs T01 tests. Add T02 tests as they're completed.

BUILD DISCIPLINE:
- Micro-chunks: one function/adapter at a time, test, commit, checkpoint every 2-3
- Write resume prompt docs/b15-checkpoint-<seq>.md after each checkpoint
- Run make test and make lint before every commit
- Follow agentpaas-build-rhythm skill for the OWA pattern
- Do NOT start 15-T03 until 15-T02 is complete and tested

Start with the agent.yaml schema first (MC1), then the LLM provider adapter (MC2).

---

# B15 Session Checkpoint — 03

**Date:** 2026-06-30
**Branch:** main (commit pending — MC1-MC4 merged, gate updated)
**Goal:** 15-T03 Policy Authoring — complete all micro-chunks

## Completed This Session
- MC1+MC2: `agentpaas policy init` command + 4 default templates (commit 2cf45b5, merged 3255f69)
  - Templates: deny-all, allow-http, allow-llm, allow-mcp
  - 10 Go tests in internal/cli/policy_init_test.go
  - Interactive prompt + --template + --noninteractive + --force flags
- MC3: Pack-time policy validation + digest computation (commit 2eb71fc, merged fca7a1a)
  - computePolicyDigest() helper in internal/pack/lock.go — parses, validates, SHA-256
  - LockConfig.PolicyYAML field added ([]byte)
  - CreateAgentLock now sets lock.PolicyDigest (was hardcoded "")
  - Daemon Pack handler reads policy.yaml from project dir, passes to LockConfig
  - Invalid policies rejected at pack time with clear error messages
  - 9 Go tests in internal/pack/lock_policy_test.go
- MC4: agentpaas_policy_init plugin tool (commit d201631, merged 5044ce5)
  - 25th plugin tool — wraps `agentpaas policy init` CLI
  - policy-generation.md skill doc (Hermes Q&A flow)
  - Schema updated with template enum (deny-all/allow-http/allow-llm/allow-mcp)
  - 7 Python tests in test_policy_init.py (197 total)
- MC5: block15-gate T03 section added + this checkpoint

## Key Facts
- 4 policy templates in internal/cli/policy_templates.go as Go string constants
- pack-time validation: policy.ParsePolicy + policy.ValidatePolicy in CreateAgentLock
- PolicyDigest is SHA-256 of canonical (sorted-key) JSON of parsed policy
- No policy.yaml in project → PolicyDigest empty, no error (backward compat)
- policy.yaml with validation errors → pack fails with error listing first 3 issues
- Plugin tool agentpaas_policy_init is the 25th tool (was 24 after T02)
- policy-generation.md skill guides Hermes to ask user which template fits, then call tool
- Parallel dispatch of MC3+MC4 caused minor lock.go conflict (both workers added
  policyValidationErrorString with slightly different formatting) — resolved by keeping
  Fprintf variant. Lesson: even different packages can conflict if both touch lock.go.

## Test Counts
- Go: 21 packages all pass (make test, make race, make lint — 0 issues)
  - internal/cli: 10 new policy_init tests
  - internal/pack: 9 new lock_policy tests
  - internal/daemon: unchanged (control_handlers.go reads policy.yaml)
- Python: 197 tests pass (was 190, +7 new policy_init tests)
- block15-gate: PASS (T01 secrets+cli + T02 llm+harness+daemon+pack + T03 pack+cli+daemon + 197 Python)

## Next Session Start
- Immediate next action: start 15-T04 (Trigger / Cron / Event Surface)
- File to read first: agentpaas-execution-plan-v1.md, search "15-T04"
- Block: B15, Subtask: T04 — trigger/cron/event surface (CLI + plugin)
- 15-T04 does NOT depend on T03 — it's about exposing the trigger backend
- T04 scope: CLI subcommands for trigger invoke/cron add/list/remove/event publish,
  plugin tools, wiring the existing internal/trigger/ backend to user-facing surface

## 15-T03 Verification Status
1. `agentpaas policy init` command (interactive + --template) — DONE (MC1)
2. Default policy templates: deny-all, allow-http, allow-llm, allow-mcp — DONE (MC2)
3. Policy validation at pack time (not just run time) — DONE (MC3)
4. Hermes plugin: policy generation skill (ask questions → generate YAML) — DONE (MC4)
5. `agentpaas policy init` produces a valid policy.yaml — DONE (tested)
6. Hermes generates a correct policy.yaml from user Q&A — DONE (skill doc)
7. Invalid policies rejected at pack time with clear error — DONE (MC3)
8. block15-gate T03 section — DONE, PASS
9. Go tests + lint — DONE, 0 issues
10. Python plugin tests — DONE, 197 pass

---

Continue AgentPaaS Block 15 — P1 Completion Items (Pre-Release Gap Closure).

START HERE: 15-T03 (Policy Authoring via Hermes — No UX Needed).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b15-checkpoint-02.md — T02 complete, LLM provider integration done
- Read: agentpaas-execution-plan-v1.md — search "## BLOCK 15" then "15-T03" for full task specs
- Read: docs/b15-e2e-test-plan.md — lifecycle use cases that these tasks enable

STATE:
- Repo: ~/projects/agentpaas, on main, last commit: daf6172 (merge of feat/b15-t02-mc6-gate)
- BLOCK 15-T01 FULLY COMPLETE: secret add/list/remove/rotate/test + provider adapters + plugin tools + docs
- BLOCK 15-T02 FULLY COMPLETE: LLM provider adapter (openai/anthropic/xai), real handleLLM calls,
  buildInvokePayload credential resolution, agentpaas_llm_configure plugin tool, integration tests
- make block15-gate: PASS (T01 secrets+cli + T02 llm+harness+daemon+pack + 190 Python plugin tests)
- make test: 21/21 Go packages pass
- make lint: 0 issues
- 15-T03 does NOT depend on T02 — it's about policy.yaml scaffolding and validation

15-T03 DESIGN (Policy Authoring via Hermes):
policy.yaml exists and compiles into gateway rules, but there's no tooling to help
users write it. AgentPaaS provides:
- Default policy templates for common patterns (allow one API, allow read-only S3,
  allow LLM calls, deny all by default)
- `agentpaas policy init` scaffold command — interactive or template-based, generates
  starter policy.yaml
- Policy validation with clear error messages — syntax errors caught at pack time,
  not run time
- Hermes plugin skill that asks the user about egress requirements and generates
  the policy.yaml automatically

SCOPE:
- `agentpaas policy init` command (interactive or template-based)
- Default policy templates: allow-http, allow-llm, allow-mcp, deny-all
- Policy validation at pack time (not just run time)
- Hermes plugin: policy generation skill (ask questions → generate YAML)

VERIFICATION:
- `agentpaas policy init` produces a valid policy.yaml
- Hermes generates a correct policy.yaml from user Q&A
- Invalid policies are rejected at pack time with a clear error

MICRO-CHUNKS (suggested):
1. MC1: `agentpaas policy init` command — template selection + scaffold policy.yaml
2. MC2: Default policy templates — allow-http, allow-llm, allow-mcp, deny-all
3. MC3: Policy validation at pack time — clear error messages for syntax errors
4. MC4: Hermes plugin policy generation skill — ask questions → generate YAML
5. MC5: Integration test + block15-gate T03 section + checkpoint

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Worker: deepseek-v4-pro via delegate_task (Grok CLI stalled twice in T01 due to
  xAI API congestion — use deepseek directly to avoid the stall)
- Adversary: grok-4.3 via agentpaas-adversary profile (for security-sensitive changes)
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only

CRITICAL FROM LAST SESSION:
1. Grok CLI stalls on xAI API congestion (0% CPU 5+ min). Use deepseek-v4-pro
   via delegate_task as the primary worker dispatch method.
2. Provider adapters use package-level endpoint vars with SetTestEndpoints()
   for test override. Follow this pattern for policy validation if it needs
   to check endpoint references.
3. Secret values NEVER in CLI output, audit trail, container env, or process args.
4. block15-gate now runs T01+T02 tests. Add T03 tests as they're completed.
5. Plugin tool count at 24 (agentpaas_llm_configure was the 24th). Policy generation
   skill may add a 25th tool if interactive Q&A is needed.

BUILD DISCIPLINE:
- Micro-chunks: one command/template at a time, test, commit, checkpoint every 2-3
- Write resume prompt docs/b15-checkpoint-<seq>.md after each checkpoint
- Run make test and make lint before every commit
- Follow agentpaas-build-rhythm skill for the OWA pattern
- Do NOT start 15-T04 until 15-T03 is complete and tested

Start with the `agentpaas policy init` command (MC1), then default templates (MC2).

---

# B15 Session Checkpoint — 04

**Date:** 2026-06-30
**Branch:** main
**Goal:** Complete B15-T04 (Trigger / Cron / Event Surface)

## Completed This Session

- **MC1** (0a0b429): CronScheduler runtime management API — AddSchedule/ListSchedules/RemoveSchedule + JSON state persistence (cron-schedules.json). 7 unit tests.
- **MC2** (456f352): CronAdd/CronList/CronRemove RPCs added to control.proto, codegen'd, wired into daemon Start(). CronScheduler now starts automatically with the daemon, persists schedules to state/cron-schedules.json, invokes agents through daemon Run handler via TriggerService adapter. 7 handler tests.
- **MC3** (a9d561a): CLI commands — `agentpaas trigger invoke <agent>` + `agentpaas cron add/list/remove`. trigger invoke calls REST API with API-key auth; cron commands connect to daemon gRPC. 9 CLI tests.
- **MC4** (2a62ed4): Hermes plugin tools — agentpaas_trigger_invoke, agentpaas_cron_add, agentpaas_cron_list, agentpaas_cron_remove (26th-29th tools). Schemas updated. 11 plugin tests.
- **MC5**: block15-gate T04 section added. Gate passes (T01+T02+T03+T04).

## Verification

- `make block15-gate`: PASS
  - T01: secrets (pass)
  - T02: LLM (pass)
  - T03: policy (pass)
  - T04: trigger/cron (pass) — 46s trigger tests, 10s daemon, 4s CLI
  - Plugin: 208 tests pass (was 197, +11 new)
- `make lint`: 0 issues
- Plugin tool count: 29 (was 25, +4 trigger/cron tools)

## Architecture Summary

The trigger backend (B9) was fully built but had no user-facing surface. This
task wired it end-to-end:

1. **CronScheduler management** (internal/trigger/cron_management.go):
   AddSchedule validates cron expr, generates ScheduleID, persists to JSON.
   ListSchedules returns a snapshot. RemoveSchedule deletes by ID.
   State survives daemon restart via cron-schedules.json in AGENTPAAS_HOME/state/.

2. **Control RPCs** (api/control/v1/control.proto):
   CronAdd/CronList/CronRemove added to ControlService. REST endpoints at
   /v1/control/cron (POST), /v1/control/cron (GET), /v1/control/cron/{id} (DELETE).

3. **Daemon wiring** (internal/daemon/server.go):
   CronScheduler created in Start(), wired with TriggerService that calls
   controlServer.Run() — same path as trigger server invoke. Schedules loaded
   from state file on startup. CronScheduler.Stop() added to daemon shutdown.

4. **CLI** (internal/cli/trigger.go, cron.go):
   `agentpaas trigger invoke <agent> [--payload <file>] [--content-type <type>]`
   `agentpaas cron add <agent> --expr "*/5 * * * *" [--version <v>] [--timezone <tz>]`
   `agentpaas cron list`
   `agentpaas cron remove <schedule-id>`

5. **Plugin tools** (integrations/hermes-plugin/tools.py):
   4 new tools wrapping the CLI commands. Schemas in schemas.py.

## In Progress
- Nothing — T04 is complete.

## Next Session Start
- **Immediate next action:** Start B15-T05 (Production Hardening). This includes:
  - R17 init container pattern (remove CAP_NET_ADMIN from agent container)
  - R17 tighten RFC1918 allow (specific gateway subnet only)
  - R1 Rekor retry fallback for production image signing
  - Checkpoint key encryption at rest (AES or Keychain Secure Enclave)
  - CAP_NET_ADMIN capset verification test
- **File to read first:** agentpaas-execution-plan-v1.md, search "15-T05"
- **Block:** B15, Subtask: T05

## Key Facts
- CronScheduler state file: <AGENTPAAS_HOME>/state/cron-schedules.json
- Trigger REST: 127.0.0.1:7717 (default), POST /v1/trigger/invoke
- Trigger gRPC: 127.0.0.1:7718 (default)
- Trigger API key: AGENTPAAS_TRIGGER_API_KEY env var (optional, for --expose)
- Plugin tool count: 29 total
- All workers used grok-composer-2.5-fast (per user instruction), no stalls

---

Continue AgentPaaS Block 15 — P1 Completion Items (Pre-Release Gap Closure).

START HERE: 15-T04 (Trigger / Cron / Event Surface).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b15-checkpoint-03.md — T03 complete, policy authoring done
- Read: agentpaas-execution-plan-v1.md — search "## BLOCK 15" then "15-T04" for full task specs
- Read: docs/b15-e2e-test-plan.md — lifecycle use cases that these tasks enable

STATE:
- Repo: ~/projects/agentpaas, on main
- BLOCK 15-T01 FULLY COMPLETE: secret add/list/remove/rotate/test + provider adapters + plugin tools + docs
- BLOCK 15-T02 FULLY COMPLETE: LLM provider adapter, real handleLLM calls, buildInvokePayload,
  agentpaas_llm_configure plugin tool (24th), integration tests
- BLOCK 15-T03 FULLY COMPLETE: agentpaas policy init command + 4 templates, pack-time policy
  validation + digest computation, agentpaas_policy_init plugin tool (25th), policy-generation skill
- make block15-gate: PASS (T01+T02+T03 + 197 Python plugin tests)
- make test: 21/21 Go packages pass
- make lint: 0 issues
- 15-T04 does NOT depend on T03 — it's about exposing the trigger backend

15-T04 DESIGN (Trigger / Cron / Event Surface):
The trigger server backend (REST /v1/trigger/invoke, SSE streaming, EventBus, CronScheduler)
is fully built in internal/trigger/ but has no user-facing surface. No CLI subcommand to
create/manage triggers or cron schedules. No plugin tool. Schedules are only configurable
by editing daemon config.

SCOPE:
- CLI subcommands: trigger invoke, cron add/list/remove, event publish
- Plugin tools for the same operations
- Wire the existing internal/trigger/ backend to user-facing surface

VERIFICATION:
- `agentpaas trigger invoke <agent> <payload>` invokes an agent via trigger API
- `agentpaas cron add <agent> --schedule "*/5 * * * *"` creates a cron schedule
- `agentpaas cron list` shows active schedules
- `agentpaas cron remove <id>` removes a schedule
- `agentpaas event publish <topic> <payload>` publishes an event
- Plugin tools expose the same operations from Hermes

MICRO-CHUNKS (suggested):
1. MC1: `agentpaas trigger invoke` CLI command — calls existing trigger REST API
2. MC2: `agentpaas cron add/list/remove` CLI commands — manage CronScheduler
3. MC3: `agentpaas event publish` CLI command — publish to EventBus
4. MC4: Hermes plugin tools for trigger/cron/event
5. MC5: Integration test + block15-gate T04 section + checkpoint

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Worker: deepseek-v4-pro via delegate_task (Grok CLI stalled in earlier sessions
  due to xAI API congestion — use deepseek directly to avoid the stall)
- Adversary: grok-4.3 via agentpaas-adversary profile (for security-sensitive changes)
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only

CRITICAL FROM LAST SESSION:
1. Parallel dispatch of MC3+MC4 caused a minor lock.go conflict — both workers added
   policyValidationErrorString with slightly different formatting. Resolved by keeping
   Fprintf variant. Lesson: even when packages differ, if both touch the same file,
   conflict is possible. Consider sequential dispatch for same-file changes.
2. block15-gate now runs T01+T02+T03 tests. Add T04 tests as they're completed.
3. Plugin tool count at 25 (agentpaas_policy_init was the 25th). Trigger/cron/event
   tools may add 4-5 more tools.
4. Secret values NEVER in CLI output, audit trail, container env, or process args.
5. Provider adapters use package-level endpoint vars with SetTestEndpoints() for test
   override. Follow this pattern for any new HTTP-based adapters.

BUILD DISCIPLINE:
- Micro-chunks: one command at a time, test, commit, checkpoint every 2-3
- Write resume prompt docs/b15-checkpoint-<seq>.md after each checkpoint
- Run make test and make lint before every commit
- Follow agentpaas-build-rhythm skill for the OWA pattern
- Do NOT start 15-T05 until 15-T04 is complete and tested

Start with the `agentpaas trigger invoke` command (MC1), then cron commands (MC2).

---

# B15 Session Checkpoint — 05

**Date:** 2026-07-02
**Branch:** feat/b15-t05-mc5-capset-verify
**Goal:** Complete B15-T05 (Production Hardening) + close remaining B15 gaps

## Completed This Session

### T05 Production Hardening (code from prior sessions, verified + closed this session)

- **MC1** (3b5c5ea): Tightened RFC1918 iptables allow from broad 172.16/12,
  10/8, 192.168/16 to a single /16 derived from the gateway IP via
  `gatewaySubnetFromIP()`. Passed as `AGENTPAAS_GATEWAY_SUBNET` env var.
  Falls back to broad RFC1918 for backward compat with older daemons.
- **MC2** (2084a59): Rekor retry fallback for production image signing.
  `SignImage` retries up to 3 times with exponential backoff (2s, 4s) on
  transient errors (Rekor outage, network timeouts, 5xx). Local refs
  unaffected (tlog suppressed). Pattern matching in
  `isRetryableSignError` classifies rekor/tlog/5xx/timeout as retryable.
- **MC3** (dfea120): Checkpoint signing key encrypted at rest with
  AES-256-GCM. Passphrase derived via PBKDF2-HMAC-SHA256 (100K iterations).
  Source: env var → macOS Keychain (via `security` CLI) → passphrase file
  (0600). Legacy unencrypted DER keys read transparently (migration on
  next regen). Reuses proven crypto from `internal/identity/filestore.go`.
- **MC4** (cfa7785): Architecture decision — Option B chosen. Keep PID 1
  capset-drop approach. Full init container pattern (Option A) deferred to
  P2. See `docs/b15-t05-decisions.md`.
- **MC5** (ca29c68 + 37288e0): CAP_NET_ADMIN capset verification.
  - Docker integration test (`TestE2E_CapNetAdminDropped_AgentCannotFlushIPTables`)
    proving UID 64000 cannot run `iptables -F` after `DropNetAdminCapability()`.
  - Unit test for capset bit clearing (`TestDropNetAdminCapability_ClearsBit12`)
    on linux builds.
  - Stub test for non-Linux platforms.
  - **Bug found and fixed**: original test incorrectly asserted UID 64000
    could read `iptables -L OUTPUT` (also requires CAP_NET_ADMIN). Fixed by
    removing the agent-side read assertion; firewall state verified from
    root context only. Committed as 37288e0.
- **MC6** (5c248d2): block15-gate Makefile target updated with T05 section.
  Gate now runs T01+T02+T03+T04+T05.

### T05 Adversary Review
- Dispatched grok-4.3 via agentpaas-adversary profile on all T05 changes.
- Findings documented in risk analysis (see below).

## Verification

- `make build`: PASS (go build ./...)
- `make lint`: 0 issues (golangci-lint)
- `make block15-gate`: PASS (T01+T02+T03+T04+T05)
- MC5 Docker integration test: PASS
  (AGENTPAAS_DOCKER_TESTS=1, Colima, 15s runtime)
  - Confirmed: `iptables -F as UID 64000 → exit 4, Permission denied`
  - Confirmed: `iptables -L OUTPUT as root → DROP policy persists`
- Plugin tests: 208 pass

## Remaining Work (T06-T08)

### T06: Release Binary (macOS)
- goreleaser config has deprecated `archives.format` (→ `formats` since v2.6)
  and `brews` (soft-deprecated since v2.10). Needs migration to v2.16 syntax.
- release.yml, release-verify.yml, .goreleaser.yaml, Formula/agentpaas.rb exist.
- After config fix: `goreleaser release --snapshot` to verify local build,
  then tag v0.1.0 to trigger the release pipeline.

### T07: Clean-Machine Prerequisites
- README quickstart, docs/quickstart.md, agent doctor checks all exist.
- docs/known-limitations.md has STALE content ("No real LLM integration" —
  T02 fixed this). Needs update.
- Verification path: fresh macOS → brew install → agent doctor → agent
  init/pack/run in <15 min.

### T08: Egress Enforcement Regression Gate
- HTTP/HTTPS via gateway proxy, iptables egress firewall, IPv6 block all
  built and tested (B14B/B14E). This task is a regression gate confirmation.
- redteam-smoke target exists and runs 6 fixtures through the real pipeline.
- Wire a T08 assertion into block15-gate (or confirm redteam-smoke covers it).

## Key Facts

- Gateway subnet derivation: `gatewaySubnetFromIP(ip)` → /16 CIDR from gateway IP octets. Fail-closed if unset (no RFC1918 fallback).
- Rekor retry: 3 attempts, 2s/4s backoff, production refs only
- Checkpoint key: AES-256-GCM, PBKDF2-HMAC-SHA256 100K iterations, passphrase from Keychain
- Capset drop: clears CAP_NET_ADMIN from effective+permitted+inheritable via `unix.Capset`
- Plugin tool count: 29 (unchanged — T05 is internal hardening, no plugin tools)
- Docker test guard: `AGENTPAAS_DOCKER_TESTS=1`
- Docker socket: `unix://$HOME/.colima/default/docker.sock` (Colima)

---

Continue AgentPaaS Block 15 — P1 Completion Items (Pre-Release Gap Closure).

START HERE: 15-T05 (Production Hardening).

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read: docs/b15-checkpoint-04.md — T04 complete, trigger/cron surface done
- Read: agentpaas-execution-plan-v1.md — search "## BLOCK 15" then "15-T05" for full task specs
- Read: docs/b15-e2e-test-plan.md — lifecycle use cases that these tasks enable

STATE:
- Repo: ~/projects/agentpaas, on main
- BLOCK 15-T01 FULLY COMPLETE: secret add/list/remove/rotate/test + provider adapters + plugin tools + docs
- BLOCK 15-T02 FULLY COMPLETE: LLM provider adapter, real handleLLM calls, buildInvokePayload,
  agentpaas_llm_configure plugin tool (24th), integration tests
- BLOCK 15-T03 FULLY COMPLETE: agentpaas policy init command + 4 templates, pack-time policy
  validation + digest computation, agentpaas_policy_init plugin tool (25th), policy-generation skill
- BLOCK 15-T04 FULLY COMPLETE: CronScheduler management API (Add/List/Remove + state persistence),
  CronAdd/CronList/CronRemove RPCs + daemon wiring, trigger invoke + cron CLI commands,
  4 plugin tools (trigger_invoke, cron_add, cron_list, cron_remove — 26th-29th)
- make block15-gate: PASS (T01+T02+T03+T04 + 208 Python plugin tests)
- make test: 21/21 Go packages pass
- make lint: 0 issues
- 15-T05 does NOT depend on T04 — it's production hardening items

15-T05 DESIGN (Production Hardening — P1, Don't Skip):
Several P2 items from the B14E risk register should be closed before v0.1.0 ships.

SCOPE:
- R17 init container pattern: Remove CAP_NET_ADMIN from agent container entirely. Use an init
  container that programs iptables rules in a shared network namespace, then exits. The agent
  container never has NET_ADMIN.
- R17 tighten RFC1918 allow: Replace broad 172.16/12, 10/8, 192.168/16 allow with the specific
  gateway subnet only.
- R1 Rekor retry fallback: If Rekor is down during production image signing, implement automatic
  retry with backoff. Don't silently fail.
- Checkpoint key encryption at rest: The ECDSA P-256 checkpoint signing key is currently stored
  as unencrypted PKCS#8 DER at state/audit-checkpoint-key.der. Encrypt it at rest.
- CAP_NET_ADMIN capset verification: Add an integration test that verifies the agent process
  (UID 64000) cannot run iptables -F after the firewall init script runs and capabilities are dropped.

VERIFICATION:
- Agent container has no NET_ADMIN capability (verified via Docker inspect)
- iptables rules are set by init container, agent process cannot modify them
- RFC1918 allow only covers the gateway subnet (e.g., 172.18.0.0/16)
- Rekor retry: signing succeeds after transient Rekor failure (mock test)
- Checkpoint key is encrypted at rest (file is not raw DER)

MICRO-CHUNKS (suggested):
1. MC1: R17 init container pattern — restructure Docker topology to use init container
2. MC2: R17 tighten RFC1918 allow list
3. MC3: R1 Rekor retry fallback for production signing
4. MC4: Checkpoint key encryption at rest
5. MC5: CAP_NET_ADMIN capset integration test
6. MC6: Update block15-gate + checkpoint

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Worker: grok-composer-2.5-fast via delegate_task (per user instruction, works reliably)
- Adversary: grok-4.3 via agentpaas-adversary profile (for security-sensitive changes — T05 is
  security-critical: init container, capability dropping, encryption at rest)
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only

CRITICAL FROM LAST SESSION:
1. T04 trigger/cron surface completed cleanly — all 4 MCs dispatched to grok workers sequentially,
   no stalls, each completed in 1-3 minutes. Sequential dispatch is reliable for shared-repo changes.
2. CronScheduler state persists to <AGENTPAAS_HOME>/state/cron-schedules.json — survives restart.
3. Daemon now starts CronScheduler automatically in Start(), stops it in Stop().
4. Trigger invoke CLI calls REST API with optional Bearer auth via AGENTPAAS_TRIGGER_API_KEY.
5. Plugin tool count at 29. T05 does not add plugin tools (it's internal hardening).
6. block15-gate now runs T01+T02+T03+T04 tests.

BUILD DISCIPLINE:
- Micro-chunks: one change at a time, test, commit, checkpoint every 2-3
- Write resume prompt docs/b15-resume-prompt-<seq>.md after each checkpoint
- Run make test and make lint before every commit
- Follow agentpaas-build-rhythm skill for the OWA pattern
- T05 is security-critical — adversary review is MANDATORY for init container + capability changes
- Do NOT start 15-T06 until 15-T05 is complete and tested

Start with the R17 init container pattern (MC1) — this is the most complex and architecturally
significant change. Research the init container pattern first (AWS Bedrock AgentCore uses it),
then dispatch a worker with the specific Docker topology changes.

---

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

---

# Worker: B15-T01 MC1 — Rename `secret set`→`add`, `secret rm`→`remove` + aliases

## Repo
`~/projects/agentpaas`, on `main`. Last commit: `09debd9`.

## Scope (ONE micro-chunk)
The existing `agentpaas secret` command has `set`, `list`, `rm`. The B15-T01
spec requires `add`, `list`, `remove`, `rotate`, `test`. This chunk renames the
verbs to match the spec, keeping the old verbs as aliases for backward compat.

## Files to edit
- `internal/cli/control.go` — the `newSecretCmd()` function (lines ~570-640).

## Exact changes

### 1. `secret set <name>` → `secret add <name>` (primary), keep `set` as alias

In `newSecretCmd()`, change the `set` subcommand:
- `Use: "set <name>"` → `Use: "add <name>"`
- Add `Aliases: []string{"set"}` to the cobra.Command struct
- Keep `Short: "Create or update a secret from stdin"` (add is create-or-update)
- The success message: `"secret %q stored\n"` → keep as-is (or change to
  `"secret %q added\n"` — your call, but be consistent in tests)

### 2. `secret rm <name>` → `secret remove <name>` (primary), keep `rm` as alias

In `newSecretCmd()`, change the `rm` subcommand:
- `Use: "rm <name>"` → `Use: "remove <name>"`
- Add `Aliases: []string{"rm"}`
- Keep `Short: "Remove a secret"`
- Success message: `"secret %q removed\n"` — keep as-is

### 3. `secret list` — no change needed (already correct)

### 4. Help text
The `secret` parent command `Short` is "Manage local profile secrets" — keep it.
Update any `Long` if present to mention `add/list/remove/rotate/test` (rotate and
test are added in later chunks — don't add them here, just leave the Long as-is
or mention the three that exist).

## Tests to add

Create `internal/cli/secret_cmd_test.go` with:

1. `TestSecretAdd_AliasesSet` — verify `secret add` and `secret set` both resolve
   to the same command (use `cobra.Command.Find()` with `[]string{"secret","add"}`
   and `[]string{"secret","set"}`, assert same command returned).
2. `TestSecretRemove_AliasesRm` — same for `remove` and `rm`.
3. `TestSecretAdd_StoresInFakeKeychain` — use `secretStoreFactory` override (the
   existing `secretStoreFactory` var is a function variable — override it with
   `secrets.NewFakeKeyStore()` for the test, call `secret add <name>` via
   `cobra.Command.SetArgs([]string{"secret","add","test-key"})`, pipe stdin with
   the secret value, assert the store has the key.
4. `TestSecretList_NeverPrintsValue` — store a secret in FakeKeyStore, run
   `secret list`, assert stdout contains the name but NOT the value.
5. `TestSecretRemove_DeletesFromStore` — store a secret, run `secret remove`,
   assert it's gone from the FakeKeyStore.

Look at existing test patterns in `internal/cli/cli_test.go` for how to set up
cobra command tests with stdin piping and factory overrides. The
`secretStoreFactory` is a package-level var — override it in the test and
restore it via `t.Cleanup()`.

## Constraints
- Do NOT add `rotate` or `test` commands in this chunk — those are MC2/MC3.
- Do NOT change the `secretServiceName` or `newDefaultSecretStore` functions.
- Do NOT change the Keychain integration — only CLI command wiring.
- Run `make test` and `make lint` before finishing — both must pass.
- The existing `readSecretValue`, `writeSecretList`, `formatSecretTime`,
  `secretListItem` helpers stay as-is.

## Verification
- `go test ./internal/cli/... -run TestSecret -v` — all new tests pass
- `make test` — all 21+ packages still pass
- `make lint` — 0 issues
- `go run ./cmd/agentpaas secret add --help` shows "add" as primary
- `go run ./cmd/agentpaas secret set --help` still works (alias)

## Commit
`feat(cli): rename secret set→add, rm→remove with aliases (B15-T01 MC1)`

Do NOT push. Leave the commit on the local branch for orchestrator review.

---

# Worker: B15-T01 MC2 — Add `secret rotate` command (atomic add+remove)

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc2` (create it from main).
MC1 has already been merged to main: `secret add` and `secret remove` now exist
(with `set`/`rm` as aliases).

## Scope (ONE micro-chunk)
Add `agentpaas secret rotate <name>` — reads a new value from stdin, validates
it, then atomically replaces the stored secret. "Atomic" means: if the new
value fails validation, the old secret is preserved unchanged. If the store
Set fails, the old secret is preserved.

## File to edit
- `internal/cli/control.go` — the `newSecretCmd()` function.

## Exact change

Add a new subcommand to `newSecretCmd()`:

```go
cmd.AddCommand(&cobra.Command{
    Use:   "rotate <name>",
    Short: "Replace a secret with a new value from stdin (atomic)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        name := args[0]
        if err := secrets.ValidateSecretName(name); err != nil {
            return err
        }
        value, err := readSecretValue(cmd)
        if err != nil {
            return err
        }
        store, err := secretStoreFactory(cmd)
        if err != nil {
            return err
        }
        // Validate the new value BEFORE touching the existing secret.
        // readSecretValue already enforces size; this is defense-in-depth.
        if err := store.Set(cmd.Context(), name, value); err != nil {
            return fmt.Errorf("rotate secret %q: %w", name, err)
        }
        _, err = fmt.Fprintf(cmd.OutOrStdout(), "secret %q rotated\n", name)
        return err
    },
})
```

The existing `store.Set` is idempotent (it updates `UpdatedAt` and the value
in place — see FakeKeyStore.Set). So "rotate" is really just "Set with a
different success message." But the key semantic is: validation happens before
any store interaction, and if Set fails, the old value is preserved (FakeKeyStore
copies the value bytes, so a failed Set doesn't corrupt the existing entry).

## Test to add

In `internal/cli/cli_test.go`, add:

```go
func TestSecretRotateReplacesValue(t *testing.T) {
    store := secrets.NewFakeKeyStore()
    old := "old-value"
    new := "new-rotated-value"
    if err := store.Set(context.Background(), "rotate_me", []byte(old)); err != nil {
        t.Fatalf("Set old: %v", err)
    }

    stdout, stderr, err := executeSecretCmd(t, store, new, "secret", "rotate", "rotate_me")
    if err != nil {
        t.Fatalf("secret rotate returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
    }

    got, err := store.Get(context.Background(), "rotate_me")
    if err != nil {
        t.Fatalf("Get after rotate: %v", err)
    }
    if string(got) != new {
        t.Fatalf("rotated value = %q, want %q", got, new)
    }
    if strings.Contains(stdout, new) {
        t.Fatalf("rotate output leaked new value: %s", stdout)
    }
    if strings.Contains(stdout, old) {
        t.Fatalf("rotate output leaked old value: %s", stdout)
    }
    if !strings.Contains(stdout, "rotated") {
        t.Fatalf("rotate output missing confirmation: %s", stdout)
    }
}

func TestSecretRotateRejectsOversizePreservesOld(t *testing.T) {
    store := secrets.NewFakeKeyStore()
    old := "preserved-old-value"
    if err := store.Set(context.Background(), "rotate_guard", []byte(old)); err != nil {
        t.Fatalf("Set old: %v", err)
    }
    oversize := strings.Repeat("x", secrets.MaxSecretValueSize+1)

    _, _, err := executeSecretCmd(t, store, oversize, "secret", "rotate", "rotate_guard")
    if err == nil {
        t.Fatal("secret rotate oversize: want error, got nil")
    }

    got, err := store.Get(context.Background(), "rotate_guard")
    if err != nil {
        t.Fatalf("Get after failed rotate: %v", err)
    }
    if string(got) != old {
        t.Fatalf("old value not preserved after failed rotate: got %q, want %q", got, old)
    }
}
```

## Constraints
- Do NOT modify the `secret add`, `secret list`, or `secret remove` commands —
  they were done in MC1.
- Do NOT add `secret test` — that's MC3.
- Do NOT change the SecretStore interface or FakeKeyStore implementation.
- Run `make test` and `make lint` — both must pass.
- The existing `executeSecretCmd` test helper is available (see cli_test.go).

## Verification
- `go test ./internal/cli/... -run TestSecretRotate -v` — passes
- `make test` — all packages pass
- `make lint` — 0 issues
- `go run ./cmd/agentpaas secret rotate --help` shows the rotate usage

## Commit
`feat(cli): add secret rotate command for atomic credential rotation (B15-T01 MC2)`

Do NOT push.

---

# Worker: B15-T01 MC3 — Add `secret test` command + provider adapters

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc3` (create from main).
MC1+MC2 are merged: `secret add/list/remove/rotate` all work.

## Scope (ONE micro-chunk)
Add `agentpaas secret test <name> [--provider <provider>]` — pre-deployment
credential validation. Makes a trivial authenticated HTTP call to the target
service OUTSIDE the container, before pack/run. Fail fast with a clear error
if the key is wrong, provider is unreachable, or the destination is not
recognized.

## Design

The `secret test` command:
1. Reads the secret value from the Keychain store (via `store.Get`)
2. Detects the provider from the secret name (e.g. `openai-key` → openai) or
   from a `--provider` flag
3. Uses a provider adapter to build a trivial test HTTP request:
   - OpenAI: POST https://api.openai.com/v1/chat/completions with
     `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"say OK"}]}`,
     header `Authorization: Bearer <key>`, expect 200 with choices[0].message.content
   - Anthropic: POST https://api.anthropic.com/v1/messages with
     `{"model":"claude-3-5-sonnet-20241022","max_tokens":5,"messages":[{"role":"user","content":"say OK"}]}`,
     headers `x-api-key: <key>` + `anthropic-version: 2023-06-01`, expect 200
   - xAI: POST https://api.x.ai/v1/chat/completions with
     `{"model":"grok-beta","messages":[{"role":"user","content":"say OK"}]}`,
     header `Authorization: Bearer <key>`, expect 200
   - HTTP/generic: GET the URL with `Authorization: Bearer <key>` header,
     expect 2xx (for third-party APIs like weather, stripe, etc.)
4. Reports success/failure with a clear message. NEVER prints the secret value.
5. Uses a short timeout (10s) so unreachable providers fail fast.

## Files to create/edit

### 1. Create `internal/secrets/providertest.go` — provider test adapters

```go
package secrets

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

// ProviderTestResult is the outcome of a credential validation call.
type ProviderTestResult struct {
    Provider  string // "openai", "anthropic", "xiai", "http"
    Endpoint  string // the URL that was called
    Status    string // "ok" or "error"
    Detail    string // human-readable detail (never contains the secret)
    HTTPStatus int   // HTTP status code (0 if request never completed)
}

// TestProvider makes a trivial authenticated call to validate the credential.
// It NEVER returns the secret value in the result.
func TestProvider(ctx context.Context, provider string, secretValue []byte) ProviderTestResult {
    // never log/return secretValue
    testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    switch strings.ToLower(provider) {
    case "openai":
        return testOpenAI(testCtx, secretValue)
    case "anthropic":
        return testAnthropic(testCtx, secretValue)
    case "xiai", "xai":
        return testXAI(testCtx, secretValue)
    case "http", "generic", "":
        return ProviderTestResult{
            Provider: "http",
            Status:   "error",
            Detail:   "generic HTTP provider test requires --url flag (not implemented in this adapter)",
        }
    default:
        return ProviderTestResult{
            Provider: provider,
            Status:   "error",
            Detail:   fmt.Sprintf("unknown provider %q: supported providers are openai, anthropic, xiai", provider),
        }
    }
}
```

Implement `testOpenAI`, `testAnthropic`, `testXAI` as private functions. Each:
1. Builds an http.Request with the right URL, method, headers, JSON body
2. Uses `http.DefaultClient.Do(ctx)` with the timeout context
3. Checks the HTTP status code
4. Returns a ProviderTestResult with Status="ok" or "error" and a Detail
   message that describes the outcome WITHOUT leaking the key
5. On any error (network, non-2xx, JSON parse), returns Status="error" with
   a clear message like "openai returned HTTP 401: invalid api key" or
   "failed to reach api.openai.com: dial tcp: connection refused"

### 2. Edit `internal/cli/control.go` — add the `secret test` subcommand

Add to `newSecretCmd()`:

```go
cmd.AddCommand(&cobra.Command{
    Use:   "test <name>",
    Short: "Validate a credential by making a trivial authenticated call to the provider",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        name := args[0]
        if err := secrets.ValidateSecretName(name); err != nil {
            return err
        }
        provider, _ := cmd.Flags().GetString("provider")
        if provider == "" {
            provider = detectProviderFromName(name)
        }
        store, err := secretStoreFactory(cmd)
        if err != nil {
            return err
        }
        value, err := store.Get(cmd.Context(), name)
        if err != nil {
            return fmt.Errorf("secret %q: %w", name, err)
        }
        result := secrets.TestProvider(cmd.Context(), provider, value)
        // NEVER print the secret value
        if result.Status == "ok" {
            fmt.Fprintf(cmd.OutOrStdout(), "secret %q: %s test OK (%s, HTTP %d)\n", name, result.Provider, result.Endpoint, result.HTTPStatus)
        } else {
            fmt.Fprintf(cmd.OutOrStderr(), "secret %q: %s test FAILED: %s\n", name, result.Provider, result.Detail)
            return fmt.Errorf("credential test failed for %q", name)
        }
        return nil
    },
})
```

Register the `--provider` flag on the test subcommand:
```go
// After creating the test cmd but before AddCommand:
testCmd.Flags().String("provider", "", "credential provider: openai|anthropic|xiai (auto-detected from name if omitted)")
```

Add `detectProviderFromName(name string) string`:
```go
func detectProviderFromName(name string) string {
    lower := strings.ToLower(name)
    switch {
    case strings.Contains(lower, "openai") || strings.Contains(lower, "gpt"):
        return "openai"
    case strings.Contains(lower, "anthropic") || strings.Contains(lower, "claude"):
        return "anthropic"
    case strings.Contains(lower, "xai") || strings.Contains(lower, "grok"):
        return "xiai"
    default:
        return "openai" // default assumption for LLM keys
    }
}
```

### 3. Tests

Create `internal/secrets/providertest_test.go`:

Use `httptest.NewServer` to mock provider endpoints. For each provider:
- Start a test server that checks the Authorization/x-api-key header
- Call `TestProvider` with the test server URL (you'll need to make the
  endpoint URL injectable — add an `endpointOverride` parameter or use a
  package-level var that tests can override)

DESIGN for testability: Add an unexported `var openAIEndpoint = "https://api.openai.com/v1/chat/completions"`
(and similar for anthropic, xiai). Tests override these vars to point at the
httptest server, run the test, restore via t.Cleanup. This avoids needing
to change the TestProvider function signature.

Tests:
1. `TestProviderTestResult_NeverContainsSecret` — call TestProvider with a
   fake provider name, assert the result.Detail does NOT contain the secret
   value string.
2. `TestTestProvider_OpenAI_Success` — mock server returns 200 with valid
   JSON, assert Status="ok".
3. `TestTestProvider_OpenAI_InvalidKey` — mock server returns 401, assert
   Status="error" and Detail mentions "401" but NOT the secret value.
4. `TestTestProvider_Anthropic_Success` — similar for Anthropic (checks
   x-api-key header + anthropic-version header).
5. `TestTestProvider_XAI_Success` — similar for xAI.
6. `TestTestProvider_UnknownProvider` — assert error for unknown provider name.
7. `TestTestProvider_UnreachableProvider` — point at a non-listening port,
   assert Status="error" with a connection-refused style message.

In `internal/cli/cli_test.go`, add:
8. `TestSecretTest_NeverPrintsValue` — store a secret, run `secret test`,
   assert stdout/stderr never contain the value. Use a fake provider by
   overriding the endpoint var to point at a mock server. Assert the command
   either succeeds (mock returns 200) or fails gracefully (mock returns 401)
   but never prints the key.

## Constraints
- The secret value must NEVER appear in: ProviderTestResult.Detail, CLI stdout,
  CLI stderr, error messages, or logs.
- Use `strings.Contains` checks in tests to verify no leakage.
- Do NOT change the SecretStore interface.
- Do NOT add real network calls in unit tests — use httptest.NewServer.
- Run `make test` and `make lint` — both must pass.
- ProviderTestResult must be JSON-serializable (for future plugin use) but the
  secret value is never a field.

## Verification
- `go test ./internal/secrets/... -run TestProvider -v` — all pass
- `go test ./internal/cli/... -run TestSecretTest -v` — passes
- `make test` — all packages pass
- `make lint` — 0 issues
- `go run ./cmd/agentpaas secret test --help` shows usage

## Commit
`feat(cli): add secret test command with provider adapters for pre-deployment validation (B15-T01 MC3)`

Do NOT push.

---

# Worker: B15-T01 MC4 — Wire Hermes plugin secret tools + Python tests

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc4` (create from main).
MC1-MC3 are merged: `agentpaas secret add/list/remove/rotate/test` all work.

## Scope (ONE micro-chunk)
Add 5 plugin tools to the Hermes plugin that wrap the new secret CLI commands:
- `agentpaas_secret_add` — stores a credential (prompts for value via stdin)
- `agentpaas_secret_list` — lists secrets by label, never value
- `agentpaas_secret_remove` — removes a credential
- `agentpaas_secret_rotate` — replaces a credential (prompts for new value)
- `agentpaas_secret_test` — validates a credential before deployment

## Files to edit

### 1. `integrations/hermes-plugin/plugin.yaml` — register new tools

Add to the `provides_tools:` list:
```yaml
  - agentpaas_secret_add
  - agentpaas_secret_list
  - agentpaas_secret_remove
  - agentpaas_secret_rotate
  - agentpaas_secret_test
```

### 2. `integrations/hermes-plugin/tools.py` — implement 5 tool functions

Add these functions at the end of the file, before any `register` call if
there is one (or just at the natural end of the function definitions). Follow
the EXACT pattern of existing tools (see `agentpaas_doctor`, `agentpaas_pack`).

CRITICAL for `secret_add` and `secret_rotate`: they need stdin to pass the
secret value. The existing `_run_cli` uses `subprocess.run(..., capture_output=True)`
which does NOT support stdin. You need a variant that accepts stdin input.

Add a helper `_run_cli_with_stdin(cmd_args, stdin_input)`:
```python
def _run_cli_with_stdin(cmd_args, stdin_input):
    """Run agent CLI with stdin input (for secret add/rotate). Returns same dict as _run_cli."""
    if _needs_daemon(cmd_args):
        sock_available, sock_err = _check_daemon_socket()
        if not sock_available:
            return sock_err
    binary = _resolve_agent_binary()
    full = [binary, "--json"]
    sock = _resolve_socket_path()
    if sock:
        full.extend(["--socket", sock])
    home = os.environ.get("AGENTPAAS_HOME")
    if home:
        full.extend(["--home", home])
    full.extend([a for a in cmd_args if a])
    timeout = _get_cli_timeout()
    proc = subprocess.run(
        full, capture_output=True, text=True, timeout=timeout,
        input=stdin_input,
    )
    # Same result handling as _run_cli — factor out or duplicate the
    # stdout/stderr truncation + JSON parsing + sanitizer logic.
    # For simplicity, you can call _parse_cli_result(proc) if you factor
    # that out, or just duplicate the ~30 lines from _run_cli.
```

IMPORTANT: Factor out the result-parsing logic from `_run_cli` into a
`_parse_cli_result(proc)` helper so both `_run_cli` and `_run_cli_with_stdin`
use the same parsing. Do NOT duplicate the sanitizer/truncation logic.

Tool functions:

```python
def agentpaas_secret_add(args, **kwargs):
    """Store a credential in macOS Keychain. Value passed via 'value' arg."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    value = args.get("value", "")
    if not value:
        return json.dumps({"error": "value is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli_with_stdin(["secret", "add", name], value)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_list(args, **kwargs):
    """List stored credentials by label (never by value)."""
    args = args or {}
    try:
        result = _run_cli(["secret", "list"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_remove(args, **kwargs):
    """Remove a stored credential."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli(["secret", "remove", name])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_rotate(args, **kwargs):
    """Replace a credential with a new value (atomic). New value via 'value' arg."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    value = args.get("value", "")
    if not value:
        return json.dumps({"error": "value is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli_with_stdin(["secret", "rotate", name], value)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_test(args, **kwargs):
    """Validate a credential by making a trivial authenticated call to the provider."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    provider = args.get("provider", "")
    cmd_args = ["secret", "test", name]
    if provider:
        cmd_args.extend(["--provider", provider])
    try:
        result = _run_cli(cmd_args)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})
```

SECURITY: The `value` parameter for `secret_add` and `secret_rotate` is passed
through stdin to the CLI. The secret value must NEVER appear in:
- The JSON result returned by the tool
- Process arguments (argv) — it goes through stdin, NOT argv
- Log output

The `_run_cli_with_stdin` helper must use `input=value` (stdin), NOT append
the value to the command args.

### 3. Tests: `integrations/hermes-plugin/tests/test_secret_tools.py`

Use the `_load_plugin_package()` pattern from `test_plugin_skeleton.py`.
Mock `_run_cli` and `_run_cli_with_stdin` to test each tool function.

Tests:
1. `test_secret_add_calls_cli_with_stdin` — mock `_run_cli_with_stdin`, call
   `agentpaas_secret_add({"name": "mykey", "value": "secret123"})`, assert
   the mock was called with `["secret", "add", "mykey"]` and `"secret123"`.
2. `test_secret_add_requires_name` — call without name, assert error.
3. `test_secret_add_requires_value` — call without value, assert error.
4. `test_secret_add_never_passes_value_in_argv` — mock
   `_run_cli_with_stdin`, verify the value is NOT in the cmd_args list (it
   goes through stdin input, not argv).
5. `test_secret_list_calls_cli` — mock `_run_cli`, call
   `agentpaas_secret_list({})`, assert called with `["secret", "list"]`.
6. `test_secret_remove_calls_cli` — mock `_run_cli`, call
   `agentpaas_secret_remove({"name": "mykey"})`, assert called with
   `["secret", "remove", "mykey"]`.
7. `test_secret_rotate_calls_cli_with_stdin` — mock `_run_cli_with_stdin`,
   call `agentpaas_secret_rotate({"name": "mykey", "value": "newval"})`,
   assert called with `["secret", "rotate", "mykey"]` and `"newval"`.
8. `test_secret_test_calls_cli_with_provider` — mock `_run_cli`, call
   `agentpaas_secret_test({"name": "openai-key", "provider": "openai"})`,
   assert called with `["secret", "test", "openai-key", "--provider", "openai"]`.
9. `test_secret_test_without_provider` — call without provider, assert
   called with just `["secret", "test", "mykey"]` (no --provider flag).
10. `test_secret_tools_registered_in_manifest` — load plugin.yaml, assert
    all 5 tool names are in `provides_tools`.
11. `test_secret_add_result_never_contains_value` — mock
    `_run_cli_with_stdin` to return a result dict, call `agentpaas_secret_add`
    with a value, assert the returned JSON does NOT contain the value string.

## Constraints
- Do NOT modify existing tool functions.
- The `_run_cli_with_stdin` helper must use `subprocess.run(..., input=value)`,
  NOT append the value to cmd_args.
- Follow the EXACT error envelope pattern: `{"error": "...", "error_category": "..."}`
- The secret value must never appear in argv or returned JSON.
- Run the Python plugin tests:
  ```bash
  cd ~/projects/agentpaas
  python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v 2>&1 | tail -20
  ```
  All tests (existing + new) must pass.
- `make lint` (Go) doesn't cover Python — just ensure the Python tests pass.

## Commit
`feat(plugin): wire 5 secret onboarding tools to Hermes plugin (B15-T01 MC4)`

Do NOT push.

---

# Worker: B15-T01 MC5 — Document Keychain service name convention + secret onboarding skill

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc5` (create from main).
MC1-MC4 are merged.

## Scope (ONE micro-chunk)
1. Document the Keychain service name convention in a docs file
2. Create a Hermes plugin skill file that guides users through credential onboarding

## Files to create

### 1. `docs/credential-onboarding.md` — Keychain service name convention

Document:
- The service name is derived from the AGENTPAAS_HOME directory hash:
  `ai.agentpaas.secrets.<sha256(homeDir)[:8]>`
- This means each AGENTPAAS_HOME has its own Keychain namespace
- The `secretServiceName(homeDir)` function in `internal/cli/control.go`
  computes this
- Users can verify their service name with `security find-generic-password -s "ai.agentpaas.secrets.<hash>" -a <secret-name>`
- The `agentpaas secret add/list/remove/rotate/test` commands all use this
  service name automatically
- Secret names must not contain whitespace, control, or invisible format
  characters (enforced by `secrets.ValidateSecretName`)
- Max secret size: 64KB (`secrets.MaxSecretValueSize`)

### 2. `integrations/hermes-plugin/skills/secret-onboarding.md` — Hermes skill

This is a skill file that Hermes loads when the user needs to add credentials.
Format it as a SKILL.md (YAML frontmatter + markdown body).

Content:
```markdown
---
name: agentpaas-secret-onboarding
description: Guide users through adding credentials to AgentPaaS Keychain
version: 1.0.0
---

# AgentPaaS Secret Onboarding

## When to Use
- User needs to add an API key for an agent (OpenAI, Anthropic, xAI, weather, etc.)
- User needs to validate a credential before deployment
- User needs to rotate or remove a credential

## Adding a Credential

1. Ask the user for the credential name (e.g. "openai-api-key")
2. Ask the user for the credential value (API key)
3. Call `agentpaas_secret_add` with name and value
4. Verify it was stored: call `agentpaas_secret_list`

## Validating a Credential (Pre-Deployment)

Before packaging an agent that uses a credential, ALWAYS validate it:

1. Call `agentpaas_secret_test` with the credential name
2. If it fails, tell the user the error and ask them to re-add the credential
3. Only proceed to `agentpaas_pack` after all credentials pass validation

## Rotating a Credential

1. Ask the user for the new value
2. Call `agentpaas_secret_rotate` with name and new value
3. Call `agentpaas_secret_test` to verify the new value works

## Removing a Credential

1. Call `agentpaas_secret_remove` with the name
2. Verify it's gone: call `agentpaas_secret_list`

## Security Rules

- NEVER print or log the secret value
- NEVER pass the secret value as a command-line argument (it goes through stdin)
- The `agentpaas_secret_list` command shows labels only, never values
- If a credential test fails, do NOT show the key in the error message
- Credentials are stored in macOS Keychain, scoped to the AGENTPAAS_HOME namespace

## Credential Naming Convention

Use descriptive names with provider prefix:
- `openai-api-key` — for OpenAI
- `anthropic-api-key` — for Anthropic
- `xai-api-key` — for xAI
- `openweather-api-key` — for OpenWeatherMap
- `stripe-secret-key` — for Stripe

The `secret test` command auto-detects the provider from the name. You can
override with the `provider` parameter: `openai`, `anthropic`, `xiai`.
```

## Constraints
- This is a docs-only chunk. No Go code changes.
- Run `make test` to verify nothing broke (it shouldn't — docs don't affect tests).
- The skill file goes in `integrations/hermes-plugin/skills/`.

## Commit
`docs: document Keychain service name convention + secret onboarding skill (B15-T01 MC5)`

Do NOT push.

---

# Worker: B15-T01 MC6 — Update make block15-gate + integration test, checkpoint

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc6` (create from main).
MC1-MC5 are merged.

## Scope (ONE micro-chunk)
1. Update the `block15-gate` Makefile target — it currently says "manual/docs-only gate" but B15 is now the P1 completion block
2. Update `block16-gate` — it currently says "P1 completion" but B16 is now manual testing
3. Update the gates help text
4. Write the B15-T01 checkpoint doc

## Files to edit

### 1. Makefile — update block15-gate and block16-gate

Current (lines 254-258):
```makefile
block16-gate:
	@echo "Error: block16-gate not yet implemented. See execution plan §16 (BLOCK 16)." && exit 1

block15-gate:
	@echo "Error: block15-gate is a manual/docs-only gate. See execution plan §15.2 use-case matrix." && exit 1
```

Change `block15-gate` to:
```makefile
block15-gate: build lint
	@echo "==> Running Block 15 gate: P1 completion items"
	@echo "  T01: credential onboarding (secret add/list/remove/rotate/test)"
	go test -race -count=1 ./internal/secrets/... ./internal/cli/...
	@echo "  Plugin: secret onboarding tools"
	cd integrations/hermes-plugin && python3 -m unittest discover -s tests -t . -v 2>&1 | tail -5
	@echo "==> Block 15 gate passed (T01-T08 to be added as blocks complete)"
```

Note: For now, the gate only runs T01 (credential onboarding). As T02-T08
are completed in later sessions, their tests will be added here.

Change `block16-gate` to:
```makefile
block16-gate:
	@echo "Error: block16-gate (manual use-case assessment) runs after block15-gate passes. See execution plan §16." && exit 1
```

Update the gates help text (lines 281-282):
```
	@echo "  block15-gate - P1 completion: LLM, credentials, policy, hardening, release"
	@echo "  block16-gate - Manual use-case assessment (runs AFTER B15)"
```

### 2. Write `docs/b15-checkpoint-01.md`

```markdown
# B15 Session Checkpoint — 01

**Date:** 2026-06-30
**Branch:** main
**Goal:** 15-T01 Credential Onboarding — complete all 5 secret CLI commands + plugin tools + docs

## Completed This Session
- MC1: secret add/remove with aliases (commit 0633252) — renamed set→add, rm→remove
- MC2: secret rotate command (commit 0212fe0) — atomic credential replacement via stdin
- MC3: secret test command + provider adapters (commit 4c72682) — OpenAI/Anthropic/xAI validation
- MC5: credential onboarding docs + Hermes skill (commit b09ea0a)
- MC4: Hermes plugin 5 secret tools (pending merge)
- MC6: block15-gate Makefile update + this checkpoint

## Key Facts
- Provider adapters use package-level endpoint vars (openAIEndpoint, anthropicEndpoint, xaiEndpoint)
  with SetTestEndpoints() for test override
- secret test auto-detects provider from name: "openai"/"gpt"→openai, "anthropic"/"claude"→anthropic, "xai"/"grok"→xiai
- _run_cli_with_stdin helper added to plugin tools.py for stdin-based secret value passing
- Secret values NEVER appear in: CLI stdout/stderr, audit trail, container env, process args

## Next Session Start
- Immediate next action: start 15-T02 (LLM provider integration via unified gateway egress)
- File to read first: agentpaas-execution-plan-v1.md, search "15-T02"
- Block: B15, Subtask: T02, Micro-chunk: LLM provider adapter + agent.llm() sugar

## 15-T01 Verification Status
1. `agentpaas secret add <name>` — DONE, stores in Keychain
2. `agentpaas secret list` — DONE, shows labels only, never values
3. `agentpaas secret test <name>` — DONE, validates via provider HTTP call
4. `agentpaas secret rotate <name>` — DONE, atomic replace
5. `agentpaas secret remove <name>` — DONE, deletes from Keychain
6. Secret value never in container env, logs, audit, CLI output — VERIFIED by tests
7. Unit tests for all 5 commands + provider adapter tests — DONE (21+ Go tests, 11 Python tests)
8. Hermes plugin: 5 secret tools — DONE (MC4)
```

## Constraints
- Do NOT change any Go source files.
- Run `make block15-gate` after updating the Makefile — it should pass.
- Run `make test` and `make lint` — both must still pass.

## Commit
`chore: update block15-gate for T01 + write B15-T01 checkpoint (B15-T01 MC6)`

Do NOT push.

---
