# E2E Regression Record — 2026-07-24

**Prompt:** `docs/execution/e2e-regression-prompt-v023-through-b33.md`  
**Repo:** `/Users/pms88/projects/agentpaas`  
**Start SHA:** `5d90a633a4c7b382a4af5afeafebe048e541d6ba`  
**End SHA:** `0ad65429c866b578e21c67e2f026682fb6ce3ac0`  
**Host:** macOS darwin/arm64, Colima Docker 29.5.2 / CLI 29.6.2  
**CLI under test:** `0.3.0-dev` from `$(repo)/bin` (not brew)  
**Publisher identity:** `e2e-regression` fingerprint `7f2bc25aa3774b90351b61dd16cc9635143768b19b8359beb1b7676ef716b259`

## Verdict

# REGRESSION GREEN

All required success criteria met with disk evidence. Two product/test fixes landed during the run (characterization docs + plugin unit expectations). Pre-existing govulncheck module advisories recorded (not introduced this session). Optional golden-slow/docker/eval: SKIP.

## Phase table

| Phase | Status | Evidence |
|-------|--------|----------|
| A Clean slate + build + doctor | **PASS** | nuclear teardown ✓; colima/docker restored; `make build-all`; doctor 7/7; PATH=`repo/bin` |
| B v0.2.3 compat | **PASS** (after E2E-001) | `go test ./test/compat/v0.2.3/... -count=1 -race` ok |
| C Gate chain through B33 | **PASS** (split) | block30 PASS; block31 PASS; block32 PASS; **block33-gate-fast PASS**; initial full `block33-gate` aborted at first golden without identity |
| D MCP + golden-fast explicit | **PASS** | mcp-admission EXIT0; mcp-container-e2e EXIT0 (~200s); golden-fast 23/23; adversary matrix ok; govulncheck exit0 w/ known deps |
| E Operator (ap-testing) | **PASS** | E1–E5 under `/Users/pms88/projects/ap-e2e-op-20260724` |
| F Fixes | **DONE** | 869c96e, 0ad6542 |
| G OWA record | **THIS FILE** | |

## Phase A detail

1. Nuclear teardown (`~/.hermes/profiles/ap-testing/.../nuclear-teardown.sh`): clean slate verified (brew agentpaas/colima/docker removed; `~/.agentpaas` gone; keychain agentpaas entries purged).
2. Restored toolchain: `brew install colima docker lima`; `colima start --cpu 4 --memory 8`.
3. `make build-all` + `make restart-local-daemon` → agentpaasd from repo bin.
4. `agentpaas doctor`: **7/7** passed.
5. `docker info`: Colima context OK (fresh VM, 0 images initially).

## Phase B detail

Initial FAIL:
- `TestB30T01_HarnessInvokeTimeoutDefault5Min_Failing`
- `TestB30T03PartB_NoUnauthorizedFixedDurablePathTimeout`

Root cause: `cmd/harness/main.go` missing same-line `legacy/compat` docs and `invokeTimeoutForPayload` mention. Runtime wiring already correct in `internal/harness` (unit tests green).

**Fix:** `869c96e fix(harness): restore legacy/compat timeout docs for B30 v0.2.3 characterization`  
Re-run: `ok github.com/AgentPaaS-ai/agentpaas/test/compat/v0.2.3`

## Phase C detail

1. First `make block33-gate` failed inside **block30 golden-fast G47**: `no publisher identity` (expected after nuclear teardown).
2. `agentpaas identity init --name e2e-regression` + `make restart-local-daemon`.
3. `make golden-fast` → **23/23 PASS** (G47 lock_provenance PASS).
4. `make block30-gate` → **PASS** (EXIT 0).
5. Sequential: `block31-gate` → **PASS**; `block32-gate` → **PASS**; `block33-gate-fast` → **PASS**.
6. Logs:
   - `docs/owa-records/_e2e-block30-gate-20260724.log`
   - `docs/owa-records/_e2e-block31-gate-20260724.log` (ends `Block 31 gate: PASS`)
   - `docs/owa-records/_e2e-block32-gate-20260724.log` (ends `Block 32 gate: PASS`)
   - `docs/owa-records/_e2e-block33-gate-fast-20260724.log` (ends `Block 33 gate FAST: PASS`)

Note: `block27-gate` T08 historically piped plugin tests through `tail -5` and did not fail make on unittest non-zero (gate hygiene smell). Plugin failures caught by direct re-run → E2E-002.

