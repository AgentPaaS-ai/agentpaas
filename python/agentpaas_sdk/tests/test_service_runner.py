"""Tests for the service runner mode (B33-T02).

Uses subprocess to run a minimal service script through the runner.
"""

import json
import os
import subprocess
import sys
import tempfile
import unittest


REPO_PYTHON = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))


class ServiceRunnerTests(unittest.TestCase):

    def _run_service(self, agent_code: str, declared_tools: list[str],
                     max_concurrency: int = 1,
                     expect_ready: bool = True):
        """Spawn a service runner subprocess and return (proc, first_msg).

        If expect_ready is False, the first message will be import_failed.
        """
        agent_dir = tempfile.mkdtemp(prefix="ap_svc_test_")
        self.addCleanup(lambda: _rmtree(agent_dir))

        agent_path = os.path.join(agent_dir, "service_agent.py")
        with open(agent_path, "w") as f:
            f.write(agent_code)

        # Build the runner wrapper as a standalone script.
        wrapper_path = os.path.join(agent_dir, "_runner.py")
        wrapper_src = _build_wrapper_src(
            agent_kind="mcp_service",
            agent_path=agent_path,
            stdout_path=os.path.join(agent_dir, "stdout.txt"),
            declared_tools=",".join(declared_tools),
            max_concurrency=str(max_concurrency),
        )
        with open(wrapper_path, "w") as f:
            f.write(wrapper_src)

        proc = subprocess.Popen(
            [sys.executable, "-u", wrapper_path],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env={**os.environ, "PYTHONPATH": REPO_PYTHON},
            text=True,
            bufsize=1,
        )
        self.addCleanup(_close_proc, proc)

        first_line = proc.stdout.readline().strip()
        if not first_line:
            stderr_data = proc.stderr.read()
            proc.wait()
            self.fail(f"no output from runner; stderr: {stderr_data}")
        msg = json.loads(first_line)
        if expect_ready:
            self.assertEqual(msg.get("type"), "ready",
                             f"expected ready, got {first_line}")
        else:
            self.assertEqual(msg.get("type"), "import_failed",
                             f"expected import_failed, got {first_line}")

        return proc, msg

    def _send_receive(self, proc, req: dict) -> dict:
        """Send a JSON line to the runner and return the response."""
        proc.stdin.write(json.dumps(req) + "\n")
        proc.stdin.flush()
        line = proc.stdout.readline().strip()
        if not line:
            return {"ok": False, "error": {"message": "eof"}}
        return json.loads(line)

    # ---- Basic dispatch tests ----

    def test_tools_list_returns_declared_tools(self):
        proc, _ = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("lookup_feedback")
def lookup_feedback(args):
    return {"items": []}

@agent.mcp_tool("list_accounts")
def list_accounts(args):
    return {"accounts": []}
""", ["lookup_feedback", "list_accounts"])
        resp = self._send_receive(proc, {"type": "mcp_tools_list", "id": "1"})
        self.assertTrue(resp["ok"])
        self.assertEqual(resp["tools"], ["list_accounts", "lookup_feedback"])

    def test_tools_call_returns_distinctive_real_value(self):
        proc, _ = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("echo")
def echo(args):
    return {"received": args.get("message", ""), "distinctive": "b33-t02-real"}
""", ["echo"])
        resp = self._send_receive(proc, {
            "type": "mcp_tools_call", "id": "call-1",
            "tool": "echo", "arguments": {"message": "hello"}
        })
        self.assertTrue(resp["ok"])
        self.assertEqual(resp["result"]["received"], "hello")
        self.assertEqual(resp["result"]["distinctive"], "b33-t02-real")

    def test_tools_call_undeclared_tool_denied(self):
        proc, _ = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("allowed_tool")
def allowed(args):
    return {"ok": True}
""", ["allowed_tool"])
        resp = self._send_receive(proc, {
            "type": "mcp_tools_call", "id": "bad-1",
            "tool": "other_tool", "arguments": {}
        })
        self.assertFalse(resp["ok"])
        self.assertIn("not registered", resp["error"]["message"])

    def test_tools_call_error_bounded_no_traceback(self):
        proc, _ = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("explode")
def explode(args):
    raise RuntimeError("bang!")
""", ["explode"])
        resp = self._send_receive(proc, {
            "type": "mcp_tools_call", "id": "err-1",
            "tool": "explode", "arguments": {}
        })
        self.assertFalse(resp["ok"])
        self.assertEqual(resp["error"]["code"], "tool_error")
        self.assertIn("bang!", resp["error"]["message"])
        self.assertNotIn("Traceback", resp["error"]["message"])

    # ---- Tool-set mismatch tests ----

    def test_tool_set_mismatch_missing_declared_fails_ready(self):
        proc, msg = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("only_one")
def only_one(args):
    return {}
""", ["only_one", "missing_tool"], expect_ready=False)
        self.assertEqual(msg["type"], "import_failed")
        self.assertEqual(msg["reason"], "tool_set_mismatch")
        self.assertIn("missing_tool", msg["detail"])

    def test_tool_set_mismatch_extra_registered_fails_ready(self):
        proc, msg = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("declared")
def declared(args):
    return {}

@agent.mcp_tool("undeclared_extra")
def extra(args):
    return {}
""", ["declared"], expect_ready=False)
        self.assertEqual(msg["type"], "import_failed")
        self.assertEqual(msg["reason"], "tool_set_mismatch")
        self.assertIn("undeclared_extra", msg["detail"])

    def test_tool_set_exact_match_ready(self):
        proc, msg = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("tool_a")
