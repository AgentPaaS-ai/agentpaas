Continue AgentPaaS Block 14A → Security Remediation.

CONTEXT (load these first):
- Load skills: agentpaas-build-rhythm, agentpaas-owa-build-orchestration
- Read checkpoint: docs/b14a0-checkpoint-1.md
- Read execution plan: agentpaas-execution-plan-v1.md search "14A" (NOT 14A0 — the next sub-segment)
- Read B13 risk analysis: docs/b13-risk-analysis.md (for context on what 14A fixes)

STATE:
- Repo: ~/projects/agentpaas, on main, 12 commits ahead of origin/main (not pushed)
- Last commit: 94cb57f docs: B14A0 checkpoint 1
- Block 14A0 COMPLETE — all 5 tasks merged, gate passed, verifier passed

14A0 SUMMARY (all done):
- T01 (run status tracking): commit 036d9e5
- T02 (orphan reconciliation): commit 9c64111 + 0f9bdab (adversary hardening)
- T03 (invoke/Stop sync): commit 036d9e5
- T04 (Docker e2e test): commit 240f8f6
- T05 (rename stubControlServer → controlServer): commit 8b41770

BLOCK 14A0 VERIFIER: PASSED
- 131 daemon tests pass, 0 lint issues, e2e Docker test passes (7.5s)
- Cross-subtask integration verified: T01 Status + T03 InvokeDone + T02 reconciliation interact correctly
- 0 stubControlServer references remaining in *.go files
- doc.go has no stale "not yet implemented" for implemented commands

OWA MODEL ALLOCATION:
- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API. Plans, dispatches, reviews, merges. Does NOT edit code.
- Worker: grok-composer-2.5-fast via Grok CLI ($0). Dispatch on specific tasks. Use print mode (-p) for one-shot.
- Adversary: grok-4.3 via agentpaas-adversary profile. Security/correctness review.
- Verifier: GLM-5.2 via agentpaas-verifier profile, block-end only.

CRITICAL FROM THIS SESSION:
1. The B13 risk analysis was stale about doc.go — commands were already marked as implemented on main. T05 only needed the type rename, not doc.go changes.
2. Adversary race condition findings for T02 were false positives — gRPC Serve(ln) starts AFTER reconciliation in server.go:311, so no RPCs can arrive during reconcile.
3. The T04 e2e test uses in-process controlServer methods (not CLI subprocesses) — simpler, faster (7.5s), and tests the real Docker flow.
4. T02 adversary hardening added managed-by label filter to prevent container label spoofing — any container with just `agentpaas.resource-type=agent` label is no longer touched.
5. All 14A0 work is on local main only — not pushed to GitHub yet.

NEXT STEPS (Block 14A — Security Remediation):
1. Push 14A0 work to GitHub and close issues #154-#158
2. Read the execution plan for 14A-T01 through 14A-T08 (8 security gaps from B13 audit)
3. Start with 14A-T01 (plugin path allow-list) — dispatch Grok worker
4. Each 14A task needs adversary review (security-sensitive)

GROK WORKER DISPATCH PATTERN (print mode, preferred):
```bash
grok --no-auto-update -m grok-composer-2.5-fast \
  -p "$(cat /tmp/b14a-tNN-prompt.md)" \
  --always-approve --no-memory --max-turns 50 \
  --cwd /Users/pms88/projects/agentpaas
```

ADVERSARY DISPATCH PATTERN:
```bash
cat > /tmp/run-adv-b14a-tNN.sh <<'SCRIPT'
PROMPT="$(cat /tmp/b14a-tNN-adversary-prompt.md)"
cd /Users/pms88/projects/agentpaas
hermes -p agentpaas-adversary chat -q "$PROMPT" -Q --toolsets terminal,file,search
SCRIPT
chmod +x /tmp/run-adv-b14a-tNN.sh
tmux new-session -d -s adv-b14a-tNN -x 200 -y 50 "/tmp/run-adv-b14a-tNN.sh"
```

Start at: Push 14A0 to GitHub, close issues #154-#158, then read 14A spec and dispatch first worker.
