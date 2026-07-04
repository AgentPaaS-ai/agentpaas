# B16 Manual Test Round 2 — Bug Findings

**Date:** 2026-07-03
**Tester:** Hermes (GLM-5.2, agentpaas profile)
**Profile:** agentpaas-test (reset from baseline, plugin installed from GitHub)
**Binaries:** Fresh build from commit d5053d4 (RC3+RC4+RC5 fixes)

## Fixes Applied This Round (all committed + pushed)

| Fix | Priority | Commit | Description |
|-----|----------|--------|-------------|
| RC3 | P0 | c245d53 | Surface Docker build errors instead of swallowing them (io.Copy→JSON decoder) |
| RC3 | P0 | a1d13e2 | Increase pack CLI timeout 30s→5min |
| RC4 | P0 | 362df9d | resolveSDKDir() for binary installs (find /usr/local/python) |
| RC5 | P0 | d5053d4 | Fix ExecWithStdin corrupting payload with multiplexed frame header |

## Test Results Summary

| Test | Result | Notes |
|------|--------|-------|
| T1a: Plugin install from GitHub | PASS | Tools work, doctor returns 6/6 healthy |
| T1b: Onboarding (plugin disabled) | SKIP | Requires separate clean profile. Known gap. |
| T2: Policy schema (CLI) | PASS (with bugs) | All 4 templates scaffold+validate. policy show broken. |
| T3: E2E lifecycle | PASS | init→policy→validate→pack→run→invoke→timeline all work |
| T3: Cron add/list/remove | PASS | Works via CLI |
| T3: Secret add/list/remove | PASS | Works via CLI |
| T3: LLM configure | PASS (cosmetic) | Config written but leaves duplicate commented-out lines |
| T3: Audit query/export | PASS | Works via `audit query` and `audit export --output` |

## New Bugs Found

### BUG-T4-01 (MEDIUM): `agentpaas policy show` returns stub error

**Command:** `agentpaas policy show <project_dir>`
**Expected:** Displays the policy.yaml contents from the project directory
**Actual:** `policy show: no active policy store in P1 stub`
**Root cause:** The `policy show` CLI command tries to query a remote/daemon
policy store instead of reading the local policy.yaml file. It's still using
P1 stub code that returns a placeholder error.
**Impact:** Users can't view their agent's policy via CLI. They must `cat
policy.yaml` manually.
**Likely fix:** Update `policy show` to read `<project_dir>/policy.yaml` when
no run_id is provided.

---

### BUG-T4-02 (LOW): `agentpaas logs` returns non-JSON output (plugin tool broken)

**Command:** `agentpaas logs <run_id>` (via plugin tool `agentpaas_logs`)
**Expected:** JSON array of log entries, parseable by the plugin
**Actual:** Returns newline-delimited JSON objects (not a JSON array). Plugin
tool reports: `CLI returned non-JSON output (length 637)`.
**Example output:**
```
{"fields":null,"level":"info","message":"...","run_id":"...","timestamp":"..."}
{"fields":null,"level":"info","message":"...","run_id":"...","timestamp":"..."}
```
**Root cause:** The CLI `logs` command outputs NDJSON (newline-delimited JSON)
or pretty-printed lines, but the plugin tool's `_run_cli` wrapper expects a
single JSON document. Either the CLI needs a `--json` flag that wraps output
in an array, or the plugin tool needs to handle NDJSON.
**Impact:** The agent can't read agent logs through the plugin tool. Must use
raw CLI.
**Likely fix:** Add `--json` flag to `agentpaas logs` that outputs
`{"entries": [...]}`, or update the plugin tool to parse NDJSON.

---

### BUG-T4-03 (LOW): `agentpaas explain-failure` says "Run failed" for successful runs

**Command:** `agentpaas explain-failure <run_id>` on a completed (successful) run
**Expected:** "Run succeeded — no failure to explain" or similar
**Actual:** `Run <run_id> failed [agent_runtime_exception]: no failure events
found for run <run_id> → ask_user`
**Root cause:** The command doesn't check run status first. If no failure
events are found, it reports the run as "failed" regardless of actual status.
**Impact:** Misleading error message. User thinks a successful run failed.
**Likely fix:** Check run status first. If status=completed/succeeded, return
"Run succeeded — nothing to explain."

---

### BUG-T4-04 (LOW): `agentpaas_llm_configure` leaves duplicate commented-out lines

**Command:** `agentpaas_llm_configure` plugin tool
**Expected:** Clean agent.yaml with the llm: section written, old comments removed
**Actual:** Writes the new llm: section (lines 5-8) but leaves the old
commented-out template lines (9-11) as duplicates:
```yaml
llm:
  provider: openai
  model: gpt-4o
  credential: test-openai-key
#   provider: openai  # openai|anthropic|xai    ← stale duplicate
#   model: gpt-4o
#   credential: openai-key
```
**Root cause:** The tool writes the llm: section by finding the
`# llm:` commented-out block and inserting before it, but doesn't remove
the commented-out block.
**Impact:** Cosmetic — the active config is correct. But confusing for users
who see two llm sections.
**Likely fix:** Remove the commented-out template block when writing the
real llm: section.

