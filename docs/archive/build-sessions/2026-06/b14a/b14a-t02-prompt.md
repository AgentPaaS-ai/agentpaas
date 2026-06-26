# Task: 14A-T02 — AGENTPAAS_CLI binary verification (GAP-2, HIGH severity)

## Context

`_resolve_agentpaas_binary()` in `integrations/hermes-plugin/tools.py` validates that
AGENTPAAS_CLI is an absolute path to an executable file (T01 adversary fix). But it
does NOT verify the binary IS agentpaas — any executable at any path is accepted. If
an attacker can set an env var, they can point AGENTPAAS_CLI at a malicious binary.

The current code (around line 240):
```python
def _resolve_agentpaas_binary():
    if env_override := os.getenv("AGENTPAAS_CLI"):
        if not env_override.strip():
            pass
        else:
            real = os.path.realpath(env_override)
            if not os.path.isabs(env_override):
                raise ValueError(...)
            if not os.path.isfile(real):
                raise ValueError(...)
            if not os.access(real, os.X_OK):
                raise ValueError(...)
            return real  # <-- accepts ANY binary, no verification it's agentpaas
    p = shutil.which("agentpaas")
    if p:
        return p
    # ... fallback candidates ...
```

## What to implement

### 1. Add path allow-list for AGENTPAAS_CLI

Add a `_CLI_BINARY_ALLOW_LIST` of directories where the agentpaas binary is allowed:
```python
_CLI_BINARY_ALLOW_LIST = (
    "/usr/local/bin",
    "/opt/homebrew/bin",
    "/usr/bin",
    "/bin",
)
```

Plus dynamic paths:
- `$HOME/.local/bin`
- The repo's `bin/` directory (derived from the plugin file location)

### 2. Add `--version` verification

After validating the path is executable, verify the binary IS agentpaas by running
`<binary> --version` and checking the output contains "agentpaas" (case-insensitive).

```python
def _verify_agentpaas_binary(path):
    """Verify a binary is actually agentpaas by checking --version output.
    Returns True if verified, False otherwise.
    Raises ValueError with details if verification fails.
    """
    try:
        result = subprocess.run(
            [path, "--version"],
            capture_output=True, text=True, timeout=5
        )
        output = (result.stdout + " " + result.stderr).lower()
        if "agentpaas" in output:
            return True
        raise ValueError(
            f"AGENTPAAS_CLI binary does not appear to be agentpaas "
            f"(--version output: {result.stdout[:200]})"
        )
    except subprocess.TimeoutExpired:
        raise ValueError(
            f"AGENTPAAS_CLI binary --version timed out (not a valid agentpaas binary?)"
        )
```

### 3. Wire into `_resolve_agentpaas_binary`

In the AGENTPAAS_CLI env override path, after the existing checks (isabs, isfile, X_OK):

1. Check the binary path is under an allowed directory
2. Run `--version` verification
3. Only return if both pass

```python
def _resolve_agentpaas_binary():
    if env_override := os.getenv("AGENTPAAS_CLI"):
        if not env_override.strip():
            pass
        else:
            real = os.path.realpath(env_override)
            if not os.path.isabs(env_override):
                raise ValueError(
                    f"AGENTPAAS_CLI must be an absolute path, got: {env_override}"
                )
            if not os.path.isfile(real):
                raise ValueError(f"AGENTPAAS_CLI is not a file: {env_override}")
            if not os.access(real, os.X_OK):
                raise ValueError(f"AGENTPAAS_CLI is not executable: {env_override}")
            # NEW: Verify the binary is under an allowed directory
            _check_binary_in_allow_list(real)
            # NEW: Verify the binary IS agentpaas
            _verify_agentpaas_binary(real)
            return real
    # ... rest unchanged ...
```

### 4. Allow-list check function

