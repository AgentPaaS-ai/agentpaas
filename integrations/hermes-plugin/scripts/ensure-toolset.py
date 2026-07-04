#!/usr/bin/env python3
"""Ensure 'agentpaas' is in platform_toolsets.cli for a Hermes profile.

Usage: python3 scripts/ensure-toolset.py <profile>

Idempotent: if agentpaas is already present, exits 0 with no changes.
If the platform_toolsets section or cli list doesn't exist, creates it.
"""
import pathlib
import sys

if len(sys.argv) != 2:
    print("Usage: ensure-toolset.py <profile>", file=sys.stderr)
    sys.exit(1)

profile = sys.argv[1]
cfg_path = pathlib.Path.home() / ".hermes" / "profiles" / profile / "config.yaml"

if not cfg_path.exists():
    print(f"  Config not found: {cfg_path}", file=sys.stderr)
    sys.exit(1)

lines = cfg_path.read_text().splitlines()

# Find platform_toolsets: section and its cli: subsection
in_cli = False
last_cli_item_idx = None

for i, line in enumerate(lines):
    stripped = line.strip()

    # Entering cli: list
    if stripped == "cli:" and "platform_toolsets" in lines[max(0, i - 1)]:
        # Check if the line before is 'platform_toolsets:'
        pass

    # Track when we're inside the cli: list under platform_toolsets
    if stripped == "cli:":
        # Look back for platform_toolsets
        for j in range(i - 1, -1, -1):
            prev = lines[j].strip()
            if prev.startswith("platform_toolsets"):
                in_cli = True
                break
            elif prev and not prev.startswith("#"):
                break
        continue

    if in_cli:
        if stripped.startswith("- "):
            if stripped == "- agentpaas":
                print("  agentpaas already in platform_toolsets.cli")
                sys.exit(0)
            last_cli_item_idx = i
        elif stripped and not stripped.startswith("#"):
            in_cli = False

if last_cli_item_idx is not None:
    # Insert after the last item in the existing cli list
    lines.insert(last_cli_item_idx + 1, "    - agentpaas")
    cfg_path.write_text("\n".join(lines) + "\n")
    print("  Added agentpaas to platform_toolsets.cli")
else:
    # No cli: list found under platform_toolsets — create the section
    # First check if platform_toolsets: exists at all
    pt_idx = None
    for i, line in enumerate(lines):
        if line.strip() == "platform_toolsets:" or line.strip().startswith("platform_toolsets:"):
            pt_idx = i
            break

    if pt_idx is not None:
        lines.insert(pt_idx + 1, "  cli:")
        lines.insert(pt_idx + 2, "    - agentpaas")
    else:
        lines.append("")
        lines.append("platform_toolsets:")
        lines.append("  cli:")
        lines.append("    - agentpaas")

    cfg_path.write_text("\n".join(lines) + "\n")
    print("  Created platform_toolsets.cli with agentpaas")
