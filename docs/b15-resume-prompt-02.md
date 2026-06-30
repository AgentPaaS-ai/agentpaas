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