```python
def _check_binary_in_allow_list(path):
    """Check that the binary path is under an allowed directory.
    Raises ValueError if not.
    """
    here = os.path.dirname(os.path.abspath(__file__))
    repo_bin = os.path.abspath(os.path.join(here, "..", "..", "..", "bin"))
    allowed_dirs = list(_CLI_BINARY_ALLOW_LIST) + [
        os.path.expanduser("~/.local/bin"),
        repo_bin,
    ]
    for allowed in allowed_dirs:
        allowed_real = os.path.realpath(allowed)
        if path == allowed_real or path.startswith(allowed_real + os.sep):
            return
    raise ValueError(
        f"AGENTPAAS_CLI binary outside allowed directories: {path}. "
        f"Allowed: {', '.join(allowed_dirs)}"
    )
```

## Important constraints

1. **Do NOT break existing tests.** The existing adversary tests in
   `test_adversary_b13_t01.py` test `_resolve_agentpaas_binary()`. Check what they
   assert and make sure your changes don't break them. Some tests set AGENTPAAS_CLI to
   `/bin/echo` — that test will now fail (correctly!) because `/bin/echo --version`
   doesn't output "agentpaas". You need to update those tests to reflect the new
   security requirement.

2. **The `--version` check adds a subprocess call.** This is acceptable — it only runs
   when AGENTPAAS_CLI env var is set (not on every tool call), and it's a 5s timeout.

3. **For the PATH lookup and fallback candidates** (shutil.which, repo bin/), do NOT
   add --version verification. Those paths are trusted by definition (system PATH or
   repo checkout). Only add the allow-list + --version check for the AGENTPAAS_CLI
   env override path.

4. **Mocking in tests:** Tests that mock subprocess should work. For the --version
   check, you can mock `subprocess.run` to return "agentpaas v0.1.0" for valid tests.

## Tests to write

Create `integrations/hermes-plugin/tests/test_binary_verification.py`:

1. **test_valid_binary_passes:** Create a fake binary script that outputs
   "agentpaas v0.1.0" on `--version`, put it in an allowed dir, set AGENTPAAS_CLI → passes
2. **test_reject_non_agentpaas_binary:** AGENTPAAS_CLI=/bin/echo → raises ValueError
3. **test_reject_binary_outside_allow_list:** Create a binary in /tmp that outputs
   "agentpaas" on --version, set AGENTPAAS_CLI → rejected (path not in allow-list)
4. **test_reject_symlink_to_non_agentpaas:** Symlink in allowed dir pointing to
   /bin/echo → rejected (--version doesn't say agentpaas)
5. **test_reject_timeout:** Binary that hangs on --version → raises ValueError
6. **test_repo_bin_allowed:** Binary in repo bin/ dir → allowed by path check
7. **test_home_local_bin_allowed:** Binary in ~/.local/bin → allowed by path check

Update existing tests in `test_adversary_b13_t01.py` that assert `/bin/echo` is
accepted — those tests need to be updated to reflect the new security requirement.
The test `test_adversary_agentpaas_cli_env_injection` currently asserts that
`/bin/echo` is accepted as AGENTPAAS_CLI. This should now FAIL (be rejected) because
echo is not agentpaas. Update the test to assert the ValueError is raised.

## Testing

```bash
cd /Users/pms88/projects/agentpaas
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
```

All existing tests must still pass (some will need updating), plus your new tests.

## Commit message

```
feat(14a-t02): AGENTPAAS_CLI binary verification (GAP-2)

Add path allow-list and --version verification for AGENTPAAS_CLI env var.
Binary must be under /usr/local/bin, /opt/homebrew/bin, /usr/bin, /bin,
~/.local/bin, or repo bin/. The --version output must contain "agentpaas".
Prevents attacker from pointing AGENTPAAS_CLI at arbitrary executables.

7 new tests in test_binary_verification.py. Updated test_adversary_b13_t01
to reflect that /bin/echo is now correctly rejected.
```

## Branch

Create branch `feat/b14a-t02` from main.
