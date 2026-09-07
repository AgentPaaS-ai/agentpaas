#!/usr/bin/env python3
"""Install and start Colima/Docker before agentpaas doctor.

The agent runs this after brew and before doctor. Do not print a human
brew-install instruction.
"""
from __future__ import annotations

import os
import shutil
import subprocess
import sys
import time
from pathlib import Path

POLL_CAP_S = 180
POLL_EVERY_S = 2
BREW_INSTALL_TIMEOUT_S = 300
COLIMA_START_TIMEOUT_S = 240
DOCKER_INFO_TIMEOUT_S = 30


def brew_bin_dir() -> Path:
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
        print("ensure-docker-runtime: brew --prefix failed", file=sys.stderr)
        raise SystemExit(1)
    return Path(prefix) / "bin"


def prepend_brew_path() -> None:
    brew_bin = str(brew_bin_dir())
    os.environ["PATH"] = brew_bin + os.pathsep + os.environ.get("PATH", "")
    os.environ.pop("DOCKER_HOST", None)


def docker_info_ok() -> bool:
    docker = shutil.which("docker")
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
    return not all(shutil.which(name) for name in ("docker", "colima", "lima"))


def brew_install_runtime() -> int:
    brew = shutil.which("brew") or "brew"
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
    colima = shutil.which("colima") or "colima"
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
    prepend_brew_path()
    if shutil.which("docker") and docker_info_ok():
        return 0

    if runtime_missing():
        if brew_install_runtime() != 0:
            return 1
        prepend_brew_path()

    if not docker_info_ok():
        colima_start()

    if shutil.which("docker") and wait_docker_ready():
        return 0
    print("ensure-docker-runtime: docker info not ready", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
