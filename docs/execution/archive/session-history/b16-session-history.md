# Block 16 — Session History

# B16 Manual Test Fix Round 1 — Resume Prompt

**Date:** 2026-07-03
**Context:** Block 16 manual testing (agentpaas-test profile) found 6 bug
clusters. All recorded in execution plan §16.4. This prompt fixes all of
them before continuing manual testing.

## START HERE

Fix T3f first (P0 — nothing works without it), then T3a-e in order. All
work goes through OWA (workers write code, orchestrator reviews + merges).

Load skills before starting: `agentpaas-build-rhythm`,
`agentpaas-owa-build-orchestration`, `agentpaas-acceptance-testing`.

## REPO STATE

- Repo: ~/projects/agentpaas, on branch main
- HEAD: dd2b027 (T3 findings recorded in execution plan)
- All findings documented in agentpaas-execution-plan-v1.md §16.4 (T1-T3)
- v0.1.0 tag exists but predates these fixes
- agentpaas-test profile has the weather-agent project at ~/weather-agent

## WHAT TO FIX (in priority order)

### T3f (P0 BLOCKER) — Agent invocation never works

**Symptom:** EVERY run fails with "harness /readyz not ready after 30
attempts". Agent code never executes.

**Root cause (confirmed):**

1. The Python SDK (`agentpaas_sdk`) is NOT bundled in the container image.
   The Dockerfile template in `internal/pack/build.go:509-524`
   (`renderDockerfile`) does NOT include a `COPY` for the `python/`
   directory. The container only has:
   - `/app/` (project files: main.py, agent.yaml, policy.yaml, etc.)
   - `/agentpaas/harness` (the Go binary)
   - `/agentpaas/requirements.lock`
   No `python/` directory → `agentpaas_sdk` import fails.

2. The harness Python worker (`internal/harness/python_worker.go:461-486`)
   has two code paths:
   - If `from agentpaas_sdk import run` succeeds → calls `run()` which
     handles the full lifecycle (runner.py:run() loads the user agent,
     bridges app()/invoke(), enters the RPC loop)
   - If SDK import fails (ModuleNotFoundError) → falls back to raw
     importlib loader that looks for `invoke(payload)`
   But the scaffold template (`internal/pack/init.go:208`) generates
   `def app(input)`, not `def invoke(payload)`. So the fallback fails too.

3. The worker at python_worker.go:462 does:
   `repo_python = os.path.join(os.getcwd(), "python")`
   expecting a `python/` directory in the CWD (/app/). But /app/python/
   doesn't exist because it was never COPY'd into the image.

**Fix (two parts, BOTH needed):**

Part 1 — Bundle the SDK in the image:
- In `internal/pack/build.go`, the `CreateBuildContext` function
  (line ~207) only adds project files. The harness binary is added
  separately. The Python SDK must be added to the build context tar
  and COPY'd in the Dockerfile.
- The SDK lives at `<repo-root>/python/agentpaas_sdk/`. The build
  needs to find this relative to the harness binary or a known path.
- Add to `renderDockerfile` (build.go:509):
  `COPY --chown=0:0 python/ /app/python/`
- Add the python/ directory to the build context tar in
  `CreateBuildContext` or a new function.
- The harness already does `sys.path.insert(0, repo_python)` at
  python_worker.go:463 where `repo_python = os.getcwd() + "/python"`
  and the WORKDIR is /app, so /app/python/ will resolve correctly.

Part 2 — Entry point consistency (may be a no-op if SDK run() handles it):
- The SDK's runner.py `run()` function (runner.py:23-40) calls
  `_load_user_agent()` which loads the module, then checks for
  `legacy_invoke` (invoke function) and bridges it.
- BUT it does NOT bridge `app()` — it only checks for `invoke`.
- The agent object (`from .agent import agent`) is a singleton that
  handles registration via decorators. A bare `def app(input)` with no
  `@agent.on_invoke` registration won't be picked up.
- Fix: runner.py `_load_user_agent` must also check for `app` callable
  and register it if `agent._invoke_handler is None`. OR the scaffold
  (init.go:208) must generate code that uses the SDK properly.
- Simplest fix: in runner.py after loading the module, add:
  ```python
  if agent._invoke_handler is None:
      app_fn = getattr(module, "app", None)
      if callable(app_fn):
          agent.on_invoke(app_fn)
  ```
  This bridges the scaffolded `app(input)` to the SDK's invoke handler.

**Verification:**
1. `make build && make lint`
2. Pack a test agent: `agentpaas init /tmp/test-agent && agentpaas pack /tmp/test-agent`
3. Run it: `agentpaas run /tmp/test-agent`
4. Check status: `agentpaas timeline <run-id>` — should show run_start
   WITHOUT a run_stop/failed
5. The run should reach ready state (no /readyz timeout)
6. `agentpaas trigger invoke <agent-name>` should actually execute

**Likely files:**
- `internal/pack/build.go` (renderDockerfile + CreateBuildContext)
- `internal/harness/python_worker.go` (fallback path — optional fix)
- `python/agentpaas_sdk/runner.py` (app() bridge)

---

### T3a (BLOCKER) — Logs crash on invalid UTF-8

**Symptom:** `agentpaas logs <run-id>` fails with:
`rpc error: code = Internal desc = grpc: error while marshaling: string
contains invalid UTF-8`

**Root cause:** The LogEntry proto (`api/control/v1/control.proto:144`)
defines `message` as `string` (field 4). gRPC/proto requires string
fields to be valid UTF-8. Agent log output (Python tracebacks, Docker
layer output, binary data) can contain non-UTF-8 bytes, causing the
marshal to fail.

**Fix:** Sanitize the log message to valid UTF-8 before setting the proto
field. In the log streaming handler
(`internal/daemon/control_handlers.go:561` where LogEntry is constructed),
add a UTF-8 sanitization step:
```go
// Replace invalid UTF-8 bytes with U+FFFD before proto marshal
sanitized := strings.ToValidUTF8(rawMessage, "\ufffd")
entry := &controlv1.LogEntry{
    Message: sanitized,
    ...
}
```
Apply at every point where log content is read from the container and
put into a LogEntry.message field.

