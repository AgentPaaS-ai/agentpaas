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

## Prerequisites

- **Hermes Agent** — already installed and running
- **Homebrew** — for installing Colima, Docker CLI, and the AgentPaaS tap
- **macOS** — Apple Silicon or Intel

## Step-by-Step

### 1. Install Docker Runtime

```bash
brew install colima docker
colima start
```

**Pitfall:** Docker CLI must be installed (`brew install docker`) before
`colima start` will succeed — Colima needs the `docker` binary on PATH.

### 2. Install AgentPaaS CLI

```bash
brew install agentpaas-ai/tap/agentpaas
```

### 3. Clear macOS Quarantine (HARD GATE — before any agentpaas command)

The brew cask is not notarized. macOS can KILL the binaries (exit 137) or
show a Gatekeeper dialog if you run them before clearing quarantine.

**HARD GATE:** Run `xattr -cr` on all four binaries (`agentpaas`,
`agentpaasd`, `agentpaas-harness-linux`, and `agentpaas-harness-linux-amd64`)
**BEFORE any agentpaas command**. Then VERIFY quarantine is cleared:

```bash
xattr -cr /opt/homebrew/bin/agentpaas /opt/homebrew/bin/agentpaasd /opt/homebrew/bin/agentpaas-harness-linux /opt/homebrew/bin/agentpaas-harness-linux-amd64
xattr -l /opt/homebrew/bin/agentpaas
```

The verification output must NOT list `com.apple.quarantine`. Do not proceed
to `agentpaas version` until this xattr check passes — a skipped xattr will
SIGKILL the binary (exit 137) or show the Gatekeeper dialog.

- [ ] Ran `xattr -cr` on all four binaries.
- [ ] Confirmed `xattr -l /opt/homebrew/bin/agentpaas` does not list
      `com.apple.quarantine`.
- [ ] Only after both checks pass, continue to `agentpaas version`.

### 4. Verify Harness Binary (bundled since v0.2.1)

```bash
file /opt/homebrew/bin/agentpaas-harness-linux
# Expected: ELF 64-bit LSB executable, ARM aarch64, statically linked
```

Skip — only build from source if this binary is missing (pre-v0.2.1 or
custom modifications).

### 5. Run Doctor

```bash
agentpaas daemon start
agentpaas doctor
```

Expected: **7/7 checks passed**.

### 6. Install the Hermes Plugin

Install the plugin from GitHub (NOT from a local clone):

```bash
hermes plugins install --force --enable https://github.com/AgentPaaS-ai/agentpaas
```

Resolve `<profile>` from `HERMES_HOME`, `hermes profile`, or the installed
path, then run the filesystem completer and verification as a HARD GATE:

```bash
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/complete-install.py <profile>
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/verify-installed-state.py <profile>
```

The completer writes the full `agentpaas-build` skill (including
`@agent.on_invoke`), adds the toolset, and upserts SOUL.md. If
`verify-installed-state.py` exits non-zero, setup is NOT complete: fix the
reported state and re-run both commands. Do not reinstall the plugin.

### LLM secrets are not part of setup

Do not ask the user to add an OpenRouter or other LLM secret during
installation or setup. LLM keys are added later, when the user builds an
agent that needs one (the build skill will prompt them).

### 7. Reopen once for live registration

Filesystem state (toolset, skill, and SOUL.md) completes without a restart.
Hermes loads slash commands (`/agentpaas-*`) and `agentpaas_*` tools only at
session start, so one session reopen is required after the verification gate.

**STOP HERE.** Setup is complete only after verification exits 0. Do NOT
offer to build, pack, or run any agent. Do NOT ask for API keys yet. The
correct end-of-setup message is:

> AgentPaaS install finished on disk. Reopen this Hermes session once so
> slash commands load (`/quit`, then `hermes -p <profile>`). Do NOT reinstall.
> Do NOT ask for API keys yet. After reopen, run `/agentpaas-doctor`.

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
| `colima start` fails with "docker not found" | Docker CLI not installed | `brew install docker` |
| doctor shows harness not found | Pre-v0.2.1 or built from source without harness | `brew upgrade agentpaas` (v0.2.1+ bundles it) |
| Plugin tools not in Hermes | Toolset not registered | Run `ensure-toolset.py` or add `agentpaas` to `platform_toolsets.cli` manually |
| "Apple could not verify agentpaas is free of malware" | `com.apple.quarantine` xattr is still set | Run `xattr -cr /opt/homebrew/bin/agentpaas /opt/homebrew/bin/agentpaasd /opt/homebrew/bin/agentpaas-harness-linux /opt/homebrew/bin/agentpaas-harness-linux-amd64`, then verify `xattr -l /opt/homebrew/bin/agentpaas` does not list `com.apple.quarantine` |
| "xattr: No such file" | Binary path wrong | Check with `which agentpaas`; path varies by Homebrew install location |
| Plugin changes not reflected during development | Dev session needs refresh | `/quit` then relaunch Hermes |
