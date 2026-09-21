"""BUG-JIRA-MCP-FALSE-VERIFIED: sample false closeouts cannot stamp verified."""

import importlib.util
import unittest
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parents[2] / "integrations" / "hermes-plugin"


def _load_verified():
    spec = importlib.util.spec_from_file_location(
        "agentpaas_hermes_plugin_verified",
        PLUGIN_ROOT / "verified.py",
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


# Exact shape of the packed-and-verified lie from BUG-JIRA-MCP-FALSE-VERIFIED:
# search returned 0 issues, get_issue/get_comments 404 on KAN-1 (not the
# seeded KAN-4), and prove was trigger invoke of an on_invoke dispatcher.
SAMPLE_FALSE_CLOSEOUT = {
    "prove_method": "trigger_invoke",
    "user_named_keys": ["KAN-4"],
    "calls": [
        {
            "tool": "list_projects",
            "http_status": 200,
            "projects": [{"key": "KAN", "name": "My Kanban Space"}],
        },
        {
            "tool": "search_issues",
            "http_status": 200,
            "issues": [],
        },
        {
            "tool": "get_issue",
            "http_status": 404,
            "key": "KAN-1",
        },
        {
            "tool": "get_comments",
            "http_status": 404,
            "key": "KAN-1",
        },
    ],
}


class FalseVerifiedCloseoutTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.verified = _load_verified()

    def _judge(self, evidence):
        return self.verified.evaluate_verified_closeout(evidence)

    def test_sample_false_closeout_cannot_stamp_verified(self):
        result = self.verified.stamp_verified(SAMPLE_FALSE_CLOSEOUT)
        self.assertFalse(result["verified"])
        self.assertFalse(result["stamped"])
        self.assertTrue(result["reasons"])

    def test_empty_search_cannot_close_verified(self):
        evidence = {
            "prove_method": "tools/call",
            "calls": [
                {
                    "tool": "list_projects",
                    "http_status": 200,
                    "projects": [{"key": "KAN"}],
                },
                {
                    "tool": "search_issues",
                    "http_status": 200,
                    "issues": [],
                },
            ],
        }
        result = self._judge(evidence)
        self.assertFalse(result["verified"])
        self.assertIn("empty_search", result["reasons"])

    def test_zero_projects_cannot_close_verified(self):
        evidence = {
            "prove_method": "tools/call",
            "calls": [
                {
                    "tool": "list_projects",
                    "http_status": 200,
                    "projects": [],
                },
            ],
        }
        result = self._judge(evidence)
        self.assertFalse(result["verified"])
        self.assertIn("zero_projects", result["reasons"])

    def test_404_on_fake_key_cannot_close_verified(self):
        for fake_key in ("NOSUCH-1", "FOO-1", "KAN-1"):
            with self.subTest(key=fake_key):
                evidence = {
                    "prove_method": "tools/call",
                    "user_named_keys": ["KAN-4"],
                    "calls": [
                        {
                            "tool": "list_projects",
                            "http_status": 200,
                            "projects": [{"key": "KAN"}],
                        },
                        {
                            "tool": "search_issues",
                            "http_status": 200,
                            "issues": [{"key": "KAN-4"}],
                        },
                        {
                            "tool": "get_issue",
                            "http_status": 404,
                            "key": fake_key,
                        },
                    ],
                }
                result = self._judge(evidence)
                self.assertFalse(result["verified"])
                self.assertIn("fake_key_404", result["reasons"])

    def test_mcp_prove_rejects_trigger_invoke(self):
        evidence = {
            "prove_method": "trigger_invoke",
            "calls": [
                {
                    "tool": "list_projects",
                    "http_status": 200,
                    "projects": [{"key": "KAN"}],
                },
                {
                    "tool": "search_issues",
                    "http_status": 200,
                    "issues": [{"key": "KAN-4"}],
                },
                {
                    "tool": "get_issue",
                    "http_status": 200,
                    "key": "KAN-4",
                    "fields": {"summary": "App store screenshots for 6.1"},
                },
            ],
        }
        result = self._judge(evidence)
        self.assertFalse(result["verified"])
        self.assertIn("prove_not_tools_call", result["reasons"])

    def test_tools_call_with_real_record_may_stamp_verified(self):
        evidence = {
            "prove_method": "tools/call",
            "user_named_keys": ["KAN-4"],
            "calls": [
                {
                    "tool": "list_projects",
                    "http_status": 200,
                    "projects": [{"key": "KAN"}],
                },
                {
                    "tool": "search_issues",
                    "http_status": 200,
                    "issues": [{"key": "KAN-4"}],
                },
                {
                    "tool": "get_issue",
                    "http_status": 200,
                    "key": "KAN-4",
                    "fields": {"summary": "App store screenshots for 6.1"},
                },
            ],
        }
        result = self.verified.stamp_verified(evidence)
        self.assertTrue(result["verified"])
        self.assertTrue(result["stamped"])
        self.assertEqual(result["reasons"], [])


if __name__ == "__main__":
    unittest.main()
