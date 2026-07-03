"""Pre-flight daemon socket check tests for B14A-T06 (GAP-8).

Updated for B16-fix-round2: socket path resolution now mirrors the Go
daemon's DiscoverSocketPath — <home>/daemon.sock (not <home>/run/agentpaas.sock),
and falls back to ~/.agentpaas/daemon.sock when no env vars are set.
"""

import os
import socket
import tempfile
import unittest
from unittest import mock

from test_plugin_skeleton import _load_plugin_package


class DaemonSocketCheckTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tools = _load_plugin_package().tools

    def setUp(self):
        self.original_env = os.environ.copy()
        self._sockets = []

    def tearDown(self):
        for sock in self._sockets:
            try:
                sock.close()
            except OSError:
                pass
        os.environ.clear()
        os.environ.update(self.original_env)

    def _bind_unix_socket(self, path):
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.bind(path)
        self._sockets.append(sock)
        return sock

    def test_socket_available_via_env(self):
        """AGENTPAAS_SOCKET env var provides a direct socket path."""
        with tempfile.TemporaryDirectory() as tmpdir:
            sock_path = os.path.join(tmpdir, "daemon.sock")
            self._bind_unix_socket(sock_path)
            os.environ["AGENTPAAS_SOCKET"] = sock_path
            os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
            os.environ.pop("AGENTPAAS_HOME", None)

            available, err = self.tools._check_daemon_socket()

        self.assertTrue(available)
        self.assertIsNone(err)

    def test_socket_not_found(self):
        """Nonexistent socket path returns daemon_unavailable."""
        os.environ["AGENTPAAS_SOCKET"] = "/tmp/nonexistent.sock"
        os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
        os.environ.pop("AGENTPAAS_HOME", None)

        available, err = self.tools._check_daemon_socket()

        self.assertFalse(available)
        self.assertEqual(err["error_category"], "daemon_unavailable")

    def test_socket_from_home(self):
        """AGENTPAAS_HOME resolves to <home>/daemon.sock (daemon convention)."""
        with tempfile.TemporaryDirectory() as tmpdir:
            sock_path = os.path.join(tmpdir, "daemon.sock")
            self._bind_unix_socket(sock_path)
            os.environ.pop("AGENTPAAS_SOCKET", None)
            os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
            os.environ["AGENTPAAS_HOME"] = tmpdir

            available, err = self.tools._check_daemon_socket()

        self.assertTrue(available)
        self.assertIsNone(err)

    def test_socket_path_is_not_socket(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            sock_path = os.path.join(tmpdir, "daemon.sock")
            with open(sock_path, "w", encoding="utf-8") as f:
                f.write("not a socket")
            os.environ["AGENTPAAS_SOCKET"] = sock_path
            os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
            os.environ.pop("AGENTPAAS_HOME", None)

            available, err = self.tools._check_daemon_socket()

        self.assertFalse(available)
        self.assertEqual(err["error_category"], "daemon_unavailable")

    def test_resolve_socket_path_priority(self):
        """AGENTPAAS_SOCKET_PATH takes priority over AGENTPAAS_SOCKET."""
        os.environ["AGENTPAAS_SOCKET_PATH"] = "/priority/path.sock"
        os.environ["AGENTPAAS_SOCKET"] = "/lower/path.sock"
        os.environ.pop("AGENTPAAS_HOME", None)

        path = self.tools._resolve_socket_path()
        self.assertEqual(path, "/priority/path.sock")

    def test_resolve_socket_path_home_fallback(self):
        """Without env socket vars, falls back to <AGENTPAAS_HOME>/daemon.sock."""
        with tempfile.TemporaryDirectory() as tmpdir:
            os.environ.pop("AGENTPAAS_SOCKET", None)
            os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
            os.environ["AGENTPAAS_HOME"] = tmpdir

            path = self.tools._resolve_socket_path()
            self.assertEqual(path, os.path.join(tmpdir, "daemon.sock"))

    def test_resolve_socket_path_default_home(self):
        """Without any env vars, falls back to ~/.agentpaas/daemon.sock."""
        os.environ.pop("AGENTPAAS_SOCKET", None)
        os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
        os.environ.pop("AGENTPAAS_HOME", None)

        path = self.tools._resolve_socket_path()
        # Should end with .agentpaas/daemon.sock
        self.assertTrue(path.endswith(os.path.join(".agentpaas", "daemon.sock")))


class RunCliSocketCheckTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tools = _load_plugin_package().tools

    def setUp(self):
        self.original_env = os.environ.copy()

    def tearDown(self):
        os.environ.clear()
        os.environ.update(self.original_env)

    def test_run_cli_returns_daemon_unavailable(self):
        os.environ["AGENTPAAS_SOCKET"] = "/tmp/nonexistent.sock"
        os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
        os.environ.pop("AGENTPAAS_HOME", None)

        with mock.patch.object(self.tools, "_resolve_agent_binary", return_value="/bin/false"):
            result = self.tools._run_cli(["status"])

        self.assertEqual(result["error_category"], "daemon_unavailable")

    def test_doctor_skips_socket_check(self):
        os.environ["AGENTPAAS_SOCKET"] = "/tmp/nonexistent.sock"
        os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
        os.environ.pop("AGENTPAAS_HOME", None)

        mock_proc = mock.Mock(returncode=0, stdout='{"ok": true}', stderr="")
        with mock.patch.object(self.tools, "_resolve_agent_binary", return_value="/bin/true"):
            with mock.patch("subprocess.run", return_value=mock_proc) as mock_run:
                result = self.tools._run_cli(["doctor"])

        mock_run.assert_called_once()
        self.assertEqual(result, {"ok": True})

    def test_run_cli_passes_socket_and_home_flags(self):
        """_run_cli always passes --socket and --home to the CLI."""
        with tempfile.TemporaryDirectory() as tmpdir:
            sock_path = os.path.join(tmpdir, "daemon.sock")
            s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            s.bind(sock_path)
            s.close()

            os.environ.pop("AGENTPAAS_SOCKET", None)
            os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
            os.environ["AGENTPAAS_HOME"] = tmpdir

            mock_proc = mock.Mock(returncode=0, stdout='{"ok": true}', stderr="")
            with mock.patch.object(self.tools, "_resolve_agent_binary", return_value="/bin/true"):
                with mock.patch("subprocess.run", return_value=mock_proc) as mock_run:
                    self.tools._run_cli(["status"])

            called_args = mock_run.call_args[0][0]
            self.assertIn("--socket", called_args)
            self.assertIn(sock_path, called_args)
            self.assertIn("--home", called_args)
            self.assertIn(tmpdir, called_args)


if __name__ == "__main__":
    unittest.main()
