"""AGENTPAAS_CLI binary verification tests for B14A-T02 (GAP-2)."""

import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from test_plugin_skeleton import REPO_ROOT, _load_plugin_package


def _make_fake_agentpaas_script(path, version_output="agentpaas v0.1.0", hang=False):
    """Write an executable script that mimics agentpaas --version."""
    if hang:
        path.write_text("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then sleep 60; fi\n")
    else:
        path.write_text(
            f"#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '{version_output}'; fi\n"
        )
    path.chmod(0o755)


class BinaryVerificationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.tools = cls.plugin.tools

    def setUp(self):
        self.original_env = os.environ.copy()

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.original_env)

    def test_valid_binary_passes(self):
        with tempfile.TemporaryDirectory(dir="/tmp") as tmpdir:
            allowed = Path(tmpdir) / "allowed_bin"
            allowed.mkdir()
            fake = allowed / "agentpaas"
            _make_fake_agentpaas_script(fake)
            with mock.patch.object(
                self.tools, "_CLI_BINARY_ALLOW_LIST", (str(allowed),)
            ):
                with mock.patch.dict(
                    os.environ, {"AGENTPAAS_CLI": str(fake)}, clear=False
                ):
                    resolved = self.tools._resolve_agentpaas_binary()
            self.assertEqual(resolved, os.path.realpath(str(fake)))

    def test_reject_non_agentpaas_binary(self):
        with mock.patch.dict(os.environ, {"AGENTPAAS_CLI": "/bin/echo"}, clear=False):
            with self.assertRaises(ValueError) as ctx:
                self.tools._resolve_agentpaas_binary()
            self.assertIn("agentpaas", str(ctx.exception).lower())

    def test_reject_binary_outside_allow_list(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            fake = Path(tmpdir) / "agentpaas"
            _make_fake_agentpaas_script(fake)
            with mock.patch.dict(
                os.environ, {"AGENTPAAS_CLI": str(fake)}, clear=False
            ):
                with self.assertRaises(ValueError) as ctx:
                    self.tools._resolve_agentpaas_binary()
                self.assertIn("outside allowed directories", str(ctx.exception))

    def test_reject_symlink_to_non_agentpaas(self):
        with tempfile.TemporaryDirectory(dir="/tmp") as tmpdir:
            allowed = Path(tmpdir) / "allowed_bin"
            allowed.mkdir()
            link = allowed / "agentpaas"
            link.symlink_to("/bin/echo")
            with mock.patch.object(
                self.tools, "_CLI_BINARY_ALLOW_LIST", (str(allowed),)
            ):
                with mock.patch.dict(
                    os.environ, {"AGENTPAAS_CLI": str(link)}, clear=False
                ):
                    with self.assertRaises(ValueError) as ctx:
                        self.tools._resolve_agentpaas_binary()
                    self.assertIn("agentpaas", str(ctx.exception).lower())

    def test_reject_timeout(self):
        with tempfile.TemporaryDirectory(dir="/tmp") as tmpdir:
            allowed = Path(tmpdir) / "allowed_bin"
            allowed.mkdir()
            fake = allowed / "agentpaas"
            _make_fake_agentpaas_script(fake, hang=True)
            with mock.patch.object(
                self.tools, "_CLI_BINARY_ALLOW_LIST", (str(allowed),)
            ):
                with mock.patch.dict(
                    os.environ, {"AGENTPAAS_CLI": str(fake)}, clear=False
                ):
                    with self.assertRaises(ValueError) as ctx:
                        self.tools._resolve_agentpaas_binary()
                    self.assertIn("timed out", str(ctx.exception))

    def test_repo_bin_allowed(self):
        repo_bin = REPO_ROOT / "bin"
        repo_bin.mkdir(parents=True, exist_ok=True)
        fake = repo_bin / "_test_agentpaas_verify"
        try:
            _make_fake_agentpaas_script(fake)
            with mock.patch.dict(
                os.environ, {"AGENTPAAS_CLI": str(fake)}, clear=False
            ):
                resolved = self.tools._resolve_agentpaas_binary()
            self.assertEqual(resolved, os.path.realpath(str(fake)))
        finally:
            if fake.exists():
                fake.unlink()

    def test_home_local_bin_allowed(self):
        local_bin = Path.home() / ".local" / "bin"
        local_bin.mkdir(parents=True, exist_ok=True)
        fake = local_bin / "_test_agentpaas_verify"
        try:
            _make_fake_agentpaas_script(fake)
            with mock.patch.dict(
                os.environ, {"AGENTPAAS_CLI": str(fake)}, clear=False
            ):
                resolved = self.tools._resolve_agentpaas_binary()
            self.assertEqual(resolved, os.path.realpath(str(fake)))
        finally:
            if fake.exists():
                fake.unlink()


if __name__ == "__main__":
    unittest.main()