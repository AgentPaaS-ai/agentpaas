"""Worker bootstrap used by the Go harness."""

from __future__ import annotations

import importlib.util
import json
import os
import sys
import traceback
from typing import Any

from ._rpc import RPCClient
from .agent import agent

# Reserved keys that must never reach agent code.
_RESERVED_AGENT_KEYS = frozenset({"credentials", "llm", "mcp", "mcp_servers"})


def _sanitize_payload(payload: dict[str, Any]) -> dict[str, Any]:
    """Strip reserved platform keys before passing to the agent handler.

    Defense-in-depth: the Go harness already strips these, but the Python
    runner also strips them so agent code never sees credentials, llm config,
    or mcp config even if they somehow leak through.
    """
    return {
        k: v
        for k, v in payload.items()
        if k not in _RESERVED_AGENT_KEYS
        and not k.startswith("__agentpaas_")
    }


def run() -> None:
    agent_path = os.environ["AGENTPAAS_AGENT_PATH"]
    stdout_path = os.environ["AGENTPAAS_STDOUT_PATH"]

    # Detect service mode from env (set by harness when agent.yaml kind=mcp_service).
    agent_kind = os.environ.get("AGENTPAAS_AGENT_KIND", "")

    if agent_kind == "mcp_service":
        _run_service_mode(agent_path, stdout_path)
        return

    _run_worker_mode(agent_path, stdout_path)


def _run_worker_mode(agent_path: str, stdout_path: str) -> None:
    """Original worker/invoke mode (backward compatible)."""
    rpc_addr = os.environ["AGENTPAAS_RPC_ADDR"]

    protocol = os.fdopen(os.dup(1), "w", buffering=1)
    sys.stdout = open(stdout_path, "a", buffering=1)

    def send(value: dict[str, Any]) -> None:
        protocol.write(json.dumps(value, separators=(",", ":")) + "\n")
        protocol.flush()

    try:
        module = _load_user_agent(agent_path)
        legacy_invoke = getattr(module, "invoke", None)
        if agent._invoke_handler is None and callable(legacy_invoke):
            agent.on_invoke(legacy_invoke)
        if agent._invoke_handler is None:
            app_fn = getattr(module, "app", None)
            if callable(app_fn):
                agent.on_invoke(app_fn)
        rpc = RPCClient(rpc_addr)
        agent.set_rpc(rpc)
    except Exception:
        send(
            {
                "type": "import_failed",
                "reason": "import_failed",
                "detail": traceback.format_exc(),
            }
        )
        sys.exit(2)

    send({"type": "ready"})
    for line in sys.stdin:
        try:
            payload = json.loads(line)
            sanitized = _sanitize_payload(payload)
            result = agent.invoke(sanitized)
            send({"type": "ok", "result": result})
        except Exception:
            send(
                {
                    "type": "failed",
                    "reason": "invoke_failed",
                    "detail": traceback.format_exc(),
                }
            )


def _run_service_mode(agent_path: str, stdout_path: str) -> None:
    """Service runner mode for kind=mcp_service (B33-T02).

    No unix socket / RPC required. Reads line-delimited JSON on stdin.
    Supports: mcp_tools_list, mcp_tools_call, shutdown.
    Loops until EOF or shutdown; one tool returning does not exit.
    """
    protocol = os.fdopen(os.dup(1), "w", buffering=1)
    sys.stdout = open(stdout_path, "a", buffering=1)

    def send(value: dict[str, Any]) -> None:
        protocol.write(json.dumps(value, separators=(",", ":")) + "\n")
        protocol.flush()

    # Load user module so @mcp_tool registrations run.
    try:
        _load_user_agent(agent_path)
    except Exception:
        send(
            {
                "type": "import_failed",
                "reason": "import_failed",
                "detail": traceback.format_exc(),
            }
        )
        sys.exit(2)

    # Validate tool-set equality: declared (from env) vs registered.
    declared_raw = os.environ.get("AGENTPAAS_MCP_DECLARED_TOOLS", "")
    declared = [t for t in declared_raw.split(",") if t] if declared_raw else []

    err = agent.validate_declared_tools(declared)
    if err:
        send(
            {
                "type": "import_failed",
                "reason": "tool_set_mismatch",
                "detail": err,
            }
        )
        sys.exit(2)

    send({"type": "ready"})

    # Main service loop.
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            send({
                "type": "error", "id": "", "ok": False,
                "error": {"code": "protocol_error", "message": "invalid JSON"},
            })
            continue

        req_type = req.get("type", "")
        req_id = req.get("id", "")

        if req_type == "mcp_tools_list":
            tools = agent.list_mcp_tools()
            send({"type": "mcp_tools_list_result", "id": req_id, "ok": True, "tools": tools})
        elif req_type == "mcp_tools_call":
            tool = req.get("tool", "")
            args = req.get("arguments", {})
            try:
                result = agent.call_mcp_tool(tool, args)
                send({"type": "mcp_tools_result", "id": req_id, "ok": True, "result": result})
            except Exception as e:
                code = getattr(e, "code", "tool_error")
                send({
                    "type": "mcp_tools_result", "id": req_id, "ok": False,
                    "error": {"code": code, "message": str(e)},
                })
        elif req_type == "shutdown":
            send({"type": "shutdown_ack", "id": req_id})
            break
        else:
            send({
                "type": "error", "id": req_id, "ok": False,
                "error": {"code": "unknown_type", "message": f"unknown message type {req_type!r}"},
            })


def _load_user_agent(agent_path: str) -> Any:
    spec = importlib.util.spec_from_file_location("agentpaas_user_agent", agent_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load agent module")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
