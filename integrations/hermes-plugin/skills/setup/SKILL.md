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

### 3. Clear macOS Quarantine

The brew cask is not notarized. Clear quarantine on all three binaries:

```bash
xattr -cr /opt/homebrew/bin/agentpaas /opt/homebrew/bin/agentpaasd /opt/homebrew/bin/agentpaas-harness-linux
```

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

### 7. Restart Hermes

```bash
/quit
hermes -p <profile>
```

**STOP HERE.** Setup is complete. Do NOT offer to build, pack, or run
any agent. Do NOT ask "would you like me to build a test agent?" The
correct end-of-setup message is:

> AgentPaaS setup complete. Restart Hermes to load the plugin and tools.

After the user restarts and asks to build something, THEN load
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
| "xattr: No such file" | Binary path wrong | Check with `which agentpaas`; path varies by Homebrew install location |
| Plugin changes not reflected | Need session restart | `/quit` then relaunch Hermes |
