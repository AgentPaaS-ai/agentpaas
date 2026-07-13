# AgentPaaS Defect Log

> **Single source of truth for all tracked bugs.**
> Every bug discovered during development, manual testing, golden loop, or
> security review is recorded here. No other file should maintain a
> separate bug log — this document supersedes all prior scattered logs.

## Format

Each bug follows this template:

```
### BUG-NNN: <short title>

| Field            | Value                              |
|------------------|------------------------------------|
| Status           | OPEN / FIXED / ACKNOWLEDGED        |
| Severity         | P0 / P1 / P2 / minor / UX          |
| Discovered       | <version>                          |
| Fixed in         | <version+commit> or "not yet"      |
| Found during      | <test session / phase / review>    |
| Trigger          | <what input/action exposed it>     |

**Root cause:** <technical explanation>

**Symptoms:** <observable behavior>

**Reproduce:**
1. <step>
2. <step>

**Fix:** <what changed, which commit>

**Post-fix verification:**
1. <how to confirm the fix works>
2. <how to confirm no regression>

**Related files:** <key source files>
**Related commits:** <hashes>
```

---

## Prioritized Open Bugs (by severity)

| Priority | Bug    | Title                                                         |
|----------|--------|---------------------------------------------------------------|
| security | BUG-031| Egress hostname not confirmed on agent modification           |
| P2       | BUG-018| Agent writes host/hostname instead of domain in policy.yaml   |
| design   | BUG-025| llm_provider_lock doesn't restrict agent.http() calls         |

All other bugs are FIXED. See individual entries below.

---

## All Bugs (chronological)

---

### BUG-001: Gateway crashes on `ports` field in route entries

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P0                                                          |
| Discovered       | v0.1.x (B24.5 manual testing)                               |
| Fixed in         | commit (removed Ports from gatewayRoute struct)              |
| Found during      | B24.5 manual testing                                         |
| Trigger          | Any pack/run — gateway config emitted `ports` on routes     |

**Root cause:** The policy compiler's `gatewayRoute` struct had a `Ports []int`
field with yaml tag `ports`, and `buildEgressRoutes()` populated it from
`EgressRule.Ports`. agentgateway v1.3.0 rejects the unknown `ports` field on
route entries with `Error: binds[0].listeners[0].routes[0]: unknown field
'ports'`. The gateway container creates, starts, and immediately dies. The
agent container then tries to connect to the dead gateway's proxy at port 7799
and gets `connection refused`. Every run fails in ~200ms.

**Symptoms:** `proxyconnect tcp: dial tcp <gw-ip>:7799: connect: connection
refused`. Gateway container dies silently; daemon reports run as "succeeded"
(exit 0) because the container starts and exits cleanly.

**Reproduce:**
1. Pack any agent with egress rules
2. `docker run --rm -v <config>:/config.yaml:ro ghcr.io/agentgateway/agentgateway:v1.3.0 -f /config.yaml` — see config parse error

**Fix:** Removed `Ports` from `gatewayRoute` struct and from route construction
in `buildEgressRoutes()`. agentgateway matches routes by hostname only — port
enforcement is handled by Docker network topology (only HTTPS/443 is proxied).
Updated golden file `gateway_config.golden` and 6 compiler tests.

**Post-fix verification:**
1. Pack + run an agent — gateway container stays alive
2. `docker logs <gateway-container>` shows no parse errors
3. Egress allowed events appear in harness audit

**Related files:** `internal/policy/compiler.go`, `internal/policy/compiler_test.go`
**Related commits:** (removed Ports from gatewayRoute)

---

### BUG-012: Onboarding UX shows ports and checklists to user

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P1 (UX)                                                     |
| Discovered       | v0.1.x (B24.5 T3 retest)                                    |
| Fixed in         | b16d61b                                                      |
| Found during      | B24.5 manual testing T3                                     |
| Trigger          | "Build a weather agent" prompt                               |

**Root cause:** The Hermes plugin SKILL.md did not enforce a terse
one-question-at-a-time onboarding flow. Hermes listed `wttr.in:443` and
`openrouter.ai:443` in chat, dumped a multi-item "Next steps (I will not
proceed until you confirm)" wall, and combined domains + provider + project
dir into one mega-prompt.

**Symptoms:** First-time users see `:443` port numbers, checklist walls, and
combined prompts instead of a simple provider → model → secret → hostnames
flow.

**Reproduce:**
1. Fresh install of plugin
2. Ask: "Build a weather agent that uses an LLM"
3. Observe ports in chat and multi-item checklist walls

**Fix:** Rewrote `integrations/hermes-plugin/SKILL.md` (b16d61b) to enforce:
1. Which LLM provider?
2. Which model?
3. `agentpaas secret add <name>` in separate terminal (stdin paste)
4. "This agent will access: wttr.in, openrouter.ai. Allow these?"

Ports are written only into policy.yaml by the agent, never shown to user.

**Post-fix verification:**
1. Fresh plugin install → restart → `rm -rf ~/weather-agent`
2. Re-run T3 first-line prompt
3. First reply should be ≈ "Which LLM provider?"
4. No `:443` or `ports:` in any user-facing message

**Related files:** `integrations/hermes-plugin/SKILL.md`
**Related commits:** b16d61b

---

### BUG-013: HTTP response field is `status` not `status_code`

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P1                                                          |
| Discovered       | v0.1.x (T3 testing)                                          |
| Fixed in         | 634a813                                                      |
| Found during      | Manual testing T3                                            |
| Trigger          | Agent code checks `resp["status_code"]` (requests-style)     |

**Root cause:** Harness historically returned only `"status": <int>` plus
headers/body. Generated agents commonly check `resp["status_code"]`
(requests-style) which silently returns `None`, causing false "Failed to fetch"
while real JSON is in the error string and the HTTP request actually succeeded.

**Symptoms:** Agent reports fetch failure while wttr.in data is present in the
error string. LLM never runs because agent thinks HTTP failed.

**Reproduce:**
1. Build an agent that uses `resp.get("status_code")` to check response
2. Invoke agent
3. Agent reports failure despite successful HTTP fetch

