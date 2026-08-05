#!/usr/bin/env python3
"""Compatibility entry point for the installed plugin tree."""
from pathlib import Path
import runpy


if __name__ == "__main__":
    runpy.run_path(
        str(
            Path(__file__).resolve().parents[1]
            / "integrations"
            / "hermes-plugin"
            / "scripts"
            / "verify-installed-state.py"
        ),
        run_name="__main__",
    )
