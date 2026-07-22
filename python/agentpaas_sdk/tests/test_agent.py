import unittest

from agentpaas_sdk import Agent, BudgetExceeded, RPCError, TaskHandle


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

    def test_llm_with_model_passes_model_to_rpc(self):
        sdk_agent = Agent()
        rpc = FakeRPC({"text": "response", "tokens": 5})
        sdk_agent.set_rpc(rpc)
        sdk_agent.llm("hello", model="gpt-4o")
        _, params = rpc.calls[0]
        self.assertEqual(params["prompt"], "hello")
        self.assertEqual(params["model"], "gpt-4o")

    def test_llm_without_model_omits_model_param(self):
        sdk_agent = Agent()
        rpc = FakeRPC({"text": "response", "tokens": 5})
        sdk_agent.set_rpc(rpc)
        sdk_agent.llm("hello")
        _, params = rpc.calls[0]
        self.assertEqual(params["prompt"], "hello")
        self.assertNotIn("model", params)

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


# --- B32-T03 delegation tests -----------------------------------------------


class DelegationTests(unittest.TestCase):
    def setUp(self):
        self.sdk_agent = Agent()

    def test_delegate_happy_path_returns_task_handle(self):
        rpc = FakeRPC({"task_id": "task-abc", "status": "ADMITTED"})
        self.sdk_agent.set_rpc(rpc)

        handle = self.sdk_agent.delegate(
            "report.verify",
            {"role": "user", "parts": [{"kind": "text", "text": "hello"}]},
        )
        self.assertIsInstance(handle, TaskHandle)
        self.assertEqual(handle.task_id, "task-abc")

        # Verify RPC params.
        _, params = rpc.calls[0]
        self.assertEqual(params["capability"], "report.verify")
        self.assertIn("idempotency_key", params)
        self.assertNotIn("host", params)
        self.assertNotIn("port", params)
        self.assertNotIn("endpoint", params)
        self.assertNotIn("url", params)

    def test_delegate_with_operation(self):
        rpc = FakeRPC({"task_id": "task-op", "status": "ADMITTED"})
        self.sdk_agent.set_rpc(rpc)

        handle = self.sdk_agent.delegate(
            "report.verify",
            {"role": "user", "parts": [{"kind": "text", "text": "check"}]},
            operation="verify_draft",
        )
        self.assertEqual(handle.task_id, "task-op")
        _, params = rpc.calls[0]
        self.assertEqual(params["operation"], "verify_draft")

    def test_delegate_with_explicit_idempotency_key(self):
        rpc = FakeRPC({"task_id": "task-abc", "status": "ADMITTED"})
        self.sdk_agent.set_rpc(rpc)

        self.sdk_agent.delegate(
            "report.verify",
            {"role": "user", "parts": []},
            idempotency_key="my-stable-key",
        )
        _, params = rpc.calls[0]
        self.assertEqual(params["idempotency_key"], "my-stable-key")

    def test_delegate_fails_on_missing_task_id(self):
        rpc = FakeRPC({"status": "ADMITTED"})  # no task_id
        self.sdk_agent.set_rpc(rpc)

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.delegate("report.verify", {"text": "hello"})
        self.assertEqual(ctx.exception.code, "invalid_response")

    def test_delegate_rejects_forbidden_response_keys(self):
        # Response containing 'capability_token' key must be rejected.
        rpc = FakeRPC({"task_id": "task-abc", "capability_token": "secret"})
        self.sdk_agent.set_rpc(rpc)

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.delegate("report.verify", {"text": "hello"})
        self.assertEqual(ctx.exception.code, "forbidden_response_key")

    def test_delegate_rejects_endpoint_key_in_response(self):
        rpc = FakeRPC({"task_id": "task-abc", "endpoint": "http://evil.test"})
        self.sdk_agent.set_rpc(rpc)

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.delegate("report.verify", {"text": "hello"})
        self.assertEqual(ctx.exception.code, "forbidden_response_key")

    def test_delegate_rejects_host_key_in_response(self):
        rpc = FakeRPC({"task_id": "task-abc", "host": "1.2.3.4"})
        self.sdk_agent.set_rpc(rpc)

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.delegate("report.verify", {"text": "hello"})
        self.assertEqual(ctx.exception.code, "forbidden_response_key")

    def test_delegate_rejects_empty_capability(self):
        rpc = FakeRPC({"task_id": "task-abc", "status": "ADMITTED"})
        self.sdk_agent.set_rpc(rpc)

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.delegate("", {"text": "hello"})
        self.assertEqual(ctx.exception.code, "invalid_params")

    def test_delegate_rejects_capability_with_newline(self):
        rpc = FakeRPC({"task_id": "task-abc", "status": "ADMITTED"})
        self.sdk_agent.set_rpc(rpc)

        with self.assertRaises(RPCError) as ctx:
            self.sdk_agent.delegate("bad\ncap", {"text": "hello"})
        self.assertEqual(ctx.exception.code, "invalid_params")

    def test_delegate_coerces_simple_dict_to_user_message(self):
        rpc = FakeRPC({"task_id": "task-xyz", "status": "ADMITTED"})
        self.sdk_agent.set_rpc(rpc)

        self.sdk_agent.delegate("report.verify", {"text": "simple message"})
        _, params = rpc.calls[0]
        msg = params["message"]
        self.assertEqual(msg["role"], "user")
        self.assertEqual(len(msg["parts"]), 1)
        self.assertEqual(msg["parts"][0]["kind"], "text")
        self.assertEqual(msg["parts"][0]["text"], "simple message")

    def test_delegate_never_accepts_host_param(self):
        """Agent.delegate MUST NOT have a host parameter."""
        import inspect

        sig = inspect.signature(Agent.delegate)
        for param_name in sig.parameters:
            self.assertNotIn(param_name.lower(), ["host", "port", "url", "endpoint", "token"])

    def test_task_handle_events(self):
        rpc = FakeRPC({"events": [
            {"event_id": "evt-1", "task_id": "task-abc", "type": "TASK_ADMITTED", "sequence": 1},
        ]})
        self.sdk_agent.set_rpc(rpc)

        # Manually create a TaskHandle (avoids needing a real delegate call).
        handle = TaskHandle("task-abc", rpc)
        events = handle.events(after_sequence=0)
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["type"], "TASK_ADMITTED")

    def test_task_handle_events_rejects_forbidden_keys(self):
        rpc = FakeRPC({"events": [], "capability_token": "leaked"})
        handle = TaskHandle("task-abc", rpc)

        with self.assertRaises(RPCError) as ctx:
            handle.events(after_sequence=0)
        self.assertEqual(ctx.exception.code, "forbidden_response_key")

    def test_task_handle_result(self):
        rpc = FakeRPC({"task_id": "task-abc", "status": "SUCCEEDED"})
        handle = TaskHandle("task-abc", rpc)

        result = handle.result()
        self.assertIsNotNone(result)
        self.assertEqual(result["status"], "SUCCEEDED")

    def test_task_handle_result_none_for_nonterminal(self):
        rpc = FakeRPC({"task_id": "task-abc", "status": "ADMITTED"})
        handle = TaskHandle("task-abc", rpc)

        result = handle.result()
        self.assertIsNone(result)


if __name__ == "__main__":
    unittest.main()
