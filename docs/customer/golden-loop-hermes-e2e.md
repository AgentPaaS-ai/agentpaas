# Hermes + Cloud golden loop (E2E stranger bar)

**Pinned to:** brew CLI **0.3.6** (rebuild same tag OK; do not bump version for this cut)
**Kind:** Hermes-driven first-time user path (NL + tools). Separate from the
terminal-only `docs/customer/golden-path.md`.
**Purpose:** From a clean Hermes test profile → install AgentPaaS → build weather
agent → local pack/invoke → **policy deny then allow** → cloud pack/push/deploy/
bind/invoke/result. Disk-verify every phase. Orch never fabricates.

## Roles

| Who | Job |
|-----|-----|
| **ap-testing** (or dedicated `agentpaas-testing` profile) | Acts as first-time user via Hermes |
| **ap-orchestrate** | Preflight, brief paste, disk verify, evidence file |

## What this is NOT

- Not the old v0.2.3 local-only golden-loop (export/friend install) — that remains
  `docs/execution/golden-loop-test.md` for share/receive.
- Not nuclear teardown of Colima/Docker on the founder laptop unless founder
  explicitly approves. Default = **Hermes profile teardown only**.

---

## Phase 0 — Orch preflight (before any Hermes session)

```bash
export PATH="/opt/homebrew/bin:$PATH"
export AGENTPAAS_CLOUD_API_URL='https://agentpaas-cloud-api.parvezsyed.workers.dev'
agentpaas version    # CLI: 0.3.6
agentpaas doctor     # 7/7
# Cloud live:
agentpaas cloud whoami
```

Evidence: paste into `docs/owa-records/golden-hermes-e2e-YYYYMMDD.md`.

---

## Phase 1 — Hermes profile teardown (clean user)

Prefer profile-local wipe (does **not** uninstall brew/colima):

```bash
PROFILE="${HERMES_GOLDEN_PROFILE:-agentpaas-testing}"
# Stop any session using the profile first.
rm -rf "$HOME/.hermes/profiles/$PROFILE/plugins/agentpaas" \
       "$HOME/.hermes/profiles/$PROFILE/skills/agentpaas" \
       "$HOME/.hermes/profiles/$PROFILE/skills/agentpaas-build" 2>/dev/null || true
# Optional: strip toolset entry (manual edit of config.yaml platform_toolsets)
# Soft-reset AgentPaaS home for this test (keeps brew):
#   mv ~/.agentpaas ~/.agentpaas.bak-golden-$$
#   mkdir -p ~/.agentpaas
```

**Verify:** no plugin dir; `hermes -p $PROFILE` starts fresh.

Full machine nuclear (brew/colima wipe) only with founder OK — script path in
legacy `golden-loop-test.md` Phase 1.

---

## Phase 2 — Install AgentPaaS (via Hermes)

```bash
hermes -p agentpaas-testing
```

Paste **one** message:

```text
Install from https://github.com/AgentPaaS-ai/agentpaas
```

**Orch verify:**
- `which agentpaas` → brew path
- `agentpaas version` contains `0.3.6`
- doctor 7/7
- plugin present under profile

No restart needed — toolset registers live (verified 2026-08-03). If tools are missing, restart once (`/quit` then reopen).

---

## Phase 3 — Identity + secret (user terminal; Hermes coaches)

Hermes must **not** ask for the API key value in chat.

```text
Set up publisher identity and OpenRouter credential for a weather agent.
Coach me: identity init name golden-user; secret add openrouter-key in MY terminal only; then secret list to confirm label only.
```

**Orch verify:** `agentpaas identity show`; `agentpaas secret list` shows `openrouter-key`.

---

## Phase 4 — Build weather agent (local)

```text
Build a weather agent at ~/golden-weather-agent that:
- takes query/city input
- fetches weather from wttr.in via agent.http
- summarizes with agent.llm (OpenRouter)
- uses @agent.on_invoke and returns final_output + answer

Follow agentpaas-build skill: ask provider/model only if needed (openrouter + deepseek/deepseek-v4-flash defaults OK for this test).
Egress hosts (confirm with me): wttr.in, openrouter.ai — no ports in the ask.
Pack linux/amd64 for later cloud. Then run and invoke locally with Folsom.
```