def tool_a(args):
    return {}

@agent.mcp_tool("tool_b")
def tool_b(args):
    return {}
""", ["tool_a", "tool_b"])

    # ---- Shutdown test ----

    def test_shutdown_ack(self):
        proc, _ = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("t1")
def t1(args):
    return {}
""", ["t1"])
        resp = self._send_receive(proc, {"type": "shutdown", "id": "sd-1"})
        self.assertEqual(resp["type"], "shutdown_ack")
        proc.wait(timeout=5)
        self.assertEqual(proc.returncode, 0)

    # ---- Serial calls test ----

    def test_serial_calls_work(self):
        proc, _ = self._run_service("""
from agentpaas_sdk import agent

@agent.mcp_tool("counter")
def counter(args):
    return {"count": args.get("n", 0) + 1}
""", ["counter"])
        for i in range(3):
            resp = self._send_receive(proc, {
                "type": "mcp_tools_call", "id": f"c-{i}",
                "tool": "counter", "arguments": {"n": i}
            })
            self.assertTrue(resp["ok"])
            self.assertEqual(resp["result"]["count"], i + 1)


def _build_wrapper_src(agent_kind, agent_path, stdout_path, declared_tools, max_concurrency):
    """Build a self-contained runner wrapper script as a string."""
    return f'''
import os, sys, json

os.environ["AGENTPAAS_AGENT_KIND"] = {agent_kind!r}
os.environ["AGENTPAAS_AGENT_PATH"] = {agent_path!r}
os.environ["AGENTPAAS_STDOUT_PATH"] = {stdout_path!r}
os.environ["AGENTPAAS_MCP_DECLARED_TOOLS"] = {declared_tools!r}
os.environ["AGENTPAAS_MCP_MAX_CONCURRENCY"] = {max_concurrency!r}

from agentpaas_sdk import agent as sdk_agent

agent_path = os.environ["AGENTPAAS_AGENT_PATH"]
declared_raw = os.environ.get("AGENTPAAS_MCP_DECLARED_TOOLS", "")
declared = [t for t in declared_raw.split(",") if t] if declared_raw else []

import importlib.util
spec = importlib.util.spec_from_file_location("test_service_agent", agent_path)
if spec is None or spec.loader is None:
    msg = json.dumps({{"type": "import_failed", "reason": "import_failed",
                        "detail": "unable to load agent module"}})
    sys.stdout.write(msg + "\\n")
    sys.stdout.flush()
    sys.exit(2)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

err = sdk_agent.validate_declared_tools(declared)
if err:
    msg = json.dumps({{"type": "import_failed", "reason": "tool_set_mismatch", "detail": err}})
    sys.stdout.write(msg + "\\n")
    sys.stdout.flush()
    sys.exit(2)

msg = json.dumps({{"type": "ready"}})
sys.stdout.write(msg + "\\n")
sys.stdout.flush()

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except json.JSONDecodeError:
        err_resp = {{"type": "error", "id": "", "ok": False,
                     "error": {{"code": "protocol_error", "message": "invalid json"}}}}
        sys.stdout.write(json.dumps(err_resp) + "\\n")
        sys.stdout.flush()
        continue
    req_type = req.get("type", "")
    req_id = req.get("id", "")
    if req_type == "mcp_tools_list":
        tools = sdk_agent.list_mcp_tools()
        resp = {{"type": "mcp_tools_list_result", "id": req_id, "ok": True, "tools": tools}}
    elif req_type == "mcp_tools_call":
        tool = req.get("tool", "")
        args = req.get("arguments", {{}})
        try:
            result = sdk_agent.call_mcp_tool(tool, args)
            resp = {{"type": "mcp_tools_result", "id": req_id, "ok": True, "result": result}}
        except Exception as e:
            code = getattr(e, "code", "tool_error")
            resp = {{"type": "mcp_tools_result", "id": req_id, "ok": False,
                     "error": {{"code": code, "message": str(e)}}}}
    elif req_type == "shutdown":
        resp = {{"type": "shutdown_ack", "id": req_id}}
        sys.stdout.write(json.dumps(resp) + "\\n")
        sys.stdout.flush()
        break
    else:
        resp = {{"type": "error", "id": req_id, "ok": False,
                 "error": {{"code": "unknown_type", "message": "unknown message type"}}}}
    sys.stdout.write(json.dumps(resp) + "\\n")
    sys.stdout.flush()
'''


def _rmtree(path):
    import shutil
    shutil.rmtree(path, ignore_errors=True)


def _close_proc(proc):
    """Gracefully close a subprocess and its pipes."""
    if proc is None:
        return
    try:
        if proc.stdin:
            proc.stdin.close()
    except Exception:
        pass
    try:
        if proc.stdout:
            proc.stdout.close()
    except Exception:
        pass
    try:
        if proc.stderr:
            proc.stderr.close()
    except Exception:
        pass
    try:
        proc.wait(timeout=2)
    except Exception:
        try:
            proc.kill()
            proc.wait(timeout=2)
        except Exception:
            pass


if __name__ == "__main__":
    unittest.main()
