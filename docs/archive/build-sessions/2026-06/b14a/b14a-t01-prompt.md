# Task: 14A-T01 — Plugin path allow-list (GAP-1, HIGH severity)

## Context

AgentPaaS Hermes plugin (`integrations/hermes-plugin/tools.py`) passes `project_dir`
from the model directly to the CLI via `_run_cli(["init", project_dir, ...])` with
zero validation. A prompt injection could instruct the model to pass `/etc` or `../../`
as project_dir, reaching the daemon Pack/Init handlers.

The Go daemon side (`operator_path_boundary_b11t06_test.go`) has path boundary tests
that reject symlinks and absolute paths like `/etc/passwd`. But the Python plugin layer
does NO pre-validation — it relies entirely on the Go daemon to refuse. The plugin is
the last line of defense and it is currently absent.

## What to implement

Add a `_validate_project_path(path)` function in `integrations/hermes-plugin/tools.py`:

1. **Resolves to absolute path** via `os.path.realpath(path)` (follows symlinks).
2. **Rejects paths outside an allow-list:**
   - The invoking project root: detected via `AGENTPAAS_PROJECT_ROOT` env var, or
     `os.getcwd()` if not set.
   - `/tmp` (for e2e test agents) — specifically `/tmp` and subdirectories under it.
   - `$HOME` — for user agent projects.
3. **Rejects paths containing `..` after resolution** (defensive — realpath should
   already resolve these, but check explicitly).
4. **Called before every `_run_cli` that takes a project_dir parameter.** Specifically
   in these functions:
   - `agentpaas_init_project` (line ~339)
   - `agentpaas_reconcile_project` (line ~359)
   - `agentpaas_validate_project` (line ~377)
   - `agentpaas_pack` (line ~398)
   - `agentpaas_policy_show` (when project_dir is passed, line ~505)
5. **Returns a structured error** on rejection:
   ```python
   {"error": "path rejected: <reason>", "error_category": "path_rejected"}
   ```
   The caller should return this as JSON immediately (do not call _run_cli).
6. **Special case:** `project_dir == "."` means "current directory" — resolve it to
   `os.getcwd()` and validate that. Do not reject `.` as invalid.

## Implementation details

```python
def _validate_project_path(path):
    """Validate a project path is within allowed directories.
    
    Returns (is_valid, resolved_path, error_dict).
    - is_valid: True if path is allowed
    - resolved_path: the realpath() of the input (for use in _run_cli)
    - error_dict: None if valid, else {"error": ..., "error_category": "path_rejected"}
    """
    if not path or not isinstance(path, str):
        return False, None, {
            "error": "project_dir is required",
            "error_category": "path_rejected",
        }
    
    # Handle "." (current directory)
    if path == ".":
        resolved = os.path.realpath(os.getcwd())
    else:
        resolved = os.path.realpath(path)
    
    # Check for .. after resolution (defensive — realpath resolves these)
    if ".." in resolved.split(os.sep):
        return False, None, {
            "error": f"path contains directory traversal: {path}",
            "error_category": "path_rejected",
        }
    
    # Build allow-list
    project_root = os.environ.get("AGENTPAAS_PROJECT_ROOT", "")
    if not project_root:
        project_root = os.getcwd()
    project_root = os.path.realpath(project_root)
    
    allowed_roots = [
        project_root,
        "/tmp",
        os.path.expanduser("~"),
    ]
    
    # Check if resolved path is under any allowed root
    is_allowed = False
    for root in allowed_roots:
        root_resolved = os.path.realpath(root)
        # Ensure path is under root (path == root or path starts with root + /)
        if resolved == root_resolved or resolved.startswith(root_resolved + os.sep):
            is_allowed = True
            break
    
    if not is_allowed:
        return False, None, {
            "error": f"project_dir outside allowed roots: {path} (resolved: {resolved})",
            "error_category": "path_rejected",
        }
    
    return True, resolved, None
```

In each tool function that takes `project_dir`, add validation before `_run_cli`:

