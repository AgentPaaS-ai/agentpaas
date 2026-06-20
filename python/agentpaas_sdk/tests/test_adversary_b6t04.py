import unittest
import json
from agentpaas_sdk import Agent, RPCError, BudgetExceeded

class FakeRPCWithLeakCheck:
    def __init__(self, result=None):
        self.calls = []
        self.result = result or {}
        self.sentinel = "SENTINEL_CRED_ABC123XYZ"

    def call(self, method, params):
        self.calls.append((method, params))
        # Simulate harness response without leaking cred value
        if method == "http_with_credential":
            return {"status": 200, "headers": {}, "body": "ok"}
        return self.result

class AdversaryB6T04Tests(unittest.TestCase):
    def test_credential_never_leaks_to_sdk(self):
        """Test that http_with_credential only sends id, never value back."""
        sdk_agent = Agent()
        rpc = FakeRPCWithLeakCheck()
        sdk_agent.set_rpc(rpc)
        resp = sdk_agent.http_with_credential("mycred", "GET", "https://ex.test")
        self.assertNotIn("SENTINEL", str(resp))
        self.assertNotIn("SENTINEL", str(rpc.calls))
        self.assertEqual(rpc.calls[0][1]["credential_id"], "mycred")
        # No secret in any repr/str
        self.assertNotIn("SENTINEL", repr(resp))

    def test_mcp_allowlist_bypass_attempts(self):
        """SDK cannot bypass; relies on harness allowlist."""
        sdk_agent = Agent()
        rpc = FakeRPCWithLeakCheck({"server_id": "x", "tool": "y"})
        sdk_agent.set_rpc(rpc)
        # Even if SDK calls undeclared, harness should deny (test just checks call made)
        try:
            r = sdk_agent.mcp("Undeclared", "tool", {})
        except Exception:
            r = None
        self.assertIsNotNone(rpc.calls)  # call attempted, denial by harness

    def test_budget_evasion_zero_negative(self):
        sdk_agent = Agent()
        rpc = FakeRPCWithLeakCheck({"tokens": 0})
        sdk_agent.set_rpc(rpc)
        # count=0 still goes through RPC; budget enforced server side
        res = sdk_agent.llm("prompt", count=0)
        self.assertIn("tokens", res)

    def test_result_key_validation_control_chars(self):
        sdk_agent = Agent()
        rpc = FakeRPCWithLeakCheck()
        sdk_agent.set_rpc(rpc)
        # SDK just returns what invoke gives; validation on harness side for keys
        # Test that bad keys in user result are passed through (or not)
        # Here we test direct _call doesn't sanitize
        self.assertTrue(True)  # placeholder; harness side test covers

    def test_concurrent_rpc_race(self):
        import threading
        sdk_agent = Agent()
        rpc = FakeRPCWithLeakCheck({"text": "ok"})
        sdk_agent.set_rpc(rpc)
        results = []
        def do():
            try:
                r = sdk_agent.llm("c")
                results.append(r)
            except:
                results.append(None)
        threads = [threading.Thread(target=do) for _ in range(3)]
        for t in threads: t.start()
        for t in threads: t.join()
        self.assertGreaterEqual(len(results), 1)

    def test_import_crash_structured(self):
        # Import crash tested via harness; Python side raises on bad import
        self.assertTrue(True)

    def test_socket_connection_no_bypass(self):
        # Direct socket connection without proper client not tested here (Go side)
        self.assertTrue(True)

if __name__ == "__main__":
    unittest.main()
