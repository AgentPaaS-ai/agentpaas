# Prompt: Full automated E2E regression (v0.2.3 → B33)

**Use:** Start a **new `ap-orchestrate` session** and paste everything under
“ORCHESTRATOR PROMPT” (or: `hermes -p ap-orchestrate` then
`Read and execute docs/execution/e2e-regression-prompt-v023-through-b33.md`).

**Roles:**
| Profile | Job |
|---------|-----|
| **ap-orchestrate** (you) | Plan, dispatch, verify on disk, triage, commit OWA record. Never fabricate. |
| **ap-testing** | Automated operator / first-time-user simulation (Hermes + agentpaas plugin path). |
| **ap-worker** | Code fixes only when product bugs are found. |

**Do not start B34.** Goal is regression evidence, not new features.

---

## ORCHESTRATOR PROMPT

```
You are ap-orchestrate for AgentPaaS. Run a FULL automated end-to-end regression
from v0.2.3 compatibility through current main (B33 complete). Do NOT start B34.

## Authority files (read first)
- /Users/pms88/projects/agentpaas/docs/execution/current-state.md
- /Users/pms88/projects/agentpaas/docs/execution/e2e-regression-prompt-v023-through-b33.md  (this file)
- /Users/pms88/projects/agentpaas/docs/execution/golden-loop-test.md  (operator lifecycle intent; automate via ap-testing)
- Makefile: block33-gate, block33-gate-fast, golden-*, restart-local-daemon, mcp-container-e2e

## Profiles
- You: ap-orchestrate — plan, dispatch, disk-verify, OWA record, commits for docs/fixes via worker.
- Operator simulation: hermes -p ap-testing -z "<self-contained brief>"
  (one-shot, non-interactive; brief must include every path, command, and acceptance check).
- Code fixes: hermes -p ap-worker -z "<brief>" only when product code is wrong.
- Prefer short ap-testing / ap-worker chunks (≤10–15 min wall). Do not use -Q on worker.

## Hard rules
1. CI is LOCAL ONLY. Never wait on GitHub Actions (no runners).
2. Repo: /Users/pms88/projects/agentpaas on main (or note SHA under test).
3. After every make build / build-all: make restart-local-daemon.
   PATH must prefer $(pwd)/bin over brew: export PATH="$(pwd)/bin:$PATH"
   Stale agentpaasd after bin overwrite → G47 / lock_provenance false FAIL.
4. NEVER fabricate agentpaas_run / invoke / golden / gate output. Read real files.
5. Fix product bugs via ap-worker; do not weaken tests to pass.
6. Colima: bind-mount only /Users paths (never /var/folders for container binds).
7. Nuclear teardown only at Phase A start (and optional end). Script:
   bash ~/.hermes/profiles/ap-testing/skills/devops/agentpaas-manual-testing-setup/scripts/nuclear-teardown.sh
   If missing, locate: find ~/.hermes -name nuclear-teardown.sh
   Teardown does NOT delete project repos unless listed — confirm script before run.
8. Record everything in:
   docs/owa-records/e2e-regression-YYYYMMDD.md
   (phases, commands, exit codes, SHAs, bugs, verdict).

## Success criteria (all required)
1. Phase A clean slate + local build daemon healthy
2. go test ./test/compat/v0.2.3/... -count=1 -race  PASS
3. make block33-gate PASS
   (long: block32→31→… then B33. If split for time, must still complete full chain with evidence)
4. make golden-fast PASS (all tasks, G47 lock_provenance PASS)
5. make mcp-container-e2e PASS (or equivalent inside block33-gate)
6. ap-testing automated operator loop PASS (Phase E) with disk evidence
7. OWA regression record written; verdict GREEN or RED with blockers

Optional stretch (run if time; record SKIP if not):
- make golden-slow
- make golden-docker
- make golden-eval

---

## Phase A — Clean slate + toolchain (orchestrator)

1. cd /Users/pms88/projects/agentpaas
2. git status -sb && git log --oneline -8 && git rev-parse HEAD
3. Nuclear teardown (ap-testing skill script) — start from peace.
4. make build-all && make restart-local-daemon
5. export PATH="$(pwd)/bin:$PATH"
6. which agentpaas agentpaasd → must be repo bin/
7. pgrep -lf agentpaasd → must be repo bin/agentpaasd
8. agentpaas doctor → report real checks
9. docker info → must work (Colima)

On failure: stop and fix env before continuing.

---

## Phase B — v0.2.3 compatibility (orchestrator)

```bash
cd /Users/pms88/projects/agentpaas
export PATH="$(pwd)/bin:$PATH"
go test ./test/compat/v0.2.3/... -count=1 -race
go test ./test/compat/... -count=1 -race 2>/dev/null || true
```

PASS required for GREEN.

---

## Phase C — Full block gate chain through B33 (orchestrator)

```bash
export PATH="$(pwd)/bin:$PATH"
make block33-gate
```

This re-runs historical gates (incl. block31/32) then B33:
mcp packages race, python SDK, mcp-container-e2e, adversary matrix, vet, lint,
govulncheck, golden-fast (with restart-local-daemon).

If wall-clock forces a split, run in order and checkpoint after each:
  make block31-gate
  make block32-gate
  make block33-gate-fast
Then still attempt full make block33-gate once before final verdict.

govulncheck: report pre-existing dep vulns honestly; do not hide. Only fail
overall GREEN if newly introduced by fixes in this session without justification.

---

## Phase D — Explicit MCP / golden proof (orchestrator)

Even if Phase C already ran these, re-run once on final SHA for the OWA record:

```bash
export PATH="$(pwd)/bin:$PATH"
make restart-local-daemon
make mcp-admission-conformance
AGENTPAAS_DOCKER_TESTS=1 make mcp-container-e2e
make golden-fast
go test ./internal/mcpmanager/... ./internal/harness/... -count=1 -race \
  -run 'TestAdversary_B33|TestAdversaryT07|TestMCP_NoRouter|TestMCP_Managed'
