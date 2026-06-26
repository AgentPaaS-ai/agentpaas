# Task: 14A-T01 Adversary Fix — Harden env var overrides in path validation

## Context

Adversary review of `_validate_project_path()` found two valid findings:

1. **AGENTPAAS_PROJECT_ROOT override:** The env var is read directly and added to
   allowed_roots without sanitization. Setting it to "/" bypasses all checks.
2. **$HOME override:** `os.path.expanduser("~")` respects $HOME env var. An attacker
   who can set HOME=/etc adds /etc to allowed_roots.

The threat model: env vars are typically set by Hermes config, not the model. But
defense-in-depth dictates we shouldn't trust env overrides for security boundaries.

## What to fix

In `integrations/hermes-plugin/tools.py`, function `_validate_project_path`:

### Fix 1: Use pwd module for home directory instead of $HOME

Replace:
```python
allowed_roots = [
    project_root,
    "/tmp",
    os.path.expanduser("~"),
]
```

With:
```python
import pwd  # add at top of file

# Use pwd module, not $HOME env var, to prevent override attacks
try:
    home_dir = pwd.getpwuid(os.getuid()).pw_dir
except (KeyError, OSError):
    home_dir = os.path.expanduser("~")  # fallback

allowed_roots = [
    project_root,
    "/tmp",
    home_dir,
]
```

### Fix 2: Validate AGENTPAAS_PROJECT_ROOT is not a system root

After resolving project_root, add validation:
```python
# Validate project root is not a system directory (defense against env override)
_SYSTEM_DIRS = ("/", "/etc", "/usr", "/bin", "/sbin", "/var", "/sys", "/dev", "/proc")
if project_root in _SYSTEM_DIRS:
    project_root = os.getcwd()  # fall back to cwd if env var is suspicious
```

### Fix 3: Remove the dead ".." check (Finding 3, MEDIUM)

The check `if ".." in resolved.split(os.sep)` after `os.path.realpath()` is dead code
because realpath already resolves all `..` components. Remove it to avoid false sense
of security. The allowed_roots check is the real protection.

## Tests to add

Add these tests to `integrations/hermes-plugin/tests/test_path_allowlist.py`:

1. **test_reject_home_override:** Set HOME=/etc, verify /etc is still rejected
   (because we use pwd module, not $HOME)
2. **test_project_root_system_dir_fallback:** Set AGENTPAAS_PROJECT_ROOT="/",
   verify paths outside cwd are rejected (fallback to cwd)
3. **test_no_dead_traversal_check:** Verify that `../../etc` from a tmp dir is
   rejected by allowed_roots, not by the ".." check (this test passes already —
   just documents that the removal doesn't break anything)

## Testing

```bash
cd /Users/pms88/projects/agentpaas
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
```

All existing 122 tests + 3 new tests must pass (125 total).

## Commit message

```
fix(14a-t01): harden env var overrides in path validation per adversary review

- Use pwd.getpwuid() instead of $HOME for home directory (prevents HOME
  override attack)
- Validate AGENTPAAS_PROJECT_ROOT is not a system dir (prevents "/" bypass)
- Remove dead ".." check after realpath (false sense of security — realpath
  already resolves all ".." components)

3 new tests: HOME override, system-dir fallback, traversal still rejected.
```

## Branch

Commit to the existing `feat/b14a-t01` branch (amend or new commit).
