"""B26-T04 contract parity tests for operator schema 1.1.0.

Verifies Python contracts.py stays aligned with Go operator categories/schema
for additive B26 enums and attempt_report nesting. Does not add Routed Run
conversational behavior (that waits for B36).
"""

from __future__ import annotations

import importlib.util
import re
import sys
import unittest
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = PLUGIN_ROOT.parents[1]


def _load_contracts():
    full_name = "agentpaas_hermes_plugin_t04.contracts"
    if full_name in sys.modules:
        return sys.modules[full_name]
    spec = importlib.util.spec_from_file_location(
        full_name, PLUGIN_ROOT / "contracts.py"
    )
    module = importlib.util.module_from_spec(spec)
    sys.modules[full_name] = module
    spec.loader.exec_module(module)
    return module


class ContractParityT04Tests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.contracts = _load_contracts()
        cls.categories_go = (REPO_ROOT / "internal/operator/categories.go").read_text()
        cls.schema_go = (REPO_ROOT / "internal/operator/schema.go").read_text()

    def test_schema_version_is_1_1_0(self):
        self.assertEqual(self.contracts.SCHEMA_VERSION, "1.1.0")
        m = re.search(r'SchemaVersion\s*=\s*"([^"]+)"', self.categories_go)
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "1.1.0")
        self.assertEqual(self.contracts.SCHEMA_VERSION, m.group(1))

    def test_new_next_actions_present(self):
        for action in ("more_time", "capability_up", "larger_context", "split_task", "stop"):
            self.assertIn(action, self.contracts.NEXT_ACTIONS)
            self.assertIn(f'"{action}"', self.categories_go)

    def test_new_error_categories_present(self):
        for cat in (
            "deployment_inactive",
            "idempotency_conflict",
            "concurrency_unavailable",
            "limit_amendment_denied",
            "unsafe_pause_boundary",
            "run_terminal",
            "feature_not_enabled",
            "missing_scope",
        ):
            self.assertIn(cat, self.contracts.ERROR_CATEGORIES)
            self.assertIn(f'"{cat}"', self.categories_go)

    def test_authority_scopes(self):
        self.assertEqual(
            self.contracts.AUTHORITY_SCOPES,
            frozenset({"default", "runs:control", "runs:amend_limits"}),
        )
        self.assertIn("runs:control", self.categories_go)
        self.assertIn("runs:amend_limits", self.categories_go)

    def test_summarize_optional_attempt_report(self):
        summarize = self.contracts.RESPONSE_CONTRACTS["summarize"]
        self.assertIn("attempt_report", summarize["optional"])
        self.assertEqual(summarize["nested"]["attempt_report"], "attempt_report")

    def test_explain_failure_latest_fields(self):
        explain = self.contracts.RESPONSE_CONTRACTS["explain-failure"]
        self.assertIn("latest_reason", explain["optional"])
        self.assertIn("latest_action", explain["optional"])
        self.assertIn("LatestReason", self.schema_go)
        self.assertIn("LatestAction", self.schema_go)

    def test_next_action_latest_reason(self):
        na = self.contracts.RESPONSE_CONTRACTS["next-action"]
        self.assertIn("latest_reason", na["optional"])

    def test_attempt_report_nested_contract(self):
        ar = self.contracts.NESTED_CONTRACTS["attempt_report"]
        for req in ("schema_version", "run_id", "attempt_id", "status"):
            self.assertIn(req, ar["required"])
        self.assertIn("llm_budget", ar["optional"])
        self.assertIn("progress", ar["optional"])
        self.assertIn("type AttemptReport struct", self.schema_go)

    def test_unknown_action_not_in_contract(self):
        self.assertNotIn("spawn_pipeline", self.contracts.NEXT_ACTIONS)
        self.assertNotIn("runs:admin", self.contracts.AUTHORITY_SCOPES)

    def test_go_all_next_actions_count_matches_python(self):
        go_actions = re.findall(
            r'Action\w+\s+NextAction\s*=\s*"([^"]+)"', self.categories_go
        )
        self.assertEqual(set(go_actions), set(self.contracts.NEXT_ACTIONS))

    def test_go_all_error_categories_match_python(self):
        go_cats = re.findall(
            r'Err\w+\s+ErrorCategory\s*=\s*"([^"]+)"', self.categories_go
        )
        self.assertEqual(set(go_cats), set(self.contracts.ERROR_CATEGORIES))


if __name__ == "__main__":
    unittest.main()