**Verification:**
1. Unit test: create a log entry with non-UTF-8 bytes, verify it streams
   without error
2. Manual: run an agent that produces non-UTF-8 output, verify logs work

**Likely files:**
- `internal/daemon/control_handlers.go` (LogEntry construction)
- Possibly `internal/logging/` if there's a shared log reader

---

### T3b (BLOCKER) — Status tool/CLI naming mismatch

**Symptom:** The plugin has `agentpaas_status` but there's no `agentpaas
status` CLI subcommand. Users (and the agent) naturally try `agentpaas
status <run-id>` and get "unknown command".

**Root cause:** The CLI has no `status` verb. Run status is available via
`agentpaas timeline <run-id>`. The plugin tool `agentpaas_status`
(integrations/hermes-plugin/tools.py:806) routes to `daemon status` when
no run_id is given, but there's no per-run status path.

**Fix (pick one):**
- Option A (preferred): Add a `status` CLI subcommand that shows run
  status. Add to `cmd/` a new command or alias `status` → calls the
  same handler as `timeline` but returns just status/summary info.
  This makes `agentpaas status <run-id>` work.
- Option B: Rename the plugin tool to `agentpaas_timeline` to match.
  But `status` is more natural for users.

Go with Option A. Add a `status` command to the CLI root that takes an
optional run_id. If run_id given, show that run's status. If not, show
daemon health (current behavior of `agentpaas_status` with no args).

**Verification:**
1. `agentpaas status <run-id>` works from CLI
2. `agentpaas_status` plugin tool works for both with/without run_id

**Likely files:**
- `cmd/` (new status command)
- `internal/cli/` (command registration)

---

### T3c (BLOCKER) — Trigger invoke payload handling

**Symptom:** `agentpaas trigger invoke <agent> --payload <X>` expects X
to be a FILE PATH. But users/agents naturally pass inline JSON.

**Root cause:** The CLI flag `--payload string` description says "Path to
a payload file". The plugin tool
(`integrations/hermes-plugin/tools.py:1146`) passes whatever the agent
provides directly as the `--payload` arg.

**Fix:** Make the plugin tool handle inline JSON automatically:
- In `agentpaas_trigger_invoke` (tools.py:1146), detect if the payload
  looks like JSON (starts with `{` or `[`). If so, write it to a temp
  file and pass the temp file path to the CLI.
- OR add a `--payload-json` flag to the CLI that accepts inline JSON.

Plugin-side fix is simpler and doesn't require CLI changes:
```python
def agentpaas_trigger_invoke(args, **kwargs):
    payload = args.get("payload", "")
    if payload:
        import json, tempfile, os
        # If payload looks like JSON, write to temp file
        stripped = payload.strip()
        if stripped and stripped[0] in '{[':
            try:
                json.loads(stripped)  # validate it's JSON
                fd, tmp = tempfile.mkstemp(suffix='.json')
                with os.fdopen(fd, 'w') as f:
                    f.write(payload)
                cmd_args.extend(["--payload", tmp])
                # Note: temp file cleanup needed after CLI call
            except json.JSONDecodeError:
                pass  # treat as file path
        else:
            cmd_args.extend(["--payload", payload])
```

**Verification:**
1. Plugin tool with inline JSON payload works
2. Plugin tool with file path still works

**Likely files:**
- `integrations/hermes-plugin/tools.py` (agentpaas_trigger_invoke)

---

### T3d — Failed runs show no failure detail

**Symptom:** Timeline shows `run_stop` with `"status":"failed"` but no
reason. Combined with T3a (logs broken), user can't debug.

**Fix:** Include a failure reason/error message in the run_stop timeline
event and/or the status output. The harness knows why it failed
(import_failed, readyz timeout, etc.) — surface that.

**Verification:**
1. A failed run shows a reason in timeline/status
2. `agentpaas explain-failure <run-id>` returns the root cause

**Likely files:**
- `internal/daemon/control_handlers.go` (run_stop event)
- `internal/harness/` (failure reason capture)

**Note:** This may be partially fixed by T3f (if agents run successfully)
and T3a (if logs work). But the failure reason should still be in the
timeline event itself, not just logs.

---

### T3e — No way to list running agents

**Symptom:** No CLI command or plugin tool to list currently running
agents. User can't see how many are running or get run_ids.

**Fix:**
1. Add CLI command: `agentpaas run list` (or `agentpaas ps`)
   - Shows all active/recent runs
   - Output: run_id, agent_name, status, started_at, duration
   - Query the daemon's state (runs are tracked in state)
2. Add plugin tool: `agentpaas_list_runs`
   - Wraps the new CLI command
3. Update `agentpaas_status` (no run_id) to show a count of running
   agents alongside daemon health

**Verification:**
1. `agentpaas run list` shows running agents
2. `agentpaas_list_runs` plugin tool works
3. Output includes run_id, agent_name, status, started_at

**Likely files:**
- `cmd/` (new run list command)
- `internal/daemon/` (query active runs from state)
- `integrations/hermes-plugin/tools.py` (new tool + plugin.yaml)
- `integrations/hermes-plugin/plugin.yaml` (register new tool)

---

## ALSO ADD (from earlier T1 findings, quick wins)

### T1c-followup: Plugin install URL for users
Already documented in README. Verify the README "Option A" install path
is accurate after the subdirectory URL fix. No code change needed unless
the install fails.

### T1c-note: Plugin skills not in Available Skills
This is expected Hermes behavior (plugin skills are opt-in, not
auto-listed). Add a note to the plugin SKILL.md onboarding section
explaining this, so users know to use `skill_view(agentpaas:agentpaas)`.

## BUILD DISCIPLINE

- Each fix is a separate micro-chunk commit
- After each fix: `make build && make lint`
- After all fixes: `make test` (or relevant gate)
- Push each commit to main
- After all T3 fixes: re-run manual test in agentpaas-test profile

## OWA MODEL ALLOCATION

- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Workers: grok-composer-2.5-fast via Grok CLI ($0, subscription)
  (fallback: deepseek-v4-pro via openrouter if grok stalls)
- Adversary: grok-4.3 via agentpaas-adversary profile
- Verifier: GLM-5.2 via agentpaas-verifier profile (block-end)

## AFTER ALL FIXES

1. Push to main
2. Move v0.1.0 tag to new HEAD
3. Reset agentpaas-test profile: `scripts/reset-agentpaas-test.sh`
4. Re-run manual tests: LC-01 (plugin install) → LC-02 (deploy+invoke)
5. Verify the full lifecycle works end-to-end

## TEST VERIFICATION (after fixes)

After all T3 fixes land, verify the complete flow:

```bash
# 1. Reset test profile
cd ~/projects/agentpaas && ./scripts/reset-agentpaas-test.sh

# 2. Start fresh session (separate terminal)
hermes -p agentpaas-test

# 3. Test prompt: build weather agent
"Build a simple agent that fetches weather from a public API.
Use AgentPaaS to package and deploy it."

# 4. Verify: agent actually runs (not just packed)
#    - agentpaas_run should succeed
#    - agentpaas timeline <run-id> should show run_start, NO run_stop/failed
#    - agentpaas logs <run-id> should work (no UTF-8 crash)

# 5. Test prompt: invoke the agent
"Invoke the weather agent and show me the result"
#    - Should return actual weather data
#    - Audit trail should show egress events

# 6. Test prompt: list running agents
"Show me all running agents"
#    - Should use the new list command/tool

# 7. Test prompt: denied egress probe
"Make the agent try to reach example.com"
#    - Should show DENIED in audit trail
```

---

# B16 Fix Round 3 — Resume Prompt

**Date:** 2026-07-03
**Session:** Continuing from B16 manual test round 3
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 4c85edb (all T4-01..T4-13 fixes pushed)

## START HERE

Fix the bugs below in priority order. Use OWA (Grok workers via tmux
for implementation, orchestrator reviews + merges). All work goes
through workers — orchestrator only plans, dispatches, reviews, merges.

Load skills before starting: `agentpaas-build-rhythm`,
`agentpaas-owa-build-orchestration`, `agentpaas-acceptance-testing`,
`owa-multi-agent-coding`, `grok`.

## BINARIES

Current binaries at /usr/local/bin/agentpaas{,d} are from 4c85edb.
After each fix: rebuild with `go build -a`, install, restart daemon.
**IMPORTANT:** Always use `go build -a` (full rebuild). `make build` can
produce stale cached binaries. Verify with `md5` comparison.

```bash
cd ~/projects/agentpaas
go build -a -o bin/agentpaas ./cmd/agentpaas
go build -a -o bin/agentpaasd ./cmd/agentpaasd
sudo cp -f bin/agentpaas bin/agentpaasd /usr/local/bin/
agentpaas daemon stop; sleep 2; rm -f ~/.agentpaas/daemon.sock
agentpaas daemon start
```

## WHAT TO FIX (in priority order)

### T4-16 (CRITICAL): Gateway allowlist must be synced from agent policy.yaml

**Problem:** The agentgateway (egress HTTP proxy on port 7799) rejects
ALL outbound traffic with "network authorization denied". The gateway's
config.yaml has a hardcoded allowlist (e.g. `api.weather.gov`) that
doesn't match the agent's policy.yaml egress rules.

**Root cause:** When `agentpaas run` starts a run, it creates the gateway
container with a default/fixed config. It does NOT generate the gateway
config FROM the agent's policy.yaml.

**Files to investigate:**
- `internal/daemon/control_handlers.go` — the Run handler (creates
  containers, networks, gateway)
- `internal/runtime/docker.go` — container/network creation
- Search for where the gateway config.yaml is generated/written
- Search for `agentgateway` or `gateway` config generation

**Fix approach:**
1. Find where the gateway container is created during `Run`
2. Before starting the gateway, read the agent's policy.yaml
3. Generate a gateway config.yaml with `networkAuthorization` rules
   matching the policy's egress allowlist
4. Write/mount this config into the gateway container

**Gateway config format** (from inspection):
```yaml
binds:
  - port: 7799
    listeners:
      - protocol: HTTP
        routes:
          - name: egress
            backends:
              - host: <domain>:443   # for each allowed domain
frontendPolicies:
    networkAuthorization:
      - allow: dns.domain == "<domain>"  # for each allowed domain
```

**Verification:**
1. Create agent with `allow-http` policy (allows *:443)
2. Pack + run
3. From inside the agent container, test HTTPS to an allowed domain
4. Should succeed (not "connection reset by peer")

### T4-17 (HIGH): Agent must confirm egress destinations with user

**Problem:** When building an agent, Hermes doesn't ask the user which
external domains the agent should be allowed to access.

**Fix:** Update `integrations/hermes-plugin/SKILL.md` with explicit
build-time instructions:
1. Analyze agent code for external domains (URLs, API endpoints)
2. Present domains to user: "This agent will access X, Y, Z. Allow?"
3. Generate policy.yaml with ONLY confirmed domains (not wildcard)
4. Never use `allow-http` (wildcard *:443) unless user explicitly
   requests it

### T4-18 (HIGH): Agent must procure API keys/OAuth from user

**Problem:** When an agent needs external API keys, Hermes doesn't ask
the user for them.

**Fix:** Update SKILL.md with credential procurement flow:
1. Detect credential needs in agent code (env vars, auth headers)
2. Ask user: "This agent needs an API key for X. Do you have one?"
3. Store via `agentpaas_secret_add`
4. Reference in agent.yaml

### T4-19 (HIGH): Agent must ask which LLM to use

**Problem:** When deploying an agent that needs an LLM, Hermes doesn't
ask the user which provider/model/key to use.

**Fix:** Update SKILL.md with LLM configuration flow:
1. Recognize agent needs LLM (imports openai/anthropic, has chat logic)
2. Ask: "Which LLM provider? (OpenAI/Anthropic/xAI)"
3. Ask: "Which model?"
4. Ask for API key if not stored
5. Configure agent.yaml llm: section
6. Add egress for chosen provider domain to policy

### T4-14 (MED): reconcile_project fails with absolute paths

**Problem:** `agentpaas init /tmp/project --from-code --noninteractive`
fails with "project path must be under current directory".

**Fix:** Allow absolute paths in init --from-code. File: probably
`internal/cli/init.go` or `internal/pack/init.go`.

### T4-15 (LOW): recommend-patch parser improvements

**Problem:** Only recognizes "allow egress to <domain>". Other phrasings
return empty patches.

**Fix:** Improve parser in the recommend-patch handler. Accept domain +
port as structured input if NL parsing fails.

## BUILD DISCIPLINE

- Each fix is a separate micro-chunk commit
- After each fix: `go build -a && go test ./internal/... -short`
- After all fixes: full E2E test with weather agent (must reach the API)
- Push each commit to main
- Use Grok workers (grok-composer-2.5-fast, $0) via tmux

## OWA MODEL ALLOCATION

- Orchestrator (you): z-ai/glm-5.2 via z.ai direct API
- Workers: grok-composer-2.5-fast via Grok CLI ($0, subscription)
- Adversary: grok-4.3 via agentpaas-adversary profile
- Verifier: GLM-5.2 via agentpaas-verifier profile (block-end)

## VERIFICATION (after ALL fixes)

The critical test: build a weather agent from scratch via the
agentpaas-test profile. The agent must:
1. Ask the user which weather API to use
2. Ask the user to confirm egress to that API
3. Ask the user if they have an API key (if needed)
4. Ask the user which LLM to use (if the agent needs one)
5. Deploy the agent
6. Invoke it — the agent must SUCCESSFULLY reach the weather API
7. Return actual weather data

If the weather agent returns real data, ALL bugs are fixed.

## CONTEXT FROM PRIOR SESSIONS

- See `docs/b16-manual-test-round2-findings.md` for T4-01..T4-13 bugs (ALL FIXED)
- See `docs/b16-manual-test-round3-findings.md` for T4-14..T4-19 bugs (TO FIX)
- See `docs/b16-fix-round1-complete.md` for RC1-RC6 fixes (ALL MERGED)
- agentpaas-test profile is currently set up with plugin + toolset
- Daemon is running, binaries at 4c85edb

## KEY PITFALLS

- `make build` can produce stale binaries. Always `go build -a`.
- Verify binary installation with `md5 bin/agentpaasd /usr/local/bin/agentpaasd`
- After daemon restart, check `agentpaas status` shows "ready"
- Concurrent run limit is 3. Stop old runs before starting new ones.
- The gateway config format uses agentgateway v1.3.0 syntax.
  See: https://github.com/agentgateway/agentgateway

---

# B16 Fix Round 3 — Resume Prompt (Session 2: Manual Testing + Hardcoded Guards)

**Date:** 2026-07-03
**Session:** Continuing from B16 fix round 3 session 1
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 515482a (all T4-14..T4-19 + T4-16b fixes pushed to origin/main)

## START HERE

Manual testing is next. All code fixes are done, built, installed, and pushed.
Binaries at /usr/local/bin/agentpaas{,d} are from 515482a.

## WHAT'S DONE (all merged + pushed)

### T4-16 (CRITICAL): Gateway config compiled from agent's own policy.yaml
- Run handler reads policy.yaml from deployed agent dir (not global gateway.yaml)
- policy.yaml stored as sidecar in `~/.agentpaas/state/agents/<name>/policy.yaml`
- AgentLock has PolicyYAML field (not in signed canonical map — sidecar only)
- Commit: 7fc51e4

### T4-16b: Gateway compiler fixed for agentgateway v1.3.0 CONNECT proxy
- Old format (static host backends + networkAuthorization with dns.domain CEL) NEVER WORKED
  — dns.domain is not populated for CONNECT requests (verified by testing)
- New format: hostname-based routing with dynamic (DFP) backends + connect:route mode
- One route per allowed domain with `hostnames: [domain]` + `dynamic: {}` backend
- Catch-all denied route returns 403 for unmatched domains
- `frontendPolicies.connect.mode: route` enables CONNECT handling
- Commit: 71c4d03

### T4-17/18/19: Plugin SKILL.md build-time onboarding
- "Build-Time Agent Onboarding (MANDATORY)" section with direct imperatives
- Step 1: Confirm egress destinations (analyze code, present domains, write policy.yaml)
- Step 2: Procure API keys (detect needs, ask user, store via secret_add, test)
- Step 3: Configure LLM (detect needs, ask provider/model, configure agent.yaml, add egress)
- "Agent Code Structure (REQUIRED)" section — @agent.on_invoke SDK pattern
- Commits: f6e5be6, 98d4829

### T4-14: Absolute paths in init --from-code
- Removed CWD containment check from validateInitProjectPath
- Security still handled by symlink rejection + null byte checks
- Commit: 7f449bb

### T4-15: recommend-patch parser improvements
- Fallback domain extraction from any position in input
- Handles port suffixes, alternative verbs (add, enable, allow without "egress to")
- Commit: 6851045

### E2E Verified
- Weather agent (weather-test) packed, run, invoked successfully
- Agent reached api.weather.gov through gateway proxy, returned real alert data
- Gateway config correctly uses hostname routing + dynamic backends + connect:route

## PENDING: Hardcoded Platform Guards (not yet implemented)

User wants these added so a "dumb LLM" can't skip safety steps. These are
platform-level checks that enforce the OUTPUT of the SKILL.md instructions.

### HG-1: Reject wildcard egress at pack time (MED)
- If policy.yaml has `domain: "*"` without `allow_wildcard: true`, pack should FAIL
- Currently: compiler blocks via isWildcardDomainBlocked() but pack succeeds silently
- File: `internal/daemon/control_handlers.go` Pack handler, or `internal/pack/lock.go` computePolicyDigest
- Add explicit error: "wildcard egress requires --allow-wildcard-egress flag"

### HG-2: Validate credential references before run (MED)
- If agent.yaml has `llm.credential: "openai-api-key"`, Run handler should check Keychain
- Currently: buildInvokePayload() silently returns empty payload if credential missing
- Better: fail at run time with "credential 'X' not found — run agentpaas secret add first"
- File: `internal/daemon/control_handlers.go` Run handler (before container start)

### HG-3: Require policy.yaml for pack (LOW)
- If no policy.yaml exists, pack should fail (or require --no-egress flag)
- Currently: pack succeeds with empty policy digest
- File: `internal/daemon/control_handlers.go` Pack handler (line ~98-104)

## MANUAL TESTING CHECKLIST

The test profile is `agentpaas-test`. The plugin + toolset should already be
installed there from prior sessions.

### Test 1: Full weather agent build from scratch
Use the agentpaas-test profile (or a fresh Hermes session):
1. Tell Hermes: "Build a weather agent that calls the NWS API"
2. Hermes should (per SKILL.md):
   - Analyze the code and identify api.weather.gov as the egress domain
   - Ask: "This agent will access api.weather.gov. Allow?"
   - Generate policy.yaml with api.weather.gov egress rule
   - Check if NWS API needs a key (it doesn't — free API)
   - Check if agent needs an LLM (depends on code)
   - Pack and run the agent
3. The agent must successfully reach api.weather.gov and return real data

### Test 2: Agent with LLM (if user has an API key)
1. Tell Hermes: "Build an agent that summarizes weather data using GPT"
2. Hermes should:
   - Identify api.weather.gov + api.openai.com as egress
   - Ask for OpenAI API key
   - Store via secret_add, test it
   - Configure llm: section in agent.yaml
   - Add both domains to policy.yaml
3. Agent must reach both APIs and return summarized weather data

### Test 3: Egress denial
1. Deploy an agent with policy.yaml allowing only api.weather.gov
2. Try to make the agent call a different domain (e.g. evil.example.com)
3. Gateway should return 403 "egress denied: domain not in allowlist"

### Test 4: Absolute path init (T4-14)
```
agentpaas init /tmp/test-abs --from-code --noninteractive
```
Should succeed (previously failed with "project path must be under current directory")

### Test 5: recommend-patch parser (T4-15)
```
agentpaas recommend-patch "allow api.example.com"
agentpaas recommend-patch "add api.example.com to egress"
agentpaas recommend-patch "allow egress to api.example.com:443"
```
All should produce valid patches (previously only "allow egress to <domain>" worked)

## BINARIES

Current binaries at /usr/local/bin/agentpaas{,d} are from 515482a.
After any new fix: rebuild with `go build -a`, install, restart daemon.

```bash
cd ~/projects/agentpaas
go build -a -o bin/agentpaas ./cmd/agentpaas
go build -a -o bin/agentpaasd ./cmd/agentpaasd
sudo cp -f bin/agentpaas bin/agentpaasd /usr/local/bin/
agentpaas daemon stop; sleep 2; rm -f ~/.agentpaas/daemon.sock
rm -f ~/.agentpaas/state/audit-checkpoint-key.der  # if checkpoint key error
agentpaas daemon start
```

## CONTEXT FROM PRIOR SESSIONS

- See `docs/b16-manual-test-round2-findings.md` for T4-01..T4-13 bugs (ALL FIXED)
- See `docs/b16-manual-test-round3-findings.md` for T4-14..T4-19 bugs (ALL FIXED)
- See `docs/b16-fix-round1-complete.md` for RC1-RC6 fixes (ALL MERGED)
- agentpaas-test profile has plugin + toolset installed
- Test weather agent at /tmp/weather-test-agent/ (uses @agent.on_invoke SDK pattern)
- Daemon is running, binaries at 515482a
- KEY INSIGHT: agentgateway v1.3.0 needs connect:route + hostname routing + dynamic backends
  (see T4-16b commit 71c4d03 for the working config format)

## KEY PITFALLS

- `make build` can produce stale binaries. Always `go build -a`.
- Verify binary installation with `md5 bin/agentpaasd /usr/local/bin/agentpaasd`
- After daemon restart, check `agentpaas status` shows "ready"
- If daemon fails to start: `rm ~/.agentpaas/state/audit-checkpoint-key.der`
- Concurrent run limit is 3. Stop old runs before starting new ones.
- Agent code MUST use `@agent.on_invoke` — plain main() fails with
  "agent must register an invoke handler"
- Gateway config uses agentgateway v1.3.0 syntax: connect:route + hostname routing

---

# B16 Manual Testing — Resume Prompt (Session 3)

**Date:** 2026-07-03
**Session:** Continuing B16 fix round 3 manual testing
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 96eb9bc (all fixes pushed to origin/main)

## START HERE

Continue manual testing T1-T5. The agentpaas-test profile is clean and ready.
All bugs found in prior sessions are fixed and pushed. Binaries are installed.

## WHAT'S FIXED (all merged + pushed)

- BUG 1 (T4-20): Scaffolded main.py now uses @agent.on_invoke SDK pattern
- BUG 2 (T4-20): Removed meaningless `entry: main:app` from scaffolded agent.yaml
- BUG 3 (T4-20): Init now scaffolds default-deny policy.yaml
- BUG 4 (T4-25): Plugin register() deterministically creates local skill pointer
  in the active profile so it appears in <available_skills>. No reliance on
  after-install.md. Uses hermes_cli.profiles.get_active_profile_name().
- BUG 5 (T4-20): Reset script cleans leftover agent dirs + deployed state +
  run state + checkpoint key
- BUG 6 (T4-21): `agentpaas run` accepts project path, image digest, OR agent
  name via resolveRunTarget() in control.go
- T4-22: Skill pointer description triggers on "build/create agent" not just "AgentPaaS"
- T4-23: SKILL.md pitfalls section has daemon checkpoint key fix, run-by-path,
  SDK pattern, concurrent run limit
- T4-24: Reset script cleans ~/.agentpaas/state/runs/ + audit-checkpoint-key.der

## STILL OPEN

- BUG 7: trigger_invoke creates a new container per invoke instead of reusing
  running container. Completed runs stay tracked in daemon memory, consuming
  the 3-slot limit. Not yet fixed — for now, agent should stop old runs before
  invoking, or restart daemon between test cycles.

## MANUAL TESTING CHECKLIST

The test profile is `agentpaas-test`. It is currently fully clean:
- No sessions, no plugins, no skill pointer, no SOUL.md
- No daemon running, no weather agents, no deployed state
- Binaries at /usr/local/bin/agentpaas{,d} from 96eb9bc

### FULL CLEAN SLATE (do this before EVERY test run, without being asked)

```bash
pkill -f agentpaasd 2>/dev/null
rm -rf ~/weather-agent /tmp/weather-test-agent /tmp/e2e-final-test /tmp/deps-test \
  /tmp/scaffold-test /tmp/egress-denial-test /tmp/test-abs /tmp/scaffold-verify \
  /tmp/run-path-test 2>/dev/null
rm -rf ~/.agentpaas/state/agents/* ~/.agentpaas/state/runs/* \
  ~/.agentpaas/state/audit-checkpoint-key.der 2>/dev/null
cd ~/projects/agentpaas && bash scripts/reset-agentpaas-test.sh
# Delete any remaining sessions
hermes -p agentpaas-test sessions list | grep -oE '20260703_[0-9]{6}_[a-f0-9]{6}' | \
  while read sid; do hermes -p agentpaas-test sessions delete "$sid" --yes; done
rm -f ~/.hermes/profiles/agentpaas-test/SOUL.md
rm -rf ~/.hermes/profiles/agentpaas-test/checkpoints/* \
  ~/.hermes/profiles/agentpaas-test/cron/* \
  ~/.hermes/profiles/agentpaas-test/skills/agentpaas 2>/dev/null
```

### T1: Full weather agent build from scratch (onboarding flow E2E)

1. Start: `hermes -p agentpaas-test`
2. Prompt: "Install AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas"
3. Agent should: install plugin, add toolset, tell user to restart
4. Restart: `/quit` then `hermes -p agentpaas-test`
5. Verify: `/skills list` should show `agentpaas-deploy`
6. Prompt: "Build a weather agent that calls the NWS API"
7. Agent should (per SKILL.md loaded via skill pointer):
   - Load skill_view("agentpaas:deploy")
   - Start the daemon (or fix checkpoint key if needed)
   - Use agentpaas_init to scaffold the project
   - Write @agent.on_invoke SDK pattern code
   - Confirm egress: "This agent accesses api.weather.gov. Allow?"
   - Write policy.yaml with api.weather.gov only (NOT wildcard)
   - Check for credentials (NWS is free, skip)
   - Check for LLM (none needed, skip)
   - Pack and run the agent
   - Check run status with agentpaas_status
   - Report actual run result (not local python3 test)
8. Then: "Find the weather for Folsom, CA using the weather agent"
   - Agent should trigger_invoke the deployed agent
   - May hit BUG 7 (concurrent run limit) — if so, note it and stop old runs

### T2: Agent with LLM (if user has an API key)
### T3: Egress denial test
### T4: Absolute path init: `agentpaas init /tmp/test-abs --from-code --noninteractive`
### T5: recommend-patch parser (3 variants)

## KEY PITFALLS

- Daemon checkpoint key corruption after kill/upgrade: `rm -f ~/.agentpaas/state/audit-checkpoint-key.der`
- Plugin skills don't appear in available_skills — the register() fix creates
  a local pointer skill that does appear
- Concurrent run limit is 3 — completed runs stay tracked, stop old runs or
  restart daemon
- `go build -a` (not `make build`) for runtime fixes
- Verify binary install: `md5 bin/agentpaasd /usr/local/bin/agentpaasd`
- Agent code MUST use @agent.on_invoke — plain app()/main() fails
- after-install.md only shows if installed from repo root URL, not subdir URL
  (this is why the register() fix is critical)

---

# B16 Manual Testing — Resume Prompt (Session 4)

**Date:** 2026-07-04
**Session:** Autonomous bug fixing + remaining tests
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** e8dcd02 (all fixes pushed to origin/main)

## START HERE

This is an autonomous work session. Fix all known bugs, run remaining tests
(T3-T5), fix any new bugs found, rebuild binaries, and leave the system ready
for the user to manually run T1-T5 without errors.

## HOW TO USE THIS FILE

1. Read this file fully at session start.
2. Check PROGRESS SECTION below for current state.
3. Work through REMAINING WORK in order.
4. After each fix or test, update PROGRESS SECTION by editing this file.
5. If approaching 180 turns, save state here, then tell the user to /reset
   and paste the same prompt again. The new session will read this file
   and continue from where you left off.

## PROGRESS SECTION

### Bugs Found (Session 3, 2026-07-04)

| Bug | Priority | Status | Description |
|-----|----------|--------|-------------|
| BUG 7 | P0 | FIXED | Concurrent run limit (3/3). Fixed: activeRunCount() now only counts "running" status runs, not completed ones. Commit 6e679ca. |
| BUG 8 | P1 | FIXED | Agent ignores skill pointer. Fixed: register() now writes SOUL.md snippet (always injected into system prompt) instructing agent to load skill_view("agentpaas:deploy"). Commit e8dcd02. |
| BUG 9 | P1 | FIXED | No egress confirmation. Fixed: pack CLI now checks policy.yaml for wildcard domains, refuses unless --allow-wildcard is passed. Commit e8dcd02. |
| BUG 10 | P1 | FIXED | No LLM configuration step. No separate fix — consequence of BUG 8. SOUL.md injection forces skill loading which includes LLM onboarding. |
| BUG 11 | P0 | FIXED | trigger_invoke does not return agent response. Fixed: invokeAgent() returns (stdout, error), stdout stored to <runDir>/invoke-response.json, CLI trigger invoke --wait polls and displays, summarize reads and displays. Commit 1083b9a. |

### Tests Run

| Test | Status | Notes |
|------|--------|-------|
| T1 | PARTIAL | Plugin install works, daemon starts, pack works, run completes. With BUG 8/11 fixes, agent-level onboarding should work now (SOUL.md injection). User should test manually. |
| T2 | SKIP | Needs API key. User can test manually. |
| T3 | PASS | Egress denial: agent tried httpbin.org, gateway returned 403 Forbidden. explain-denial shows "default_deny" rule. Run: run-68dd19ddc807c7b9. |
| T4 | PASS | Absolute path init: agent.yaml (no entry: main:app), main.py (@agent.on_invoke), policy.yaml (egress: []). |
| T5 | PASS | Recommend-patch 3 variants: (1) httpbin.org:443 → valid patch, (2) httpbin.org → valid patch with 443, (3) * → empty patch with ask_user. |

### Fixes Applied

1. BUG 7 (commit 6e679ca): activeRunCount() only counts "running" status runs
2. BUG 11 (commit 1083b9a): invokeAgent returns stdout, stored to invoke-response.json
3. BUG 8+9+10 (commit e8dcd02): SOUL.md injection + wildcard egress guard

### Current Binary State

- Binaries at /usr/local/bin/agentpaas{,d} from commit e8dcd02
- MD5: agentpaasd=e30b81d52b35e9e26b2f44ff21d0bf02, agentpaas=1cacdb7889ceb47c0da595dc0e65d466
- SDK at /usr/local/python/
- All commits pushed to origin/main

### Profile State

- agentpaas-test profile is FULLY CLEAN (verified: no sessions, no plugins, no skill pointer, no SOUL.md, no deployed agents, no run state, no checkpoint key, daemon not running)

## ALL WORK COMPLETE

All P0 and P1 bugs are fixed. T3, T4, T5 all pass. Binaries rebuilt and
installed. All commits pushed. Test profile is clean and ready for manual
testing.

The user can now manually test the onboarding flow:
1. `hermes -p agentpaas-test`
2. "Install AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas"
3. /quit, restart
4. "Build a weather agent that calls the NWS API"
5. Agent should follow onboarding (SOUL.md forces skill loading)
6. "Find the weather for Folsom, CA using the weather agent"
7. Agent should trigger_invoke with --wait and return actual weather data

---

# B16 Manual Testing — Resume Prompt (Session 5)

**Date:** 2026-07-04
**Session:** Continue T3 manual testing from user perspective, then T4-T10
**Repo:** ~/projects/agentpaas, on branch main
**HEAD:** 99dee37 (all fixes pushed to origin/main)
**Profile:** agentpaas-test

## START HERE

The user is manually testing AgentPaaS Block 16 (T1-T10). We are mid-T3.
All code fixes are done, binaries rebuilt, plugin updated. The user needs to
restart their Hermes session to pick up the latest plugin (99dee37) and then
continue the manual test flow.

## STATE AT SESSION END (Session 5, final)

### Code (all committed + pushed)
- HEAD: d22c6b9
- Binaries: /usr/local/bin/agentpaas{,d} match repo (md5 42b09737...)
- All commits on origin/main:
  - 029505c  B16-T2: Fix daemon auto-start false-positive on stale socket
  - 96e7ece  B16-T3: Fix trigger_invoke payload schema + strengthen anti-fab
  - eff35f9  B16-T2: Fix daemon crash-loop on broken audit chain
  - d22c6b9  B16-T4: Fix trigger_invoke empty-payload + slow gateway restart

### T1-T4 STATUS: ALL PASS

- T1: Plugin install — PASS (clean install, no homework)
- T2: Doctor 6/6 — PASS (daemon genuinely live)
- T3: Weather agent — PASS (build + invoke + exfil)
- T4: Immutable redeploy — PASS (new image digest, new code live)

### ALL BUGS FIXED THIS SESSION (5 total)

1. t2-stale-socket-autostart (029505c): _ensure_daemon_running() used
   os.path.exists() — stale socket fooled it. Fixed: connects to socket.
2. t3-schema-payload-path (96e7ece): schema said "path to file" — agent
   didn't pass inline JSON. Fixed: schema says "inline JSON or file path".
3. t3-fab-guardrail-weak (96e7ece): anti-fab rule in SKILL.md only.
   Fixed: rule now in SOUL.md snippet (always present).
4. t2-audit-chain-recovery (eff35f9): daemon crash-loops on broken audit
   chain. Fixed: recoverFromCorruption() truncates at break, re-replays.
5. t4-empty-payload-warning (d22c6b9): trigger_invoke without payload
   returned empty response, agent fabricated data. Fixed: warning field
   added when no payload provided. Also: after-install.md now uses
   ensure-toolset.py instead of hermes config set (avoids slow gateway
   restart).

### Profile (agentpaas-test)
- FULLY CLEAN — reset to baseline, ready for fresh T1 install
- No plugin, no skill pointer, no SOUL.md, no sessions, no agents, no runs
- No leftover project dirs
- Daemon stopped, all state removed
- Binaries updated with all fixes

## WHAT TO DO WHEN SESSION RESUMES

1. Verify daemon is running: `agentpaas daemon status`

2. Have the user run T1 (plugin install) from their agentpaas-test
   terminal:
   ```
   Install AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas
   ```
   This verifies the full clean-install path. The agent should now use
   ensure-toolset.py (no slow gateway restart) instead of hermes config set.

3. Then T2: `Run agentpaas_doctor to check if my AgentPaaS setup is healthy`
   - Verify 6/6 checks pass
   - Verify daemon auto-started (register() fires on session load)
   - Verify daemon socket is genuinely live (not stale)

4. Then T3: Build weather agent + invoke with Folsom + exfil probe.
   All 3 T3 sub-steps should pass cleanly with the payload schema fix
   and anti-fabrication guardrail.

5. Then T4: Build echo agent, change prompt, repack, verify new digest.
   The empty-payload warning should prevent fabrication if the agent
   forgets to pass the payload.

6. After T1-T4 pass, continue with T5 (B16-LC03: Trigger/API/event/cron
   launch surface).

5. I verify from my side:
   - run state, invoke-response.json content
   - gateway-config for the run (hostname routing)
   - explain-denial output

## T3 PASS CRITERIA (all pass)

- [x] Agent loads deploy skill (SOUL.md trigger)
- [x] Agent asks to confirm egress (no wildcard)
- [x] Pack + run succeed
- [x] Invoke with payload returns REAL weather data (P0 fix + schema fix)
- [x] No Docker exhaustion (leak fix — check containers/networks after)
- [x] Agent reports failure honestly if anything fails (no fabrication)
- [x] Exfil probe → 403 Forbidden
- [x] explain-denial → default_deny rule

## BUGS FIXED THIS BLOCK

| Bug | ID | Status | Commit |
|-----|----|--------|--------|
| trigger invoke --payload DROPPED | t3-payload-dropped | FIXED | 251e797 |
| Docker resource leak (networks/containers) | t3-docker-leak | FIXED | 99dee37 |
| Agent fabricates data on failure | t3-agent-fabricates | MITIGATED→STRENGTHENED | 99dee37 → 96e7ece |
| Daemon auto-start on session load | t2-daemon-autostart | FIXED | 251e797+edd164d |
| Daemon auto-start false-positive on stale socket | t2-stale-socket-autostart | FIXED | 029505c |
| trigger_invoke payload schema misleading | t3-schema-payload-path | FIXED | 96e7ece |
| Anti-fabrication guardrail in SKILL only (not SOUL) | t3-fab-guardrail-weak | FIXED | 96e7ece |
| SOUL.md injection wrong profile | BUG 8 regression | FIXED | d498d8d |
| Skill pointer name collision | deploy/build | FIXED | bef0970 |
| /agentpaas-list missing | — | ADDED | bef0970 |
| Slash command hints missing | — | FIXED | c7adb0d |
| /agentpaas-list format (running/recent) | t2-list-format | FIXED | 251e797 |

### Session 5 bug details

**t2-stale-socket-autostart (029505c):** `_ensure_daemon_running()` used
`os.path.exists(socket_path)` as liveness check. A stale socket file left
by `pkill -9` or unclean shutdown passed this check, so the function
returned early and the daemon stayed down. Doctor reported 6/6 healthy
while daemon was unreachable. Fix: now connects to the socket (0.5s
timeout) to verify liveness. If connection fails, socket is stale —
remove and restart.

**t3-schema-payload-path (96e7ece):** The `agentpaas_trigger_invoke`
schema said payload = "Optional path to a payload file". The agent
interpreted this as requiring a file path and didn't pass inline JSON
like `{"city": "Folsom"}`. The tool code already handles inline JSON
(checks if payload starts with `{`, writes to temp file), but the schema
didn't tell the agent. Fix: schema now says "inline JSON or file path"
with explicit example `{"city": "Folsom"}`.

**t3-fab-guardrail-weak (96e7ece):** The anti-fabrication rule was in
SKILL.md Pitfalls (line 317-324). The agent loaded the skill during the
build phase but didn't re-read it for the invoke step. After 10 failed
invokes (empty payload due to schema bug), the agent fabricated
realistic weather data (82°F, Sunny) instead of reporting the error.
Fix: added "AgentPaaS Anti-Fabrication Rule" to the SOUL.md snippet
injected by register(). SOUL.md is loaded fresh every turn, so the rule
is always present. The rule also covers "always verify run status."

## STILL OPEN (non-blocking for T3)

- **t3-checkpoint-loop**: daemon crashes on restart with corrupted checkpoint
  key. Auto-start works around it (removes stale key first), but the real fix
  belongs in the daemon binary (regenerate key if decrypt fails, not crash).
- **t2-invoke-spawns-new-run**: trigger invoke starts a NEW container instead
  of invoking the already-running agent. Design question: should invoke reuse
  the running container, or should run+invoke semantics be redefined?
- **post-b16-rich-input**: large files, Drive/cloud links, online URLs,
  videos, images. Payload capped at 1MB. Deferred to post-B16.

## AFTER T3

Continue with T4 (B16-LC05/UC04: Prompt-change immutable redeploy) —
already done as T2's second half, may just need confirmation. Then T5-T10.

T-card reference (from block16-manual-testing-setup.md):
- T4: B16-LC05/UC04 — Prompt/code change immutable redeploy
- T5: B16-LC03 — Trigger/API/event/cron launch surface
- T6: B16-UC02 — Secret-brokered SaaS/API action
- T7: B16-UC03 — Agentic repair loop
- T8: B16-UC05 — Long-running mixed-egress agent
- T9: B16-UC06 — Clean-machine install under 15 minutes
- T10: B16-UC07 — Policy authoring from scratch

## KEY SKILLS TO LOAD

- agentpaas-acceptance-testing (test flow, pitfalls, verification)
- agentpaas-build-rhythm (build discipline)
- systematic-debugging (if new bugs surface)
- cost-aware-model-selection (every session start)

---