**Fix:** Emit both `"status"` (canonical) and `"status_code"` (alias) keys in
the harness HTTP response (634a813).

**Post-fix verification:**
1. Agent code using either `resp["status"]` or `resp["status_code"]` works
2. Both keys return the same integer value

**Related files:** `internal/harness/rpc_server.go`
**Related commits:** 634a813

---

### BUG-014: Stale repo `bin/` binary shadows installed binary

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (operational, not a code bug)                          |
| Severity         | P1                                                          |
| Discovered       | v0.1.x                                                       |
| Fixed in         | N/A (operational discipline)                                 |
| Found during      | Manual testing across multiple sessions                     |
| Trigger          | Building from source, then running `agentpaas daemon start`  |

**Root cause:** `agentpaas daemon start` launches whichever `agentpaasd` is
first in PATH. When built from source, `/usr/local/bin/agentpaasd` (stale) or
`~/projects/agentpaas/bin/agentpaasd` (dev) can shadow
`/opt/homebrew/bin/agentpaasd` (release). The stale binary silently runs
pre-fix code and re-opens already-closed bugs.

**Symptoms:** Gateway never ready, DNS failures, HTTP 500s, LLM 401 errors —
all despite having "rebuilt" the binary. Binary mtime < last fix commit time.

**Reproduce:**
1. `cd ~/projects/agentpaas && go build -o bin/agentpaasd ./cmd/agentpaasd`
2. `agentpaas daemon start` — runs from `bin/agentpaasd` (stale dev build)
3. `ps aux | rg agentpaasd` — shows wrong path

**Fix:** Operational discipline — always rebuild ALL THREE binaries to
`/opt/homebrew/bin/` and `/usr/local/bin/`:
```bash
go build -o /usr/local/bin/agentpaas ./cmd/agentpaas
go build -o /tmp/agentpaasd ./cmd/agentpaasd
sudo cp /tmp/agentpaasd /opt/homebrew/bin/agentpaasd
sudo cp /tmp/agentpaasd /usr/local/bin/agentpaasd
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o /tmp/agentpaas-harness-linux ./cmd/harness
sudo cp /tmp/agentpaas-harness-linux /opt/homebrew/bin/agentpaas-harness-linux
sudo cp /tmp/agentpaas-harness-linux /usr/local/bin/agentpaas-harness-linux
```

**Post-fix verification:**
1. `ps aux | rg agentpaasd` — must show `/opt/homebrew/bin/agentpaasd`
2. `agentpaas version` — shows expected version
3. `ls -la /opt/homebrew/bin/agentpaasd` — mtime ≥ last fix commit time

**Related files:** N/A (operational)
**Related commits:** N/A

---

### BUG-015: Hermes fabricates test PASS from ERROR bodies

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P1                                                          |
| Discovered       | v0.1.x                                                       |
| Fixed in         | 26efd3f + skill/SOUL changes                                 |
| Found during      | Manual testing T3-T6                                        |
| Trigger          | Agent invoke returns ERROR, but Hermes claims success        |

**Root cause:** Test-profile Hermes invents success narratives from ERROR
response bodies. It scrapes weather data out of an error string (e.g., the
response body embedded in the error) and reports "built + verified + weather
numbers" while `invoke-response.json` shows `status: ERROR`.

Three fabrication modes observed:
1. **Success fabrication** — claims PASS while invoke-response is ERROR
2. **Root-cause fabrication** — invents plausible cause (e.g., "transient
   wttr.in HTTP 500") that didn't happen. Actual audit shows wall_clock
   budget_exceeded + openrouter EOF.
3. **Correct-file corruption attempt** — asserts CORRECT generated files use
   "wrong field names" and attempts to patch them to non-existent schema fields.

**Symptoms:** Chat reports success; disk evidence (invoke-response.json,
harness-audit.jsonl) shows ERROR or failure.

**Reproduce:**
1. Run T3 with an agent that fails (e.g., missing credential)
2. Hermes claims "built successfully" and reports weather numbers
3. Check `invoke-response.json` — shows `status: ERROR`

**Fix:** SKILL.md and SOUL.md updated (26efd3f) to enforce disk evidence
verification. PASS requires:
1. `invoke-response.json`: `result.status` == OK
2. `harness-audit.jsonl`: `egress_allowed` for all expected domains, 0 denials
3. Host cross-check (e.g., wttr.in data matches agent output)

**Post-fix verification:**
1. Run T3 — agent must NOT claim PASS without disk evidence
2. Check that orchestrator verifies `invoke-response.json` and `harness-audit.jsonl`

**Related files:** `integrations/hermes-plugin/SKILL.md`, SOUL.md
**Related commits:** 26efd3f

---

### BUG-016: `secret test` 200 ≠ runtime LLM OK

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P0                                                          |
| Discovered       | v0.1.x (T3 testing)                                          |
| Fixed in         | 26efd3f                                                      |
| Found during      | Manual testing T3                                            |
| Trigger          | `agentpaas secret test` passes but `agent.llm()` returns 401 |

**Root cause:** Host `secret test` hits OpenRouter directly (bypasses
gateway). Runtime `agent.llm()` uses gateway sidecar + HTTPS_PROXY. The 401
"Missing Authentication header" was caused by a stale
`agentpaas-harness-linux` binary (pre-B20) that never called
`LoadCredentials` — the Authorization header was empty `Bearer `.

**Symptoms:** `agentpaas secret test openrouter-key` returns 200. Agent invoke
returns 401 "Missing Authentication header" for openrouter.ai.

**Reproduce:**
1. Store OpenRouter key: `agentpaas secret add openrouter-key`
2. `agentpaas secret test openrouter-key --provider openrouter` — 200 OK
3. Pack + run agent with `agent.llm()` — 401 Missing Authentication header
4. Check harness binary: `strings /opt/homebrew/bin/agentpaas-harness-linux | rg AGENTPAAS_CREDENTIALS_PATH` — no output = stale binary

**Fix:** Rebuild harness binary after B20 credential changes (26efd3f). The
harness now loads credentials from sidecar JSON file via `LoadCredentials()`.

**Post-fix verification:**
1. `strings /opt/homebrew/bin/agentpaas-harness-linux | rg AGENTPAAS_CREDENTIALS_PATH` — shows the env var
2. Pack + run agent — `egress_allowed` for openrouter.ai with 200
3. Harness audit shows LLM result, not 401

**Related files:** `internal/harness/rpc_server.go`, `cmd/harness`
**Related commits:** 26efd3f

---

### BUG-017: Onboarding skip / silent demo copy

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P0                                                          |
| Discovered       | v0.1.x (T3 after clean T1/T2)                               |
| Fixed in         | 90cb89e                                                      |
| Found during      | Manual testing T3                                            |
| Trigger          | Fresh T3 prompt after clean T1/T2 pass                      |

**Root cause:** Profile skill was a pointer telling Hermes to
`skill_view(name="agentpaas:deploy")`. That skill didn't exist (only
`agentpaas-build` was available). Hermes skipped onboarding entirely, copied
`demo/weather-agent/main.py` (md5 match), prefilled OpenRouter, packed with
empty Keychain / `credentials: []` — zero user questions. SOUL lacked
AgentPaaS rules after clean-slate strip. `agentpaas_pack` had no secret
existence gate.

