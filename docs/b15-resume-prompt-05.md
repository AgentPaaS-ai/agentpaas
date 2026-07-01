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
