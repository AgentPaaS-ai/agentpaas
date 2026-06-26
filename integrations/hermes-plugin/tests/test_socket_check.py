"""Pre-flight daemon socket check tests for B14A-T06 (GAP-8)."""

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

    def test_socket_available(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            sock_path = os.path.join(tmpdir, "agentpaas.sock")
            self._bind_unix_socket(sock_path)
            os.environ["AGENTPAAS_SOCKET"] = sock_path
            os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
            os.environ.pop("AGENTPAAS_HOME", None)

            available, err = self.tools._check_daemon_socket()

        self.assertTrue(available)
        self.assertIsNone(err)

    def test_socket_not_found(self):
        os.environ["AGENTPAAS_SOCKET"] = "/tmp/nonexistent.sock"
        os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
        os.environ.pop("AGENTPAAS_HOME", None)

        available, err = self.tools._check_daemon_socket()

        self.assertFalse(available)
        self.assertEqual(err["error_category"], "daemon_unavailable")

    def test_socket_not_configured(self):
        os.environ.pop("AGENTPAAS_SOCKET", None)
        os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
        os.environ.pop("AGENTPAAS_HOME", None)

        available, err = self.tools._check_daemon_socket()

        self.assertFalse(available)
        self.assertEqual(err["error_category"], "daemon_unavailable")

    def test_socket_path_is_not_socket(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            sock_path = os.path.join(tmpdir, "agentpaas.sock")
            with open(sock_path, "w", encoding="utf-8") as f:
                f.write("not a socket")
            os.environ["AGENTPAAS_SOCKET"] = sock_path
            os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
            os.environ.pop("AGENTPAAS_HOME", None)

            available, err = self.tools._check_daemon_socket()

        self.assertFalse(available)
        self.assertEqual(err["error_category"], "daemon_unavailable")

    def test_socket_from_home(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            run_dir = os.path.join(tmpdir, "run")
            os.makedirs(run_dir)
            sock_path = os.path.join(run_dir, "agentpaas.sock")
            self._bind_unix_socket(sock_path)
            os.environ.pop("AGENTPAAS_SOCKET", None)
            os.environ.pop("AGENTPAAS_SOCKET_PATH", None)
            os.environ["AGENTPAAS_HOME"] = tmpdir

            available, err = self.tools._check_daemon_socket()

        self.assertTrue(available)
        self.assertIsNone(err)


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


if __name__ == "__main__":
    unittest.main()