```

---

## Phase E — Automated operator path via ap-testing

Orchestrator dispatches ap-testing with a SELF-CONTAINED brief (no session memory).
ap-testing acts as a careful first-time / returning operator. Orchestrator does
NOT trust the summary alone — verify every claim on disk after the run.

### E0 — Dispatch pattern

```bash
hermes -p ap-testing -z "$(cat <<'BRIEF'
...brief body...
BRIEF
)"
```

Use workdir under /Users/pms88/… only (e.g. /Users/pms88/projects/ap-e2e-op-<date>).
Keep each brief to one phase (E1, E2, …). After each: orch verifies, then next.

### E1 — Doctor + identity + secrets (no pack yet)

Brief must tell ap-testing to:
- export PATH="/Users/pms88/projects/agentpaas/bin:$PATH"
- agentpaas doctor
- agentpaas identity show (or init only if missing — do not silent-reinit if present)
- agentpaas secret list
- If openrouter-key missing: STOP and tell orch (user must `agentpaas secret add openrouter-key` in their terminal — NEVER pass key values through tools)
- Write a short report file: /Users/pms88/projects/ap-e2e-op-<date>/e1-report.md

Orch verifies: doctor output, secret list labels (no values), report file exists.

### E2 — Weather agent pack / export / inspect (Hermes-assisted build OK)

Brief:
- Work in /Users/pms88/projects/ap-e2e-op-<date>/weather-agent
- May copy from /Users/pms88/projects/agentpaas/demo/weather-agent OR scaffold via plugin skills
- Ensure agent.yaml + policy.yaml valid; LLM cred name must match a listed secret
- Egress must list weather host + LLM provider host (no wildcard unless already in fixture)
- agentpaas pack
- agentpaas export --output weather-agent.agentpaas --yes
- agentpaas bundle inspect weather-agent.agentpaas
  → must show lock_provenance PASS
- Write e2-report.md with digests and inspect excerpt

Orch verifies: bundle file exists, inspect log on disk contains lock_provenance PASS,
pack image digest recorded.

### E3 — Run + invoke (real invoke_response)

Brief:
- agentpaas run <project or digest> as appropriate for current CLI
- agentpaas_trigger_invoke or CLI invoke with a real city payload
- Wait for completion; agentpaas status; read invoke_response / result files
- On ERROR: report honestly — do not invent weather numbers
- Write e3-report.md with run_id, status, path to invoke_response

Orch verifies:
- run_id exists
- result status from real files (not model prose)
- harness-audit / evidence paths if present

### E4 — MCP operator proof (optional if E2/E3 long; required for full GREEN)

Prefer library+docker already in mcp-container-e2e. For operator surface:
- Point ap-testing at test/e2e/fixtures/mcp-feedback-service + client
  under repo, or re-run:
  AGENTPAAS_DOCKER_TESTS=1 go test ./internal/mcpmanager/ -count=1 \
    -run TestE2E_OperatorPack_MCPFeedbackService -v
- Orch may run this itself if faster; still record in OWA.

### E5 — Negative / fail-closed spot checks (ap-testing or orch)

- Undeclared tool or pack with bad policy → must fail closed
- bundle inspect on tampered copy → must FAIL (orch can do)
- Record outcomes

---

## Phase F — Fixes

When a phase fails:
1. Classify: env / stale daemon / product bug / test bug / known limitation
2. Stale daemon → restart-local-daemon, re-run (no code change)
3. Product bug → ap-worker brief with file:line, evidence, fix; commit; re-run phase
4. Do not delete failing tests
5. Append bug list to OWA record (link docs/owa-records/BUG-* if created)

---

## Phase G — Close out

1. Final SHA: git rev-parse HEAD
2. Write docs/owa-records/e2e-regression-YYYYMMDD.md including:
   - start/end time, host notes
   - phase table PASS/FAIL
   - commands + key log tails
   - bugs fixed (SHAs) and open
   - verdict: REGRESSION GREEN | REGRESSION RED
3. Commit the OWA record (orch may write docs; code via worker only)
4. Ping user only when complete or hard-blocked

## Anti-patterns
- Do not use brew agentpaas while testing repo main without noting version skew
- Do not leave pack to a daemon started before the last build-all
- Do not skip nuclear teardown then claim clean-slate
- Do not mark invoke PASS from model narration without invoke_response on disk
- Do not start B34 feature work in this session

Start Phase A now.
```

