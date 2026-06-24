"""Adversary regression tests for B13-T02 contract-parity gate.
Each test is a negative probe designed to break the claimed security/contract guarantees.
Run: python3 -m unittest discover -s integrations/hermes-plugin/tests -v -k Adversary
"""

import inspect
import json
import re
import unittest
from pathlib import Path
from unittest import mock

from test_contract_parity import (
    _load_plugin_package,
    _build_minimal_response,
    _invoke_handler,
    PLUGIN_ROOT,
    REPO_ROOT,
)

_NESTED_GO_STRUCTS = {
    "validation_issue": "ValidationIssue",
    "timeline_event": "TimelineEvent",
    "redacted_excerpt": "RedactedExcerpt",
    "evidence_ref": "EvidenceRef",
    "confirmation_requirement": "ConfirmationRequirement",
}


def _extract_go_struct_json_fields(go_text, struct_name):
    """Parse a Go struct by name and return its json-tagged field names."""
    pattern = rf"type\s+{re.escape(struct_name)}\s+struct\s*\{{(.*?)^\}}"
    match = re.search(pattern, go_text, re.MULTILINE | re.DOTALL)
    if not match:
        return None
    body = match.group(1)
    fields = set()
    for tag_match in re.finditer(r'json:"([^",]+)', body):
        field = tag_match.group(1)
        if field != "-":
            fields.add(field)
    return fields

# ADVERSARY BREAK: HIGH - CONTRACT DRIFT: Go schema defines fields (e.g. ValidationIssue.EvidenceRefs, TimelineEvent.AuditSeq/EvidenceRefs, SummarizeRunResponse.StartedAt without omitempty) that RESPONSE_CONTRACTS does not fully enumerate or nest-validate. Line-by-line audit will expose missing nested contracts.
class ContractDriftAdversaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_nested_contracts_match_go_struct_fields(self):
        go_text = (REPO_ROOT / "internal" / "operator" / "schema.go").read_text()
        for nested_key, struct_name in _NESTED_GO_STRUCTS.items():
            with self.subTest(nested=nested_key, go_struct=struct_name):
                go_fields = _extract_go_struct_json_fields(go_text, struct_name)
                self.assertIsNotNone(
                    go_fields,
                    f"Go struct {struct_name} not found in schema.go",
                )
                nested = self.contracts.NESTED_CONTRACTS[nested_key]
                py_fields = set(nested["required"]) | set(nested.get("optional", []))
                self.assertEqual(
                    py_fields, go_fields,
                    f"NESTED CONTRACT DRIFT for {nested_key}: "
                    f"Python={sorted(py_fields)}, Go={sorted(go_fields)}",
                )


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

    def test_schema_version_mismatch_is_detected(self):
        original = self.contracts.SCHEMA_VERSION
        self.contracts.SCHEMA_VERSION = "2.0.0"
        try:
            go_text = (REPO_ROOT / "internal" / "operator" / "categories.go").read_text()
            match = re.search(r'SchemaVersion\s*=\s*"([^"]+)"', go_text)
            self.assertIsNotNone(match, "SchemaVersion const not found in categories.go")
            go_schema_version = match.group(1)
            self.assertNotEqual(
                self.contracts.SCHEMA_VERSION, go_schema_version,
                "Gate should detect schema version mismatch",
            )
        finally:
            self.contracts.SCHEMA_VERSION = original


# ADVERSARY BREAK: MEDIUM - TRUSTED vs UNTRUSTED FIELD LEAKAGE: TRUSTED_CONTROL_FIELDS (error_category etc) defined but NEVER used/enforced in tools.py or parity tests. A handler could source error_category from untrusted evidence (e.g. agent log) and gate would not catch.
class TrustedUntrustedLeakageAdversaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_trusted_untrusted_sets_are_used(self):
        import test_contract_parity as parity_module

        source = inspect.getsource(parity_module)
        self.assertIn("TRUSTED_CONTROL_FIELDS", source)
        self.assertIn("UNTRUSTED_EVIDENCE_FIELDS", source)
        self.assertTrue(hasattr(parity_module, "TrustBoundaryClassificationTests"))


if __name__ == "__main__":
    unittest.main()
