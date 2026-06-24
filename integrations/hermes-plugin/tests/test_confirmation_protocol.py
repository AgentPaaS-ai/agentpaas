"""Confirmation protocol tests for B13-T03 trust-boundary actions."""

import importlib.util
import json
import sys
import types
import unittest
from pathlib import Path
from unittest import mock

PLUGIN_ROOT = Path(__file__).resolve().parents[1]


def _load_plugin_package():
    """Load the plugin as a package despite the hyphenated directory name."""
    pkg_name = "agentpaas_hermes_plugin"
    cached = sys.modules.get(pkg_name)
    if cached is not None and hasattr(cached, "register"):
        return cached

    pkg = types.ModuleType(pkg_name)
    pkg.__path__ = [str(PLUGIN_ROOT)]
    pkg.__package__ = pkg_name
    sys.modules[pkg_name] = pkg

    for mod_name in ("schemas", "tools", "contracts"):
        full_name = f"{pkg_name}.{mod_name}"
        spec = importlib.util.spec_from_file_location(
            full_name,
            PLUGIN_ROOT / f"{mod_name}.py",
        )
        module = importlib.util.module_from_spec(spec)
        module.__package__ = pkg_name
        sys.modules[full_name] = module
        setattr(pkg, mod_name, module)
        spec.loader.exec_module(module)

    init_spec = importlib.util.spec_from_file_location(
        pkg_name,
        PLUGIN_ROOT / "__init__.py",
        submodule_search_locations=[str(PLUGIN_ROOT)],
    )
    init_mod = importlib.util.module_from_spec(init_spec)
    init_mod.__package__ = pkg_name
    sys.modules[pkg_name] = init_mod
    init_spec.loader.exec_module(init_mod)
    return init_mod


class ConfirmationTestBase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.tools = cls.plugin.tools
        cls.contracts = cls.plugin.contracts

    def setUp(self):
        self.tools._reset_confirmation_state()

    def tearDown(self):
        self.tools._reset_confirmation_state()


class SessionRunTrackingTests(ConfirmationTestBase):
    def test_run_registers_session_run(self):
        with mock.patch.object(
            self.tools, "_run_cli", return_value={"run_id": "run_123"}
        ):
            self.tools.agentpaas_run({"image_or_project": "demo"})
        self.assertTrue(self.tools._is_session_run("run_123"))

    def test_stop_session_run_no_confirmation(self):
        self.tools._register_session_run("run_123")
        mock_result = {"schema_version": "1.0.0", "status": "stopped"}
        with mock.patch.object(self.tools, "_run_cli", return_value=mock_result):
            result = json.loads(
                self.tools.agentpaas_stop({"run_id": "run_123"})
            )
        self.assertNotIn("requires_confirmation", result)
        self.assertEqual(result, mock_result)

    def test_stop_unrelated_run_requires_confirmation(self):
        result = json.loads(self.tools.agentpaas_stop({"run_id": "run_999"}))
        self.assertTrue(result["requires_confirmation"])
        self.assertEqual(result["risk_level"], "medium")
        self.assertIn("run_999", result["rationale"])

    def test_successful_stop_removes_from_session(self):
        self.tools._register_session_run("run_123")
        self.assertTrue(self.tools._is_session_run("run_123"))
        with mock.patch.object(
            self.tools, "_run_cli", return_value={"status": "stopped"}
        ):
            self.tools.agentpaas_stop({"run_id": "run_123"})
        self.assertFalse(self.tools._is_session_run("run_123"))