**Orch verify (disk):**
```bash
RUN=$(ls -t ~/.agentpaas/state/runs | head -1)
cat ~/.agentpaas/state/runs/$RUN/invoke-response.json   # status OK / final_output
rg 'egress_allowed|egress_denied' ~/.agentpaas/state/runs/$RUN/harness-audit.jsonl | tail
```

---

## Phase 5 — Egress fail then pass (policy teaching moment)

```text
Demonstrate governance:
1) Remove wttr.in from policy.yaml (keep openrouter.ai).
2) Repack and invoke Folsom — expect policy denial / failed weather fetch. Show explain-denial if available.
3) Add wttr.in back, confirm hostname with me, repack, invoke again — expect success.
```

**Orch verify:**
- Fail run: audit has denial or ERROR with clear reason
- Pass run: invoke-response OK + wttr.in allowed

---

## Phase 6 — Cloud path (Hermes drives CLI)

Hermes runs (or coaches exact commands):

```text
Send this agent to AgentPaaS Cloud and prove invoke there.

Required env (every shell):
  export AGENTPAAS_CLOUD_API_URL='https://agentpaas-cloud-api.parvezsyed.workers.dev'
  export CLOUDFLARE_API_TOKEN from Keychain label agentpaas-cloudflare-api-token (user pastes; never print)

Steps:
1) Open the tenant claim link in a browser, then run `cloud login`; fallback for CI/scripts: `cloud login --token-stdin` with the provider-issued token
2) cloud whoami
3) pack already done — print Lock path from pack output
4) cloud push --lock <absolute lock>
5) cloud deploy latest   # or --lock
6) cloud secrets push openrouter-key
7) cloud secrets bind <dep_id> openrouter-key --as bearer --host openrouter.ai
8) cloud invoke-token <dep_id>
9) cloud invoke <dep_id> --body '{"query":"weather in Folsom"}'
10) cloud result <run_id>
11) cloud logs <run_id>
12) cloud usage
13) cloud undeploy <dep_id> — prove slot freed; then cloud deploy latest again and confirm it succeeds (free-capacity loop)
Report every Run ID and final_output. On 503 print full error body.
```

**Orch verify:**
- deploy status ready
- bind listed via `cloud secrets bindings <dep>`
- run terminal **succeeded** with real weather text
- artifact URL optional

---

## Phase 7 — Awe checklist (first-time user surface)

Hermes should have exercised without founder debug:

| Surface | Seen |
|---------|------|
| doctor 7/7 | |
| pack Lock path printed | |
| local invoke OK | |
| policy deny then allow | |
| cloud whoami | |
| push admitted | |
| deploy latest | |
| secrets push + bind | |
| invoke + result final_output | |
| usage | |
| honest error on misconfig (if any) single line | |

---

## Phase 8 — Evidence

Write `docs/owa-records/golden-hermes-e2e-YYYYMMDD.md` with:
- profile name, brew version/commit
- local run_id + invoke-response excerpt
- deny run_id + allow run_id
- cloud dep_id, run_id, result excerpt
- GO / NO-GO

---

## Failure map

| Symptom | Fix |
|---------|-----|
| whoami empty | AGENTPAAS_CLOUD_API_URL |
| deploy cf_bind_not_configured | Worker secrets CF_* trio |
| secrets_misconfigured | SECRETS_MASTER_KEY |
| llm credential not declared | secrets bind on deployment |
| container exit 1 | amd64 harness + instance_type dev |
| plugin missing tools | reinstall plugin + restart Hermes |

## Related

- Terminal short path: `docs/customer/golden-path.md`
- Legacy local share loop: `docs/execution/golden-loop-test.md`
- Pre-M8 residuals: `docs/owa-records/m7.5-pre-m8-residuals.md`
