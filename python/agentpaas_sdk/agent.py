"""User-facing AgentPaaS SDK helpers."""

from __future__ import annotations

from typing import Any, Callable

from ._rpc import BudgetExceeded, RPCClient, RPCError

InvokeHandler = Callable[[dict[str, Any]], dict[str, Any]]


class Agent:
    def __init__(self) -> None:
        self._invoke_handler: InvokeHandler | None = None
        self._rpc: RPCClient | None = None

    def on_invoke(self, fn: InvokeHandler) -> InvokeHandler:
        self._invoke_handler = fn
        return fn

    def set_rpc(self, rpc: RPCClient) -> None:
        self._rpc = rpc

    def clear_rpc(self) -> None:
        self._rpc = None

    def invoke(self, payload: dict[str, Any]) -> dict[str, Any]:
        if self._invoke_handler is not None:
            return self._invoke_handler(payload)
        raise RuntimeError("agent must register an invoke handler with @agent.on_invoke")

    def llm(self, prompt: str, **kwargs: Any) -> dict[str, Any]:
        params = {"prompt": prompt, **kwargs}
        return self._call("llm", params)

    def record_iteration(self) -> dict[str, Any]:
        return self._call("record_iteration", {})

    def http(self, method: str, url: str, **kwargs: Any) -> dict[str, Any]:
        params = {"method": method, "url": url, **kwargs}
        return self._call("http", params)

    def http_with_credential(
        self,
        credential_id: str,
        method: str,
        url: str,
        **kwargs: Any,
    ) -> dict[str, Any]:
        params = {
            "credential_id": credential_id,
            "method": method,
            "url": url,
            **kwargs,
        }
        return self._call("http_with_credential", params)

    def mcp(self, server_id: str, tool: str, input: dict[str, Any]) -> dict[str, Any]:
        return self._call("mcp", {"server_id": server_id, "tool": tool, "input": input})

    def _call(self, method: str, params: dict[str, Any]) -> dict[str, Any]:
        if self._rpc is None:
            raise RPCError("SDK RPC is not connected", "rpc_not_connected")
        result = self._rpc.call(method, params)
        if isinstance(result, dict):
            return result
        return {"result": result}


agent = Agent()
