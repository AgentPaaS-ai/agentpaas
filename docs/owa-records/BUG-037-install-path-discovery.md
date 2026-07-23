# BUG-037 — Install/share path assumes agentpaas already on PATH (first-user friction)

**Status:** OPEN  
**Severity:** P2 (UX / first-time receiver)  
**Found:** 2026-07-23 B32 pre-v0.3.0 manual testing (receiver half, Phase 11)  
**Build:** CLI 0.3.0-dev commit ed22b0f  
**Founder:** PATH (or equivalent discovery) should be automated enough that a new user downloading a shared agent does not need to know how to set PATH. Fix later; note only now.

## Symptom

After inspect, Hermes correctly told the user to run install in **their** terminal (consent-gated — good). The command was:

```bash
agentpaas install /Users/pms88/received-weather-agent.agentpaas
```

That only works if `agentpaas` is already on the shell PATH. In this RC loop the CLI lived under `/tmp/agentpaas-rc-prefix/bin` (and/or `~/.local/bin` symlinks + profile `.env`). A real first-time receiver who:

- installed via brew but opened a shell without Homebrew on PATH, or
- installed from source / RC without a durable PATH, or
- only ever used Hermes and never configured a login shell PATH,

gets `command not found: agentpaas` and has no product-guided recovery.

Related earlier failure mode (same session): Hermes slash `/agentpaas-doctor` / plugin tools failed with bare Errno 2 when PATH was thin after restart (see `docs/owa-records/b32-deferred-doctor-cli-path.md`). Install handoff has the same discovery gap.

## Expected

A new user who has completed first-time AgentPaaS setup (brew cask or documented install) can run the install command Hermes prints **without** manually exporting PATH.

When CLI is missing from PATH, Hermes/plugin/doctor must:

1. Detect missing binary (not raw Errno 2).
2. Tell them **how** to fix in one step, e.g.:
   - `brew install --cask agentpaas` (stable), or
   - open a **login** terminal and retry, or
   - set `AGENTPAAS_CLI=/absolute/path/to/agentpaas` in the Hermes profile env,
   - or offer a copy-pasteable full-path install:  
     `/opt/homebrew/bin/agentpaas install /path/to/bundle.agentpaas`
3. Prefer printing **absolute path** to the resolved CLI in guided install instructions when Hermes can resolve it via `AGENTPAAS_CLI` / known locations even if user shell PATH is wrong.

## Actual

- Guided install string used bare `agentpaas` with no path.
- RC/dev installs required operator knowledge (`export PATH=...`, rc-prefix, profile `.env`).
- Product docs/skills assume PATH already correct after install.

## Why it matters

Receiver golden path is “friend sent me a `.agentpaas` file → inspect in Hermes → install → run.”  
PATH is not part of that mental model. Friction here looks like “AgentPaaS is broken” rather than “shell PATH.”

## Non-goals

- Do **not** remove terminal consent for install (fingerprint / egress / credential map stay human-gated).
- Do **not** auto-install without consent from the plugin.
- Brew cask on default PATH remains the primary fix for production; this bug is discovery + guidance + absolute-path handoff when resolution fails or only Hermes knows the binary.

## Proposed fix directions (later)

1. **Guided install command uses absolute CLI** when plugin resolved `AGENTPAAS_CLI` / `which` successfully inside Hermes:  
   `{abs_cli} install {bundle}`.
2. **Preflight before printing install:** if user-shell check unavailable, still emit full path from plugin resolution.
3. **Doctor / missing-CLI envelope** (BUG doctor-path deferred): `cli_not_found` + hint with brew + `AGENTPAAS_CLI`.
4. **Post-brew install message:** “Open a new terminal” / ensure `/opt/homebrew/bin` on PATH for GUI apps.
5. **Optional:** `hermes` slash `/agentpaas-install` that launches an **interactive** terminal/TTY consent flow without requiring the user to type PATH (still not silent auto-approve).

## Evidence

- Phase 11: Hermes asked user to run bare `agentpaas install …` after secret confirmed.
- Same day: ap-testing PATH/env issues for doctor and CLI discovery; RC prefix not on default PATH.
- Bundle path was absolute (good); CLI path was not.

## Related

- BUG-036 silent identity init (onboarding trust)
- `docs/owa-records/b32-deferred-doctor-cli-path.md` (slash doctor Errno 2)
- Consent-gated install design remains correct; only **invocation discovery** is broken

## Index

Also listed in `docs/execution/blocks/b32-manual-testing.md` bugs table.
