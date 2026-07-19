"""B29-T01 CHARACTERIZATION TEST — freezes current behavior; B29
replacement tasks are expected to update or fail these tests.

Observation 1: agent.llm() is buffered. The current Agent class:
  - has exactly ONE ``_call("llm", params)`` per llm() invocation
  - returns a single complete dict, not an iterator/stream
  - has NO ``llm_stream`` method
  - passes only ``prompt`` and optionally ``model`` in params
  - does NOT support a multi-role ``messages`` field or streaming flag
"""

import inspect
import unittest

from agentpaas_sdk import Agent, RPCError


class _RecordingRPC:
    """Fake RPC client that records every call."""

    def __init__(self):
        self.calls = []

    def call(self, method, params):
        self.calls.append((method, dict(params)))
        return {"result": "ok"}


class _BadRPC:
    """Fake RPC that refuses all calls, to prove llm() hits RPC exactly once."""

    def call(self, method, params):
        raise RPCError("rpc not connected", "rpc_not_connected")


class B29T01CharacterizationTests(unittest.TestCase):
    """Characterization: agent.llm() is a single buffered RPC call."""

    def test_llm_makes_exactly_one_call_to_llm_method(self):
        """agent.llm() makes exactly ONE _call("llm", params)."""
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)

        result = agent.llm("hello")

        self.assertEqual(len(rpc.calls), 1,
                         "llm() must make exactly one RPC call")
        method, params = rpc.calls[0]
        self.assertEqual(method, "llm")
        self.assertIn("prompt", params)
        self.assertEqual(params["prompt"], "hello")
        # Returns a single complete dict
        self.assertIsInstance(result, dict)
        self.assertEqual(result, {"result": "ok"})

    def test_llm_returns_single_dict_not_iterator(self):
        """llm() returns a dict — there is no iterator, no stream."""
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)

        result = agent.llm("test")

        # Must be a plain dict, not an iterator/generator
        self.assertIs(type(result), dict,
                      f"llm() returned {type(result)}; expected dict, not an iterator/stream")

    def test_llm_with_model_passes_model_in_params(self):
        """The params dict contains prompt and optionally model."""
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)

        agent.llm("hi", model="gpt-4o")

        _, params = rpc.calls[0]
        self.assertEqual(params["prompt"], "hi")
        self.assertEqual(params["model"], "gpt-4o")

    def test_llm_without_model_omits_model_param(self):
        """When model is not passed, it is absent from params."""
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)

        agent.llm("hi")

        _, params = rpc.calls[0]
        self.assertNotIn("model", params)

    def test_no_messages_field_in_params_for_prompt_llm(self):
        """The params dict does NOT contain a multi-role 'messages' field."""
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)

        agent.llm("hi", model="gpt-4o")

        _, params = rpc.calls[0]
        self.assertNotIn("messages", params,
                         "params must not contain 'messages' — only 'prompt' and optionally 'model'")

    def test_no_streaming_flag_in_params(self):
        """The params dict does NOT contain a streaming flag."""
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)

        agent.llm("hi")

        _, params = rpc.calls[0]
        self.assertNotIn("stream", params)
        self.assertNotIn("streaming", params)

    def test_no_llm_stream_method_exists(self):
        """The Agent class does NOT have an llm_stream method."""
        source = inspect.getsource(Agent)
        self.assertNotIn("llm_stream", source,
                         "Agent class must not have an llm_stream method")

    def test_llm_without_rpc_raises_RPCError(self):
        """llm() without set_rpc raises RPCError — proves it always hits RPC."""
        agent = Agent()
        with self.assertRaises(RPCError):
            agent.llm("test")


if __name__ == "__main__":
    unittest.main()
