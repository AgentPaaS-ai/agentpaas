"""Path allow-list validation tests for B14A-T01 (GAP-1)."""

import json
import os
import tempfile
import unittest
from contextlib import contextmanager
from unittest import mock

from test_plugin_skeleton import PLUGIN_ROOT, _load_plugin_package


@contextmanager
def _chdir(path):
    """Temporarily change the working directory."""
    prev = os.getcwd()
    os.chdir(path)
    try:
        yield
    finally:
        os.chdir(prev)


class PathAllowlistValidationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.tools = cls.plugin.tools

    def test_valid_project_dir(self):
        project_root = os.path.realpath(PLUGIN_ROOT)
        subdir = os.path.join(project_root, "tests")
        with mock.patch.dict(
            os.environ, {"AGENTPAAS_PROJECT_ROOT": project_root}, clear=False
        ):
            is_valid, resolved, err = self.tools._validate_project_path(subdir)
        self.assertTrue(is_valid)
        self.assertEqual(resolved, os.path.realpath(subdir))
        self.assertIsNone(err)

    def test_valid_dot(self):
        project_root = os.path.realpath(PLUGIN_ROOT)
        with _chdir(project_root):
            with mock.patch.dict(
                os.environ, {"AGENTPAAS_PROJECT_ROOT": project_root}, clear=False
            ):
                is_valid, resolved, err = self.tools._validate_project_path(".")
        self.assertTrue(is_valid)
        self.assertEqual(resolved, project_root)
        self.assertIsNone(err)

    def test_valid_tmp(self):
        is_valid, resolved, err = self.tools._validate_project_path("/tmp/test-agent")
        self.assertTrue(is_valid)
        self.assertEqual(resolved, os.path.realpath("/tmp/test-agent"))
        self.assertIsNone(err)

    def test_valid_home(self):
        home = os.path.expanduser("~")
        path = os.path.join(home, "my-agent")
        is_valid, resolved, err = self.tools._validate_project_path(
            os.path.join("~", "my-agent")
        )
        self.assertTrue(is_valid)
        self.assertEqual(resolved, os.path.realpath(path))
        self.assertIsNone(err)

    def test_reject_etc_passwd(self):
        is_valid, resolved, err = self.tools._validate_project_path("/etc/passwd")
        self.assertFalse(is_valid)
        self.assertIsNone(resolved)
        self.assertEqual(err["error_category"], "path_rejected")
        self.assertIn("/etc/passwd", err["error"])

    def test_reject_parent_traversal(self):
        base = "/var/tmp"
        if not os.path.isdir(base):
            self.skipTest("/var/tmp not available")
        with tempfile.TemporaryDirectory(dir=base) as project:
            project = os.path.realpath(project)
            with _chdir(project):
                with mock.patch.dict(
                    os.environ, {"AGENTPAAS_PROJECT_ROOT": project}, clear=False
                ):
                    is_valid, resolved, err = self.tools._validate_project_path(
                        "../../etc"
                    )
            self.assertFalse(is_valid)
            self.assertIsNone(resolved)
            self.assertEqual(err["error_category"], "path_rejected")

    def test_reject_absolute_outside(self):
        is_valid, resolved, err = self.tools._validate_project_path("/var/log")
        self.assertFalse(is_valid)
        self.assertIsNone(resolved)
        self.assertEqual(err["error_category"], "path_rejected")
        self.assertIn("/var/log", err["error"])

    def test_reject_symlink_escape(self):
        project_root = os.path.realpath(PLUGIN_ROOT)
        link_name = os.path.join(project_root, "_b14a_symlink_escape_test")
        try:
            if os.path.lexists(link_name):
                os.unlink(link_name)
            os.symlink("/etc", link_name)
            with mock.patch.dict(
                os.environ, {"AGENTPAAS_PROJECT_ROOT": project_root}, clear=False
            ):
                is_valid, resolved, err = self.tools._validate_project_path(link_name)
            self.assertFalse(is_valid)
            self.assertIsNone(resolved)
            self.assertEqual(err["error_category"], "path_rejected")
        finally:
            if os.path.lexists(link_name):
                os.unlink(link_name)

    def test_reject_empty(self):
        is_valid, resolved, err = self.tools._validate_project_path("")
        self.assertFalse(is_valid)
        self.assertIsNone(resolved)
        self.assertEqual(err["error_category"], "path_rejected")
        self.assertIn("required", err["error"])

    def test_reject_none(self):
        is_valid, resolved, err = self.tools._validate_project_path(None)
        self.assertFalse(is_valid)
        self.assertIsNone(resolved)
        self.assertEqual(err["error_category"], "path_rejected")

    def test_reject_home_override(self):
        """HOME=/etc must not add /etc to allowed_roots (pwd module used instead)."""
        with mock.patch.dict(os.environ, {"HOME": "/etc"}, clear=False):
            is_valid, resolved, err = self.tools._validate_project_path("/etc/passwd")
        self.assertFalse(is_valid)
        self.assertIsNone(resolved)
        self.assertEqual(err["error_category"], "path_rejected")

    def test_project_root_system_dir_fallback(self):
        """AGENTPAAS_PROJECT_ROOT=/ must fall back to cwd, not allow everything."""
        cwd = os.path.realpath(os.getcwd())
        outside = "/var/log" if cwd != "/var/log" else "/etc"
        with mock.patch.dict(
            os.environ, {"AGENTPAAS_PROJECT_ROOT": "/"}, clear=False
        ):
            is_valid, resolved, err = self.tools._validate_project_path(outside)
        self.assertFalse(is_valid)
        self.assertIsNone(resolved)
        self.assertEqual(err["error_category"], "path_rejected")

    def test_no_dead_traversal_check(self):
        """../../etc is rejected by allowed_roots after realpath (no '..' check)."""
        with tempfile.TemporaryDirectory(dir="/tmp") as project:
            project = os.path.realpath(project)
            with _chdir(project):
                with mock.patch.dict(
                    os.environ, {"AGENTPAAS_PROJECT_ROOT": project}, clear=False
                ):
                    is_valid, resolved, err = self.tools._validate_project_path(
                        "../../etc"
                    )
            self.assertFalse(is_valid)
            self.assertIsNone(resolved)
            self.assertEqual(err["error_category"], "path_rejected")
            self.assertIn("outside allowed roots", err["error"])


class PathAllowlistIntegrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.tools = cls.plugin.tools

    def test_init_project_rejects_bad_path(self):
        with mock.patch.object(self.tools, "_run_cli") as mock_cli:
            result = json.loads(
                self.tools.agentpaas_init_project({"project_dir": "/etc"})
            )
        self.assertEqual(result["error_category"], "path_rejected")
        mock_cli.assert_not_called()

    def test_pack_rejects_bad_path(self):
        with mock.patch.object(self.tools, "_run_cli") as mock_cli:
            result = json.loads(
                self.tools.agentpaas_pack({"project_dir": "/etc"})
            )
        self.assertEqual(result["error_category"], "path_rejected")
        mock_cli.assert_not_called()

    def test_validate_rejects_bad_path(self):
        with mock.patch.object(self.tools, "_run_cli") as mock_cli:
            result = json.loads(
                self.tools.agentpaas_validate_project({"project_dir": "/etc"})
            )
        self.assertEqual(result["error_category"], "path_rejected")
        mock_cli.assert_not_called()


if __name__ == "__main__":
    unittest.main()