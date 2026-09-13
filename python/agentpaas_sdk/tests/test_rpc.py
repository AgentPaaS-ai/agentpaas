"""RPC client read-deadline tests.

A hanging harness that never writes a response line must not block
agent.llm() forever. call() (and the call_stream handshake) raise
RPCError(code=llm_rpc_timeout) when the unix-socket read deadline fires.
"""

from __future__ import annotations

import json
import os
import shutil
import socket
import tempfile
import threading
import unittest

from agentpaas_sdk._rpc import RPCClient, RPCError


class _HangingHarness:
    """Unix-socket server that accepts a connection and never writes a line."""

    def __init__(self) -> None:
        self._tmpdir = tempfile.mkdtemp(prefix="ap-rpc-hang-")
        self.addr = os.path.join(self._tmpdir, "rpc.sock")
        self._server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self._server.bind(self.addr)
        self._server.listen(1)
        self._conns: list[socket.socket] = []
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._serve, daemon=True)
        self._thread.start()

    def _serve(self) -> None:
        self._server.settimeout(0.2)
        while not self._stop.is_set():
            try:
                conn, _ = self._server.accept()
            except (TimeoutError, socket.timeout, OSError):
                continue
            self._conns.append(conn)
            try:
                conn.recv(65536)
            except OSError:
                pass
            # Hold the connection open without writing a response line.
            self._stop.wait()

    def close(self) -> None:
        self._stop.set()
        try:
            self._server.close()
        except OSError:
            pass
        for conn in self._conns:
            try:
                conn.close()
            except OSError:
                pass
        shutil.rmtree(self._tmpdir, ignore_errors=True)


class _ReplyingHarness:
    """Unix-socket server that replies to one RPC request with a success line."""

    def __init__(self, result: dict) -> None:
        self._tmpdir = tempfile.mkdtemp(prefix="ap-rpc-ok-")
        self.addr = os.path.join(self._tmpdir, "rpc.sock")
        self._server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self._server.bind(self.addr)
        self._server.listen(1)
        self._conns: list[socket.socket] = []
        self._thread = threading.Thread(
            target=self._serve, args=(result,), daemon=True
        )
        self._thread.start()

    def _serve(self, result: dict) -> None:
        try:
            self._server.settimeout(2)
            conn, _ = self._server.accept()
            self._conns.append(conn)
            _ = conn.recv(65536)
            payload = json.dumps({"ok": True, "result": result}) + "\n"
            conn.sendall(payload.encode("utf-8"))
        except OSError:
            return

    def close(self) -> None:
        try:
            self._server.close()
        except OSError:
            pass
        for conn in self._conns:
            try:
                conn.close()
            except OSError:
                pass
        shutil.rmtree(self._tmpdir, ignore_errors=True)


class RPCReadTimeoutTests(unittest.TestCase):
    def setUp(self) -> None:
        self._prev_timeout = os.environ.get("AGENTPAAS_RPC_READ_TIMEOUT_SEC")
        os.environ["AGENTPAAS_RPC_READ_TIMEOUT_SEC"] = "0.3"

    def tearDown(self) -> None:
        if self._prev_timeout is None:
            os.environ.pop("AGENTPAAS_RPC_READ_TIMEOUT_SEC", None)
        else:
            os.environ["AGENTPAAS_RPC_READ_TIMEOUT_SEC"] = self._prev_timeout

    def test_call_raises_timeout_when_harness_never_writes(self) -> None:
        harness = _HangingHarness()
        self.addCleanup(harness.close)
        client = RPCClient(harness.addr)
        self.addCleanup(client.close)

        with self.assertRaises(RPCError) as caught:
            client.call("llm", {"prompt": "hi"})

        self.assertEqual(caught.exception.code, "llm_rpc_timeout")
        self.assertIn("timed out", str(caught.exception).lower())

    def test_call_stream_handshake_raises_timeout_when_harness_never_writes(self) -> None:
        harness = _HangingHarness()
        self.addCleanup(harness.close)
        client = RPCClient(harness.addr)
        self.addCleanup(client.close)

        with self.assertRaises(RPCError) as caught:
            next(client.call_stream("llm_stream", {"prompt": "hi"}))

        self.assertEqual(caught.exception.code, "llm_rpc_timeout")

    def test_call_still_returns_when_harness_replies(self) -> None:
        harness = _ReplyingHarness({"text": "pong", "tokens": 1})
        self.addCleanup(harness.close)
        client = RPCClient(harness.addr)
        self.addCleanup(client.close)

        result = client.call("llm", {"prompt": "hi"})
        self.assertEqual(result["text"], "pong")
        self.assertEqual(result["tokens"], 1)
