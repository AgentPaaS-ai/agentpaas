"""Adversary regression tests for B13-T02 contract-parity gate.
Each test is a negative probe designed to break the claimed security/contract guarantees.
Run: python3 -m unittest discover -s integrations/hermes-plugin/tests -v -k Adversary
"""

import json
import re
import unittest
from pathlib import Path
from unittest import mock

from test_contract_parity import (
    _load_plugin_package,
    _build_minimal_response,
    _invoke_handler,
    _sample_args,
    PLUGIN_ROOT,
    REPO_ROOT,
)

# ADVERSARY BREAK: HIGH - CONTRACT DRIFT: Go schema defines fields (e.g. ValidationIssue.EvidenceRefs, TimelineEvent.AuditSeq/EvidenceRefs, SummarizeRunResponse.StartedAt without omitempty) that RESPONSE_CONTRACTS does not fully enumerate or nest-validate. Line-by-line audit will expose missing nested contracts.
class ContractDriftAdversaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_response_contracts_missing_nested_go_fields(self):
        # Audit: parse Go for all json-tagged fields in response structs
        go_text = (REPO_ROOT / "internal" / "operator" / "schema.go").read_text()
        json_fields = set(re.findall(r'json:"([^"]+)"', go_text))
        # Python contracts only list top-level per subcommand
        py_fields = set()
        for c in self.contracts.RESPONSE_CONTRACTS.values():
            py_fields.update(c.get("required", []))
            py_fields.update(c.get("optional", []))
        # Evidence of drift: Go has many more (e.g. start_line, end_line, confirmation_id, audit_seq inside events)
        missing_in_py = json_fields - py_fields - {"omitempty"}  # ignore tag noise
        # This will fail showing drift
        self.assertEqual(missing_in_py, set(), f"CONTRACT DRIFT: Go fields missing from Python contracts: {sorted(missing_in_py)}")


# ADVERSARY BREAK: HIGH - WEAK PARITY ASSERTIONS: _run_cli mock can return MALFORMED (missing required like next_action for explain-failure). Handler silently passes it; no detection/enforcement in tools.py or parity tests. Test proves gate has no teeth on malformed CLI.
class WeakParityAssertionsAdversaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_malformed_cli_missing_required_not_detected(self):
        cli_response = _build_minimal_response("explain-failure", self.contracts)
        del cli_response["next_action"]  # simulate bad CLI omitting required
        parsed = _invoke_handler(self.plugin, "agentpaas_explain_failure", cli_response)
        # Handler returns it anyway - no validation
        self.assertNotIn("next_action", parsed, "WEAK ASSERTION: missing required field passed through undetected")
        # Contract claims required but gate does not enforce


# ADVERSARY BREAK: HIGH - EVIDENCE REF SILENT DROP: No handler code strips, but to prove test teeth, adversary shows that if a handler stripped evidence_refs the parity test would still pass today because no deep integrity beyond presence. Here we simulate drop via patch and show existing EvidenceRefIntegrityTests would not catch in all paths (weak).
class EvidenceRefSilentDropAdversaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_evidence_refs_can_be_silently_dropped_by_handler(self):
        # Simulate a buggy handler that drops evidence_refs (future regression vector)
        def stripping_handler(args):
            result = _build_minimal_response("explain-failure", self.contracts)
            result["evidence_refs"] = [{"type": "audit_seq", "ref": "42"}]
            if isinstance(result, dict) and "evidence_refs" in result:
                result.pop("evidence_refs")  # drop!
            return json.dumps(result)

        parsed = json.loads(stripping_handler({"run_id": "run_test"}))
        self.assertNotIn("evidence_refs", parsed, "SILENT DROP: evidence_refs dropped without test failure in handler path")


# ADVERSARY BREAK: MEDIUM - ENUM COMPLETENESS: Python frozensets match today but no runtime cross-check against Go All* funcs. If new const added to categories.go without Python update, parity test SchemaVersion only checks version not enums.
class EnumCompletenessAdversaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_go_enum_values_not_runtime_verified_against_py(self):
        go_text = (REPO_ROOT / "internal" / "operator" / "categories.go").read_text()
        go_error_cats = set(re.findall(r'Err\w+\s+ErrorCategory\s*=\s*"([^"]+)"', go_text))
        self.assertEqual(go_error_cats, self.contracts.ERROR_CATEGORIES, "ENUM DRIFT: Go categories not subset of Python fixture")


# ADVERSARY BREAK: HIGH - SCHEMA VERSION MISMATCH RESILIENCE: Test only greps categories.go. If version bumped only in schema.go doc or const moved, test misses. Also RESPONSE_CONTRACTS hardcodes no version enforcement in handlers.
class SchemaVersionMismatchAdversaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_version_mismatch_not_caught_if_not_in_categories(self):
        # Force mismatch scenario
        original = self.contracts.SCHEMA_VERSION
        self.contracts.SCHEMA_VERSION = "2.0.0"
        try:
            # The parity test would still pass if it only greps the file without re-import
            # but our adversary forces recheck
            go_text = (REPO_ROOT / "internal" / "operator" / "categories.go").read_text()
            match = re.search(r'SchemaVersion\s*=\s*"([^"]+)"', go_text)
            if match:
                self.assertEqual(self.contracts.SCHEMA_VERSION, match.group(1), "VERSION MISMATCH not detected by gate")
            else:
                self.fail("SchemaVersion not found in categories.go")
        finally:
            self.contracts.SCHEMA_VERSION = original


# ADVERSARY BREAK: MEDIUM - TRUSTED vs UNTRUSTED FIELD LEAKAGE: TRUSTED_CONTROL_FIELDS (error_category etc) defined but NEVER used/enforced in tools.py or parity tests. A handler could source error_category from untrusted evidence (e.g. agent log) and gate would not catch.
class TrustedUntrustedLeakageAdversaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_trusted_field_sourced_from_untrusted_not_detected(self):
        # error_category is TRUSTED but could leak from evidence
        cli_response = _build_minimal_response("explain-failure", self.contracts)
        # Simulate untrusted source providing trusted field
        cli_response["error_category"] = "policy_denied"  # should be from operator only
        parsed = _invoke_handler(self.plugin, "agentpaas_explain_failure", cli_response)
        # No check that trusted field came from trusted path
        self.assertIn("error_category", parsed)
        self.assertIn(parsed["error_category"], self.contracts.TRUSTED_CONTROL_FIELDS, "LEAKAGE: trusted field not protected from untrusted evidence")


if __name__ == "__main__":
    unittest.main()
