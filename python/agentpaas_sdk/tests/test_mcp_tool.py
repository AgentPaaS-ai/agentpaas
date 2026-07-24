import json
import unittest

import agentpaas_sdk
from agentpaas_sdk import Agent, RPCError


class FakeRPC:
    def __init__(self, result=None, error=None):
        self.calls = []
        self.result = result or {}
        self.error = error

    def call(self, method, params):
        self.calls.append((method, params))
        if self.error is not None:
            raise self.error
        return self.result


class MCPToolSDKTests(unittest.TestCase):
    """Tests for @agent.mcp_tool registration and call semantics."""

    def setUp(self):
        # Create a fresh agent per test.
        self.sdk_agent = Agent()

    def test_mcp_tool_registers_callable(self):
        @self.sdk_agent.mcp_tool("lookup_feedback")
        def lookup_feedback(arguments):
            return {"items": []}

        tools = self.sdk_agent.list_mcp_tools()
        self.assertEqual(tools, ["lookup_feedback"])

    def test_mcp_tool_register_multiple_sorted(self):
        @self.sdk_agent.mcp_tool("tool_b")
        def tool_b(args):
            return {"b": 1}

        @self.sdk_agent.mcp_tool("tool_a")
        def tool_a(args):
            return {"a": 1}

        tools = self.sdk_agent.list_mcp_tools()
        self.assertEqual(tools, ["tool_a", "tool_b"])

    def test_mcp_tool_call_returns_result(self):
        @self.sdk_agent.mcp_tool("lookup_feedback")
        def lookup_feedback(arguments):
            return {"account_id": arguments.get("account_id"), "items": []}

        result = self.sdk_agent.call_mcp_tool("lookup_feedback", {"account_id": "a-1"})
        self.assertEqual(result, {"account_id": "a-1", "items": []})

    def test_call_mcp_tool_unknown_raises(self):
        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.call_mcp_tool("unknown_tool", {})
        self.assertIn("not registered", str(ctx.exception))

    def test_duplicate_registration_fails(self):
        @self.sdk_agent.mcp_tool("dup")
        def first(args):
            return {}

        with self.assertRaises(RPCError) as ctx:
            @self.sdk_agent.mcp_tool("dup")
            def second(args):
                return {}
        self.assertIn("duplicate", str(ctx.exception).lower())

    def test_unsafe_tool_name_fails(self):
        for bad in ["", "1bad", "bad tool", "bad\ntool", "bad\x00tool"]:
            with self.subTest(name=bad):
                with self.assertRaises(RPCError):
                    @self.sdk_agent.mcp_tool(bad)
                    def fn(args):
                        return {}

    def test_unsafe_tool_name_special_chars_fails(self):
        for bad in ["tool!name", "tool@name", "tool#name"]:
            with self.subTest(name=bad):
                with self.assertRaises(RPCError):
                    @self.sdk_agent.mcp_tool(bad)
                    def fn(args):
                        return {}

    def test_arguments_must_be_dict(self):
        @self.sdk_agent.mcp_tool("echo")
        def echo(arguments):
            return arguments

        for bad in [None, "str", 123, [1, 2, 3], True]:
            with self.subTest(arg=bad):
                with self.assertRaises(RPCError) as ctx:
                    self.sdk_agent.call_mcp_tool("echo", bad)
                self.assertIn("arguments", str(ctx.exception).lower())

    def test_result_must_be_json_serializable(self):
        @self.sdk_agent.mcp_tool("bad_result")
        def bad_result(arguments):
            class NotSerializable:
                pass
            return NotSerializable()

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.call_mcp_tool("bad_result", {})
        self.assertIn("serializ", str(ctx.exception).lower())

    def test_control_fields_rejected_in_args(self):
        @self.sdk_agent.mcp_tool("secure_tool")
        def secure_tool(arguments):
            return {"ok": True}

        for bad_key in ["__agentpaas_secret", "__agentpaas_token"]:
            with self.subTest(key=bad_key):
                with self.assertRaises(RPCError) as ctx:
                    self.sdk_agent.call_mcp_tool("secure_tool", {bad_key: "leaked"})
                self.assertIn("reserved", str(ctx.exception).lower())

    def test_control_fields_rejected_in_result(self):
        @self.sdk_agent.mcp_tool("leaker")
        def leaker(arguments):
            return {"__agentpaas_secret": "should not leak"}

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.call_mcp_tool("leaker", {})
        self.assertIn("reserved", str(ctx.exception).lower())

    def test_tool_exception_maps_to_bounded_error(self):
        @self.sdk_agent.mcp_tool("broken")
        def broken(arguments):
            raise ValueError("something went wrong")

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.call_mcp_tool("broken", {})
        self.assertEqual(ctx.exception.code, "tool_error")
        # Should NOT contain traceback.
        self.assertNotIn("Traceback", str(ctx.exception))
        # Should contain the error message.
        self.assertIn("something went wrong", str(ctx.exception))

    def test_tool_exception_no_traceback_leakage(self):
        @self.sdk_agent.mcp_tool("explode")
        def explode(arguments):
            raise RuntimeError("internal secret: sk-abc123")

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.call_mcp_tool("explode", {})
        # The error message should NOT leak traceback lines.
        self.assertNotIn("File \"", str(ctx.exception))
        self.assertNotIn("line ", str(ctx.exception))

    def test_declared_tools_validate_missing(self):
        """Simulate the runner's tool-set validation: declared vs registered mismatch."""
        @self.sdk_agent.mcp_tool("existing")
        def existing(args):
            return {}

        declared = ["existing", "missing_tool"]
        # This is what the runner would check.
        err = self.sdk_agent.validate_declared_tools(declared)
        self.assertIsNotNone(err)
        self.assertIn("missing_tool", str(err))
        self.assertIn("not registered", str(err))

    def test_declared_tools_validate_extra(self):
        @self.sdk_agent.mcp_tool("declared1")
        def d1(args):
            return {}

        @self.sdk_agent.mcp_tool("undeclared_extra")
        def extra(args):
            return {}

        declared = ["declared1"]
        err = self.sdk_agent.validate_declared_tools(declared)
        self.assertIsNotNone(err)
        self.assertIn("undeclared_extra", str(err))

    def test_declared_tools_validate_exact_match(self):
        @self.sdk_agent.mcp_tool("tool_a")
        def a(args):
            return {}

        @self.sdk_agent.mcp_tool("tool_b")
        def b(args):
            return {}

        declared = ["tool_a", "tool_b"]
        err = self.sdk_agent.validate_declared_tools(declared)
        self.assertIsNone(err)

    def test_get_max_concurrency_default(self):
        self.assertEqual(self.sdk_agent.get_max_concurrency(), 1)

    def test_get_max_concurrency_from_env(self):
        import os
        os.environ["AGENTPAAS_MCP_MAX_CONCURRENCY"] = "4"
        try:
            self.assertEqual(self.sdk_agent.get_max_concurrency(), 4)
        finally:
            del os.environ["AGENTPAAS_MCP_MAX_CONCURRENCY"]

    def test_tool_result_roundtrip_complex(self):
        @self.sdk_agent.mcp_tool("complex_tool")
        def complex_tool(arguments):
            return {
                "items": [{"id": 1, "name": "test"}],
                "count": 1,
                "nested": {"key": [1, 2, 3]},
            }

        result = self.sdk_agent.call_mcp_tool("complex_tool", {"filter": "all"})
        self.assertEqual(result["count"], 1)
        self.assertEqual(result["items"][0]["name"], "test")

    def test_forbidden_response_keys_in_result(self):
        @self.sdk_agent.mcp_tool("leaky")
        def leaky(arguments):
            return {"endpoint": "http://secret.test/api"}

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.call_mcp_tool("leaky", {})
        self.assertIn("forbidden", str(ctx.exception).lower())

    def test_list_mcp_tools_returns_copy_not_reference(self):
        @self.sdk_agent.mcp_tool("t1")
        def t1(args):
            return {}

        tools = self.sdk_agent.list_mcp_tools()
        tools.append("injected")
        self.assertNotIn("injected", self.sdk_agent.list_mcp_tools())


if __name__ == "__main__":
    unittest.main()
