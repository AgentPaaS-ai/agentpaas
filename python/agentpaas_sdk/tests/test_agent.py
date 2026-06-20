import unittest

from agentpaas_sdk import Agent, BudgetExceeded, RPCError


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


class AgentTests(unittest.TestCase):
    def test_on_invoke_registers_handler(self):
        sdk_agent = Agent()

        @sdk_agent.on_invoke
        def handle(payload):
            return {"value": payload["value"]}

        self.assertEqual(sdk_agent.invoke({"value": "ok"}), {"value": "ok"})
        self.assertIs(sdk_agent._invoke_handler, handle)

    def test_llm_calls_rpc_and_returns_response(self):
        sdk_agent = Agent()
        rpc = FakeRPC({"text": "fake", "tokens": 2})
        sdk_agent.set_rpc(rpc)

        self.assertEqual(sdk_agent.llm("hello world"), {"text": "fake", "tokens": 2})
        self.assertEqual(rpc.calls, [("llm", {"prompt": "hello world"})])

    def test_http_with_credential_sends_only_id_to_rpc(self):
        sdk_agent = Agent()
        rpc = FakeRPC({"status": 200, "headers": {}, "body": "ok"})
        sdk_agent.set_rpc(rpc)

        response = sdk_agent.http_with_credential("cred-id", "GET", "https://example.test")

        self.assertEqual(response["body"], "ok")
        _, params = rpc.calls[0]
        self.assertEqual(params["credential_id"], "cred-id")
        self.assertNotIn("secret", str(params).lower())

    def test_mcp_denied_error_is_raised(self):
        sdk_agent = Agent()
        sdk_agent.set_rpc(FakeRPC(error=RPCError("denied", "mcp_denied")))

        with self.assertRaises(RPCError) as got:
            sdk_agent.mcp("server", "tool", {})

        self.assertEqual(got.exception.code, "mcp_denied")
        self.assertIn("mcp_denied", str(got.exception))

    def test_budget_exceeded_is_fail_closed(self):
        sdk_agent = Agent()
        sdk_agent.set_rpc(FakeRPC(error=BudgetExceeded("over", "BUDGET_EXCEEDED")))

        with self.assertRaises(BudgetExceeded):
            sdk_agent.llm("too many tokens")


if __name__ == "__main__":
    unittest.main()
