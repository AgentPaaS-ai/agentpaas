# Worker: B15-T01 MC4 — Wire Hermes plugin secret tools + Python tests

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc4` (create from main).
MC1-MC3 are merged: `agentpaas secret add/list/remove/rotate/test` all work.

## Scope (ONE micro-chunk)
Add 5 plugin tools to the Hermes plugin that wrap the new secret CLI commands:
- `agentpaas_secret_add` — stores a credential (prompts for value via stdin)
- `agentpaas_secret_list` — lists secrets by label, never value
- `agentpaas_secret_remove` — removes a credential
- `agentpaas_secret_rotate` — replaces a credential (prompts for new value)
- `agentpaas_secret_test` — validates a credential before deployment

## Files to edit

### 1. `integrations/hermes-plugin/plugin.yaml` — register new tools

Add to the `provides_tools:` list:
```yaml
  - agentpaas_secret_add
  - agentpaas_secret_list
  - agentpaas_secret_remove
  - agentpaas_secret_rotate
  - agentpaas_secret_test
```

### 2. `integrations/hermes-plugin/tools.py` — implement 5 tool functions

Add these functions at the end of the file, before any `register` call if
there is one (or just at the natural end of the function definitions). Follow
the EXACT pattern of existing tools (see `agentpaas_doctor`, `agentpaas_pack`).

CRITICAL for `secret_add` and `secret_rotate`: they need stdin to pass the
secret value. The existing `_run_cli` uses `subprocess.run(..., capture_output=True)`
which does NOT support stdin. You need a variant that accepts stdin input.

Add a helper `_run_cli_with_stdin(cmd_args, stdin_input)`:
```python
def _run_cli_with_stdin(cmd_args, stdin_input):
    """Run agent CLI with stdin input (for secret add/rotate). Returns same dict as _run_cli."""
    if _needs_daemon(cmd_args):
        sock_available, sock_err = _check_daemon_socket()
        if not sock_available:
            return sock_err
    binary = _resolve_agent_binary()
    full = [binary, "--json"]
    sock = _resolve_socket_path()
    if sock:
        full.extend(["--socket", sock])
    home = os.environ.get("AGENTPAAS_HOME")
    if home:
        full.extend(["--home", home])
    full.extend([a for a in cmd_args if a])
    timeout = _get_cli_timeout()
    proc = subprocess.run(
        full, capture_output=True, text=True, timeout=timeout,
        input=stdin_input,
    )
    # Same result handling as _run_cli — factor out or duplicate the
    # stdout/stderr truncation + JSON parsing + sanitizer logic.
    # For simplicity, you can call _parse_cli_result(proc) if you factor
    # that out, or just duplicate the ~30 lines from _run_cli.
```

IMPORTANT: Factor out the result-parsing logic from `_run_cli` into a
`_parse_cli_result(proc)` helper so both `_run_cli` and `_run_cli_with_stdin`
use the same parsing. Do NOT duplicate the sanitizer/truncation logic.

Tool functions:

```python
def agentpaas_secret_add(args, **kwargs):
    """Store a credential in macOS Keychain. Value passed via 'value' arg."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    value = args.get("value", "")
    if not value:
        return json.dumps({"error": "value is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli_with_stdin(["secret", "add", name], value)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_list(args, **kwargs):
    """List stored credentials by label (never by value)."""
    args = args or {}
    try:
        result = _run_cli(["secret", "list"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_remove(args, **kwargs):
    """Remove a stored credential."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli(["secret", "remove", name])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_rotate(args, **kwargs):
    """Replace a credential with a new value (atomic). New value via 'value' arg."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    value = args.get("value", "")
    if not value:
        return json.dumps({"error": "value is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli_with_stdin(["secret", "rotate", name], value)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_test(args, **kwargs):
    """Validate a credential by making a trivial authenticated call to the provider."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    provider = args.get("provider", "")
    cmd_args = ["secret", "test", name]
    if provider:
        cmd_args.extend(["--provider", provider])
    try:
        result = _run_cli(cmd_args)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})
```

SECURITY: The `value` parameter for `secret_add` and `secret_rotate` is passed
through stdin to the CLI. The secret value must NEVER appear in:
- The JSON result returned by the tool
- Process arguments (argv) — it goes through stdin, NOT argv
- Log output

The `_run_cli_with_stdin` helper must use `input=value` (stdin), NOT append
the value to the command args.

### 3. Tests: `integrations/hermes-plugin/tests/test_secret_tools.py`

Use the `_load_plugin_package()` pattern from `test_plugin_skeleton.py`.
Mock `_run_cli` and `_run_cli_with_stdin` to test each tool function.

Tests:
1. `test_secret_add_calls_cli_with_stdin` — mock `_run_cli_with_stdin`, call
   `agentpaas_secret_add({"name": "mykey", "value": "secret123"})`, assert
   the mock was called with `["secret", "add", "mykey"]` and `"secret123"`.
2. `test_secret_add_requires_name` — call without name, assert error.
3. `test_secret_add_requires_value` — call without value, assert error.
4. `test_secret_add_never_passes_value_in_argv` — mock
   `_run_cli_with_stdin`, verify the value is NOT in the cmd_args list (it
   goes through stdin input, not argv).
5. `test_secret_list_calls_cli` — mock `_run_cli`, call
   `agentpaas_secret_list({})`, assert called with `["secret", "list"]`.
6. `test_secret_remove_calls_cli` — mock `_run_cli`, call
   `agentpaas_secret_remove({"name": "mykey"})`, assert called with
   `["secret", "remove", "mykey"]`.
7. `test_secret_rotate_calls_cli_with_stdin` — mock `_run_cli_with_stdin`,
   call `agentpaas_secret_rotate({"name": "mykey", "value": "newval"})`,
   assert called with `["secret", "rotate", "mykey"]` and `"newval"`.
8. `test_secret_test_calls_cli_with_provider` — mock `_run_cli`, call
   `agentpaas_secret_test({"name": "openai-key", "provider": "openai"})`,
   assert called with `["secret", "test", "openai-key", "--provider", "openai"]`.
9. `test_secret_test_without_provider` — call without provider, assert
   called with just `["secret", "test", "mykey"]` (no --provider flag).
10. `test_secret_tools_registered_in_manifest` — load plugin.yaml, assert
    all 5 tool names are in `provides_tools`.
11. `test_secret_add_result_never_contains_value` — mock
    `_run_cli_with_stdin` to return a result dict, call `agentpaas_secret_add`
    with a value, assert the returned JSON does NOT contain the value string.

## Constraints
- Do NOT modify existing tool functions.
- The `_run_cli_with_stdin` helper must use `subprocess.run(..., input=value)`,
  NOT append the value to cmd_args.
- Follow the EXACT error envelope pattern: `{"error": "...", "error_category": "..."}`
- The secret value must never appear in argv or returned JSON.
- Run the Python plugin tests:
  ```bash
  cd ~/projects/agentpaas
  python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v 2>&1 | tail -20
  ```
  All tests (existing + new) must pass.
- `make lint` (Go) doesn't cover Python — just ensure the Python tests pass.

## Commit
`feat(plugin): wire 5 secret onboarding tools to Hermes plugin (B15-T01 MC4)`

Do NOT push.