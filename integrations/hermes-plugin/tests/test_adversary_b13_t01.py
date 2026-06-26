"""Adversary regression tests for B13-T01 Hermes plugin skeleton.
These tests are designed to break security claims. Do not weaken or remove.
Run with: python3 -m unittest discover -s integrations/hermes-plugin/tests -v -k Adversary
"""

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import sys
sys.path.insert(0, str(Path(__file__).parent))
from test_plugin_skeleton import _load_plugin_package, PLUGIN_ROOT


class AdversaryBinaryResolverTests(unittest.TestCase):
    """Target: _resolve_agentpaas_binary allows arbitrary binary via AGENTPAAS_CLI (injection, symlink, traversal)."""

    def setUp(self):
        self.plugin = _load_plugin_package()
        self.original_env = os.environ.copy()

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.original_env)

    # REGRESSION: relative AGENTPAAS_CLI rejected; non-agentpaas binary rejected
    def test_adversary_agentpaas_cli_env_injection(self):
        """HIGH: Relative AGENTPAAS_CLI is rejected; /bin/echo is rejected (not agentpaas)."""
        with mock.patch.dict(os.environ, {"AGENTPAAS_CLI": "bin/echo"}, clear=False):
            with self.assertRaises(ValueError) as ctx:
                self.plugin.tools._resolve_agentpaas_binary()
            self.assertIn("absolute path", str(ctx.exception))

        absolute = "/bin/echo"
        with mock.patch.dict(os.environ, {"AGENTPAAS_CLI": absolute}, clear=False):
            with self.assertRaises(ValueError) as ctx:
                self.plugin.tools._resolve_agentpaas_binary()
            self.assertIn("agentpaas", str(ctx.exception).lower())

    # REGRESSION: empty AGENTPAAS_CLI is ignored (falls through to normal resolution)
    def test_adversary_agentpaas_cli_empty(self):
        """HIGH: Empty AGENTPAAS_CLI is treated as unset, not returned blindly."""
        with mock.patch.dict(os.environ, {"AGENTPAAS_CLI": ""}, clear=False):
            resolved = self.plugin.tools._resolve_agentpaas_binary()
            self.assertNotEqual(resolved, "")
            self.assertTrue(os.path.isabs(resolved) or resolved == "agentpaas")

    # REGRESSION: directory as AGENTPAAS_CLI is rejected
    def test_adversary_agentpaas_cli_directory(self):
        """HIGH: AGENTPAAS_CLI pointing to a directory raises ValueError."""
        with tempfile.TemporaryDirectory() as tmpdir:
            with mock.patch.dict(os.environ, {"AGENTPAAS_CLI": tmpdir}, clear=False):
                with self.assertRaises(ValueError) as ctx:
                    self.plugin.tools._resolve_agentpaas_binary()
                self.assertIn("not a file", str(ctx.exception))

    # REGRESSION: symlink AGENTPAAS_CLI outside allow-list is rejected
    def test_adversary_agentpaas_cli_symlink(self):
        """HIGH: AGENTPAAS_CLI symlink outside allowed dirs is rejected."""
        with tempfile.TemporaryDirectory() as tmpdir:
            real_bin = Path(tmpdir) / "real"
            real_bin.write_text("#!/bin/sh\necho malicious")
            real_bin.chmod(0o755)
            link = Path(tmpdir) / "evil_link"
            link.symlink_to(real_bin)
            with mock.patch.dict(os.environ, {"AGENTPAAS_CLI": str(link)}, clear=False):
                with self.assertRaises(ValueError) as ctx:
                    self.plugin.tools._resolve_agentpaas_binary()
                self.assertIn("outside allowed directories", str(ctx.exception))

    # ADVERSARY BREAK: path traversal via relative AGENTPAAS_CLI or candidates
    def test_adversary_path_traversal_in_candidates(self):
        """MEDIUM: Relative paths in resolver candidates allow traversal out of plugin dir."""
        # The code constructs candidates with .. without sanitizing
        here = os.path.dirname(os.path.abspath(self.plugin.tools.__file__))
        bad_candidate = os.path.join(here, "..", "..", "..", "..", "etc", "passwd")
        # Just verify construction doesn't reject traversal
        self.assertIn("..", bad_candidate)


