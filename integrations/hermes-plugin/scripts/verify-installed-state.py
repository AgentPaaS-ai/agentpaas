#!/usr/bin/env python3
"""Verify the installed AgentPaaS filesystem state for a Hermes profile.

Usage: python3 scripts/verify-installed-state.py <profile-name>

The check is filesystem-only. It validates the plugin shim, completed
onboarding files, and the relevant Hermes configuration. A daemon socket is
informational because a cold install does not start the daemon.
"""
from pathlib import Path
import re
import socket
import sys


def _has_list_item(config_text: str, section: str, item: str) -> bool:
    """Return whether an exact list item exists in a named top-level section."""
    match = re.search(rf"(?m)^{re.escape(section)}:\s*\n(?P<body>(?:^[ \t]+.*\n?)*)", config_text)
    if not match:
        return False
    return bool(re.search(rf"(?m)^\s*-\s*{re.escape(item)}\s*$", match.group("body")))


def _plugin_files(plugin_dir: Path):
    """Return the accepted tools and skill paths, preferring the root layout."""
    root_tools = plugin_dir / "tools.py"
    root_skill = plugin_dir / "SKILL.md"
    nested = plugin_dir / "integrations" / "hermes-plugin"
    nested_tools = nested / "tools.py"
    nested_skill = nested / "SKILL.md"
    if root_tools.is_file() and root_skill.is_file():
        return root_tools, root_skill, "root plugin files"
    return nested_tools, nested_skill, "nested shim plugin files"


def main() -> int:
    if len(sys.argv) != 2:
        print("Usage: verify-installed-state.py <profile-name>")
        return 1

    profile_name = sys.argv[1]
    profile_dir = Path.home() / ".hermes" / "profiles" / profile_name
    if not profile_dir.is_dir():
        print(f"FAIL: profile directory does not exist: {profile_dir}")
        return 1

    issues = []
    passed = []
    info = []
    plugin_dir = profile_dir / "plugins" / "agentpaas"

    if plugin_dir.is_dir():
        passed.append("plugin directory exists")
    else:
        issues.append("plugin directory missing: plugins/agentpaas/")

    for name in ("plugin.yaml", "__init__.py"):
        if (plugin_dir / name).is_file():
            passed.append(f"plugin file exists: {name}")
        else:
            issues.append(f"plugin file missing: plugins/agentpaas/{name}")

    tools_path, skill_path, layout = _plugin_files(plugin_dir)
    if tools_path.is_file():
        passed.append(f"plugin tools.py exists ({layout})")
    else:
        issues.append("plugin tools.py missing at root or integrations/hermes-plugin/")
    if skill_path.is_file():
        passed.append(f"plugin SKILL.md exists ({layout})")
    else:
        issues.append("plugin SKILL.md missing at root or integrations/hermes-plugin/")

    soul_md = profile_dir / "SOUL.md"
    if soul_md.is_file() and "AgentPaaS Onboarding Rule" in soul_md.read_text(encoding="utf-8"):
        passed.append("SOUL.md has onboarding rule")
    elif soul_md.is_file():
        issues.append("SOUL.md exists but missing 'AgentPaaS Onboarding Rule' snippet")
    else:
        issues.append("SOUL.md missing — complete-install.py must write the onboarding rule")

    skill_pointer = profile_dir / "skills" / "agentpaas" / "SKILL.md"
    if skill_pointer.is_file():
        passed.append("skill pointer exists: skills/agentpaas/SKILL.md")
    else:
        issues.append("skill pointer missing: skills/agentpaas/SKILL.md")

    config_path = profile_dir / "config.yaml"
    if config_path.is_file():
        config_text = config_path.read_text(encoding="utf-8")
        if _has_list_item(config_text, "platform_toolsets", "agentpaas"):
            passed.append("platform_toolsets.cli contains 'agentpaas'")
        else:
            issues.append("platform_toolsets.cli does NOT contain 'agentpaas' — tools will be invisible")
        if _has_list_item(config_text, "plugins", "agentpaas"):
            passed.append("plugins.enabled contains 'agentpaas'")
        else:
            issues.append("plugins.enabled does NOT contain 'agentpaas'")
        entries = re.search(r"(?m)^\s+agentpaas:\s*$", config_text)
        if entries:
            passed.append("plugins.entries.agentpaas exists")
        elif _has_list_item(config_text, "plugins", "agentpaas") and plugin_dir.is_dir():
            passed.append("plugins.entries.agentpaas omitted by Hermes; enabled plugin directory is present")
        else:
            issues.append("plugins.entries.agentpaas missing and enabled plugin directory is unavailable")
    else:
        issues.append("config.yaml does not exist")

    daemon_socket = Path.home() / ".agentpaas" / "daemon.sock"
    if daemon_socket.is_socket():
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        try:
            sock.settimeout(0.5)
            sock.connect(str(daemon_socket))
        except OSError:
            info.append("daemon socket exists but is not connectable (informational)")
        else:
            info.append("daemon socket is live")
        finally:
            sock.close()
    else:
        info.append("daemon socket not found (daemon not running — informational)")

    print(f"\nProfile: {profile_name}")
    print(f"Path:    {profile_dir}")
    print(f"Passed:  {len(passed)}")
    print(f"Failed:  {len(issues)}")
    if passed:
        print("\nPASSED:")
        for item in passed:
            print(f"  ✓ {item}")
    if info:
        print("\nINFO:")
        for item in info:
            print(f"  • {item}")
    if issues:
        print("\nFAILED:")
        for issue in issues:
            print(f"  ✗ {issue}")
        print(f"\nReference state NOT met: {len(issues)} issue(s)")
        return 1
    print(f"\n✓ Reference state met: all {len(passed)} checks passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
