import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[3]
SCRIPT = REPO_ROOT / "integrations" / "hermes-plugin" / "scripts" / "complete-install.py"
SETUP_SKILL = REPO_ROOT / "integrations" / "hermes-plugin" / "skills" / "setup" / "SKILL.md"


class CompleteInstallTests(unittest.TestCase):
    def test_completes_install_and_is_idempotent(self):
        with tempfile.TemporaryDirectory() as home:
            profile = "cold-test"
            profile_dir = Path(home) / ".hermes" / "profiles" / profile
            profile_dir.mkdir(parents=True)
            (profile_dir / "config.yaml").write_text(
                "platform_toolsets:\n  cli:\n    - other\nplugins:\n  enabled:\n    - agentpaas\n",
                encoding="utf-8",
            )

            first = self._run_script(SCRIPT, home, profile)
            self.assertEqual(first.returncode, 0, first.stderr)
            self.assertIn("complete", first.stdout.lower())

            config = (profile_dir / "config.yaml").read_text(encoding="utf-8")
            self.assertIn("    - agentpaas", config)
            skill = profile_dir / "skills" / "agentpaas" / "SKILL.md"
            self.assertTrue(skill.is_file())
            skill_text = skill.read_text(encoding="utf-8")
            self.assertIn("agentpaas-build", skill_text)
            self.assertIn("on_invoke", skill_text)
            self.assertIn("# AgentPaaS Onboarding Rule", (profile_dir / "SOUL.md").read_text(encoding="utf-8"))

            second = self._run_script(SCRIPT, home, profile)
            self.assertEqual(second.returncode, 0, second.stderr)

    def test_setup_skill_requires_complete_install_and_reopen(self):
        text = SETUP_SKILL.read_text(encoding="utf-8")
        self.assertIn("complete-install.py", text)
        self.assertIn("verify-installed-state.py", text)
        self.assertIn("exits non-zero", text)
        self.assertIn("one session reopen", text.lower())
        self.assertNotIn("no restart is needed", text.lower())
        self.assertNotIn("available in this session", text.lower())

    @staticmethod
    def _run_script(script, home, profile):
        env = os.environ.copy()
        env["HOME"] = home
        return subprocess.run(
            [sys.executable, str(script), profile],
            capture_output=True,
            text=True,
            env=env,
            check=False,
        )


if __name__ == "__main__":
    unittest.main()