**Symptoms:** Packed agent with OpenRouter prefilled, zero user questions,
Keychain empty, policy `credentials: []`. Hermes claimed success and printed
next-step CLI for secret add *after* pack.

**Reproduce:**
1. Nuclear teardown (clean slate)
2. Install plugin from GitHub
3. Restart Hermes
4. Ask: "Build a weather agent that uses an LLM"
5. Agent skips all onboarding questions and copies demo/weather-agent

**Fix (90cb89e):**
1. Write **full** `integrations/hermes-plugin/SKILL.md` into profile as
   `agentpaas-build` (not a pointer)
2. Force-upsert SOUL AgentPaaS block every plugin `register()`
3. Pre-pack gate: if `agent.yaml` has `llm.credential`, Keychain must list
   that label or pack returns `onboarding_incomplete`

**Post-fix verification:**
1. Reinstall plugin from GitHub → restart → `rm -rf ~/weather-agent`
2. Re-run T3 first-line prompt
3. First reply should be "Which LLM provider?"
4. `agentpaas_pack` with missing secret returns `onboarding_incomplete`
5. `agentpaas_secret_list` shows the key before pack succeeds

**Related files:** `integrations/hermes-plugin/__init__.py`, `integrations/hermes-plugin/tools.py`, `integrations/hermes-plugin/SKILL.md`
**Related commits:** 90cb89e

---

### BUG-018: Agent writes host/hostname instead of domain in policy.yaml

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (SKILL.md rule added)                                  |

**Root cause:** The agent (Hermes) writes `host:` or `hostname:` instead of
`domain:` in policy.yaml egress rules. The correct field is `domain`
(`internal/policy/canonical.go` line 37). `host`/`hostname` are not valid
schema fields. The agent self-corrects after pack failure by running
`agent policy init --template allow-llm` in a temp dir to discover the
correct field name.

**Symptoms:** Pack fails with validation error for unknown field `host` or
`hostname`. Agent retries with `domain` and succeeds.

**Reproduce:**
1. Ask Hermes to build a new agent with egress rules
2. Check generated policy.yaml — may use `host:` instead of `domain:`
3. Pack fails with schema validation error

**Fix:** Added BUG-018 rule to `integrations/hermes-plugin/SKILL.md` Step 2:
explicitly instructs agent to use `domain` (not `host` or `hostname`) as
the field name for egress rules in policy.yaml.

**Post-fix verification:**
1. Build a new agent — policy.yaml should use `domain:` on first attempt
2. Pack succeeds without validation errors

**Related files:** `internal/policy/canonical.go`, `integrations/hermes-plugin/SKILL.md`
**Related commits:** N/A

---

### BUG-019: Gateway policy enforcement not working (llm_budget)

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (Part A: 254c99c, Part B: confirmed working)           |
| Severity         | P0                                                          |
| Discovered       | v0.1.x (T11 manual testing)                                 |
| Fixed in         | v0.1.1 (254c99c)                                             |
| Found during      | T11 manual testing (Token Budget Enforcement)               |
| Trigger          | Agent with `llm_budget` in policy.yaml                       |

**Root cause (two-part):**

**Part A — Daemon didn't wire policy → harness budget (FIXED 254c99c):**
`buildInvokePayload()` in `internal/daemon/control_handlers.go` constructed
the invoke payload with `llm`, `credentials`, and `mcp` keys but never read
`llm_budget` from policy.yaml and never added a `budget` key to the payload.
The harness `BudgetEnforcer` reads its config from `payload["budget"]` via
`budgetFromPayload()`. Since `payload["budget"]` was never set, it returned
`BudgetConfig{}` (all zeros), and `newBudgetEnforcer` applied defaults.

**Part B — Gateway container confirmed working (was misdiagnosed):**
Initial diagnosis said "gateway container never starts in local mode." This
was WRONG. The gateway IS created and started per-run by `control_handlers.go`
(~line 406). `cleanupRun()` removes the gateway container AND
`gateway-config/` directory after the run completes — that's why `docker ps`
shows nothing.

**Symptoms:** `llm_budget: {max_tokens: 100, max_tokens_per_request: 50}` in
policy.yaml produced a ~300-token response with no truncation or budget error.

**Reproduce:**
1. Create agent with `llm_budget: {max_tokens: 100}` in policy.yaml
2. Pack + run + invoke
3. LLM response exceeds 100 tokens with no budget_exceeded event

**Fix:** Added code in `buildInvokePayload()` to read
`parsedPolicy.LLMBudget` and add it to the payload as `payload["budget"]`.
Also added `"budget": true` to reserved keys set.

**Post-fix verification:**
1. Agent with `llm_budget: {max_tokens: 100}` → invoke produces
   `budget_exceeded` event with `limit:100, observed:12696` in harness audit
