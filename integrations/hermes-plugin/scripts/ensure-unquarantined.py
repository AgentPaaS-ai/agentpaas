#!/usr/bin/env python3
"""HARD GATE: brew AgentPaaS bins must not carry com.apple.quarantine.

Brew post-install is the product path that clears quarantine. This script
is the agent-side verify: if dirty, xattr -cr the four bins, then
re-verify. Exits 0 when clean, non-zero if a bin is missing or quarantine
remains.

Do not print a "run xattr yourself" instruction. Agents call this after
brew, before `agentpaas version` / doctor.

Override the bin directory with AGENTPAAS_BIN_DIR (tests). Otherwise use
`$(brew --prefix)/bin` — never hardcode /opt/homebrew only.
"""
from __future__ import annotations

import os
import shutil
import subprocess
import sys
from pathlib import Path

BINS = (
    "agentpaas",
    "agentpaasd",
    "agentpaas-harness-linux",
    "agentpaas-harness-linux-amd64",
)

XATTR = "/usr/bin/xattr"


def brew_bin_dir() -> Path:
    override = os.environ.get("AGENTPAAS_BIN_DIR")
    if override:
        return Path(override)
    brew = shutil.which("brew") or "brew"
    result = subprocess.run(
        [brew, "--prefix"],
        capture_output=True,
        text=True,
        check=False,
    )
    prefix = (result.stdout or "").strip()
    if result.returncode != 0 or not prefix:
        print("ensure-unquarantined: brew --prefix failed", file=sys.stderr)
        raise SystemExit(1)
    return Path(prefix) / "bin"


def xattr_list(path: Path) -> str:
    if not Path(XATTR).is_file():
        return ""
    result = subprocess.run(
        [XATTR, "-l", str(path)],
        capture_output=True,
        text=True,
        check=False,
    )
    return (result.stdout or "") + (result.stderr or "")


def xattr_clear(path: Path) -> None:
    if not Path(XATTR).is_file():
        return
    subprocess.run(
        [XATTR, "-cr", str(path)],
        capture_output=True,
        text=True,
        check=False,
    )


def quarantine_present(listing: str) -> bool:
    return "com.apple.quarantine" in listing


def ensure(bin_dir: Path) -> int:
    missing = []
    dirty = []
    for name in BINS:
        path = bin_dir / name
        if not path.exists():
            missing.append(str(path))
            continue
        listing = xattr_list(path)
        if quarantine_present(listing):
            xattr_clear(path)
            listing = xattr_list(path)
            if quarantine_present(listing):
                dirty.append(str(path))
    if missing:
        print(
            "ensure-unquarantined: missing bins: " + ", ".join(missing),
            file=sys.stderr,
        )
        return 1
    if dirty:
        print(
            "ensure-unquarantined: quarantine remains: " + ", ".join(dirty),
            file=sys.stderr,
        )
        return 1
    return 0


def main() -> int:
    return ensure(brew_bin_dir())


if __name__ == "__main__":
    raise SystemExit(main())
