"""Subprocess output cap and configurable timeout tests for B14A-T03 (GAP-3)."""

import json
import os
import unittest
from unittest import mock

from test_plugin_skeleton import _load_plugin_package


class CliTimeoutTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tools = _load_plugin_package().tools

    def setUp(self):
        self.original_env = os.environ.copy()

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.original_env)

    def test_default_timeout(self):
        os.environ.pop("AGENTPAAS_CLI_TIMEOUT", None)
        self.assertEqual(self.tools._get_cli_timeout(), 300)

    def test_custom_timeout(self):
        with mock.patch.dict(os.environ, {"AGENTPAAS_CLI_TIMEOUT": "60"}):
            self.assertEqual(self.tools._get_cli_timeout(), 60)

    def test_timeout_min_clamp(self):
        with mock.patch.dict(os.environ, {"AGENTPAAS_CLI_TIMEOUT": "5"}):
            self.assertEqual(self.tools._get_cli_timeout(), 10)

    def test_timeout_max_clamp(self):
        with mock.patch.dict(os.environ, {"AGENTPAAS_CLI_TIMEOUT": "999"}):
            self.assertEqual(self.tools._get_cli_timeout(), 600)

    def test_invalid_timeout(self):
        with mock.patch.dict(os.environ, {"AGENTPAAS_CLI_TIMEOUT": "abc"}):
            self.assertEqual(self.tools._get_cli_timeout(), 300)


class OutputCapTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tools = _load_plugin_package().tools

    def setUp(self):
        self.original_env = os.environ.copy()

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.original_env)

    def _mock_proc(self, stdout="", stderr="", returncode=0):
        return mock.Mock(returncode=returncode, stdout=stdout, stderr=stderr)

    def _oversized_stdout(self, payload, total_size=102400):
        """Build stdout larger than _STDOUT_CAP with valid JSON in the first cap bytes."""
        core = json.dumps(payload)
        pad = total_size - len(core)
        self.assertGreater(pad, 0)
        return core + (" " * pad)

    def test_stdout_truncation(self):
        cap = self.tools._STDOUT_CAP
        overhead = len(json.dumps({"data": ""}))
        payload = {"data": "x" * (cap - overhead)}
        stdout = self._oversized_stdout(payload)

        with mock.patch("subprocess.run", return_value=self._mock_proc(stdout=stdout)):
            result = self.tools._run_cli(["doctor"])

        self.assertTrue(result["output_truncated"])
        self.assertEqual(result["output_size"], 102400)
        self.assertEqual(result["data"], payload["data"])
        self.assertEqual(len(json.dumps(payload)), cap)

    def test_stderr_truncation(self):
        stderr = "e" * 20480
        with mock.patch(
            "subprocess.run",
            return_value=self._mock_proc(stderr=stderr, returncode=1),
        ):
            result = self.tools._run_cli(["doctor"])

        self.assertTrue(result["stderr_truncated"])
        self.assertEqual(result["stderr_size"], 20480)
        self.assertEqual(len(result["error"]), self.tools._STDERR_CAP)

    def test_no_truncation_for_small_output(self):
        stdout = json.dumps({"ok": True})
        with mock.patch("subprocess.run", return_value=self._mock_proc(stdout=stdout)):
            result = self.tools._run_cli(["doctor"])

        self.assertNotIn("output_truncated", result)
        self.assertNotIn("stderr_truncated", result)
        self.assertEqual(result, {"ok": True})

    def test_non_json_output_with_truncation(self):
        stdout = "N" * 102400
        with mock.patch("subprocess.run", return_value=self._mock_proc(stdout=stdout)):
            result = self.tools._run_cli(["doctor"])

        self.assertTrue(result["output_truncated"])
        self.assertEqual(result["output_size"], 102400)
        self.assertEqual(result["error_category"], "cli_non_json_output")
        self.assertEqual(len(result["raw_output_truncated"]), 2000)

    def test_successful_json_with_truncation(self):
        cap = self.tools._STDOUT_CAP
        overhead = len(json.dumps({"status": "ok", "blob": ""}))
        payload = {"status": "ok", "blob": "y" * (cap - overhead)}
        stdout = self._oversized_stdout(payload)

        with mock.patch("subprocess.run", return_value=self._mock_proc(stdout=stdout)):
            result = self.tools._run_cli(["doctor"])

        self.assertTrue(result["output_truncated"])
        self.assertEqual(result["output_size"], 102400)
        self.assertEqual(result["status"], "ok")
        self.assertEqual(result["blob"], payload["blob"])


if __name__ == "__main__":
    unittest.main()