# Adversary Review: 14A-T01 — Plugin path allow-list

You are a security adversary. Your job is to BREAK the path allow-list implementation.

## Target code

File: `integrations/hermes-plugin/tools.py`
Function: `_validate_project_path(path)` (around line 339)
Wiring: called in `agentpaas_init_project`, `agentpaas_reconcile_project`,
`agentpaas_validate_project`, `agentpaas_pack`, `agentpaas_policy_show`.

The function:
1. Resolves symlinks via `os.path.realpath(os.path.expanduser(path))`
2. Treats "." as cwd
3. Rejects ".." in resolved path
4. Allows paths under: AGENTPAAS_PROJECT_ROOT (or cwd), /tmp, $HOME
5. Returns (is_valid, resolved, error_dict)

## Attack vectors to try

1. **Symlink chain:** Create symlink A → B → /etc. Does realpath resolve the full chain?
2. **TOCTOU race:** Is there a window between validation and _run_cli call where the
   path could be changed? (Check if `resolved` is used in _run_cli, not the original path)
3. **AGENTPAAS_PROJECT_ROOT override:** Can an attacker set this env var to "/" and
   bypass all checks?
4. **Path with null bytes:** `"/tmp\x00/etc"` — does Python reject this?
5. **Path with trailing slash:** `/tmp/` vs `/tmp` — does the prefix check work?
6. **Case sensitivity on macOS:** `/TMP` vs `/tmp` on case-insensitive filesystems
7. **Relative path with traversal:** `./../../etc` — does realpath resolve this correctly?
8. **Absolute path disguised as relative:** `../../etc/passwd` from /tmp
9. **Double-encoded traversal:** Not applicable (this isn't URL decoding)
10. **Path that resolves to allowed root exactly:** `/tmp` itself — is it allowed?
11. **AGENTPAAS_PROJECT_ROOT set to empty string:** Does it fall through to cwd?
12. **$HOME set to /etc:** Can an attacker override HOME to bypass?

## Instructions

1. Read the target code in `integrations/hermes-plugin/tools.py`
2. Read the tests in `integrations/hermes-plugin/tests/test_path_allowlist.py`
3. For each attack vector, analyze whether the code is vulnerable
4. If you find a real vulnerability, write a proof-of-concept test that demonstrates it
5. Report findings with severity (CRITICAL/HIGH/MEDIUM/LOW/FALSE_POSITIVE)

## Output format

```
FINDING 1: [severity] [title]
Description: ...
Proof: ...
Fix recommendation: ...

FINDING 2: ...
```

If no vulnerabilities found, say "NO VULNERABILITIES FOUND" and explain why each
attack vector is mitigated.
