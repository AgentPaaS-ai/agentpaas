"""Regression tests for B13-T03 confirmation protocol hardening."""

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


class SessionTrackingBypassRegressionTests(ConfirmationTestBase):
    def test_direct_register_session_run_raises(self):
        with self.assertRaises(RuntimeError) as ctx:
            self.tools._register_session_run("run_controlled_by_attacker")
        self.assertIn("prohibited", str(ctx.exception))

    def test_internal_register_without_sentinel_raises(self):
        with self.assertRaises(RuntimeError) as ctx:
            self.tools._internal_register_session_run("run_999")
        self.assertIn("private", str(ctx.exception))

    def test_arbitrary_run_still_requires_stop_confirmation(self):
        result = json.loads(
            self.tools.agentpaas_stop({"run_id": "run_controlled_by_attacker"})
        )
        self.assertIn("requires_confirmation", result)
        self.assertTrue(result["requires_confirmation"])


class GlobalStatePollutionRegressionTests(ConfirmationTestBase):
    def test_reset_clears_session_runs(self):
        with mock.patch.object(
            self.tools, "_run_cli", return_value={"run_id": "run_pollute"}
        ):
            self.tools.agentpaas_run({"image_or_project": "demo"})
        self.assertTrue(self.tools._is_session_run("run_pollute"))
        self.tools._reset_confirmation_state()
        self.assertFalse(self.tools._is_session_run("run_pollute"))


class SelfConfirmKeyBypassRegressionTests(ConfirmationTestBase):
    def test_confirm_id_alternative_key_refused(self):
        with mock.patch.object(self.tools, "_run_cli") as mock_cli:
            result = json.loads(
                self.tools.agentpaas_recommend_policy_patch(
                    {"confirm_id": "cf_abcdef123456"}
                )
            )
            mock_cli.assert_not_called()
        self.assertEqual(result["error_category"], "policy_denied")
        self.assertIn("cannot self-confirm", result["error"])

    def test_nested_confirmation_id_refused(self):
        with mock.patch.object(self.tools, "_run_cli") as mock_cli:
            result = json.loads(
                self.tools.agentpaas_export_audit(
                    {
                        "output_path": "/tmp/x",
                        "meta": {"confirmation_id": "cf_abcdef123456"},
                    }
                )
            )
            mock_cli.assert_not_called()
        self.assertEqual(result["error_category"], "policy_denied")
        self.assertIn("cannot self-confirm", result["error"])


class RemoteExportDetectionRegressionTests(ConfirmationTestBase):
    def test_file_scheme_requires_confirmation(self):
        result = json.loads(
            self.tools.agentpaas_export_audit(
                {"output_path": "file:///etc/passwd"}
            )
        )
        self.assertTrue(result["requires_confirmation"])

    def test_data_uri_requires_confirmation(self):
        result = json.loads(
            self.tools.agentpaas_export_audit(
                {"output_path": "data:text/plain;base64,ZXZpbA=="}
            )
        )
        self.assertTrue(result["requires_confirmation"])

    def test_scheme_relative_requires_confirmation(self):
        result = json.loads(
            self.tools.agentpaas_export_audit(
                {"output_path": "//evil.example.com/x"}
            )
        )
        self.assertTrue(result["requires_confirmation"])

    def test_empty_and_none_output_path_require_confirmation(self):
        result = json.loads(
            self.tools.agentpaas_export_audit({"output_path": ""})
        )
        self.assertTrue(result["requires_confirmation"])
        result = json.loads(self.tools.agentpaas_export_audit({}))
        self.assertTrue(result["requires_confirmation"])


class ReplayProtectionRegressionTests(ConfirmationTestBase):
    def test_empty_confirmation_id_refused(self):
        is_replay, should_refuse = self.tools._check_confirmation_replay("")
        self.assertFalse(is_replay)
        self.assertTrue(should_refuse)

    def test_replay_detected_on_second_use(self):
        cid = "cf_abcdef1234567890"
        r1 = json.loads(
            self.tools.agentpaas_recommend_policy_patch({"confirmation_id": cid})
        )
        self.assertIn("cannot self-confirm", r1["error"])
        r2 = json.loads(
            self.tools.agentpaas_recommend_policy_patch({"confirmation_id": cid})
        )
        self.assertIn("replay refused", r2.get("error", ""))


class ConfirmationIdValidationRegressionTests(ConfirmationTestBase):
    def test_malicious_looking_id_rejected(self):
        malicious = "cf_'; rm -rf /; echo '"
        valid, err = self.tools._validate_confirmation_id(malicious)
        self.assertFalse(valid)
        self.assertIn("hex", err)


class ExpiryForgeryRegressionTests(ConfirmationTestBase):
    def test_far_future_expiry_clamped(self):
        issued_at = 1000.0
        far_future = issued_at + 999999
        self.assertTrue(
            self.tools._is_confirmation_expired(issued_at, far_future)
        )

    def test_self_confirm_still_refused_with_future_id(self):
        result = json.loads(
            self.tools.agentpaas_recommend_policy_patch(
                {"confirmation_id": "cf_abcdef1234567890"}
            )
        )
        self.assertIn("cannot self-confirm", result["error"])


class EvidenceRefRegressionTests(ConfirmationTestBase):
    def test_stop_always_generates_own_evidence(self):
        result = json.loads(self.tools.agentpaas_stop({"run_id": "run_unowned"}))
        self.assertTrue(result["requires_confirmation"])
        self.assertEqual(len(result.get("evidence_refs", [])), 1)


if __name__ == "__main__":
    unittest.main()