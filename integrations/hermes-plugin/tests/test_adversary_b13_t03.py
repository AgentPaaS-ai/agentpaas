"""Adversary tests for B13-T03 confirmation protocol weaknesses."""

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


class AdversarySessionTrackingBypassTests(ConfirmationTestBase):
    # ADVERSARY BREAK HIGH: _register_session_run is exposed on the module and allows
    # registering arbitrary run_ids not created by this session, bypassing stop confirmation.

    def test_can_register_arbitrary_run_to_bypass_stop(self):
        # Secure behavior: only runs from agentpaas_run() should be session runs
        self.tools._register_session_run("run_controlled_by_attacker")
        result = json.loads(
            self.tools.agentpaas_stop({"run_id": "run_controlled_by_attacker"})
        )
        self.assertIn(
            "requires_confirmation", result, "should require confirmation for non-owned run"
        )


class AdversaryGlobalStatePollutionTests(ConfirmationTestBase):
    # ADVERSARY BREAK MEDIUM: module-level _session_runs and _used_confirmation_ids
    # are global mutable state; reset exists but any importer can pollute across tests/sessions.

    def test_state_not_isolated_without_explicit_reset(self):
        self.tools._register_session_run("run_pollute")
        # Without reset, other tests would see it (simulated by direct access)
        self.assertTrue(self.tools._is_session_run("run_pollute"))


class AdversarySelfConfirmKeyBypassTests(ConfirmationTestBase):
    # ADVERSARY BREAK HIGH: self-confirm refusal ONLY checks exact key "confirmation_id".
    # Other keys (confirm_id, confirmationId, nested) bypass the check entirely.

    def test_confirm_id_alternative_key_bypasses_refusal(self):
        result = json.loads(
            self.tools.agentpaas_recommend_policy_patch(
                {"confirm_id": "cf_abcdef123456"}
            )
        )
        # If bypass works, it will call CLI instead of refusing
        self.assertNotIn("error_category", result) or self.assertNotEqual(
            result.get("error_category"), "policy_denied"
        )

    def test_nested_confirmation_id_bypasses(self):
        result = json.loads(
            self.tools.agentpaas_export_audit(
                {"output_path": "/tmp/x", "meta": {"confirmation_id": "cf_abcdef123456"}}
            )
        )
        self.assertNotIn("error", result)


class AdversaryRemoteExportDetectionBypassTests(ConfirmationTestBase):
    # ADVERSARY BREAK HIGH: remote scheme detection is incomplete.
    # file://, data:, javascript:, //, path traversal, None/empty all bypass.

    def test_file_scheme_bypasses_remote_check(self):
        result = json.loads(
            self.tools.agentpaas_export_audit({"output_path": "file:///etc/passwd"})
        )
        self.assertNotIn("requires_confirmation", result)

    def test_data_uri_bypasses(self):
        result = json.loads(
            self.tools.agentpaas_export_audit(
                {"output_path": "data:text/plain;base64,ZXZpbA=="}
            )
        )
        self.assertNotIn("requires_confirmation", result)

    def test_scheme_relative_bypasses(self):
        result = json.loads(
            self.tools.agentpaas_export_audit({"output_path": "//evil.example.com/x"})
        )
        self.assertNotIn("requires_confirmation", result)

    def test_empty_and_none_output_path_treated_local(self):
        result = json.loads(self.tools.agentpaas_export_audit({"output_path": ""}))
        self.assertNotIn("requires_confirmation", result)
        result = json.loads(self.tools.agentpaas_export_audit({}))  # None
        self.assertNotIn("requires_confirmation", result)


class AdversaryReplayProtectionFlawsTests(ConfirmationTestBase):
    # ADVERSARY BREAK HIGH: replay set is module-global, not thread-safe,
    # empty ID not rejected by replay, usable before/after.

    def test_empty_confirmation_id_not_replayed(self):
        # validation fails but if somehow passed, or check replay first
        is_replay, _ = self.tools._check_confirmation_replay("")
        self.assertFalse(is_replay)  # accepts empty, adds it

    def test_can_use_id_before_and_after_registration(self):
        cid = "cf_abcdef1234567890"
        # First use registers in replay set via self-confirm path
        r1 = json.loads(
            self.tools.agentpaas_recommend_policy_patch({"confirmation_id": cid})
        )
        self.assertIn("cannot self-confirm", r1["error"])
        # Second use detects replay - but we want to show flaw if any gap
        r2 = json.loads(
            self.tools.agentpaas_recommend_policy_patch({"confirmation_id": cid})
        )
        self.assertIn("replay refused", r2.get("error", ""))


class AdversaryConfirmationIdValidationWeaknessTests(ConfirmationTestBase):
    # ADVERSARY BREAK MEDIUM: validation only prefix+len, allows malicious looking IDs
    # (injection strings pass format check). len check is <9 not matching docstring.

    def test_malicious_looking_id_passes_validation(self):
        malicious = "cf_'; rm -rf /; echo '"
        valid, err = self.tools._validate_confirmation_id(malicious)
        self.assertTrue(valid, f"malicious ID should be rejected but got {err}")


class AdversaryExpiryForgeryTests(ConfirmationTestBase):
    # ADVERSARY BREAK LOW: _is_confirmation_expired always returns False (no real check),
    # future timestamps or monkeypatch can make expired look valid.

    def test_future_timestamp_not_expired(self):
        # Since impl always False, any ID "valid"
        result = json.loads(
            self.tools.agentpaas_recommend_policy_patch(
                {"confirmation_id": "cf_future123456"}
            )
        )
        self.assertIn("cannot self-confirm", result["error"])


class AdversaryEvidenceRefManipulationTests(ConfirmationTestBase):
    # ADVERSARY BREAK MEDIUM: stop confirmation hardcodes evidence_refs;
    # caller cannot inject but also no validation of provided refs if extended.

    def test_stop_always_generates_own_evidence(self):
        result = json.loads(self.tools.agentpaas_stop({"run_id": "run_unowned"}))
        self.assertTrue(result["requires_confirmation"])
        self.assertEqual(len(result.get("evidence_refs", [])), 1)


if __name__ == "__main__":
    unittest.main()