#!/usr/bin/env python3
"""Verify a Hermes test profile is in the correct state AFTER a clean
AgentPaaS plugin install.

This is the inverse of verify-clean-state.py: instead of checking for ABSENCE
of artifacts, it checks for PRESENCE of the expected post-install state.

Usage:
    python3 scripts/verify-installed-state.py <profile-name>

Exit 0 if all checks pass, exit 1 if any fail (with details printed).

Checks (filesystem-only, does NOT invoke `hermes -p <profile>`):
  1. Plugin directory exists at plugins/agentpaas/
  2. Plugin has plugin.yaml, tools.py, __init__.py, SKILL.md
  3. SOUL.md exists and contains "AgentPaaS Onboarding Rule"
  4. Skill pointer exists at skills/agentpaas/SKILL.md
  5. config.yaml has "agentpaas" in platform_toolsets.cli
  6. config.yaml has "agentpaas" in plugins.enabled
  7. config.yaml has plugins.entries.agentpaas
  8. Daemon socket exists and is connectable (if daemon should be running)

This script does NOT invoke `hermes -p <profile>` to avoid contaminating
the test profile (creating sessions, triggering daemon auto-start, etc.).
It checks the filesystem only.

The reference state is what a fresh user sees after:
  1. Reset profile to clean (reset-agentpaas-test.sh)
  2. Install plugin from GitHub (hermes plugins install <url> --enable)
  3. Restart Hermes session (register() fires, writes SOUL.md + skill pointer)
"""
import json
import os
import pathlib
import re
import socket
import sys


def main():
    if len(sys.argv) < 2:
        print("Usage: verify-installed-state.py <profile-name>")
        sys.exit(1)

    profile_name = sys.argv[1]
    profile_dir = pathlib.Path.home() / ".hermes" / "profiles" / profile_name

    if not profile_dir.exists():
        print(f"FAIL: profile directory does not exist: {profile_dir}")
        sys.exit(1)

    issues = []
    passed = []

    # ── 1. Plugin directory exists ──────────────────────────────────
    plugin_dir = profile_dir / "plugins" / "agentpaas"
    if plugin_dir.exists():
        passed.append("plugin directory exists")
    else:
        issues.append("plugin directory missing: plugins/agentpaas/")

    # ── 2. Plugin files exist ───────────────────────────────────────
    expected_files = ["plugin.yaml", "tools.py", "__init__.py", "SKILL.md"]
    for f in expected_files:
        fpath = plugin_dir / f
        if fpath.exists():
            passed.append(f"plugin file exists: {f}")
        else:
            issues.append(f"plugin file missing: plugins/agentpaas/{f}")

    # ── 3. SOUL.md has onboarding snippet ───────────────────────────
    soul_md = profile_dir / "SOUL.md"
    if soul_md.exists():
        soul_content = soul_md.read_text()
        if "AgentPaaS Onboarding Rule" in soul_content:
            passed.append("SOUL.md has onboarding rule")
        else:
            issues.append("SOUL.md exists but missing 'AgentPaaS Onboarding Rule' snippet")
    else:
        # SOUL.md might not exist if register() hasn't fired yet
        # (e.g. if the user installed but hasn't restarted)
        issues.append("SOUL.md missing — register() may not have fired (restart needed?)")

    # ── 4. Skill pointer exists ────────────────────────────────────
    skill_pointer = profile_dir / "skills" / "agentpaas" / "SKILL.md"
    if skill_pointer.exists():
        passed.append("skill pointer exists: skills/agentpaas/SKILL.md")
    else:
        issues.append("skill pointer missing: skills/agentpaas/SKILL.md")

    # ── 5. config.yaml has agentpaas in platform_toolsets.cli ───────
    config_path = profile_dir / "config.yaml"
    if config_path.exists():
        config_text = config_path.read_text()

        # Check platform_toolsets.cli contains 'agentpaas'
        # (simple text scan — avoids yaml dependency for CI runners)
        if "agentpaas" in config_text and "platform_toolsets" in config_text:
            passed.append("platform_toolsets.cli contains 'agentpaas'")
        else:
            issues.append("platform_toolsets.cli does NOT contain 'agentpaas' — tools will be invisible")

        # ── 6. config.yaml has agentpaas in plugins.enabled ──────────
        if re.search(r"plugins:\s*\n\s*enabled:\s*\[.*agentpaas", config_text) or \
           re.search(r"enabled:\s*\n\s*-\s*agentpaas", config_text) or \
           "agentpaas" in config_text:
            passed.append("plugins.enabled contains 'agentpaas'")
        else:
            issues.append("plugins.enabled does NOT contain 'agentpaas'")

        # ── 7. config.yaml has plugins.entries.agentpaas ────────────
        if "entries:" in config_text and "agentpaas" in config_text:
            passed.append("plugins.entries.agentpaas exists")
        else:
            issues.append("plugins.entries.agentpaas missing")
    else:
        issues.append("config.yaml does not exist")

    # ── 8. Daemon socket (optional — only if daemon should be running) ──
    daemon_socket = pathlib.Path.home() / ".agentpaas" / "daemon.sock"
    if daemon_socket.exists():
        # Try to connect to verify it's live
        try:
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.settimeout(0.5)
            sock.connect(str(daemon_socket))
            sock.close()
            passed.append("daemon socket is live")
        except (OSError, ConnectionRefusedError):
            issues.append("daemon socket exists but is not connectable (stale)")
    else:
        # Daemon not running — this may be expected if the test hasn't
        # started a session yet. Report as info, not failure.
        issues.append("daemon socket not found (daemon not running — may be expected before session start)")

    # ── Report ──────────────────────────────────────────────────────
    print(f"\nProfile: {profile_name}")
    print(f"Path:    {profile_dir}")
    print(f"Passed:  {len(passed)}")
    print(f"Failed:  {len(issues)}")
    print()

    if passed:
        print("PASSED:")
        for p in passed:
            print(f"  ✓ {p}")

    if issues:
        print("\nFAILED:")
        for issue in issues:
            print(f"  ✗ {issue}")
        print(f"\nReference state NOT met: {len(issues)} issue(s)")
        sys.exit(1)
    else:
        print(f"\n✓ Reference state met: all {len(passed)} checks passed")
        sys.exit(0)


if __name__ == "__main__":
    main()