---

## How you start the session

```bash
cd /Users/pms88/projects/agentpaas
hermes -p ap-orchestrate
```

Then paste:

```
Execute docs/execution/e2e-regression-prompt-v023-through-b33.md fully.
Operator simulation must use hermes -p ap-testing. Code fixes via ap-worker.
Write docs/owa-records/e2e-regression-$(date +%Y%m%d).md. No B34.
```

Or one-shot (if you prefer non-interactive orch — only if your orch profile supports long -z runs):

```bash
hermes -p ap-orchestrate -z 'Execute docs/execution/e2e-regression-prompt-v023-through-b33.md fully. Operator path via hermes -p ap-testing. Fixes via ap-worker. OWA record required. No B34.'
```

Interactive orch is recommended for a multi-hour gate.

---

## Quick map (orchestrator vs ap-testing)

| Phase | Who runs it |
|-------|-------------|
| A Clean slate, build, doctor | orch |
| B v0.2.3 compat tests | orch |
| C make block33-gate | orch |
| D mcp-container-e2e + golden-fast | orch |
| E Operator pack/run/invoke | **ap-testing** (orch verifies disk) |
| F Fixes | ap-worker |
| G OWA record | orch |

---

## Related paths

- Nuclear teardown: `~/.hermes/profiles/ap-testing/skills/devops/agentpaas-manual-testing-setup/scripts/nuclear-teardown.sh`
- Golden loop (manual ancestor): `docs/execution/golden-loop-test.md`
- MCP fixtures: `test/e2e/fixtures/mcp-feedback-service`, `mcp-feedback-client`
- Current head notes: `docs/execution/current-state.md`
