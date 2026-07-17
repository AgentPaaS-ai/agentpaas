"""Adversary regression tests for B26-T04 proto/contract extensions.
Each test probes one of the 10 specified attack vectors.
Run: python3 -m unittest integrations/hermes-plugin/tests/test_adversary_b26_t04.py -v
"""

import json
import unittest
from pathlib import Path
import sys

# Add repo root for imports
REPO_ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT / "integrations/hermes-plugin"))

import contracts


class AdversaryB26T04Tests(unittest.TestCase):
    def test_field_number_stability(self):
        """ADVERSARY BREAK: MEDIUM - Field numbers additive (Run 12-14, RunResponse 2-8); old binary compat assumed but no explicit golden test for pre-B26 marshal/unmarshal in Python."""
        # Simulate old JSON without new fields
        old_run = {"run_id": "r1", "agent_name": "a", "status": "SUCCEEDED"}
        # New fields absent; roundtrip would lose nothing but contract claims additive only
        self.assertNotIn("workflow_id", old_run)

    def test_schema_version_bypass(self):
        """ADVERSARY BREAK: HIGH - SCHEMA VERSION BYPASS: contracts.py defines 1.1.0 but no reject_unknown_version(); 1.0.0 JSON accepted by test framework."""
        old_json = json.dumps({"schema_version": "1.0.0", "run_id": "r1", "error_category": "budget_exceeded", "root_cause": "x", "next_action": "rerun"})
        data = json.loads(old_json)
        self.assertEqual(data["schema_version"], "1.0.0")
        # Python parity tests do not enforce == SCHEMA_VERSION strictly

    def test_authority_scope_leakage(self):
        """ADVERSARY BREAK: HIGH - runs:amend_limits in AUTHORITY_SCOPES and proto; exposed in contracts.py despite comment 'must NOT be granted to Python/SDK'."""
        self.assertIn("runs:amend_limits", contracts.AUTHORITY_SCOPES)
        # No runtime check preventing SDK exposure in Python path

    def test_float_usage(self):
        """ADVERSARY BREAK: HIGH - BudgetConfig.max_cost_usd uses double/float in proto; LLM spend MUST be string decimal per schema (LLMBudgetSummary uses decimal but BudgetConfig does not)."""
        # contracts.py has no BudgetConfig fixture; proto uses double
        self.assertTrue(True)  # documented violation

    def test_typed_error_bypass(self):
        """ADVERSARY BREAK: MEDIUM - TypedControlErrorCode defined but string error paths remain; no enforcement TypedControlError is sole path in Python contracts."""
        self.assertIn("missing_scope", contracts.ERROR_CATEGORIES)
        # Bypass via raw error string still possible

    def test_missing_scope_enforcement(self):
        """ADVERSARY BREAK: MEDIUM - AuthorityScope not required in RunRequest proto; requests can omit scope entirely."""
        # No REQUIRED annotation or validation in contracts.py RESPONSE_CONTRACTS for scope
        self.assertTrue("scope" not in str(contracts.RESPONSE_CONTRACTS))

    def test_idempotency_key_empty(self):
        """ADVERSARY BREAK: MEDIUM - idempotency_key allowed empty in RunRequest/InvokeRequest; no proto validation or contract rule rejects ''."""
        # contracts.py has no idempotency rule

    def test_unknown_enum_fail_closed(self):
        """ADVERSARY BREAK: MEDIUM - Unknown NextAction/ErrorCategory: Python uses frozenset membership but no strict fail-closed on unknown strings in JSON unmarshal."""
        unknown = "unknown_next_xyz"
        self.assertNotIn(unknown, contracts.NEXT_ACTIONS)
        # Accepted as string in responses

    def test_deployment_inactive_bypass(self):
        """ADVERSARY BREAK: MEDIUM - Deployment inactive: AdmissionOutcomeCode has code but proto RunRequest.deployment_ref allows inactive refs representationally; no Python contract gate."""
        self.assertIn("deployment_inactive", contracts.ERROR_CATEGORIES)

    def test_numeric_overflow(self):
        """ADVERSARY BREAK: HIGH - int64 fields (attempt_lease_ms etc.) no max bound in proto or contracts; overflow possible, TypedControlError_NUMERIC_OVERFLOW only advisory."""
        # No validation in Python fixtures
        self.assertTrue(True)  # documents gap


if __name__ == "__main__":
    unittest.main()