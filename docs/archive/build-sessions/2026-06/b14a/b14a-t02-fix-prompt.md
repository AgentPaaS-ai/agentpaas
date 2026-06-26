# Task: 14A-T02 Adversary Fix — Harden HOME env override in binary allow-list

## Context

Adversary review found that `_check_binary_in_allow_list()` uses
`os.path.expanduser("~/.local/bin")` which respects the $HOME env var. An attacker
who can set HOME can redirect `~/.local/bin` to an attacker-controlled directory,
bypassing the allow-list.

This is the same class of vulnerability as T01's HOME override fix.

## What to fix

In `integrations/hermes-plugin/tools.py`, function `_check_binary_in_allow_list`:

Replace:
```python
allowed_dirs = list(_CLI_BINARY_ALLOW_LIST) + [
    os.path.expanduser("~/.local/bin"),
    repo_bin,
]
```

With:
```python
# Use pwd module, not $HOME env var, to prevent override attacks
# (same fix as _validate_project_path in T01)
import pwd
try:
    home_dir = pwd.getpwuid(os.getuid()).pw_dir
except (KeyError, OSError):
    home_dir = os.path.expanduser("~")

allowed_dirs = list(_CLI_BINARY_ALLOW_LIST) + [
    os.path.join(home_dir, ".local", "bin"),
    repo_bin,
]
```

Note: `pwd` is already imported at the top of the file from the T01 fix. If not,
add `import pwd` near the top with the other imports.

## Tests

The adversary wrote `integrations/hermes-plugin/tests/test_adversary_14a_t02.py`
with 3 tests. The first test (`test_adversary_home_override_bypasses_local_bin_allowlist`)
currently demonstrates the bypass SUCCEEDS. After the fix, this test should be
UPDATED to assert the bypass is now BLOCKED:

```python
def test_adversary_home_override_blocked(self):
    """HIGH: Setting HOME to redirect ~/.local/bin is now blocked (uses pwd module)."""
    with tempfile.TemporaryDirectory() as tmpdir:
        attacker_home = Path(tmpdir) / "attacker"
        attacker_local = attacker_home / ".local" / "bin"
        attacker_local.mkdir(parents=True)
        fake = attacker_local / "agentpaas"
        _make_fake_agentpaas_script(fake, version_output="agentpaas v0.1.0")
        with mock.patch.dict(
            os.environ,
            {"HOME": str(attacker_home), "AGENTPAAS_CLI": str(fake)},
            clear=False,
        ):
            # After fix: HOME override is blocked, binary is rejected
            with self.assertRaises(ValueError) as ctx:
                self.plugin.tools._resolve_agentpaas_binary()
            self.assertIn("outside allowed", str(ctx.exception))
```

The other two tests (version output injection, shell script faking version) are
acceptable P1 limitations — the --version check is defense-in-depth, not a
cryptographic verification. Keep those tests as documentation of the known
limitation. They should still pass (the --version check accepts these, which
is the current behavior).

## Testing

```bash
cd /Users/pms88/projects/agentpaas
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
```

All existing tests + the adversary test file must pass (135 tests total:
132 existing + 3 adversary tests, with 1 updated).

## Commit message

```
fix(14a-t02): harden HOME env override in binary allow-list per adversary review

Use pwd.getpwuid() instead of $HOME for ~/.local/bin path resolution.
Prevents attacker from redirecting ~/.local/bin via HOME env var.
Updated adversary test to verify bypass is now blocked.
```

## Branch

Commit to the existing `feat/b14a-t02` branch.
