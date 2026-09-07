"""Adversary break tests: brew 0.4.0 quarantine clear (do not weaken).

Permanent RED until product actually clears com.apple.quarantine on
Homebrew-Cleaner 0555 keg bins and stops telling humans to run xattr.

Run:
  python3 -m unittest integrations/hermes-plugin/tests/test_adv_xattr_040.py -v
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = PLUGIN_ROOT.parents[1]
SCRIPT = PLUGIN_ROOT / "scripts" / "ensure-unquarantined.py"
FORMULA = REPO_ROOT / "Formula" / "agentpaas.rb"
GORELEASER = REPO_ROOT / ".goreleaser.yaml"
PLUGIN_SKILL = PLUGIN_ROOT / "SKILL.md"

BINS = (
    "agentpaas",
    "agentpaasd",
    "agentpaas-harness-linux",
    "agentpaas-harness-linux-amd64",
)

XATTR = Path("/usr/bin/xattr")
HAS_XATTR = sys.platform == "darwin" and XATTR.is_file()

CUSTOMER_DOCS = (
    REPO_ROOT / "README.md",
    REPO_ROOT / "demo" / "README.md",
    REPO_ROOT / "docs" / "quickstart.md",
    REPO_ROOT / "docs" / "troubleshooting.md",
    REPO_ROOT / "docs" / "customer" / "trial" / "21-install-macos.md",
    REPO_ROOT / "docs" / "customer" / "trial" / "60-troubleshooting.md",
    REPO_ROOT / "docs" / "customer" / "RELEASE-v0.3.7.md",
)


def _write_bins(bin_dir: Path) -> None:
    for name in BINS:
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


class AdvFormulaPostInstallPermsTests(unittest.TestCase):
    """SC1: Formula post_install runs after Homebrew Cleaner chmod 0555."""

    def test_post_install_chmods_u_plus_w_before_xattr(self):
        text = FORMULA.read_text(encoding="utf-8")
        self.assertIn("def post_install", text)
        start = text.find("def post_install")
        block = text[start:]
        self.assertRegex(
            block,
            r"chmod|u\+w",
            "post_install xattr -cr on keg 0555 bins is EACCES; "
            "Homebrew Cleaner.clean runs before post_install",
        )


class AdvEnsureUnquarantinedPermsTests(unittest.TestCase):
    """SC6: agent-side script must heal brew 0555 bins, not just 0755 temps."""

    @unittest.skipUnless(HAS_XATTR, "macOS xattr required")
    def test_clears_quarantine_on_0555_bins(self):
        with tempfile.TemporaryDirectory() as tmp:
            bin_dir = Path(tmp)
            _write_bins(bin_dir)
            for name in BINS:
                path = bin_dir / name
                _set_quarantine(path)
                path.chmod(0o555)
                self.assertIn("com.apple.quarantine", _list_xattr(path))
            result = _run_script(bin_dir)
            self.assertEqual(
                result.returncode,
                0,
                "0555 keg mode is what brew leaves; script must chmod u+w "
                f"then clear. stderr={result.stderr!r} stdout={result.stdout!r}",
            )
            for name in BINS:
                with self.subTest(bin=name):
                    self.assertNotIn(
                        "com.apple.quarantine", _list_xattr(bin_dir / name)
                    )


class AdvCustomerDocsXattrTests(unittest.TestCase):
    """SC5: do not tell the user to run xattr as the product answer."""

    def test_customer_docs_do_not_instruct_user_xattr_cr(self):
        missing = [p for p in CUSTOMER_DOCS if not p.is_file()]
        self.assertFalse(missing, f"missing docs: {missing}")
        offenders = []
        for path in CUSTOMER_DOCS:
            text = path.read_text(encoding="utf-8")
            if re.search(r"xattr\s+-cr", text):
                offenders.append(str(path.relative_to(REPO_ROOT)))
        self.assertEqual(
            offenders,
            [],
            "GitHub install still tells humans to run xattr -cr",
        )


class AdvDoctorCheckCountTests(unittest.TestCase):
    def test_plugin_skill_doctor_table_is_7_not_6(self):
        text = PLUGIN_SKILL.read_text(encoding="utf-8")
        needle = "Run system diagnostics (6 checks)"
        self.assertFalse(
            needle in text,
            "slash table says 6 checks while install path requires 7/7",
        )


class AdvGoreleaserHookNoChmodNeededIf0755(unittest.TestCase):
    """Cask extract is 0755; hook must still exist (non-break contract)."""

    def test_hook_still_targets_staged_path_four_bins(self):
        text = GORELEASER.read_text(encoding="utf-8")
        self.assertIn("staged_path", text)
        for name in BINS:
            self.assertIn(name, text)


if __name__ == "__main__":
    unittest.main()
