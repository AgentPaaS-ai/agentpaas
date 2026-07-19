"""Governed model streaming types for the AgentPaaS SDK.

A StreamEvent mirrors the Go runtime.StreamEvent shape. Event kinds match the
governed streaming contract: response_started; output_delta; tool_call_delta;
usage_update; response_completed; response_failed.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


# Event kind constants (mirror internal/runtime/streaming.go).
KIND_RESPONSE_STARTED = "response_started"
KIND_OUTPUT_DELTA = "output_delta"
KIND_TOOL_CALL_DELTA = "tool_call_delta"
KIND_USAGE_UPDATE = "usage_update"
KIND_RESPONSE_COMPLETED = "response_completed"
KIND_RESPONSE_FAILED = "response_failed"

TERMINAL_KINDS = frozenset({KIND_RESPONSE_COMPLETED, KIND_RESPONSE_FAILED})


@dataclass
class StreamEvent:
    """One governed model stream event.

    Payload is the bounded event payload (bytes decoded to str when possible).
    A terminal event (response_completed/response_failed) ends the stream.
    """

    call_id: str
    request_id: str
    sequence: int
    kind: str
    payload: str = ""
    target_identity: str = ""

    @classmethod
    def from_rpc(cls, result: Any) -> "StreamEvent":
        """Build a StreamEvent from a harness RPC streaming response line.

        The harness returns a dict with call_id, request_id, sequence, kind,
        payload (str), and optionally target_identity.
        """
        if not isinstance(result, dict):
            raise RPCStreamingError("stream event must be a dict")
        kind = result.get("kind")
        if not kind:
            raise RPCStreamingError("stream event missing kind")
        payload = result.get("payload") or ""
        if isinstance(payload, (bytes, bytearray)):
            payload = payload.decode("utf-8", errors="replace")
        return cls(
            call_id=str(result.get("call_id") or ""),
            request_id=str(result.get("request_id") or ""),
            sequence=int(result.get("sequence") or 0),
            kind=str(kind),
            payload=payload,
            target_identity=str(result.get("target_identity") or ""),
        )

    @property
    def is_terminal(self) -> bool:
        return self.kind in TERMINAL_KINDS


class RPCStreamingError(RuntimeError):
    """Raised when a streaming RPC response is malformed."""
