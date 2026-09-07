---
name: agentpaas-setup
description: >
  Install, configure, and verify AgentPaaS on macOS. Covers the full
  bootstrap: Docker/Colima, CLI via Homebrew tap, harness binary (bundled
  since v0.2.1), Hermes plugin installation, daemon start, and doctor
  verification. Use when the user says "set up agentpaas" or "install
  agentpaas" and nothing is installed yet. Prerequisite: Hermes Agent
  already installed and running.
---

# AgentPaaS Setup (macOS)

## Overview

AgentPaaS runs every agent inside a locked-down Docker container with
default-deny network policy. Credentials are brokered through a gateway
sidecar. All egress is logged to a tamper-evident audit trail.

This skill covers the **one-time bootstrap** on macOS. For building and
running an agent after setup is complete, load `agentpaas:deploy` via the
`agentpaas-build` pointer skill.

When the user says "Install AgentPaaS from github
https://github.com/AgentPaaS-ai/agentpaas", that is the FULL product:
CLI + Colima/Docker + plugin. Plugin-only is a fail. The GitHub URL is
how they found us. Brew is how the CLI lands. Both are required.

## Prerequisites

- **Hermes Agent** — already installed and running
- **Homebrew** — for installing Colima, Docker CLI, and the AgentPaaS tap
- **macOS** — Apple Silicon or Intel

## Step-by-Step

### 1. Install AgentPaaS CLI

Official tap only. This MUST land **0.4.0** (not HEAD, not 0.4.1):

```bash
brew install agentpaas-ai/tap/agentpaas
```

Never `--HEAD`. Never a testing tap.

### 2. Quarantine HARD GATE (before any agentpaas command / doctor)

Brew post-install clears macOS quarantine on the four bins (`agentpaas`,
`agentpaasd`, `agentpaas-harness-linux`, `agentpaas-harness-linux-amd64`).
Do not tell the user to run xattr.

**HARD GATE** before any `agentpaas` command (including version) and
before doctor:

1. Run `python3 <plugin-or-clone>/scripts/ensure-unquarantined.py`
   (or the installed plugin copy). The script uses `$(brew --prefix)/bin`,
   never `/opt/homebrew` only.
2. VERIFY `xattr -l "$(brew --prefix)/bin/agentpaas"` does not list
   `com.apple.quarantine`.
3. If it still does, the **agent** runs `xattr -cr` on all four bins
   using `$(brew --prefix)/bin/<name>`, then re-verifies.
4. Do not proceed until clean. A skipped gate is SIGKILL 137 / Gatekeeper.

Then `agentpaas version` must show 0.4.0. If it shows HEAD, 0.4.1, or
anything else: STOP. Do not doctor.

### 3. Verify Harness Binary (bundled since v0.2.1)

```bash
file "$(brew --prefix)/bin/agentpaas-harness-linux"
# Expected: ELF 64-bit LSB executable, ARM aarch64, statically linked
```

Skip — only build from source if this binary is missing (pre-v0.2.1 or
custom modifications).

### 4. Docker runtime (agent, not the user)

GitHub install ALWAYS runs `ensure-docker-runtime.py` via
`complete-install.py` BEFORE `agentpaas doctor`. Do not tell the user to brew install docker or colima.
The agent runs the script.

NEVER set `DOCKER_HOST`. NEVER sudo `/var/run/docker.sock`. Runtime
start is `colima start --mount /Users:w`. Unset `DOCKER_HOST` if it is
set. Daemon uses Docker CLI as-is.

### 5. Install the Hermes Plugin (Docker runtime + doctor)

Install the plugin from GitHub (NOT from a local clone):

```bash
hermes plugins install --force --enable https://github.com/AgentPaaS-ai/agentpaas
```

Resolve `<profile>` from `HERMES_HOME`, `hermes profile`, or the installed
path, then run the filesystem completer and verification as a HARD GATE.
`complete-install.py` ALWAYS runs `ensure-docker-runtime.py` BEFORE
`agentpaas doctor`. Do not tell the user to brew install docker or colima.

```bash
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/complete-install.py <profile>
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/verify-installed-state.py <profile>
```

The completer writes the full `agentpaas-build` skill (including
`@agent.on_invoke`), adds the toolset, upserts SOUL.md, starts Colima/Docker,
starts the daemon, and requires `Overall: 7/7 checks passed`. If
`verify-installed-state.py` exits non-zero, setup is NOT complete: fix the
reported state and re-run both commands. Do not reinstall the plugin.
Do not report installed or verified if doctor is not 7/7. 7/7 is the only
success line.

### LLM secrets are not part of setup

Do not ask the user to add an OpenRouter or other LLM secret during
installation or setup. LLM keys are added later, when the user builds an
agent that needs one (the build skill will prompt them).

### 7. Reopen once for live registration

Hermes loads slash commands (`/agentpaas-*`) and `agentpaas_*` tools at
session start, so one session reopen is required after the verification gate.

**STOP HERE.** Setup is complete only after verification exits 0. The
correct end-of-setup message is:

> Reopen this session: /quit then hermes -p <profile>
> After reopen, run `/agentpaas-doctor`.

After setup, when the user asks to build something, THEN load
`agentpaas:deploy`. Until then, do nothing.

## Verification Checklist

- [ ] `colima status` — shows "Running"
- [ ] `docker info --format '{{.ServerVersion}}'` — returns version
- [ ] `agentpaas version` — shows CLI version + commit
- [ ] `agentpaas doctor` — 7/7 checks pass
- [ ] `agentpaas daemon start` — daemon running
- [ ] Plugin enabled: `ls ~/.hermes/profiles/<profile>/plugins/agentpaas/`
- [ ] Toolset registered: `grep agentpaas ~/.hermes/profiles/<profile>/config.yaml`
- [ ] Skill pointer exists: `ls ~/.hermes/profiles/<profile>/skills/agentpaas/SKILL.md`

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| `colima start` fails with "docker not found" | Docker CLI not installed | Agent re-runs `ensure-docker-runtime.py`. Do not tell the user to brew install docker. |
| doctor shows harness not found | Pre-v0.2.1 or built from source without harness | `brew upgrade agentpaas` (v0.2.1+ bundles it) |
| Plugin tools not in Hermes | Toolset not registered | Run `ensure-toolset.py` or add `agentpaas` to `platform_toolsets.cli` manually |
| "Apple could not verify agentpaas is free of malware" | `com.apple.quarantine` xattr is still set | Agent re-runs `ensure-unquarantined.py` or `xattr -cr` on `$(brew --prefix)/bin` for all four bins, then re-verifies `xattr -l "$(brew --prefix)/bin/agentpaas"` does not list `com.apple.quarantine`. Do not tell the user to run xattr. |
| "xattr: No such file" | Binary path wrong | Use `$(brew --prefix)/bin`, not a hardcoded prefix; `which agentpaas` if still missing |
| Plugin changes not reflected during development | Dev session needs refresh | `/quit` then relaunch Hermes |
