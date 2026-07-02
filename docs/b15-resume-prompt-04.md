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
