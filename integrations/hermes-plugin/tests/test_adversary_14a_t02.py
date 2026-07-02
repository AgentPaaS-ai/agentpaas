"""Adversary regression tests for B14A-T02 AGENTPAAS_CLI binary verification (GAP-2).
These tests are designed to break security claims. Do not weaken or remove.
Run with: python3 -m unittest discover -s integrations/hermes-plugin/tests -v -k AdversaryT02
"""

import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).parent))
from test_plugin_skeleton import _load_plugin_package


def _make_fake_agentpaas_script(path, version_output="agentpaas v0.1.0"):
    """Write an executable script that mimics agentpaas --version (or fake output)."""
    path.write_text(
        f"#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '{version_output}'; fi\n"
    )
    path.chmod(0o755)


class AdversaryBinaryVerificationTests(unittest.TestCase):
    """Target: weaknesses in _check_binary_in_allow_list + _verify_agentpaas_binary."""

    def setUp(self):
        self.plugin = _load_plugin_package()
        self.original_env = os.environ.copy()

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.original_env)

    # ADVERSARY BREAK: HOME env override makes ~/.local/bin attacker-controlled
    # Severity: HIGH — allow-list bypass via env var manipulation (blocked by pwd fix)
    def test_adversary_home_override_blocked(self):
        """HIGH: Setting HOME to redirect ~/.local/bin is now blocked (uses pwd module)."""
        with tempfile.TemporaryDirectory() as tmpdir:
            attacker_home = Path(tmpdir) / "attacker"
            attacker_local = attacker_home / ".local" / "bin"
            attacker_local.mkdir(parents=True)
            fake = attacker_local / "agentpaas"
            _make_fake_agentpaas_script(fake, version_output="agentpaas v0.1.0")
            with mock.patch.dict(
                os.environ,
                {"HOME": str(attacker_home), "AGENTPAAS_CLI": str(fake)},
                clear=False,
            ):
                # After fix: HOME override is blocked, binary is rejected
                with self.assertRaises(ValueError) as ctx:
                    self.plugin.tools._resolve_agentpaas_binary()
                self.assertIn("outside allowed", str(ctx.exception))

    # ADVERSARY BREAK: --version substring check accepts any output containing "agentpaas"
    # Severity: MEDIUM — weak verification allows malicious wrappers/scripts that fake version
    def test_adversary_version_output_injection_passes_verify(self):
        """MEDIUM: Binary whose --version embeds 'agentpaas' (e.g. malicious wrapper) is accepted."""
        with tempfile.TemporaryDirectory(dir="/tmp") as tmpdir:
            allowed = Path(tmpdir) / "allowed_bin"
            allowed.mkdir()
            fake = allowed / "agentpaas"
            # Outputs text containing 'agentpaas' but is clearly not the real CLI
            _make_fake_agentpaas_script(fake, version_output="malicious-agentpaas-wrapper v1.0")
            with mock.patch.object(
                self.plugin.tools, "_CLI_BINARY_ALLOW_LIST", (str(allowed),)
            ):
                with mock.patch.dict(
                    os.environ, {"AGENTPAAS_CLI": str(fake)}, clear=False
                ):
                    # Currently passes verify because "agentpaas" in lower(output)
                    resolved = self.plugin.tools._resolve_agentpaas_binary()
                    self.assertEqual(resolved, os.path.realpath(str(fake)))
                    # This demonstrates the weak check; real fix would require stronger verification (sig, hash, etc.)

    # ADVERSARY BREAK: shell script faking --version but evil on other invocations
    # (same mechanism as above; explicit test for script case)
    def test_adversary_shell_script_fakes_version(self):
        """MEDIUM: AGENTPAAS_CLI can be a shell script that only fakes --version."""
        with tempfile.TemporaryDirectory(dir="/tmp") as tmpdir:
            allowed = Path(tmpdir) / "allowed_bin"
            allowed.mkdir()
            script = allowed / "agentpaas"
            _make_fake_agentpaas_script(script, version_output="agentpaas v0.1.0")
            with mock.patch.object(
                self.plugin.tools, "_CLI_BINARY_ALLOW_LIST", (str(allowed),)
            ):
                with mock.patch.dict(
                    os.environ, {"AGENTPAAS_CLI": str(script)}, clear=False
                ):
                    resolved = self.plugin.tools._resolve_agentpaas_binary()
                    self.assertTrue(os.path.isfile(resolved))


if __name__ == "__main__":
    unittest.main()