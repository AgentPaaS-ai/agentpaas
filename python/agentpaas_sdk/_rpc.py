"""Line-delimited JSON RPC client for the AgentPaaS harness."""

from __future__ import annotations

import json
import socket
import threading
import uuid
from typing import Any


class RPCError(RuntimeError):
    """Raised when the harness rejects an SDK RPC call."""

    def __init__(self, message: str, code: str = "rpc_error") -> None:
        super().__init__(message)
        self.code = code

    def __str__(self) -> str:
        return f"RPCError({self.code}): {super().__str__()}"


class BudgetExceeded(RPCError):
    """Raised when an SDK call exceeds the active harness budget."""


class ProgressError(RPCError):
    """Raised when the harness rejects a progress call."""


class CheckpointRejected(RPCError):
    """Raised when a safe_to_resume checkpoint is rejected."""


class ArtifactRejected(RPCError):
    """Raised when an artifact reference is rejected."""


class LeaseExpired(RPCError):
    """Raised when the attempt lease has expired."""


class StreamingNotSupported(RPCError):
    """Raised when the connected harness does not support streaming."""


# Maps RPC error codes to exception classes for progress-related calls.
_PROGRESS_ERROR_MAP = {
    "INVALID_PROGRESS": ProgressError,
    "CHECKPOINT_REJECTED": CheckpointRejected,
    "ARTIFACT_REJECTED": ArtifactRejected,
    "LEASE_EXPIRED": LeaseExpired,
}


class RPCClient:
    def __init__(self, addr: str) -> None:
        if not addr:
            raise RPCError("AGENTPAAS_RPC_ADDR is required", "missing_rpc_addr")
        self._addr = addr
        self._sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self._sock.connect(addr)
        self._file = self._sock.makefile("rwb", buffering=0)
        self._lock = threading.Lock()

    def close(self) -> None:
        try:
            self._file.close()
        finally:
            self._sock.close()

    def call(self, method: str, params: dict[str, Any] | None = None) -> Any:
        request_id = uuid.uuid4().hex
        payload = {
            "id": request_id,
            "method": method,
            "params": params or {},
        }
        data = json.dumps(payload, separators=(",", ":")).encode("utf-8") + b"\n"
        with self._lock:
            self._file.write(data)
            line = self._file.readline()
        if not line:
            raise RPCError("harness rpc connection closed", "rpc_closed")
        response = json.loads(line.decode("utf-8"))
        if response.get("ok"):
            return response.get("result")
        message = response.get("error") or "harness rpc call failed"
        code = response.get("code") or "rpc_error"
        if code == "BUDGET_EXCEEDED":
            raise BudgetExceeded(message, code)
        exc_cls = _PROGRESS_ERROR_MAP.get(code)
        if exc_cls is not None:
            raise exc_cls(message, code)
        raise RPCError(message, code)

    def call_stream(self, method: str, params: dict[str, Any] | None = None):
        """Send an RPC request and yield successive response lines.

        The harness streams one JSON response per line. The first line is the
        handshake (ok=true with a result, or ok=false with an error). If the
        first line is an error with code ``streaming_not_supported`` (or any
        error), :class:`StreamingNotSupported` (or :class:`RPCError`) is raised
        before any events are yielded.

        Subsequent lines are stream events, yielded as decoded dicts, until the
        harness closes the response stream (EOF) or a terminal event is
        received.
        """
        request_id = uuid.uuid4().hex
        payload = {
            "id": request_id,
            "method": method,
            "params": params or {},
            "stream": True,
        }
        data = json.dumps(payload, separators=(",", ":")).encode("utf-8") + b"\n"
        with self._lock:
            self._file.write(data)
            first = self._file.readline()
        if not first:
            raise RPCError("harness rpc connection closed", "rpc_closed")
        first_resp = json.loads(first.decode("utf-8"))
        if not first_resp.get("ok"):
            message = first_resp.get("error") or "harness rpc call failed"
            code = first_resp.get("code") or "rpc_error"
            if code == "streaming_not_supported":
                raise StreamingNotSupported(message, code)
            if code == "BUDGET_EXCEEDED":
                raise BudgetExceeded(message, code)
            raise RPCError(message, code)
        # If the handshake itself carries a terminal result (no further
        # lines), yield nothing and return.
        yield from self._read_stream_lines()

    def _read_stream_lines(self):
        # Read subsequent JSON lines until EOF. Each line is a stream event.
        with self._lock:
            while True:
                line = self._file.readline()
                if not line:
                    return
                yield json.loads(line.decode("utf-8"))