class PolicyPatchConfirmationTests(ConfirmationTestBase):
    def test_recommend_patch_passes_through_confirmation(self):
        cli_response = {
            "schema_version": self.contracts.SCHEMA_VERSION,
            "proposed_patch": "---\n+++",
            "risk_level": "medium",
            "rationale": "Policy change needed.",
            "next_action": "review_policy_patch",
            "confirmation": {
                "requires_confirmation": True,
                "confirmation_id": "cf_daemon123",
                "risk_level": "medium",
            },
        }
        with mock.patch.object(self.tools, "_run_cli", return_value=cli_response):
            result = json.loads(
                self.tools.agentpaas_recommend_policy_patch(
                    {"destination": "https://example.test"}
                )
            )
        self.assertEqual(result["confirmation"]["requires_confirmation"], True)
        self.assertEqual(result["confirmation"]["confirmation_id"], "cf_daemon123")

    def test_self_confirm_refused(self):
        result = json.loads(
            self.tools.agentpaas_recommend_policy_patch(
                {"confirmation_id": "cf_abc123"}
            )
        )
        self.assertIn("error", result)
        self.assertEqual(result["error_category"], "policy_denied")
        self.assertTrue(result["requires_confirmation"])
        self.assertEqual(result["next_action"], "ask_user")
        self.assertIn("cannot self-confirm", result["error"])

    def test_confirmation_id_validation_format(self):
        valid, err = self.tools._validate_confirmation_id("")
        self.assertFalse(valid)
        self.assertIn("required", err)

        valid, err = self.tools._validate_confirmation_id("cf_short")
        self.assertFalse(valid)
        self.assertIn("too short", err)

        valid, err = self.tools._validate_confirmation_id("bad_abcdef123456")
        self.assertFalse(valid)
        self.assertIn("cf_", err)

        valid, err = self.tools._validate_confirmation_id("cf_abcdef123456")
        self.assertTrue(valid)
        self.assertEqual(err, "")


class AuditExportConfirmationTests(ConfirmationTestBase):
    def test_local_export_no_confirmation(self):
        mock_result = {"schema_version": "1.0.0", "exported": True}
        with mock.patch.object(self.tools, "_run_cli", return_value=mock_result):
            result = json.loads(
                self.tools.agentpaas_export_audit(
                    {"output_path": "/tmp/audit.json"}
                )
            )
        self.assertNotIn("requires_confirmation", result)
        self.assertEqual(result, mock_result)

    def test_remote_export_requires_confirmation(self):
        result = json.loads(
            self.tools.agentpaas_export_audit(
                {"output_path": "s3://bucket/audit.json"}
            )
        )
        self.assertTrue(result["requires_confirmation"])
        self.assertEqual(result["risk_level"], "high")
        self.assertIn("s3://bucket/audit.json", result["rationale"])

    def test_self_confirm_export_refused(self):
        result = json.loads(
            self.tools.agentpaas_export_audit(
                {"confirmation_id": "cf_abc123", "output_path": "/tmp/audit.json"}
            )
        )
        self.assertIn("error", result)
        self.assertEqual(result["error_category"], "policy_denied")
        self.assertTrue(result["requires_confirmation"])
        self.assertIn("cannot self-confirm", result["error"])


class ConfirmationReplayExpiryTests(ConfirmationTestBase):
    def test_confirmation_id_replay_refused(self):
        confirmation_id = "cf_abcdef123456"
        first = json.loads(
            self.tools.agentpaas_recommend_policy_patch(
                {"confirmation_id": confirmation_id}
            )
        )
        self.assertIn("cannot self-confirm", first["error"])

        second = json.loads(
            self.tools.agentpaas_recommend_policy_patch(
                {"confirmation_id": confirmation_id}
            )
        )
        self.assertIn("replay refused", second["error"])
        self.assertEqual(second["error_category"], "policy_denied")

    def test_expired_confirmation_refused(self):
        confirmation_id = "cf_abcdef123456"
        with mock.patch.object(self.tools, "_is_confirmation_expired", return_value=True):
            result = json.loads(
                self.tools.agentpaas_export_audit(
                    {
                        "confirmation_id": confirmation_id,
                        "output_path": "/tmp/audit.json",
                    }
                )
            )
        self.assertIn("expired", result["error"])
        self.assertEqual(result["error_category"], "policy_denied")
        self.assertTrue(result["requires_confirmation"])


if __name__ == "__main__":
    unittest.main()