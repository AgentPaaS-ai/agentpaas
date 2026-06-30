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
