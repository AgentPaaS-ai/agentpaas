#!/usr/bin/env python3
"""Install and start Colima/Docker before agentpaas doctor.

The agent runs this after brew and before doctor. Do not print a human
brew-install instruction.
"""
from __future__ import annotations

import os
import shutil
import stat
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

POLL_CAP_S = 180
POLL_EVERY_S = 2
BREW_INSTALL_TIMEOUT_S = 300
COLIMA_START_TIMEOUT_S = 240
DOCKER_INFO_TIMEOUT_S = 30
HOMEBREW_INSTALL_TIMEOUT_S = 600
HOMEBREW_BIN_DIRS = ("/opt/homebrew/bin", "/usr/local/bin")
HOMEBREW_INSTALLER_URL = (
    "https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh"
)


def resolve_bin(name: str) -> str | None:
    found = shutil.which(name)
    if found:
        return found
    for directory in HOMEBREW_BIN_DIRS:
        candidate = Path(directory) / name
        try:
            st = candidate.lstat()
        except OSError:
            continue
        if stat.S_ISDIR(st.st_mode):
            continue
        if os.access(candidate, os.X_OK):
            return str(candidate)
    return None


def brew_bin_dir() -> Path:
    brew = resolve_bin("brew")
    if brew:
        result = subprocess.run(
            [brew, "--prefix"],
            capture_output=True,
            text=True,
            check=False,
            timeout=10,
        )
        prefix = (result.stdout or "").strip()
        if result.returncode == 0 and prefix:
            return Path(prefix) / "bin"
    for directory in HOMEBREW_BIN_DIRS:
        path = Path(directory)
        if path.is_dir():
            return path
    print("ensure-docker-runtime: Homebrew bin dir not found", file=sys.stderr)
    raise SystemExit(1)


def prepend_brew_path() -> None:
    brew_bin = str(brew_bin_dir())
    parts = [brew_bin, *HOMEBREW_BIN_DIRS]
    os.environ["PATH"] = os.pathsep.join(parts) + os.pathsep + os.environ.get("PATH", "")
    os.environ.pop("DOCKER_HOST", None)


def docker_info_ok() -> bool:
    docker = resolve_bin("docker")
    if not docker:
        return False
    try:
        result = subprocess.run(
            [docker, "info"],
            capture_output=True,
            text=True,
            check=False,
            timeout=DOCKER_INFO_TIMEOUT_S,
        )
    except (OSError, subprocess.SubprocessError):
        return False
    return result.returncode == 0


def runtime_missing() -> bool:
    return not all(resolve_bin(name) for name in ("docker", "colima", "lima"))


def install_homebrew() -> int:
    installer = ""
    try:
        with tempfile.NamedTemporaryFile(
            prefix="homebrew-install-",
            suffix=".sh",
            delete=False,
        ) as fh:
            installer = fh.name
        urllib.request.urlretrieve(HOMEBREW_INSTALLER_URL, installer)
        env = os.environ.copy()
        env["NONINTERACTIVE"] = "1"
        result = subprocess.run(
            ["/bin/bash", installer],
            check=False,
            timeout=HOMEBREW_INSTALL_TIMEOUT_S,
            env=env,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        print(f"ensure-docker-runtime: Homebrew install failed: {exc}", file=sys.stderr)
        return 1
    finally:
        if installer:
            try:
                os.remove(installer)
            except OSError:
                pass
    if result.returncode != 0:
        print("ensure-docker-runtime: Homebrew install failed", file=sys.stderr)
        return 1
    return 0


def ensure_homebrew() -> int:
    if resolve_bin("brew"):
        return 0
    if install_homebrew() != 0:
        return 1
    if not resolve_bin("brew"):
        print("ensure-docker-runtime: brew missing after install", file=sys.stderr)
        return 1
    return 0


def brew_install_runtime() -> int:
    brew = resolve_bin("brew")
    if not brew:
        print("ensure-docker-runtime: brew not found", file=sys.stderr)
        return 1
    cmd = "brew install docker colima lima"
    argv = [brew, *cmd.split()[1:]]
    try:
        result = subprocess.run(
            argv,
            check=False,
            timeout=BREW_INSTALL_TIMEOUT_S,
        )
    except subprocess.SubprocessError as exc:
        print(f"ensure-docker-runtime: brew install failed: {exc}", file=sys.stderr)
        return 1
    if result.returncode != 0:
        print("ensure-docker-runtime: brew install failed", file=sys.stderr)
        return 1
    return 0


def colima_start() -> None:
    colima = resolve_bin("colima")
    if not colima:
        print("ensure-docker-runtime: colima not found", file=sys.stderr)
        return
    cmd = "colima start --mount /Users:w"
    argv = [colima, *cmd.split()[1:]]
    try:
        subprocess.run(
            argv,
            check=False,
            timeout=COLIMA_START_TIMEOUT_S,
        )
    except subprocess.SubprocessError as exc:
        print(f"ensure-docker-runtime: colima start failed: {exc}", file=sys.stderr)


def wait_docker_ready() -> bool:
    deadline = time.monotonic() + POLL_CAP_S
    while time.monotonic() < deadline:
        if docker_info_ok():
            return True
        time.sleep(POLL_EVERY_S)
    return False


def main() -> int:
    if ensure_homebrew() != 0:
        return 1
    prepend_brew_path()
    if resolve_bin("docker") and docker_info_ok():
        return 0

    if runtime_missing():
        if brew_install_runtime() != 0:
            return 1
        prepend_brew_path()

    if not docker_info_ok():
        colima_start()

    if resolve_bin("docker") and wait_docker_ready():
        return 0
    print("ensure-docker-runtime: docker info not ready", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
