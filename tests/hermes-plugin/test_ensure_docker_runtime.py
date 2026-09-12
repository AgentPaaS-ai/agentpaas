"""Contract tests: GitHub install must start Colima/Docker before doctor.

File reads are the product. Do not run brew or colima in these tests.

Run:
  python3 -m unittest tests/hermes-plugin/test_ensure_docker_runtime.py -v
"""

from __future__ import annotations

import re
import stat
import unittest
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parents[2] / "integrations" / "hermes-plugin"
SCRIPT = PLUGIN_ROOT / "scripts" / "ensure-docker-runtime.py"
COMPLETE = PLUGIN_ROOT / "scripts" / "complete-install.py"
PLUGIN_SKILL = PLUGIN_ROOT / "SKILL.md"
SETUP_SKILL = PLUGIN_ROOT / "skills" / "setup" / "SKILL.md"

OPTIONAL_DOCKER = "If `docker` or `colima` is missing"
INSTALL_URL = (
    "https://github.com/AgentPaaS-ai/agentpaas/tree/main/install"
)


class EnsureDockerRuntimeScriptTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = SCRIPT.read_text(encoding="utf-8") if SCRIPT.is_file() else ""
        cls.complete = COMPLETE.read_text(encoding="utf-8") if COMPLETE.is_file() else ""

    def test_script_exists(self):
        self.assertTrue(SCRIPT.is_file(), f"missing {SCRIPT}")
        mode = SCRIPT.stat().st_mode
        self.assertTrue(mode & stat.S_IXUSR, "ensure-docker-runtime.py must be executable")

    def test_source_installs_docker_colima_lima(self):
        self.assertIn("brew install docker colima lima", self.source)

    def test_source_starts_colima_with_users_mount(self):
        self.assertIn("colima start --mount /Users:w", self.source)

    def test_source_does_not_set_docker_host(self):
        self.assertNotIn("DOCKER_HOST=", self.source)
        self.assertNotIn("DOCKER_HOST=", self.complete)

    def test_source_does_not_tell_human_to_install_docker(self):
        lower = self.source.lower()
        self.assertNotIn("run brew install docker yourself", lower)
        self.assertNotIn("please brew install docker", lower)
        self.assertNotIn("tell the user to", lower)
        self.assertNotIn("yourself", lower)

    def test_source_probes_homebrew_prefixes(self):
        self.assertIn("/opt/homebrew/bin", self.source)
        self.assertIn("/usr/local/bin", self.source)
        self.assertIn("/opt/homebrew/bin", self.complete)
        self.assertIn("/usr/local/bin", self.complete)

    def test_source_bootstraps_homebrew_without_curl_pipe(self):
        self.assertIn("urllib", self.source)
        self.assertIn("/bin/bash", self.source)
        self.assertIn("NONINTERACTIVE", self.source)
        self.assertNotIn("curl |", self.source)
        self.assertNotIn("curl|", self.source)
        joined = " ".join(self.source.split())
        self.assertNotIn("| bash", joined)
        self.assertNotIn("| python", joined)

    def test_source_does_not_exit_only_because_which_brew_is_none(self):
        self.assertNotRegex(
            self.source,
            r"shutil\.which\(\s*[\"']brew[\"']\s*\)\s+or\s+[\"']brew[\"']",
        )


class DockerRuntimeSkillContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = PLUGIN_SKILL.read_text(encoding="utf-8")
        cls.setup = SETUP_SKILL.read_text(encoding="utf-8")

    def test_skills_do_not_treat_docker_as_optional(self):
        for label, text in (("plugin", self.plugin), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertNotIn(OPTIONAL_DOCKER, text)

    def test_skills_contain_ensure_docker_runtime(self):
        for label, text in (("plugin", self.plugin), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertIn("ensure-docker-runtime", text)

    def test_skills_do_not_tell_user_to_brew_install_docker(self):
        for label, text in (("plugin", self.plugin), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertNotIn("do not tell the user", text.lower())
                self.assertIn("ensure-docker-runtime", text)
                for line in text.splitlines():
                    if re.search(r"brew install (docker|colima)", line, re.I):
                        self.assertRegex(
                            line,
                            r"(?i)agent",
                            f"{label} SKILL tells the user to brew install docker/colima: {line}",
                        )

    def test_skills_keep_docker_host_and_colima_mount_rules(self):
        for label, text in (("plugin", self.plugin), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertIn("NEVER set `DOCKER_HOST`", text)
                self.assertNotIn("sudo", text.lower())
                self.assertIn("docker.sock", text)
                self.assertIn("colima start --mount /Users:w", text)
                self.assertIn("0.4.0", text)
                self.assertNotIn("Do not tell the user to run xattr", text)
                self.assertIn("7/7", text)

    def test_skills_install_plugin_subdir(self):
        for label, text in (("plugin", self.plugin), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertIn(INSTALL_URL, text)
                self.assertNotIn("scan_on_install", text)
                self.assertNotIn("plugins.scan_on_install", text)

    def test_skills_do_not_name_exfil_webhook_host(self):
        for label, text in (("plugin", self.plugin), ("setup", self.setup)):
            with self.subTest(skill=label):
                self.assertNotIn("webhook.site", text)


if __name__ == "__main__":
    unittest.main()
