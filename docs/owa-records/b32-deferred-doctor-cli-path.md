# Deferred: slash /agentpaas-doctor bare Errno 2 when CLI off PATH

**Found:** B32 pre-v0.3.0 manual test (2026-07-23), Phase 2→3 Hermes restart after RC prefix install.
**Symptom:** `Doctor failed: [Errno 2] No such file or directory: 'agentpaas'`
**Cause:** `_resolve_agentpaas_binary()` returns bare `"agentpaas"`; `_run_cli` FileNotFoundError; no `hint`/`error_category`.
**Product impact:** Test-path amplified (temp RC prefix). Real brew cask install usually OK; GUI Hermes / source install can hit same class.
**Fix (later, not blocking this manual run):** structured `cli_not_found` + multi-line Hint (brew, AGENTPAAS_CLI abs path, profile .env PATH, plugin≠CLI); catch FileNotFoundError in `_run_cli`; improve `_cmd_doctor` formatting. Prompt draft: `/tmp/b32-doctor-path-fix-prompt.txt`
**Founder:** note only — code later.
