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
