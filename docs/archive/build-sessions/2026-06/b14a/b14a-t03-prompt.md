# Task: 14A-T03 — Subprocess output cap + configurable timeout (GAP-3, MEDIUM)

## Context

`_run_cli()` in `integrations/hermes-plugin/tools.py` (around line 296) uses:
```python
proc = subprocess.run(full, capture_output=True, text=True, timeout=300)
```

Problems:
1. **300s timeout is hardcoded** — no way to configure or shorten. A hung CLI command
   blocks the Hermes session for 5 minutes.
2. **No output size cap** — `capture_output=True` captures ALL stdout/stderr into memory.
   If the CLI returns valid JSON larger than 50KB, it passes through untruncated.
3. **No stderr size cap** — a noisy CLI could OOM the plugin process.

The execution plan says "tool output >50KB → truncated." The plugin currently truncates
only on JSONDecodeError (raw_output_truncated: proc.stdout[:2000]).

## What to implement

In `integrations/hermes-plugin/tools.py`, function `_run_cli`:

### 1. Configurable timeout via env var

```python
def _get_cli_timeout():
    """Get CLI timeout from AGENTPAAS_CLI_TIMEOUT env var, with bounds.
    Default: 300s. Min: 10s. Max: 600s.
    """
    default = 300
    env_val = os.environ.get("AGENTPAAS_CLI_TIMEOUT", "")
    if not env_val:
        return default
    try:
        timeout = int(env_val)
    except (ValueError, TypeError):
        return default
    # Clamp to [10, 600]
    return max(10, min(600, timeout))
```

### 2. Output size caps

```python
_STDOUT_CAP = 51200  # 50KB
_STDERR_CAP = 10240  # 10KB
```

### 3. Updated `_run_cli` function

Replace the current `_run_cli` with:

```python
def _run_cli(cmd_args):
    """Run agent CLI with --json; return sanitized operator dict."""
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
    proc = subprocess.run(full, capture_output=True, text=True, timeout=timeout)
    
    # Cap output sizes
    stdout_truncated = False
    stderr_truncated = False
    stdout_size = len(proc.stdout) if proc.stdout else 0
    stderr_size = len(proc.stderr) if proc.stderr else 0
    
    stdout = proc.stdout
    if stdout_size > _STDOUT_CAP:
        stdout = proc.stdout[:_STDOUT_CAP]
        stdout_truncated = True
    stderr = proc.stderr
    if stderr_size > _STDERR_CAP:
        stderr = proc.stderr[:_STDERR_CAP]
        stderr_truncated = True
    
    if proc.returncode != 0:
        result = {
            "error": stderr.strip(),
            "exit_code": proc.returncode,
            "error_category": "cli_error",
        }
        if stdout_truncated:
            result["output_truncated"] = True
            result["output_size"] = stdout_size
        if stderr_truncated:
            result["stderr_truncated"] = True
            result["stderr_size"] = stderr_size
        return result
    try:
        result = json.loads(stdout)
    except json.JSONDecodeError:
        result = {
            "error": f"CLI returned non-JSON output (length {stdout_size})",
            "raw_output_truncated": stdout[:2000],
            "exit_code": proc.returncode,
            "error_category": "cli_non_json_output",
        }
        if stdout_truncated:
            result["output_truncated"] = True
            result["output_size"] = stdout_size
        return result
    if _sanitizer and isinstance(result, dict):
        result = _sanitizer.sanitize_response(result)
    # Add truncation metadata to successful results
    if isinstance(result, dict) and (stdout_truncated or stderr_truncated):
        if stdout_truncated:
            result["output_truncated"] = True
            result["output_size"] = stdout_size
        if stderr_truncated:
            result["stderr_truncated"] = True
            result["stderr_size"] = stderr_size
    return result
```

## Tests to write

Create `integrations/hermes-plugin/tests/test_output_cap.py`:

1. **test_default_timeout:** No env var set → timeout is 300
2. **test_custom_timeout:** AGENTPAAS_CLI_TIMEOUT=60 → timeout is 60
3. **test_timeout_min_clamp:** AGENTPAAS_CLI_TIMEOUT=5 → timeout is 10 (clamped)
4. **test_timeout_max_clamp:** AGENTPAAS_CLI_TIMEOUT=999 → timeout is 600 (clamped)
5. **test_invalid_timeout:** AGENTPAAS_CLI_TIMEOUT="abc" → timeout is 300 (default)
6. **test_stdout_truncation:** Mock subprocess.run to return 100KB stdout. Verify
   result has output_truncated=True, output_size=102400, and the parsed JSON is
   truncated to 50KB
7. **test_stderr_truncation:** Mock subprocess.run to return 20KB stderr with non-zero
   exit code. Verify result has stderr_truncated=True, stderr_size=20480
8. **test_no_truncation_for_small_output:** Mock subprocess.run to return 1KB stdout.
   Verify no truncation flags
9. **test_non_json_output_with_truncation:** Mock subprocess.run to return 100KB
   non-JSON stdout. Verify raw_output_truncated is set AND output_truncated=True
10. **test_successful_json_with_truncation:** Mock subprocess.run to return 100KB valid
    JSON. Verify result is parsed JSON + output_truncated=True

Use `unittest.mock.patch` to mock `subprocess.run`. Create a mock `CompletedProcess`
object with stdout/stderr/returncode attributes.

## Testing

```bash
cd /Users/pms88/projects/agentpaas
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
```

All 135 existing tests + 10 new tests must pass (145 total).

## Commit message

```
feat(14a-t03): subprocess output cap + configurable timeout (GAP-3)

Add AGENTPAAS_CLI_TIMEOUT env var (default 300s, clamped to [10, 600]).
Cap stdout at 50KB, stderr at 10KB. Add output_truncated/output_size
metadata to results. Prevents memory exhaustion from noisy CLI output
and allows shorter timeouts for faster failure detection.

10 new tests in test_output_cap.py.
```

## Branch

Create branch `feat/b14a-t03` from main.
