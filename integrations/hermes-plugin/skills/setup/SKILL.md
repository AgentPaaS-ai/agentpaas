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

Then register the toolset and create the skill pointer:

```bash
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/ensure-toolset.py <profile>
```

Create `~/.hermes/profiles/<profile>/skills/agentpaas/SKILL.md` with:
```yaml
---
name: agentpaas-build
description: >
  Build, deploy, package, run, and govern AI agents. Use when the user
  asks to build, create, deploy, pack, or run any agent. You MUST load
  the full skill with skill_view(name="agentpaas:deploy") for onboarding
  instructions, code structure requirements (@agent.on_invoke SDK
  pattern), egress policy rules, credential onboarding, and LLM
  configuration.
---

# AgentPaaS Deploy Pointer

When the user asks to build, create, deploy, pack, run, or govern any
agent, you MUST load the real skill immediately:

skill_view(name="agentpaas:deploy")

This pointer exists because plugin skills do not appear in the
available_skills index. Load it BEFORE writing any agent code or calling
agentpaas tools.
```

### LLM secrets are not part of setup

Do not ask the user to add an LLM secret during installation or setup. LLM
keys are added later, when the user builds an agent that needs one (the build
skill will prompt them).

### 7. Verify live registration

The plugin and tools register live in the current session; no restart is
needed. If `agentpaas_*` tools do not appear, restart Hermes once as a
fallback:

```bash
/quit
hermes -p <profile>
```

**STOP HERE.** Setup is complete. Do NOT offer to build, pack, or run
any agent. Do NOT ask "would you like me to build a test agent?" The
correct end-of-setup message is:

> AgentPaaS setup complete. The plugin and tools are available in this
> session. Restart Hermes only if the `agentpaas_*` tools do not appear.

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