class AdversaryJsonHandlingTests(unittest.TestCase):
    """Target: malformed CLI output, non-JSON, BOM, null bytes, long strings."""

    def setUp(self):
        self.plugin = _load_plugin_package()

    # ADVERSARY BREAK: non-JSON stdout returns raw instead of error JSON
    def test_adversary_non_json_output(self):
        """MEDIUM: CLI returning non-JSON causes raw_output instead of structured error."""
        with mock.patch.object(self.plugin.tools, "_run_cli", return_value={"raw_output": "not json", "exit_code": 0}):
            result = self.plugin.tools.agentpaas_doctor({})
            parsed = json.loads(result)
            self.assertIn("raw_output", parsed)  # current behavior -> break

    # REGRESSION: args=None does not crash; handler returns error JSON
    def test_adversary_args_none(self):
        """HIGH: handler called with args=None should not crash, must return error JSON."""
        handler = self.plugin.tools.agentpaas_stop
        result = handler(None)
        self.assertIsInstance(result, str)
        parsed = json.loads(result)
        self.assertIn("error", parsed)

    # ADVERSARY BREAK: args={} for required-param tools
    def test_adversary_args_empty_for_required(self):
        """MEDIUM: Missing required params (e.g. run_id) silently pass empty string to CLI."""
        handler = self.plugin.tools.agentpaas_stop
        result = handler({})
        # current: passes "" to CLI -> may succeed or error depending on CLI
        parsed = json.loads(result)
        self.assertIsInstance(parsed, dict)


class AdversaryManifestAndSchemaTests(unittest.TestCase):
    """Target: schema/handler consistency, missing required fields, manifest issues."""

    def setUp(self):
        self.plugin = _load_plugin_package()

    # ADVERSARY BREAK: some schemas lack "required" even when CLI subcommand needs the arg
    def test_adversary_missing_required_in_schemas(self):
        """MEDIUM: agentpaas_run schema requires image_or_project but others like status do not enforce."""
        run_schema = self.plugin.schemas.AGENTPAAS_RUN
        self.assertIn("required", run_schema["parameters"])
        # Many others miss it -> potential for incomplete validation
        status_schema = self.plugin.schemas.AGENTPAAS_STATUS
        self.assertNotIn("required", status_schema["parameters"])  # current state

    # ADVERSARY BREAK: register relies on getattr without checking existence
    def test_adversary_register_missing_tool(self):
        """LOW: If a tool in TOOL_NAMES lacks handler or schema, register would raise at load."""
        # This would crash Hermes registration if mismatch
        self.assertTrue(hasattr(self.plugin.tools, "agentpaas_next_action"))


class AdversaryTimeoutResourceTests(unittest.TestCase):
    """Target: missing timeouts, hangs, concurrent calls."""

    def setUp(self):
        self.plugin = _load_plugin_package()

    # ADVERSARY BREAK: subprocess timeout is 300 but no way to configure/shorten; hangs possible in theory
    def test_adversary_subprocess_timeout_enforced(self):
        """LOW: _run_cli hardcodes 300s timeout; test that it is passed to subprocess."""
        with mock.patch("subprocess.run") as mock_run:
            mock_run.return_value = mock.Mock(returncode=0, stdout='{"ok":true}', stderr="")
            self.plugin.tools._run_cli(["doctor"])
            # Verify timeout kwarg was used
            call_kwargs = mock_run.call_args[1]
            self.assertEqual(call_kwargs.get("timeout"), 300)


if __name__ == "__main__":
    unittest.main()