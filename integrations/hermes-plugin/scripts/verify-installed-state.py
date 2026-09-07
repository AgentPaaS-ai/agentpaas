#!/usr/bin/env python3
"""Verify the installed AgentPaaS filesystem state for a Hermes profile.

Usage: python3 scripts/verify-installed-state.py <profile-name>

The check is filesystem-only when AGENTPAAS_SKIP_RUNTIME=1 (daemon
socket informational). Otherwise docker, a live daemon, and
`Overall: 7/7 checks passed` are required.
"""
from pathlib import Path
import os
import re
import shutil
import socket
import subprocess
import sys


def _has_list_item(config_text: str, section: str, item: str) -> bool:
    """Return whether an exact list item exists in a named top-level section."""
    match = re.search(rf"(?m)^{re.escape(section)}:\s*\n(?P<body>(?:^[ \t]+.*\n?)*)", config_text)
    if not match:
        return False
    return bool(re.search(rf"(?m)^\s*-\s*{re.escape(item)}\s*$", match.group("body")))


def _skip_runtime() -> bool:
    return os.environ.get("AGENTPAAS_SKIP_RUNTIME") == "1"


def _prepend_brew_path() -> bool:
    brew = shutil.which("brew") or "brew"
    result = subprocess.run(
        [brew, "--prefix"],
        capture_output=True,
        text=True,
        check=False,
        timeout=10,
    )
    prefix = (result.stdout or "").strip()
    if result.returncode != 0 or not prefix:
        return False
    brew_bin = str(Path(prefix) / "bin")
    os.environ["PATH"] = brew_bin + os.pathsep + os.environ.get("PATH", "")
    os.environ.pop("DOCKER_HOST", None)
    return True


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

    skip_runtime = _skip_runtime()
    daemon_socket = Path.home() / ".agentpaas" / "daemon.sock"
    daemon_live = False
    if daemon_socket.is_socket():
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        try:
            sock.settimeout(0.5)
            sock.connect(str(daemon_socket))
            daemon_live = True
        except OSError:
            daemon_live = False
        finally:
            sock.close()
    if skip_runtime:
        if daemon_live:
            info.append("daemon socket is live")
        elif daemon_socket.is_socket():
            info.append("daemon socket exists but is not connectable (informational)")
        else:
            info.append("daemon socket not found (daemon not running — informational)")
    elif daemon_live:
        passed.append("daemon is running")
    else:
        issues.append("daemon not running")

    if not skip_runtime:
        if not _prepend_brew_path():
            issues.append("brew --prefix failed; cannot locate docker")
        if not shutil.which("docker"):
            issues.append("docker CLI not on PATH")
        else:
            try:
                docker_info = subprocess.run(
                    ["docker", "info"],
                    capture_output=True,
                    text=True,
                    check=False,
                    timeout=30,
                )
            except (OSError, subprocess.SubprocessError):
                issues.append("docker info failed")
            else:
                if docker_info.returncode != 0:
                    issues.append("docker info failed")
                else:
                    passed.append("docker info ok")
        try:
            doctor = subprocess.run(
                ["agentpaas", "doctor"],
                capture_output=True,
                text=True,
                check=False,
                timeout=120,
            )
            doctor_out = doctor.stdout or ""
        except (OSError, subprocess.SubprocessError):
            doctor_out = ""
        if "Overall: 7/7 checks passed" not in doctor_out:
            issues.append("agentpaas doctor is not Overall: 7/7 checks passed")
        else:
            passed.append("agentpaas doctor Overall: 7/7 checks passed")

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
