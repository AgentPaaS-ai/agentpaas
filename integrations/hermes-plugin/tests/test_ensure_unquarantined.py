"""Tests for ensure-unquarantined.py (agent-side brew quarantine gate).

Run:
  python3 -m unittest integrations/hermes-plugin/tests/test_ensure_unquarantined.py -v
"""

from __future__ import annotations

import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
SCRIPT = PLUGIN_ROOT / "scripts" / "ensure-unquarantined.py"

BINS = (
    "agentpaas",
    "agentpaasd",
    "agentpaas-harness-linux",
    "agentpaas-harness-linux-amd64",
)

XATTR = Path("/usr/bin/xattr")
HAS_XATTR = sys.platform == "darwin" and XATTR.is_file()


def _load_mod():
    spec = importlib.util.spec_from_file_location("ensure_unquarantined", SCRIPT)
    assert spec is not None and spec.loader is not None
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def _write_bins(bin_dir: Path, names=BINS) -> None:
    for name in names:
        path = bin_dir / name
        path.write_bytes(b"fake-bin\n")
        path.chmod(0o755)


def _run_script(bin_dir: Path) -> subprocess.CompletedProcess:
    env = os.environ.copy()
    env["AGENTPAAS_BIN_DIR"] = str(bin_dir)
    return subprocess.run(
        [sys.executable, str(SCRIPT)],
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )


def _set_quarantine(path: Path) -> None:
    subprocess.run(
        [str(XATTR), "-w", "com.apple.quarantine", "0081;fake", str(path)],
        check=True,
        capture_output=True,
        text=True,
    )


def _list_xattr(path: Path) -> str:
    result = subprocess.run(
        [str(XATTR), "-l", str(path)],
        capture_output=True,
        text=True,
        check=False,
    )
    return (result.stdout or "") + (result.stderr or "")


class EnsureUnquarantinedTests(unittest.TestCase):
    def test_script_exists(self):
        self.assertTrue(SCRIPT.is_file(), f"missing {SCRIPT}")

    def test_clean_bins_exit_zero(self):
        with tempfile.TemporaryDirectory() as tmp:
            bin_dir = Path(tmp)
            _write_bins(bin_dir)
            result = _run_script(bin_dir)
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_missing_bin_exits_nonzero(self):
        with tempfile.TemporaryDirectory() as tmp:
            bin_dir = Path(tmp)
            _write_bins(bin_dir, names=BINS[:-1])
            result = _run_script(bin_dir)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("missing", result.stderr.lower())

    @unittest.skipUnless(HAS_XATTR, "macOS xattr required")
    def test_clears_fake_quarantine_then_exits_zero(self):
        with tempfile.TemporaryDirectory() as tmp:
            bin_dir = Path(tmp)
            _write_bins(bin_dir)
            for name in BINS:
                _set_quarantine(bin_dir / name)
                self.assertIn("com.apple.quarantine", _list_xattr(bin_dir / name))
            result = _run_script(bin_dir)
            self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
            for name in BINS:
                with self.subTest(bin=name):
                    self.assertNotIn(
                        "com.apple.quarantine", _list_xattr(bin_dir / name)
                    )

    def test_remaining_quarantine_exits_nonzero(self):
        mod = _load_mod()
        with tempfile.TemporaryDirectory() as tmp:
            bin_dir = Path(tmp)
            _write_bins(bin_dir)

            def fake_list(_path):
                return "com.apple.quarantine: still-here\n"

            orig_list = mod.xattr_list
            orig_clear = mod.xattr_clear
            try:
                mod.xattr_list = fake_list
                mod.xattr_clear = lambda _path: None
                code = mod.ensure(bin_dir)
            finally:
                mod.xattr_list = orig_list
                mod.xattr_clear = orig_clear
            self.assertEqual(code, 1)


if __name__ == "__main__":
    unittest.main()