```python
def agentpaas_init_project(args, **kwargs):
    args = args or {}
    project_dir = args.get("project_dir", ".")
    is_valid, resolved, err = _validate_project_path(project_dir)
    if not is_valid:
        return json.dumps(err)
    try:
        result = _run_cli(
            ["init", resolved, "--noninteractive", "--runtime", args.get("runtime", "python")]
        )
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})
```

**IMPORTANT:** Use `resolved` (the validated path) in the `_run_cli` call, not the
original `project_dir`. This prevents TOCTOU: if the model passes a symlink, we use the
real path the symlink resolves to.

## Tests to write

Create `integrations/hermes-plugin/tests/test_path_allowlist.py` with these test cases:

1. **test_valid_project_dir:** path under project root → valid
2. **test_valid_dot:** `.` → resolves to cwd, valid if cwd is under allowed root
3. **test_valid_tmp:** `/tmp/test-agent` → valid
4. **test_valid_home:** `~/my-agent` → valid
5. **test_reject_etc_passwd:** `/etc/passwd` → rejected with path_rejected
6. **test_reject_parent_traversal:** `../../etc` → rejected
7. **test_reject_absolute_outside:** `/var/log` → rejected (not under any allowed root)
8. **test_reject_symlink_escape:** create symlink in project root pointing to /etc,
   pass symlink path → rejected (resolved path is /etc, not under allowed root)
9. **test_reject_empty:** empty string → rejected
10. **test_reject_none:** None → rejected
11. **test_init_project_rejects_bad_path:** call `agentpaas_init_project` with `/etc`
    → returns JSON with error_category: "path_rejected", does NOT call _run_cli
12. **test_pack_rejects_bad_path:** call `agentpaas_pack` with `/etc` → same
13. **test_validate_rejects_bad_path:** call `agentpaas_validate_project` with `/etc` → same

Use the `_load_plugin_package()` helper from `test_plugin_skeleton.py` to load the plugin.
Mock `_run_cli` to ensure it's NOT called when path validation fails.

## Testing instructions

```bash
cd /Users/pms88/projects/agentpaas
# Add __init__.py to tests dir for unittest discover to work
touch integrations/hermes-plugin/tests/__init__.py
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
rm integrations/hermes-plugin/tests/__init__.py
```

All 109 existing tests must still pass, plus your new tests.

## Constraints

- Do NOT modify the Go daemon's path validation — this is Python-plugin-layer only.
- Do NOT strip the `.` special case — it's the default for most tool calls.
- Do NOT add any new dependencies.
- Do NOT change the _run_cli function signature.
- Match existing code style (no type hints beyond what's already there, 4-space indent).
- The `tests/__init__.py` file is needed for unittest discover to import the tests
  directory. Create it if it doesn't exist (but remove it before committing — the
  existing test pattern loads via `_load_plugin_package()` instead).

Wait — actually, CHECK: do the existing tests use `__init__.py` or not? If the existing
tests don't have `__init__.py` and still run fine, then follow that pattern. If they
need `__init__.py`, create it and commit it.

Run the existing tests first to see:
```bash
cd /Users/pms88/projects/agentpaas
touch integrations/hermes-plugin/tests/__init__.py
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin 2>&1 | tail -5
rm integrations/hermes-plugin/tests/__init__.py
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin 2>&1 | tail -5
```

If the second command (without __init__.py) fails, then __init__.py is needed and
should be committed. If both work, don't commit __init__.py.

## Commit message

```
feat(14a-t01): plugin path allow-list (GAP-1)

Add _validate_project_path() in tools.py that resolves symlinks and rejects
paths outside project root, /tmp, and $HOME. Called before every _run_cli
that takes a project_dir parameter. Defense-in-depth on top of Go daemon
path boundary checks.

13 new tests in test_path_allowlist.py covering: valid paths (project root,
/tmp, home, dot), rejected paths (/etc/passwd, ../../, /var/log, symlinks
escaping root), and integration tests verifying _run_cli is NOT called
when path validation fails.
```

## Branch

Create branch `feat/b14a-t01` from main. Commit to it.
