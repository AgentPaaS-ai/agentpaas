# BUG-039 — Source-bundle install warns uv.lock missing (noisy UX for requirements.txt agents)

**Status:** OPEN  
**Severity:** P3 (UX / low priority)  
**Found:** 2026-07-23 B32 pre-v0.3.0 manual testing (receiver install, local-rebuild)  
**Build:** CLI 0.3.0-dev commit ed22b0f  

## Symptom

During `agentpaas install` of a source-only bundle (rebuild path):

```text
WARNING: uv.lock missing in bundle source; rebuild uses unlocked dependencies (deps_unlocked_rebuild=true)
Continue without locked deps? [type 'yes']:
```

Agent was a normal Python project with `requirements.txt` only — never used uv. Extra confirm slows install and sounds like a failure for a common layout.

## Expected

- If pack/export recorded deps via requirements.txt (or lock equivalent), don’t force a scary uv.lock prompt — or auto-continue with a single clear line: “Using requirements.txt (no uv.lock).”
- Reserve interactive confirm for true high-risk unlocked rebuilds when neither lock nor requirements pin is present.
- Docs: when to include uv.lock vs requirements.txt in bundles.

## Actual

Hard stop + type `yes` for missing uv.lock even when requirements.txt is in the bundle and install succeeds (deps_unlocked_rebuild=true).

## Evidence

- Bundle export Phase 8: source files included requirements.txt, not uv.lock.
- Install completed after `yes`; mode local-rebuild; run OK.

## Fix later (low priority)

- Soften prompt when requirements.txt present.
- Or generate/export a lock at pack time when missing (heavier).
- Copy-paste default in non-interactive CI: flag to allow unlocked rebuild.

## Related

- Source bundle “rebuild required (no prebuilt image)” is separate and expected without `--with-image`.