## Phase D detail (final SHA `0ad6542`)

| Check | Result |
|-------|--------|
| `make mcp-admission-conformance` | PASS |
| `AGENTPAAS_DOCKER_TESTS=1 make mcp-container-e2e` | PASS (~200s race) |
| `make golden-fast` | **23/23 PASS**, G47 PASS |
| Adversary `-run 'TestAdversary_B33\|TestAdversaryT07\|TestMCP_NoRouter\|TestMCP_Managed'` | PASS |
| `govulncheck ./...` | exit 0; **5 vulns / 2 modules** pre-existing (docker client / stdcopy call graph) — not introduced this session |

Log: `docs/owa-records/_e2e-phase-d-20260724.log`

## Phase E — ap-testing operator path

Workdir: `/Users/pms88/projects/ap-e2e-op-20260724`

### E1 Doctor / identity / secrets — PASS
- Report: `e1-report.md`
- doctor 7/7; identity e2e-regression; initially missing openrouter-key (user supplied OOB; stored via stdin)

### E2 Pack / export / inspect — PASS
- Bundle: `weather-agent.agentpaas` (1.1MB)
- Image digest (pack): `sha256:25e9d54c051ea0b94f54f0fd65559a05d9f16f59b461c6a5e9c62c675388f749`
- Bundle digest: `baa9c37de32ed80ead37fa5ab1690acc81b251ae447b713fe783f76fe834333f`
- **lock_provenance PASS** (disk: `e2-inspect.log:15`)
- All 9 integrity checks PASS

### E3 Run + invoke — PASS (disk-verified)
| Run | Status | invoke-response |
|-----|--------|-----------------|
| `run-6c376ef353d9a6c5` | completed 5.993s | `~/.agentpaas/state/runs/run-6c376ef353d9a6c5/invoke-response.json` status **OK**, Folsom weather, weather_fetched true |
| `run-d7710eb4b2a24fde` | completed 11.514s | `~/.agentpaas/state/runs/run-d7710eb4b2a24fde/invoke-response.json` status **OK**, Folsom 92°F sunny |

Orch verified JSON on disk (not model prose alone).

### E4 MCP operator proof — PASS
- `TestE2E_CrossContainer_LookupFeedback` PASS
- `TestE2E_OperatorPack_MCPFeedbackService` PASS
- `/undeclared-tool-denied` PASS (secret_backdoor → unknown_tool)
- Log: `docs/owa-records/_e2e-mcp-e4-20260724.log`

### E5 Fail-closed — PASS
- Tampered bundle: `invalid tar header` fail-closed (`e5-tamper-inspect.log`)
- Bad policy pack: schema reject fail-closed (`e5-bad-policy-pack.log`)
- Undeclared MCP tool: denied (E4)

## Bugs

| ID | Severity | Status | Notes |
|----|----------|--------|-------|
| E2E-001 | low (docs) | **FIXED** `869c96e` | main.go legacy/compat timeout annotations for B30 characterization |
| E2E-002 | low (test drift) | **FIXED** `0ad6542` | plugin tests expect `--wait` on `trigger invoke` (prod tools.py) |
| E2E-003 | gate hygiene | **OPEN** (non-blocking) | `block27-gate` T08 pipes unittest to `tail -5` → make may print PASS despite FAIL; recommend `set -o pipefail` or drop tail |
| govuln | pre-existing | **OPEN** (known) | 5 vulns / 2 modules via docker API client graph; not session-introduced |

## Commits this session

```
0ad6542 test(plugin): expect --wait on trigger invoke CLI args
869c96e fix(harness): restore legacy/compat timeout docs for B30 v0.2.3 characterization
```

## Optional stretch

| Item | Status |
|------|--------|
| make golden-slow | SKIP |
| make golden-docker | SKIP |
| make golden-eval | SKIP |

## Anti-pattern checks

- [x] No brew agentpaas for test path (repo bin only after teardown)
- [x] restart-local-daemon after every build-all
- [x] Nuclear teardown at Phase A start
- [x] Invoke PASS only with invoke-response.json on disk
- [x] No B34 feature work

## Operator notes for next clean-slate run

1. After nuclear teardown: restore colima/docker, `make build-all`, `make restart-local-daemon`.
2. `agentpaas identity init --name <slug>` before any pack/golden G47.
3. `agentpaas secret add openrouter-key` (user stdin) before weather invoke.
4. Prefer `make block33-gate-fast` for B33 delta; full chain is multi-hour due to nested historical gates.
