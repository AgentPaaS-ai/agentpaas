"""B29-T02: Runtime profile and normalized envelopes — SDK-level tests.

Covers:
  - agent.llm() backward compatibility (no regression vs T01).
  - agent.llm_stream() exists and raises StreamingNotSupported when the
    harness does not advertise streaming.
  - agent.messages() accepts multi-role messages and calls the normalized
    envelope path (single buffered RPC with a ``messages`` field).
"""

import unittest

from agentpaas_sdk import Agent, RPCError, StreamingNotSupported


class _RecordingRPC:
    """Fake RPC client that records every call."""

    def __init__(self):
        self.calls = []

    def call(self, method, params):
        self.calls.append((method, dict(params)))
        return {"result": "ok"}


class LLMBackwardCompatTests(unittest.TestCase):
    """agent.llm() must remain a single buffered RPC call."""

    def test_llm_still_makes_single_buffered_call(self):
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)
        result = agent.llm("hello")
        self.assertEqual(len(rpc.calls), 1)
        self.assertEqual(rpc.calls[0], ("llm", {"prompt": "hello"}))
        self.assertEqual(result, {"result": "ok"})

    def test_llm_with_model_still_passes_model(self):
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)
        agent.llm("hi", model="gpt-4o")
        _, params = rpc.calls[0]
        self.assertEqual(params["prompt"], "hi")
        self.assertEqual(params["model"], "gpt-4o")


class LLMStreamTests(unittest.TestCase):
    """agent.llm_stream() exists and raises typed error without streaming."""

    def test_llm_stream_method_exists(self):
        agent = Agent()
        self.assertTrue(
            hasattr(agent, "llm_stream"),
            "Agent must expose an additive llm_stream method",
        )

    def test_llm_stream_with_prompt_raises_streaming_not_supported(self):
        agent = Agent()
        agent.set_rpc(_RecordingRPC())
        with self.assertRaises(StreamingNotSupported) as got:
            # Not a for-loop: the method raises before yielding.
            agent.llm_stream(prompt="hello")
        self.assertEqual(got.exception.code, "streaming_not_supported")

    def test_llm_stream_with_messages_raises_streaming_not_supported(self):
        agent = Agent()
        agent.set_rpc(_RecordingRPC())
        with self.assertRaises(StreamingNotSupported):
            agent.llm_stream(messages=[{"role": "user", "content": "hi"}])

    def test_llm_stream_requires_exactly_one_of_prompt_or_messages(self):
        agent = Agent()
        agent.set_rpc(_RecordingRPC())
        # Neither supplied.
        with self.assertRaises(RPCError) as got:
            agent.llm_stream()
        self.assertEqual(got.exception.code, "INVALID_ENVELOPE")
        # Both supplied.
        with self.assertRaises(RPCError) as got:
            agent.llm_stream(prompt="hi", messages=[{"role": "user", "content": "x"}])
        self.assertEqual(got.exception.code, "INVALID_ENVELOPE")

    def test_llm_stream_rejects_empty_messages(self):
        agent = Agent()
        agent.set_rpc(_RecordingRPC())
        with self.assertRaises(RPCError) as got:
            agent.llm_stream(messages=[])
        self.assertEqual(got.exception.code, "INVALID_ENVELOPE")

    def test_llm_stream_raises_without_rpc(self):
        """llm_stream raises StreamingNotSupported even without an RPC —
        the streaming capability gate is checked before transport."""
        agent = Agent()
        with self.assertRaises(StreamingNotSupported):
            agent.llm_stream(prompt="hi")


class MessagesTests(unittest.TestCase):
    """agent.messages() accepts multi-role messages and hits the envelope path."""

    def test_messages_makes_single_buffered_call_with_messages_field(self):
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)
        msgs = [
            {"role": "system", "content": "you are helpful"},
            {"role": "user", "content": "hi"},
        ]
        result = agent.messages(msgs)
        self.assertEqual(len(rpc.calls), 1)
        method, params = rpc.calls[0]
        self.assertEqual(method, "llm")
        self.assertIn("messages", params)
        self.assertEqual(params["messages"], msgs)
        self.assertEqual(result, {"result": "ok"})

    def test_messages_with_model_passes_model(self):
        agent = Agent()
        rpc = _RecordingRPC()
        agent.set_rpc(rpc)
        agent.messages([{"role": "user", "content": "hi"}], model="gpt-4o")
        _, params = rpc.calls[0]
        self.assertEqual(params["model"], "gpt-4o")
        self.assertIn("messages", params)

    def test_messages_rejects_empty_list(self):
        agent = Agent()
        agent.set_rpc(_RecordingRPC())
        with self.assertRaises(RPCError) as got:
            agent.messages([])
        self.assertEqual(got.exception.code, "INVALID_ENVELOPE")

    def test_messages_rejects_non_list(self):
        agent = Agent()
        agent.set_rpc(_RecordingRPC())
        with self.assertRaises(RPCError) as got:
            agent.messages("not a list")  # type: ignore[arg-type]
        self.assertEqual(got.exception.code, "INVALID_ENVELOPE")

    def test_messages_without_rpc_raises_rpc_error(self):
        agent = Agent()
        with self.assertRaises(RPCError):
            agent.messages([{"role": "user", "content": "hi"}])


if __name__ == "__main__":
    unittest.main()
