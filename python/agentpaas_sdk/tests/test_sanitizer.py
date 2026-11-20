"""Tests for _sanitize_payload in the Python runner."""

import unittest

# Import the private function from runner.
from agentpaas_sdk.runner import _sanitize_payload, _RESERVED_AGENT_KEYS


class SanitizePayloadTests(unittest.TestCase):
    def test_strips_reserved_keys(self):
        payload = {
            "credentials": [{"id": "k1", "value": "secret"}],
            "llm": {"provider": "openai"},
            "mcp": {"server": "s1"},
            "mcp_servers": [{"server_id": "s1"}],
            "question": "What is 2+2?",
        }
        result = _sanitize_payload(payload)
        self.assertNotIn("credentials", result)
        self.assertNotIn("llm", result)
        self.assertNotIn("mcp", result)
        self.assertNotIn("mcp_servers", result)
        self.assertEqual(result["question"], "What is 2+2?")

    def test_strips_agentpaas_prefix_keys(self):
        payload = {
            "__agentpaas_token": "abc123",
            "__agentpaas_internal": {"x": 1},
            "user_input": "hello",
        }
        result = _sanitize_payload(payload)
        self.assertNotIn("__agentpaas_token", result)
        self.assertNotIn("__agentpaas_internal", result)
        self.assertEqual(result["user_input"], "hello")

    def test_passes_user_keys(self):
        payload = {
            "question": "What is 2+2?",
            "input": "some input",
            "message": "a message",
            "custom_key": 42,
            "nested": {"a": 1},
        }
        result = _sanitize_payload(payload)
        self.assertEqual(result, payload)

    def test_empty_payload_returns_empty_dict(self):
        self.assertEqual(_sanitize_payload({}), {})

    def test_all_reserved_keys_removed_from_result(self):
        payload = {
            "credentials": [],
            "llm": {},
            "mcp": {},
            "mcp_servers": [],
            "__agentpaas_foo": "bar",
        }
        result = _sanitize_payload(payload)
        self.assertEqual(result, {})

    def test_reserved_keys_set_is_immutable(self):
        # Verify _RESERVED_AGENT_KEYS is a frozenset containing expected keys.
        self.assertIsInstance(_RESERVED_AGENT_KEYS, frozenset)
        self.assertIn("credentials", _RESERVED_AGENT_KEYS)
        self.assertIn("llm", _RESERVED_AGENT_KEYS)
        self.assertIn("mcp", _RESERVED_AGENT_KEYS)
        self.assertIn("mcp_servers", _RESERVED_AGENT_KEYS)


if __name__ == "__main__":
    unittest.main()