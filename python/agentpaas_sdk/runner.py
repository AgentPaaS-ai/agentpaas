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


def _load_user_agent(agent_path: str) -> Any:
    spec = importlib.util.spec_from_file_location("agentpaas_user_agent", agent_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load agent module")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