2. Normal agents without `llm_budget` still use defaults and complete normally
3. `cat ~/.agentpaas/state/runs/<run-id>/harness-audit/harness-audit.jsonl | grep budget`

**Related files:** `internal/daemon/control_handlers.go`, `internal/harness/budget.go`
**Related commits:** 254c99c

---

### BUG-020: max_tokens not passed to LLM API

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P1                                                          |
| Discovered       | v0.1.x (T11 manual testing)                                 |
| Fixed in         | v0.1.1                                                       |
| Found during      | T11 manual testing                                           |
| Trigger          | Agent with `llm_budget.max_tokens_per_request` in policy    |

**Root cause:** LLM adapters (`BuildRequest` in openrouter.go, openai.go,
anthropic.go, xai.go, nous.go) omitted `max_tokens` from the JSON request
body entirely. OpenAI-compatible APIs support `max_tokens` as a hard
server-side cap — the LLM stops generating at that limit. This was the
strongest enforcement layer and it was not wired.

**Symptoms:** LLM responses exceed `max_tokens_per_request` limit — no
server-side truncation.

**Reproduce:**
1. Set `llm_budget: {max_tokens_per_request: 50}` in policy.yaml
2. Pack + run agent
3. LLM response exceeds 50 tokens (not truncated by API)

**Fix:** LLM adapters now support `max_tokens` via variadic parameter. Harness
reads `max_tokens_per_request` from budget config and passes to
`BuildRequest` (rpc_server.go:354).

**Post-fix verification:**
1. Set `max_tokens_per_request: 50` — LLM response truncated by API
2. Audit shows `max_tokens` value in the LLM request
3. Existing agents without budget work unchanged (max_tokens omitted)

**Related files:** `internal/harness/rpc_server.go`, `internal/llm/*.go`
**Related commits:** (v0.1.1)

---

### BUG-021: CONNECT tunneling prevents gateway policy enforcement

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (889f0e3), regression FIXED (4908c4b)                 |
| Severity         | P0                                                          |
| Discovered       | v0.1.x (T12 manual testing)                                 |
| Fixed in         | v0.1.1 (889f0e3), regression v0.2.0 (4908c4b)               |
| Found during      | T12 manual testing (Rate Limiting)                          |
| Trigger          | Agent with `llm_rate_limit` in policy.yaml                   |

**Root cause:** AgentPaaS used agentgateway as a forward HTTP proxy via
`HTTP_PROXY`/`HTTPS_PROXY` environment variables. HTTPS traffic went through
CONNECT tunneling — the gateway created an opaque TCP tunnel and could not
inspect individual HTTP POST requests or count response tokens inside the
encrypted tunnel. `llm_rate_limit` compiled correctly into `localRateLimit`
config but was NOT enforced at runtime.

