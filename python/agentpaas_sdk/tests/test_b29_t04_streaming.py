"""B29-T04: governed model streaming — SDK-level tests.

Covers:
  - agent.llm_stream() yields StreamEvent objects when the harness supports
    streaming (a fake RPC with call_stream).
  - agent.llm_stream() raises StreamingNotSupported when the harness RPC does
    not expose call_stream (plain buffered RPC).
  - agent.llm_stream() raises StreamingNotSupported when the harness handshake
    returns code streaming_not_supported.
  - agent.llm_stream() raises StreamingNotSupported when no RPC is connected.
  - Input validation (exactly one of prompt/messages) still enforced.
"""

import unittest

from agentpaas_sdk import Agent, RPCError, StreamEvent, StreamingNotSupported


class _StreamingRPC:
    """Fake RPC that supports call_stream and yields scripted event lines."""

    def __init__(self, events=None, handshake_error=None, code=None):
        # events: list of dicts to yield as stream lines.
        self.events = events or []
        self.handshake_error = handshake_error
        self.code = code
        self.last_method = None
        self.last_params = None

    def call(self, method, params):
        # Plain buffered call; not used by llm_stream but provided for completeness.
        raise RPCError("not supported", "rpc_error")

    def call_stream(self, method, params):
        self.last_method = method
        self.last_params = dict(params)
        if self.handshake_error is not None:
            raise RPCError(self.handshake_error, self.code or "rpc_error")
        for ev in self.events:
            yield ev


class _PlainRPC:
    """Fake RPC with only call(); no call_stream."""

    def __init__(self):
        self.calls = []

    def call(self, method, params):
        self.calls.append((method, dict(params)))
        return {"result": "ok"}


class LLMStreamYieldsEventsTests(unittest.TestCase):
    def test_llm_stream_yields_stream_events(self):
        events = [
            {"call_id": "c1", "request_id": "r1", "sequence": 1,
             "kind": "response_started", "payload": ""},
            {"call_id": "c1", "request_id": "r1", "sequence": 2,
             "kind": "output_delta", "payload": "Hel"},
            {"call_id": "c1", "request_id": "r1", "sequence": 3,
             "kind": "output_delta", "payload": "lo"},
            {"call_id": "c1", "request_id": "r1", "sequence": 4,
             "kind": "usage_update",
             "payload": '{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}'},
            {"call_id": "c1", "request_id": "r1", "sequence": 5,
             "kind": "response_completed", "payload": ""},
        ]
        rpc = _StreamingRPC(events=events)
        agent = Agent()
        agent.set_rpc(rpc)
        got = list(agent.llm_stream(prompt="hi"))
        self.assertEqual(len(got), 5)
        self.assertTrue(all(isinstance(e, StreamEvent) for e in got))
        self.assertEqual(got[0].kind, "response_started")
        self.assertEqual(got[1].payload, "Hel")
        self.assertEqual(got[-1].kind, "response_completed")
        self.assertTrue(got[-1].is_terminal)
        # The streaming RPC method is llm_stream with the prompt param.
        self.assertEqual(rpc.last_method, "llm_stream")
        self.assertEqual(rpc.last_params["prompt"], "hi")

    def test_llm_stream_yields_events_with_messages(self):
        events = [
            {"call_id": "c1", "request_id": "r1", "sequence": 1,
             "kind": "response_started", "payload": ""},
            {"call_id": "c1", "request_id": "r1", "sequence": 2,
             "kind": "response_completed", "payload": ""},
        ]
        rpc = _StreamingRPC(events=events)
        agent = Agent()
        agent.set_rpc(rpc)
        msgs = [{"role": "user", "content": "hi"}]
        got = list(agent.llm_stream(messages=msgs))
        self.assertEqual(len(got), 2)
        self.assertEqual(rpc.last_params["messages"], msgs)

    def test_llm_stream_passes_model(self):
        events = [
            {"call_id": "c1", "request_id": "r1", "sequence": 1,
             "kind": "response_completed", "payload": ""},
        ]
        rpc = _StreamingRPC(events=events)
        agent = Agent()
        agent.set_rpc(rpc)
        list(agent.llm_stream(prompt="hi", model="gpt-4o"))
        self.assertEqual(rpc.last_params["model"], "gpt-4o")


class LLMStreamNotSupportedTests(unittest.TestCase):
    def test_llm_stream_raises_when_rpc_has_no_call_stream(self):
        agent = Agent()
        agent.set_rpc(_PlainRPC())
        with self.assertRaises(StreamingNotSupported) as got:
            # Materialize the generator so the raise actually fires.
            list(agent.llm_stream(prompt="hi"))
        self.assertEqual(got.exception.code, "streaming_not_supported")

    def test_llm_stream_raises_on_streaming_not_supported_code(self):
        rpc = _StreamingRPC(handshake_error="no streaming", code="streaming_not_supported")
        agent = Agent()
        agent.set_rpc(rpc)
        with self.assertRaises(StreamingNotSupported) as got:
            list(agent.llm_stream(prompt="hi"))
        self.assertEqual(got.exception.code, "streaming_not_supported")

    def test_llm_stream_raises_without_rpc(self):
        agent = Agent()
        with self.assertRaises(StreamingNotSupported):
            list(agent.llm_stream(prompt="hi"))


class LLMStreamValidationTests(unittest.TestCase):
    def test_requires_exactly_one_of_prompt_or_messages(self):
        agent = Agent()
        agent.set_rpc(_StreamingRPC())
        # Neither.
        with self.assertRaises(RPCError) as got:
            list(agent.llm_stream())
        self.assertEqual(got.exception.code, "INVALID_ENVELOPE")
        # Both.
        with self.assertRaises(RPCError) as got:
            list(agent.llm_stream(prompt="hi", messages=[{"role": "user", "content": "x"}]))
        self.assertEqual(got.exception.code, "INVALID_ENVELOPE")

    def test_rejects_empty_messages(self):
        agent = Agent()
        agent.set_rpc(_StreamingRPC())
        with self.assertRaises(RPCError) as got:
            list(agent.llm_stream(messages=[]))
        self.assertEqual(got.exception.code, "INVALID_ENVELOPE")


class StreamEventTests(unittest.TestCase):
    def test_from_rpc_decodes_payload(self):
        ev = StreamEvent.from_rpc({
            "call_id": "c1", "request_id": "r1", "sequence": 1,
            "kind": "output_delta", "payload": "hello",
        })
        self.assertEqual(ev.payload, "hello")
        self.assertEqual(ev.sequence, 1)
        self.assertFalse(ev.is_terminal)

    def test_from_rpc_bytes_payload(self):
        ev = StreamEvent.from_rpc({
            "call_id": "c1", "request_id": "r1", "sequence": 1,
            "kind": "output_delta", "payload": b"hello",
        })
        self.assertEqual(ev.payload, "hello")

    def test_terminal_property(self):
        completed = StreamEvent.from_rpc({
            "call_id": "c1", "request_id": "r1", "sequence": 2,
            "kind": "response_completed", "payload": "",
        })
        failed = StreamEvent.from_rpc({
            "call_id": "c1", "request_id": "r1", "sequence": 2,
            "kind": "response_failed", "payload": "boom",
        })
        self.assertTrue(completed.is_terminal)
        self.assertTrue(failed.is_terminal)

    def test_from_rpc_rejects_missing_kind(self):
        with self.assertRaises(Exception):
            StreamEvent.from_rpc({"call_id": "c1", "request_id": "r1", "sequence": 1})


if __name__ == "__main__":
    unittest.main()
