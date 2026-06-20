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
        _load_user_agent(agent_path)
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
            result = agent.invoke(payload)
            send({"type": "ok", "result": result})
        except Exception:
            send(
                {
                    "type": "failed",
                    "reason": "invoke_failed",
                    "detail": traceback.format_exc(),
                }
            )


def _load_user_agent(agent_path: str) -> None:
    spec = importlib.util.spec_from_file_location("agentpaas_user_agent", agent_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load agent module")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