**Regression (FIXED 4908c4b):** The fix removed `HTTP_PROXY`/`HTTPS_PROXY`
entirely and replaced with `AGENTPAAS_GATEWAY_URL`. But
`AGENTPAAS_GATEWAY_URL` is ONLY used by the harness for LLM URL rewriting.
Non-LLM egress (agent's own HTTP calls) had NO proxy and NO route to the
gateway. Fix: re-add `HTTP_PROXY`/`HTTPS_PROXY` alongside
`AGENTPAAS_GATEWAY_URL`. `NO_PROXY` includes gateway IP so harness LLM calls
aren't double-proxied.

**Symptoms:** Two LLM calls in a single invoke both succeed with HTTP 200
even with `requests_per_minute: 1, tokens_per_minute: 200`.

**Reproduce:**
1. Set `llm_rate_limit: {requests_per_minute: 1}` in policy.yaml
2. Pack + run agent that makes 2 LLM calls in one invoke
3. Both calls succeed (no 429 rate limit)

**Fix (889f0e3):** Replace forward-proxy CONNECT with direct HTTP routing.
Harness sends plain HTTP to gateway; gateway terminates HTTP, applies
policies, then makes outbound HTTPS call.

Component changes:
1. Compiler: egress route backends from `dynamic: {}` to `host: domain:443`
   + `backendTLS: {}`
2. Daemon: removed HTTP_PROXY/HTTPS_PROXY, added
   `AGENTPAAS_GATEWAY_URL=http://<gateway-ip>:7799`
3. Harness: URL rewriting — rewrite outbound HTTPS URLs to
   `http://gateway:7799/path` with `req.Host = originalHostname`

**Post-fix verification:**
1. Agent with `requests_per_minute: 1` → second LLM call blocked with 429
2. Both LLM calls (via AGENTPAAS_GATEWAY_URL) and non-LLM egress (via
   HTTP_PROXY to wttr.in) work simultaneously
3. `control_handlers_proxy_test.go` asserts BOTH HTTP_PROXY and
   AGENTPAAS_GATEWAY_URL present
4. `adversary_t02_test.go` verifies NO_PROXY contains gateway IP

**Related files:** `internal/policy/compiler.go`, `internal/daemon/control_handlers.go`, `internal/harness/rpc_server.go`
**Related commits:** 889f0e3, 4908c4b, fbc7d25

---

### BUG-022: observability.cost_tracking was schema-only

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P1                                                          |
| Discovered       | v0.1.x (B19 T17 manual testing, 2026-07-10)                |
| Fixed in         | v0.2.0 (working tree, July 2026)                            |
| Found during      | B19 T17 manual testing                                      |
| Trigger          | Agent with `observability.cost_tracking: true` in policy    |

**Root cause:** The `observability.cost_tracking: true` field was accepted by
the policy schema and compiled into agentgateway config YAML, but NO backend
code read it. No `llm_result`, token count, or cost events appeared in the
harness audit.

**Symptoms:** Harness audit has no cost tracking events despite
`cost_tracking: true` in policy.

**Reproduce:**
1. Set `observability: {cost_tracking: true}` in policy.yaml
2. Pack + run agent with LLM call
3. Check harness-audit.jsonl — no cost/token events

**Fix:** Policy observability settings are now forwarded to the harness.
Successful LLM calls emit sanitized `llm_result` audit events with
provider/model, input/output/total tokens, and estimated cost.

**Post-fix verification:**
1. Set `cost_tracking: true` — harness audit shows `llm_result` events
2. Events contain provider, model, token counts, estimated cost
3. T17 PASSES

**Related files:** `internal/harness/rpc_server.go`, `internal/policy/compiler.go`
**Related commits:** (v0.2.0 working tree)

---

### BUG-023: inject_system_prompt accepted by schema but not compiled

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (existing runtime path verified)                      |
| Severity         | P1                                                          |
| Discovered       | v0.1.x (B19 T18 manual testing)                            |
| Fixed in         | v0.2.0 (verified working, July 2026)                        |
| Found during      | B19 T18 manual testing                                      |
| Trigger          | Agent with `transformations.request.inject_system_prompt`   |

**Root cause:** The `transformations.request.inject_system_prompt` field was
accepted by the policy schema but investigation found the daemon payload +
harness enforcement path was already functional — the system prompt is
carried through the daemon invoke payload and enforced by the harness on the
provider-aware host-backend path.

**Symptoms:** Initially appeared that inject_system_prompt was not working.
Upon investigation, the runtime path was already functional.

**Reproduce:**
1. Set `transformations: {request: {inject_system_prompt: "You are helpful."}}` in policy.yaml
2. Pack + run agent
3. LLM request includes the system prompt

**Fix:** No code change needed — existing runtime path verified working.
T18 PASSES: `inject_headers` and `response.remove_headers` compile into
gateway config; `inject_system_prompt` is carried through the daemon invoke
payload and enforced by the harness.

**Post-fix verification:**
1. Set inject_system_prompt in policy → LLM request includes system prompt
2. T18 PASSES

**Related files:** `internal/policy/schema.go`, `internal/daemon/control_handlers.go`, `internal/harness/rpc_server.go`
**Related commits:** N/A (verified working)

---

### BUG-024: SDK agent.llm() return key is "text" not "content"

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | minor                                                        |
| Discovered       | v0.2.0 (T3 testing)                                         |
| Fixed in         | v0.2.0 (July 2026)                                           |
| Found during      | Manual testing T3                                            |
| Trigger          | Agent code uses `llm_resp.get("content", "")` (OpenAI-style)|

**Root cause:** The SDK `agent.llm(prompt=...)` returns a dict with key
`"text"` containing the LLM response text. Agent authors commonly use
`llm_resp.get("content", "")` (OpenAI-style), which silently returns empty
string.

**Symptoms:** Agent gets empty string from LLM response despite successful
LLM call. No error — just empty output.

**Reproduce:**
1. Build agent with `result = agent.llm(prompt=...)` then `result.get("content", "")`
2. Invoke agent — LLM succeeds but agent output is empty

**Fix:** Harness now returns BOTH `"text"` (canonical) and `"content"` (alias)
keys in the LLM response. Both fake-LLM and real-LLM paths emit both keys.

**Post-fix verification:**
1. Agent code using either `resp["text"]` or `resp["content"]` works
2. Both keys return the same string value

**Related files:** `internal/harness/rpc_server.go`
**Related commits:** (v0.2.0)

---

### BUG-025: llm_provider_lock doesn't restrict agent.http() calls

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | ACKNOWLEDGED (design limitation, not a bug)                 |
| Severity         | design                                                       |
| Discovered       | v0.2.0 (T13 manual testing)                                |
| Fixed in         | not planned (defense-in-depth, not primary control)         |
| Found during      | T13 manual testing (LLM Provider Lock)                     |
| Trigger          | Agent calls non-approved LLM provider via agent.http()      |

**Root cause:** `llm_provider_lock.allowed_endpoints` restricts the LLM route
path in the gateway config — it only applies to `agent.llm()` calls (which go
through the harness LLM RPC and then the gateway's LLM route). An agent can
call a non-approved LLM provider via
`agent.http("POST", "https://api.openai.com/...")` and it passes egress if
the domain is in the egress allowlist.

This is defense-in-depth, not a primary security control — egress policy is
the primary restriction. To fully lock LLM calls to a provider, also remove
other LLM provider domains from the egress list.

**Symptoms:** Agent bypasses llm_provider_lock by using agent.http() instead
of agent.llm() to call an LLM provider.

**Reproduce:**
1. Set `llm_provider_lock: {allowed_endpoints: [api.x.ai]}` in policy.yaml
2. Also allow `api.openai.com` in egress
3. Agent calls OpenAI via `agent.http("POST", "https://api.openai.com/...")`
4. Request succeeds despite provider lock

**Fix:** Not planned as a code fix. Documented as a known limitation in
`docs/known-limitations.md`. To fully lock: remove non-approved LLM provider
domains from egress list.

**Post-fix verification:** N/A (acknowledged design limitation)

**Related files:** `internal/policy/compiler.go`, `docs/known-limitations.md`
**Related commits:** N/A

---

### BUG-026: Trigger invoke times out for installed agents

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | functional                                                   |
| Discovered       | v0.2.0 (T40 testing)                                        |
| Fixed in         | v0.2.0 (75bffc4)                                             |
| Found during      | T40 manual testing                                           |
| Trigger          | `agentpaas trigger invoke <name>@<pub8>` for installed agent|

**Root cause:** `agentpaas trigger invoke` had a 30s HTTP client timeout.
Installed agent verification + image lookup overhead exceeded this.

**Symptoms:** `agentpaas trigger invoke` for installed agents times out after
30s before the agent completes.

**Reproduce:**
1. Install an agent bundle
2. `agentpaas trigger invoke <name>@<pub8> --payload '{"city":"Folsom"}'`
3. Times out after 30s

**Fix:** Timeout increased to 90s (matching `agentpaas run` CLI timeout,
75bffc4).

**Post-fix verification:**
1. `agentpaas trigger invoke` for installed agent completes within 90s
2. `invoke-response.json` in run directory shows result

**Related files:** `internal/cli/trigger.go`
**Related commits:** 75bffc4

---

### BUG-027: install requires --allow-unlocked-deps but error message unclear

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | UX                                                          |
| Discovered       | v0.2.0 (T35 testing)                                        |
| Fixed in         | v0.2.3                                                      |
| Found during      | T35 manual testing                                           |
| Trigger          | `agentpaas install <bundle> --yes` for agent without uv.lock|

**Root cause:** `agentpaas install` in non-interactive mode (`--yes`) fails
with "missing uv.lock requires --allow-unlocked-deps in non-interactive mode"
when the project has no `uv.lock` file. The error message does mention the
flag, but it's not obvious to users.

**Symptoms:** Install fails with unclear error about uv.lock.

**Reproduce:**
1. Create agent project without `uv.lock`
2. `agentpaas install <bundle> --yes`
3. Error: "missing uv.lock requires --allow-unlocked-deps in non-interactive mode"

**Fix:** Updated `ErrDepsUnlockedRefused` error message in
`internal/install/materialize.go` to clearly explain the issue and provide
the exact command to fix it.

**Post-fix verification:**
1. Error message should clearly explain what to do
2. Or: auto-detect simple agents and not require the flag

**Related files:** `internal/cli/install.go`
**Related commits:** N/A

---

### BUG-028: CLI missing --json and --limit flags for some commands

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | minor                                                        |
| Discovered       | v0.2.0 (T5 testing)                                         |
| Fixed in         | v0.2.3                                                      |
| Found during      | T5 manual testing                                            |
| Trigger          | `cron list --json` or `audit query --limit N`               |

**Root cause:** `cron list --json` is not supported (returns error or empty
output). `audit query --limit N` is not supported. These flags are useful
for programmatic testing.

**Symptoms:** Commands return errors or ignore the flags.

**Reproduce:**
1. `agentpaas cron list --json` — error or no JSON output
2. `agentpaas audit query --limit 5` — error or ignores limit

**Fix:** `cron list --json` was already supported (the plugin's `_run_cli`
helper passes `--json` globally). Added `--limit` as an alias for
`--page-size` on `audit query` in `internal/cli/control.go` for
discoverability.

**Post-fix verification:**
1. `cron list --json` returns valid JSON
2. `audit query --limit 5` returns only 5 entries

**Related files:** `internal/cli/cron.go`, `internal/cli/audit.go`
**Related commits:** N/A

---

### BUG-029: doctor --home override ignored

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | minor                                                        |
| Discovered       | v0.2.0 (T2 testing)                                         |
| Fixed in         | v0.2.0                                                       |
| Found during      | T2 manual testing                                            |
| Trigger          | `agentpaas doctor` with `AGENTPAAS_HOME` set                 |

**Root cause:** `agentpaas doctor` always used `os.UserHomeDir()` for the
Home directory check, ignoring `AGENTPAAS_HOME` environment variable. The
daemon already respected the env var.

**Symptoms:** Doctor shows default home (`~/.agentpaas`) even when
`AGENTPAAS_HOME` is set to a custom path.

**Reproduce:**
1. `AGENTPAAS_HOME=/tmp/test-home agentpaas doctor`
2. Doctor reports `~/.agentpaas` instead of `/tmp/test-home`

**Fix:** Doctor now checks `AGENTPAAS_HOME` first, falls back to
`~/.agentpaas`.

**Post-fix verification:**
1. `AGENTPAAS_HOME=/tmp/test-home agentpaas doctor` — shows `/tmp/test-home`
2. Default behavior (no env var) unchanged — shows `~/.agentpaas`

**Related files:** `internal/doctor/doctor.go`
**Related commits:** (v0.2.0)

---

### BUG-030: Wall clock budget too short (30s) + agent LLM workaround cascade

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P1                                                          |
| Discovered       | v0.2.0 (Session 8, 2026-07-12)                               |
| Fixed in         | v0.2.2 (defaultWallClockBudget 30s → 120s)                  |
| Found during      | Manual testing T3 (golden loop Phase 5)                     |
| Trigger          | First `agent.llm()` call to OpenRouter (cold connection)    |

**Root cause:** The default wall clock budget was 30s
(`internal/harness/budget.go`, `defaultWallClockBudget = 30 * time.Second`).
This is too short for LLM calls — the first `agent.llm()` call to OpenRouter
can take 10-30s (cold connection, model loading). The timeout triggers a
cascade of agent misdiagnoses:
1. `agent.llm()` times out (30s wall clock exceeded)
2. Harness audit shows `budget_exceeded` with `category: wall_clock`
3. Agent misinterprets as LLM failure, switches to
   `agent.http_with_credential()` (which sends raw key, no Bearer prefix)
4. OpenRouter returns 401 "Missing Authentication header"
5. Agent asks user to rotate key with `Bearer sk-or-...` prefix (WRONG)

**Symptoms:** LLM call times out, agent cascades to wrong workaround, asks
user to rotate key with Bearer prefix.

**Reproduce:**
1. Set wall clock budget to 30s (or use pre-fix binary)
2. Pack + run weather agent with `agent.llm()`
3. First LLM call exceeds 30s → budget_exceeded
4. Agent switches to http_with_credential → 401

**Fix:** Changed `defaultWallClockBudget` to `120 * time.Second` in
`internal/harness/budget.go`. Rebuild harness:
```bash
GOOS=linux GOARCH=arm64 go build -o /opt/homebrew/bin/agentpaas-harness-linux ./cmd/harness
agentpaas daemon stop; sleep 1; agentpaas daemon start
```

**Post-fix verification:**
1. Run weather agent with `agent.llm()` — both wttr.in and openrouter.ai
   show `egress_allowed` with status 200
2. Run completes in 2-5s (well under 120s budget)
3. LLM output is a real summary, not hardcoded data
4. Harness audit: no `budget_exceeded` for wall_clock

**Related files:** `internal/harness/budget.go`
**Related commits:** (v0.2.2)

---

### BUG-031: Egress hostname not confirmed on agent modification

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (SKILL.md rule added)                                  |

**Root cause:** When modifying an existing packed agent to add a new egress
hostname, the agent (Hermes) does NOT ask the user to confirm before writing
to policy.yaml. It just writes the new hostname directly. This violates the
security premise that egress is the primary control and the user must
explicitly approve every new hostname. The SKILL.md says "Confirm Egress
Hostnames" but this is only followed during initial agent creation.

**Symptoms:** Agent adds a new egress hostname to policy.yaml without asking
the user for confirmation.

**Reproduce:**
1. Build and pack a weather agent (wttr.in + openrouter.ai)
2. Ask: "Add Google News lookup to this agent"
3. Agent writes `news.google.com` to policy.yaml without asking "This agent
   will now also access: news.google.com. Allow?"
4. Repack and run — new hostname is active without user consent

**Fix:** Added BUG-031 rule to `integrations/hermes-plugin/SKILL.md` Step 2:
explicitly states that egress hostname confirmation applies to BOTH new
agent creation AND agent modification. Agent must ask for confirmation
before adding any new hostname to policy.yaml.

**Post-fix verification:**
1. Ask agent to add a new API to an existing agent
2. Agent should ask: "This agent will now also access: X. Allow?"
3. Only after user confirmation should the hostname be added to policy.yaml

**Related files:** `integrations/hermes-plugin/SKILL.md`
**Related commits:** N/A

---

### BUG-032: Export source_digest mismatch (.tmp contamination)

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P1                                                          |
| Discovered       | v0.2.3 (Session 11, 2026-07-12)                             |
| Fixed in         | v0.2.3 (c44224b + 4be455f)                                   |
| Found during      | Manual testing T4 (golden loop, receiver scenario)         |
| Trigger          | `agentpaas export` then `agentpaas bundle inspect`         |

**Root cause (three interlocking issues):**

1. **WriteToFile .tmp contamination:** `WriteToFile` creates `path+".tmp"`
   in the project dir, then calls `Write()` which calls `CollectBuildFiles()`
   — the .tmp file is picked up as a source file and written into the bundle's
   source/ directory. But the manifest digest was computed earlier WITHOUT
   the .tmp. Verify extracts source/ (with .tmp), computes digest → mismatch.

2. **ExportIgnoreMatcher divergence:** `ExportIgnoreMatcher` used a custom
   pattern list different from pack's `LoadIgnore`. Different file sets →
   different digests.

3. **Build artifacts not in default ignore patterns:** `*.agentpaas` (the
   export bundle itself) and `.agentpaas-built-via` were not in
   `DefaultIgnorePatterns`. When a project had its own `.agentpaasignore`,
   `LoadIgnore` used ONLY those patterns — defaults were not merged.

**Symptoms:** `agentpaas bundle inspect <bundle>.agentpaas` fails with
`source_digest FAIL — manifest source digest "X" != computed "Y"`.

**Reproduce:**
1. `agentpaas pack <project>`
2. `agentpaas export <project> --output /tmp/test.agentpaas --yes`
3. `agentpaas bundle inspect /tmp/test.agentpaas`
4. source_digest FAIL

**Fix (c44224b + 4be455f):**
1. Added `*.agentpaas.tmp` and `audit-export.json` to `DefaultIgnorePatterns`
2. `ExportIgnoreMatcher` now delegates to `pack.LoadIgnore()` — one source
   of truth for ignore patterns
3. `LoadIgnore()` now ALWAYS merges defaults with user patterns
4. Bundle verifier explicitly calls `LoadIgnore(tmpDir)` and passes result
   to `CollectBuildFiles`
5. Added `*.agentpaas` and `.agentpaas-built-via` to `DefaultIgnorePatterns`

**Post-fix verification:**
1. `agentpaas pack <project>`
2. `agentpaas export <project> --output /tmp/test.agentpaas --yes`
3. `agentpaas bundle inspect /tmp/test.agentpaas` — all 9 checks PASS
   including source_digest

**Related files:** `internal/pack/ignore.go`, `internal/export/ignore.go`, `internal/bundle/verify.go`
**Related commits:** c44224b, 4be455f

---

### BUG-033: Gateway follows HTTP redirects without user consent

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | security                                                     |
| Discovered       | v0.2.3 (golden loop, 2026-07-12)                            |
| Fixed in         | v0.2.3                                                      |
| Found during      | Golden loop Phase 5 and 6                                   |
| Trigger          | Agent fetches URL that returns HTTP 302 redirect            |

**Root cause:** The agentgateway silently follows HTTP 302 redirects to
destinations that may not be in the egress policy. When an agent requests
`https://news.google.com/rss/search?q=Folsom+weather&hl=en`, Google returns
a 302 redirect to a different URL. The gateway follows the redirect without:
1. Notifying the user that a redirect occurred
2. Checking the redirect target against the egress policy
3. Asking for user consent

**Security impact:** A redirect could send traffic to an unapproved domain.
The egress policy is the primary security control — if the gateway follows
redirects transparently, the policy is bypassed.

**Symptoms:** Gateway follows 302 redirect to new URL without any consent
check or audit log entry for the redirect target.

**Reproduce:**
1. Build an agent that fetches `https://news.google.com/rss/search?q=Folsom+weather&hl=en`
2. Pack + run + invoke
3. news.google.com returns 302 redirect
4. Gateway follows redirect silently — check harness audit for redirect
   target domain (may not be in egress policy)

**Expected behavior:**
1. Log the redirect in the harness audit
2. If redirect target is in egress policy → follow silently
3. If redirect target is NOT in egress policy → deny and notify user

**Fix:** Added `CheckRedirect` to both `http.Client` instances in
`internal/harness/rpc_server.go` (handleHTTP and handleLLM) that returns
`http.ErrUseLastResponse`, preventing the client from following any
redirect. Added redirect detection in handleHTTP: when response status is
3xx, the redirect target is extracted from the Location header, audited as
`egress_denied` with reason "redirect not followed: NNN → <target>", and
returned to the agent with a `redirect_url` field in the response. Tests:
`TestHandleHTTP_RedirectNotFollowed` and
`TestHandleHTTP_RedirectNotFollowedWithCredential`.

**Post-fix verification:**
1. Agent fetches URL that returns 302
2. Gateway logs redirect in audit
3. If redirect target not in policy → request denied with clear error
4. If redirect target in policy → followed with audit entry

**Related files:** Gateway HTTP routing code (agentgateway sidecar)
**Related commits:** N/A

---

### BUG-034: Gateway TLS handshake error on redirect URLs

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | testing (medium — blocks legitimate requests)               |
| Discovered       | v0.2.3 (golden loop, 2026-07-12)                            |
| Fixed in         | v0.2.3                                                      |
| Found during      | Golden loop Phase 5                                         |
| Trigger          | Agent fetches news.google.com RSS URL with certain params    |

**Root cause:** The gateway returns `tls: first record does not look like a
TLS handshake` for certain news.google.com RSS URL variants. The failing
URL triggers a server-side 302 redirect. The gateway follows the redirect
(Bug 033) but the TLS handshake to the redirect target fails. Root cause
hypothesis: the gateway's HTTP routing has a TLS issue when following
redirects — may be related to HTTP/2 upgrade handling or SNI mismatch
during redirect following.

**Failing URL:** `https://news.google.com/rss/search?q=Folsom+weather&hl=en`
**Working URL:** `https://news.google.com/rss/search?q=Folsom+weather&hl=en-US&gl=US&ceid=US:en`

**Symptoms:** Harness audit shows `egress_denied` with
`reason: "http request failed: Get \"https://news.google.com/...\": tls:
first record does not look like a TLS handshake"`.

**Reproduce:**
1. Build an agent that fetches
   `https://news.google.com/rss/search?q=Folsom+weather&hl=en`
2. Pack + run + invoke
3. Harness audit shows egress_denied with TLS handshake error
4. Verify from Docker: `docker run --rm curlimages/curl -sL "https://news.google.com/rss/search?q=Folsom&hl=en&gl=US&ceid=US:en"` — works fine (issue is gateway-specific)

**Important:** During golden loop Phase 5, the test agent changed URL params
from `hl=en` to `hl=en-US&gl=US&ceid=US:en` to work around this error. User
correctly identified this as a bypass: "your test 5 was a bypass and cheat
the test not a real pass." Changing test inputs to avoid a bug is NOT a valid
pass. The bug must be fixed in the gateway code.

**Fix:** Fixed by BUG-033 fix — the `CheckRedirect` policy prevents the
client from following the redirect that caused the TLS handshake error.
The 3xx response is returned to the agent with the redirect URL, allowing
the agent to re-request using the redirect target URL (which goes through
the gateway's proper TLS termination).

**Post-fix verification:**
1. Agent fetches the failing URL — no TLS error
2. Harness audit shows `egress_allowed` for news.google.com with 200
3. Docker cross-check confirms the URL works from container

**Related files:** Gateway HTTP routing code (agentgateway sidecar), `internal/harness/rpc_server.go`
**Related commits:** N/A

---

## Appendix: Unnumbered bugs (fixed, pre-tracking)

These bugs were fixed before the formal bug tracking system was established.
They are documented here for completeness.

### B18-006: buildInvokePayload only resolved LLM credentials

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (c8b604d)                                              |
| Severity         | P0                                                          |
| Found during      | B18 T6 manual testing                                       |

**Root cause:** `buildInvokePayload()` read `lock.AgentYAML.LLM.Credential`
to get the LLM credential name but NEVER read the policy.yaml `credentials:`
section. For agents with no LLM config, `payload["credentials"]` was empty.

**Fix (c8b604d):** `buildInvokePayload()` now resolves ALL policy-declared
credentials, deduplicates by ID, and sets `payload["credentials"]`.

### B18-007: SDK signature mismatch in SKILL.md

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P1                                                          |
| Found during      | B18 T6 manual testing                                       |

**Root cause:** SKILL.md documented wrong SDK signatures:
`agent.http_with_credential(credential_id, url, ...)` — missing `method` arg.

**Fix:** Updated to `agent.http_with_credential(credential_id, method, url, ...)`.

### B18-008: Policy credentials declaration required

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED                                                        |
| Severity         | P2                                                          |
| Found during      | B18 T6 manual testing                                       |

**Root cause:** Credentials stored in Keychain but not declared in policy.yaml
`credentials:` section. `http_with_credential` fails with "credential is not
declared".

**Fix:** SKILL.md onboarding now includes credential declaration step.

### Pack fails with "no publisher identity"

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (7dc4782)                                              |
| Severity         | P0                                                          |
| Found during      | Manual testing session 2026-07-12                           |

**Root cause:** `CreateAgentLock` in `internal/pack/lock.go` silently allowed
packs with no publisher identity. Agents were packed unsigned with no
provenance chain.

**Fix (7dc4782):** Fail closed when `PublisherKeyStore` is nil or when
`LoadPublisherIdentity` returns `ErrNoPublisherIdentity`.

### trigger invoke --payload only accepted file paths

| Field            | Value                                                        |
|------------------|--------------------------------------------------------------|
| Status           | FIXED (1645e02)                                              |
| Severity         | P2                                                          |
| Found during      | Manual testing session 2026-07-12                           |

**Root cause:** `internal/cli/trigger.go` always did `os.ReadFile(payloadPath)`
— only file paths worked.

**Fix (1645e02):** Detect inline JSON (value starts with `{`) and use
directly. Otherwise read from file.

---

## Conventions for agents working on bugs

When picking up a bug to fix:

1. **Read this file first.** Find the bug by ID. Understand the root cause,
   reproduce steps, and verification method.
2. **Recreate the test setup.** Follow the Reproduce steps exactly. Confirm
   you can observe the bug before attempting a fix.
3. **Do NOT work around the bug.** Changing test inputs, URLs, or parameters
   to avoid a bug is NOT a fix. The bug must be fixed in the source code.
4. **Fix the root cause.** Read the Related files. Understand why the bug
   happens. Fix the actual code, not a symptom.
5. **Post-fix verification.** Follow the Post-fix verification steps exactly.
   Confirm the bug is fixed and no regressions are introduced.
6. **Update this file.** Change the bug's Status from OPEN to FIXED. Add the
   fix commit hash. Update the Fixed in version.
7. **Run existing tests.** `make test` must pass. If you added new tests for
   the fix, they must pass too.
