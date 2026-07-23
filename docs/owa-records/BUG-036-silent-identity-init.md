# BUG-036 — Silent publisher identity init (first-time UX / trust)

**Status:** OPEN  
**Severity:** P2 (UX / trust — not a containment break)  
**Found:** 2026-07-23 B32 pre-v0.3.0 manual testing (ap-testing, Phase 4 weather agent)  
**Build:** CLI 0.3.0-dev commit ed22b0f  
**Founder note:** first-time identity must be an explicit user step (“what publisher name?” / run in their terminal). Silent init is weaker UX/trust.

## Symptom

During first weather-agent build after nuclear teardown, the user was **not** asked to set up a publisher identity. Pack and invoke still succeeded. Post-hoc disk check showed identity already present:

```
Name:        parvezsyed
Fingerprint: 2b0c adb0 c5ec 2de8 … ae1e
Created:     2026-07-23T10:50:48-07:00
```

Timeline relative to pack/invoke:
- 10:50 identity created
- 10:51 openrouter-key added (user terminal — correct)
- 10:58 pack weather-agent + Folsom invoke

So identity was created in-session without an explicit user-facing confirm/prompt the founder saw.

## Expected (product + skill contract)

Publisher identity is a **trust anchor** (signing packs/bundles). First-time creation must be:

1. Explicit user decision (choose display name).
2. Prefer user-run in **their** terminal: `agentpaas identity init` (or Hermes clearly asks name and waits for confirm before any init).
3. Never silent / never assumed.

Existing skill already states this for pack and export:

- `integrations/hermes-plugin/SKILL.md` (~205–221): if `identity show` fails, **tell the user** to run `agentpaas identity init` in their terminal; one-time setup.
- Export section (~394–396): if no identity, tell USER to run init; **Do NOT create the identity yourself.**
- Tool `agentpaas_identity_show` is read-only and documents terminal-gated creation (`tools.py`).

## Actual

Agent proceeded without surfacing the identity step to the user. Identity appeared as `parvezsyed` before pack — likely terminal-tool `agentpaas identity init --name parvezsyed` (or equivalent) run by the agent, not a user-confirmed step.

## Why it matters

- User did not consciously create the key that will sign shared bundles.
- Fingerprint out-of-band verify story is weaker if the user never saw init.
- Violates documented onboarding contract even when pack “works.”

## Non-goals / not this bug

- Invoke results were real (disk-verified Folsom + Seattle); this bug is only identity onboarding UX.
- Not a missing identity at pack time (pack correctly requires identity).

## Proposed fix directions (do not implement in this note)

1. **Hard gate in skill/onboarding:** before pack, if no identity → stop and only instruct user terminal init; refuse agent-run `identity init` except when user explicitly asked in-chat with a chosen name.
2. **CLI/plugin:** no non-interactive identity create from plugin tools (confirm no `identity_init` tool; block agent from `terminal` running init without user echo of name — soft policy).
3. **Doctor or pack preflight message:** “Publisher identity: missing — run agentpaas identity init” as a first-class check Hermes must relay.
4. **Manual test pin:** golden-loop Phase 4 verify user was prompted OR session log shows user-confirmed name before init timestamp.

## Evidence paths

- `agentpaas identity show` → parvezsyed, created 10:50:48
- Pack audit: `~/.agentpaas/state/audit.jsonl` event_type=pack ~17:58Z
- Manual session: founder report “didn’t ask me to setup publisher identity”

## Related

- BUG-012 / BUG-017 class: onboarding skip / silent steps
- Golden loop `docs/execution/golden-loop-test.md` Phase 4 step 7 (tell user identity init if missing)
- Deferred with BUG-036 for v0.3.x polish (not blocking Phase 4 PASS of weather invoke)
