Continue AgentPaaS Block 13 build. T01 is done and merged; pick up at B13-T02 and run the OWA loop through T02-T09 + block13-gate.

CONTEXT (load these first):
- Load skills: agentpaas-owa-build-orchestration, owa-multi-agent-coding, cost-aware-model-selection
- Read docs/owa-records/b13-t01.md (what T01 did + adversary breaks found)
- Read the B13 block in agentpaas-execution-plan-v1.md (search "BLOCK 13") — it was updated THIS session to lock the plugin spec
- Read Block 13 subtasks (B13-T01..T09) in docs/agentpaas-subtask-decomposition-v1.md

STATE:
- Repo: ~/projects/agentpaas, on main, T01 merged (commit 0dd8322)
- Plan changes committed (e07f643): single Hermes plugin, /agentpaas slash commands, ctx.dispatch_tool, requires_env, NO MCP (P2). T08 (slash cmds) + T09 (SKILL.md) added.
- T01 delivered: integrations/hermes-plugin/ with plugin.yaml, __init__.py (register(ctx)), schemas.py (18 tools), tools.py (shell out to CLI), tests/ — 19/19 tests pass.

OWA MODEL ALLOCATION (confirmed via OWA records B7-B12 + profiles):
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API. Plans, dispatches, reviews, merges. Does NOT edit code.
- Worker: grok-composer-2.5-fast via Grok CLI ($0). Dispatch on git worktree via tmux. ~2-5 min/subtask.
- Adversary: grok-4.3 via `hermes -p agentpaas-adversary chat` ($0). terminal+file only.
- Verifier: GLM-5.2 via `hermes -p agentpaas-verifier chat`, ONCE at block-end.
- Local-first mode: no GitHub issues/PRs mid-build. Merge locally. Checkpoint push at block end.

CRITICAL DISCOVERIES THIS SESSION (don't re-derive):
1. B13 is the FIRST PYTHON BLOCK. The Go-centric OWA scripts (local-gate.sh, codex-worker-local.sh) DON'T apply. Tests run via `python3 -m unittest discover -s integrations/hermes-plugin/tests`. No pyproject/pytest in repo — use plain unittest, matching python/agentpaas_sdk/tests/ pattern.
2. BINARY COLLISION: `agent` on PATH is Grok's binary, NOT AgentPaaS. The plugin resolves the AgentPaaS CLI via: AGENTPAAS_CLI env → `which agentpaas` → repo dev bin/agentpaas → last-resort. AgentPaaS CLI is `go build -o bin/agentpaas ./cmd/agent`.
3. AgentPaaS CLI exists with all 17 B11 operator methods + --json flag. Run `./bin/agentpaas --help` to see them.
4. KNOWN GAP: `validate` CLI subcommand does NOT support --json yet (B11 gap). Track for T02 contract-parity gate.
5. Worker dispatch pattern that works: tmux new-session -d, write prompt to /tmp file, `grok --no-auto-update -m grok-composer-2.5-fast -p "$(cat /tmp/prompt)" --always-approve --cwd <worktree>`. Always export PATH with /opt/homebrew/bin:$HOME/.local/bin in the tmux wrapper.

REMAINING SUBTASKS (in order):
- B13-T02: Schema-generated tool wrappers, contract-parity gate (CI fails if operator method lacks wrapper or drops evidence refs). Build from internal/operator/schema.go (the B11 contracts — EvidenceRef, RedactedExcerpt, ConfirmationRequirement, NextAction enums all defined there).
- B13-T03: Confirmation protocol — trust-boundary actions return requires_confirmation/confirmation_id/risk_level. B11's ConfirmationRequirement struct already defines these fields.
- B13-T04: Prompt-injection boundary — separate trusted control fields (status, error_category, next_action, confirmation) from untrusted evidence (excerpts, logs, traces). Write negative tests.
- B13-T05: agentpaas-deploy e2e flow (detect → init → validate → pack → run → dashboard DENIED probe).
- B13-T06: prompt-change immutable redeploy (edit project → validate → pack → verify → run, distinct digests).
- B13-T07: demo matrix fixtures (3 demos: weather agent, secret-brokered SaaS, agentic repair loop).
- B13-T08: /agentpaas slash commands (deploy/status/logs/metrics/repair) via ctx.register_command, each a thin orchestrator over ctx.dispatch_tool.
- B13-T09: bundled SKILL.md via ctx.register_skill + plugin.yaml requires_env.
Then: implement `make block13-gate` in Makefile (currently a stub that exits 1), run block-end verifier, write docs/owa-records/b13-block-end.md.

OWA LOOP PER SUBTASK (the established rhythm):
1. git worktree add -b feat/b<N>-t<NN> /tmp/agentpaas-b<N>-t<NN> main
2. Write worker prompt to /tmp, dispatch Grok via tmux
3. Run tests yourself (don't trust worker self-report)
4. Dispatch adversary (grok-4.3) on same worktree
5. If breaks → fix worker (Grok, same branch) → re-test
6. git merge --no-ff to main, write docs/owa-records/b<N>-t<NN>.md
7. git worktree remove --force + branch -D

Start at B13-T02.
