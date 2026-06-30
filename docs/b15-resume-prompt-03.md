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
