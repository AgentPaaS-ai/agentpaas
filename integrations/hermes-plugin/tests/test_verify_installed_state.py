import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[3]
COMPLETE_SCRIPT = REPO_ROOT / "scripts" / "complete-install.py"
VERIFY_SCRIPT = REPO_ROOT / "scripts" / "verify-installed-state.py"


class VerifyInstalledStateTests(unittest.TestCase):
    def test_accepts_shim_plugin_tree_after_complete_install(self):
        with tempfile.TemporaryDirectory() as home:
            profile_dir = self._make_profile(home)
            self._make_shim_plugin(profile_dir)

            complete = self._run(COMPLETE_SCRIPT, home)
            self.assertEqual(complete.returncode, 0, complete.stderr)

            verify = self._run(VERIFY_SCRIPT, home)
            self.assertEqual(verify.returncode, 0, verify.stdout + verify.stderr)
            self.assertIn("nested shim plugin files", verify.stdout)
            self.assertIn("daemon socket not found", verify.stdout)

    def test_missing_nested_tools_fails_for_shim_plugin_tree(self):
        with tempfile.TemporaryDirectory() as home:
            profile_dir = self._make_profile(home)
            plugin_dir = self._make_shim_plugin(profile_dir)
            (plugin_dir / "integrations" / "hermes-plugin" / "tools.py").unlink()

            complete = self._run(COMPLETE_SCRIPT, home)
            self.assertEqual(complete.returncode, 0, complete.stderr)

            verify = self._run(VERIFY_SCRIPT, home)
            self.assertNotEqual(verify.returncode, 0)
            self.assertIn("plugin tools.py missing", verify.stdout)

    @staticmethod
    def _make_profile(home):
        profile_dir = Path(home) / ".hermes" / "profiles" / "cold-test"
        profile_dir.mkdir(parents=True)
        (profile_dir / "config.yaml").write_text(
            "platform_toolsets:\n  cli:\n    - other\n"
            "plugins:\n  enabled:\n    - agentpaas\n",
            encoding="utf-8",
        )
        return profile_dir

    @staticmethod
    def _make_shim_plugin(profile_dir):
        plugin_dir = profile_dir / "plugins" / "agentpaas"
        nested = plugin_dir / "integrations" / "hermes-plugin"
        nested.mkdir(parents=True)
        (plugin_dir / "plugin.yaml").write_text("name: agentpaas\n", encoding="utf-8")
        (plugin_dir / "__init__.py").write_text("", encoding="utf-8")
        (nested / "tools.py").write_text("", encoding="utf-8")
        (nested / "SKILL.md").write_text("# AgentPaaS\n", encoding="utf-8")
        return plugin_dir

    @staticmethod
    def _run(script, home):
        env = os.environ.copy()
        env["HOME"] = home
        return subprocess.run(
            [sys.executable, str(script), "cold-test"],
            capture_output=True,
            text=True,
            env=env,
            check=False,
        )


if __name__ == "__main__":
    unittest.main()