---

### BUG-T4-05 (LOW): `requires_env: AGENTPAAS_SOCKET_PATH` in plugin.yaml is misleading

**File:** `integrations/hermes-plugin/plugin.yaml`
**Issue:** The plugin declares `requires_env: [AGENTPAAS_SOCKET_PATH]`, but
this environment variable is NOT required — the plugin auto-discovers the
socket via `~/.agentpaas/daemon.sock` fallback. The `requires_env` declaration
may cause Hermes to warn users about a missing required env var.
**Impact:** May confuse users into thinking they need to set
`AGENTPAAS_SOCKET_PATH` manually.
**Likely fix:** Remove `requires_env` from plugin.yaml, or change it to
`optional_env`.

---

### BUG-T4-06 (INFO): `/agentpaas-doctor` slash command referenced in install message but doesn't exist

**File:** `after-install.md` (shown after `hermes plugins install`)
**Issue:** The post-install message says "Then try: /agentpaas-doctor to
verify your setup is healthy." But there is no `/agentpaas-doctor` slash
command — the plugin provides tools (agentpaas_doctor), not slash commands.
**Impact:** User follows the instruction, gets "unknown command."
**Likely fix:** Update after-install.md to say 'Ask the agent: "Run
agentpaas_doctor"' instead of referencing a non-existent slash command.

---

### BUG-T4-07 (INFO): GitHub plugin install does not add toolset to platform_toolsets.cli

**Already documented** in acceptance-testing skill. The `hermes plugins install
--enable` command registers the plugin and its slash commands, but does NOT
add the `agentpaas` toolset to `platform_toolsets.cli`. The agent can see
slash commands in autocomplete but cannot call `agentpaas_*` tools.

**Workaround (documented in README + after-install.md):**
```bash
hermes -p <profile> config set platform_toolsets.cli '["terminal", "file", "web", "skills", "todo", "code_execution", "agentpaas"]'
```

**Root cause:** Hermes framework gap — `cmd_install()` and `cmd_enable()` in
`hermes_cli/plugins_cmd.py` do not call `_toggle_plugin_toolset()`. Only the
dashboard enable path does.

---

### BUG-T4-08 (INFO): `agentpaas run list` shows stale runs with "[succeeded]" status

**Command:** `agentpaas run list`
**Output:**
```
Active runs (2):
  run-08913aa1358b4771  [succeeded]
  run-ca0f28413d691f94  [succeeded]
```
**Issue:** Completed runs are listed under "Active runs" with status
"[succeeded]". This is technically correct (they are recent), but "Active
runs" implies currently running. The status filter may need adjustment.
**Impact:** Minor confusion — user sees completed runs under "Active."
**Likely fix:** Rename to "Recent runs" or filter to only show running/pending
by default with a `--all` flag for completed.

---

### BUG-T4-09 (MEDIUM): `agentpaas status <run_id>` shows "0 invocations" after successful invoke

**Command:** `agentpaas status run-824cf3dc063ad993` (after trigger invoke succeeded)
**Output:** `Summary: Run ... completed after 209ms, 0 invocations, 0 policy denials`
**Issue:** The invocation count is 0 even though `trigger invoke` was called
and the run completed successfully. The invoke count isn't being tracked or
isn't being reported correctly.
**Root cause:** Likely the `trigger invoke` creates a NEW run rather than
invoking within the existing run. Each `run` + each `trigger invoke` = separate
run with its own lifecycle. The "invocations" counter on the status may refer
to something else (auto-invoke count?).
**Impact:** User can't tell from status whether the agent was actually invoked.
**Likely fix:** Clarify semantics — either count trigger invokes in the
status, or document that `trigger invoke` creates a new run.

---

## Architecture Observations (not bugs)

1. **`trigger invoke` creates a new run, not an invoke within an existing run.**
   This is by design — each invocation is a fresh container lifecycle. The
   `run <agent_name>` command starts a run that auto-invokes once and exits.
   `trigger invoke` starts a completely new run that also auto-invokes. They
   are independent, not nested.

2. **Egress firewall is skipped on macOS (no iptables in Colima VM).** This is
   expected — the harness logs "egress firewall skipped (iptables not found)".
   Policy enforcement happens at the network layer (Docker network isolation),
   not via iptables on macOS.

3. **`agentpaas version` shows "Commit: unknown" and "Docker: unknown".** The
   binaries are built without ldflags for version injection. This is cosmetic
   but makes it hard to verify which commit is deployed.

## Recommendation

Fix BUG-T4-01 (policy show) and BUG-T4-02 (logs non-JSON) first — these are
the most user-facing. BUG-T4-03 (explain-failure on success) and BUG-T4-09
(invocation count) are next. The rest are cosmetic/info-level.
