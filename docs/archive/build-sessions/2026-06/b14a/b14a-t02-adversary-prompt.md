# Adversary Review: 14A-T02 — AGENTPAAS_CLI binary verification

You are a security adversary. Your job is to BREAK the binary verification implementation.

## Target code

File: `integrations/hermes-plugin/tools.py`
Functions: `_check_binary_in_allow_list(path)`, `_verify_agentpaas_binary(path)`,
`_resolve_agentpaas_binary()` (around line 240-310)

The implementation:
1. `_CLI_BINARY_ALLOW_LIST` = system bin dirs (/usr/local/bin, /opt/homebrew/bin, /usr/bin, /bin)
2. `_check_binary_in_allow_list(path)` — checks path is under an allowed dir (system dirs + ~/.local/bin + repo bin/)
3. `_verify_agentpaas_binary(path)` — runs `<binary> --version` with 5s timeout, checks output contains "agentpaas"
4. Both checks only run for the AGENTPAAS_CLI env override path, not for PATH lookup or fallback candidates

## Attack vectors to try

1. **Symlink from allowed dir to malicious binary:** Create symlink in /usr/local/bin pointing to /tmp/evil. Does the allow-list check pass? Does --version catch it?
2. **Binary that outputs "agentpaas" then does something malicious:** The --version check only verifies output, not behavior. Is this acceptable for P1?
3. **Race condition (TOCTOU):** Is there a window between the checks and the actual use in _run_cli where the binary could be swapped?
4. **Path with spaces or special chars:** "/usr/local/bin/my agentpaas" — does the allow-list check handle this?
5. **Binary that takes a long time on --version but eventually outputs "agentpaas":** 5s timeout — is this enough? Could this be a DoS vector?
6. **AGENTPAAS_CLI pointing to a shell script:** A script that outputs "agentpaas" on --version but does evil on other commands. Is this mitigated?
7. **Allow-list bypass via /usr/bin symlink:** On some systems, /usr/bin might be a symlink. Does the realpath check handle this?
8. **~/.local/bin expansion:** Can HOME be overridden to make ~/.local/bin point to an attacker-controlled dir?
9. **Repo bin/ path traversal:** The repo_bin is computed as ../../bin from the plugin file. Can this be manipulated?
10. **--version output injection:** What if the binary's --version output contains "agentpaas" embedded in other text? Is the substring check sufficient?

## Instructions

1. Read the target code in `integrations/hermes-plugin/tools.py`
2. Read the tests in `integrations/hermes-plugin/tests/test_binary_verification.py`
3. For each attack vector, analyze whether the code is vulnerable
4. If you find a real vulnerability, write a proof-of-concept test
5. Report findings with severity (CRITICAL/HIGH/MEDIUM/LOW/FALSE_POSITIVE)
