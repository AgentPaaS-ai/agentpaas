"""Contract tests: brew 0.4.0 post-install clears macOS quarantine.

File reads are the product. No live brew.

Run:
  python3 -m unittest tests/hermes-plugin/test_brew_quarantine_contract.py -v
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

try:
    import yaml
except ImportError:
    yaml = None

PLUGIN_ROOT = Path(__file__).resolve().parents[2] / "integrations" / "hermes-plugin"
REPO_ROOT = PLUGIN_ROOT.parents[1]
FORMULA = REPO_ROOT / "Formula" / "agentpaas.rb"
GORELEASER = REPO_ROOT / ".goreleaser.yaml"
PLUGIN_SKILL = PLUGIN_ROOT / "SKILL.md"
SETUP_SKILL = PLUGIN_ROOT / "skills" / "setup" / "SKILL.md"

BINS = (
    "agentpaas",
    "agentpaasd",
    "agentpaas-harness-linux",
    "agentpaas-harness-linux-amd64",
)

OFFICIAL_BREW = "brew install agentpaas-ai/tap/agentpaas"


class FormulaQuarantineContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = FORMULA.read_text(encoding="utf-8")

    def test_version_is_0_4_0(self):
        self.assertIn('version "0.4.1"', self.text)
        self.assertNotIn('version "0.4.2"', self.text)

    def test_defines_post_install_xattr_on_four_bins(self):
        self.assertIn("def post_install", self.text)
        self.assertIn("xattr", self.text)
        self.assertIn("/usr/bin/xattr", self.text)
        self.assertIn("-cr", self.text)
        for name in BINS:
            with self.subTest(bin=name):
                self.assertIn(name, self.text)

    def test_keeps_version_assertion(self):
        self.assertRegex(self.text, r"assert_match\(/0\\.4\\.1/")


class GoreleaserCaskHookContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = GORELEASER.read_text(encoding="utf-8")
        cls.data = yaml.safe_load(cls.text) if yaml is not None else None

    def test_homebrew_casks_has_post_install_hook(self):
        self.assertIn("homebrew_casks:", self.text)
        if self.data is None:
            self.assertIn("hooks:", self.text)
            self.assertIn("staged_path", self.text)
            self.assertIn("xattr", self.text)
            for name in BINS:
                self.assertIn(name, self.text)
            return
        casks = self.data.get("homebrew_casks") or []
        self.assertTrue(casks, "homebrew_casks must be a non-empty list")
        cask = casks[0]
        hook = ((cask.get("hooks") or {}).get("post") or {}).get("install")
        self.assertIsInstance(hook, str)
        self.assertIn("staged_path", hook)
        self.assertIn("xattr", hook)
        self.assertIn("-cr", hook)
        for name in BINS:
            with self.subTest(bin=name):
                self.assertIn(name, hook)


class PluginSkillQuarantineContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = PLUGIN_SKILL.read_text(encoding="utf-8")
        cls.setup = SETUP_SKILL.read_text(encoding="utf-8")
        cls.plugin_install = cls._section(cls.plugin, "## Installation")
        cls.setup_body = cls._body(cls.setup)

    @staticmethod
    def _body(text: str) -> str:
        if text.startswith("---"):
            parts = text.split("---", 2)
            if len(parts) >= 3:
                return parts[2]
        return text

    @staticmethod
    def _section(text: str, heading: str) -> str:
        idx = text.find(heading)
        if idx < 0:
            return ""
        rest = text[idx + len(heading) :]
        nxt = re.search(r"\n## ", rest)
        return heading + (rest if nxt is None else rest[: nxt.start()])

    def test_github_install_is_full_product(self):
        for label, text in (("plugin", self.plugin), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertIn("https://github.com/AgentPaaS-ai/agentpaas", text)
                self.assertIn("FULL product", text)
                self.assertIn("Plugin-only is a fail", text)

    def test_official_tap_lands_0_4_0(self):
        for label, text in (("plugin", self.plugin_install), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertIn(OFFICIAL_BREW, text)
                self.assertIn("0.4.0", text)
                self.assertIn("HEAD", text)
                self.assertIn("0.4.1", text)
                self.assertNotIn("ap-testing/agentpaas", text)

    def test_verify_quarantine_gone_before_doctor(self):
        slices = (
            ("plugin", self.plugin_install),
            ("setup", self.setup_body),
        )
        for label, text in slices:
            with self.subTest(skill=label):
                self.assertIn("com.apple.quarantine", text)
                self.assertIn("brew --prefix", text)
                self.assertIn("7/7", text)
                lower = text.lower()
                self.assertIn("before", lower)
                self.assertIn("doctor", lower)
                qpos = lower.find("quarantine")
                dpos = lower.find("doctor")
                self.assertNotEqual(qpos, -1)
                self.assertNotEqual(dpos, -1)
                self.assertLess(qpos, dpos)

    def test_agent_runs_xattr_without_hiding_it_from_the_user(self):
        for label, text in (("plugin", self.plugin), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertNotIn("do not tell the user", text.lower())
                self.assertIn("xattr", text)
                self.assertNotIn("Then clear quarantine", text)
                self.assertNotIn("Ran `xattr -cr`", text)
                self.assertNotRegex(text, r"(?i)please run [`']?xattr")
                self.assertNotRegex(text, r"(?i)fix \| run `xattr")


if __name__ == "__main__":
    unittest.main()
