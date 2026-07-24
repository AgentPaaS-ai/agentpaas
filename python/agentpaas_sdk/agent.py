"""User-facing AgentPaaS SDK helpers."""

from __future__ import annotations

import uuid
from typing import Any, Callable

from ._rpc import (
    ArtifactRejected,
    BudgetExceeded,
    CheckpointRejected,
    LeaseExpired,
    ProgressError,
    RPCClient,
    RPCError,
    StreamingNotSupported,
)
from .streaming import StreamEvent

InvokeHandler = Callable[[dict[str, Any]], dict[str, Any]]

# --- TaskHandle (B32-T03) ---------------------------------------------------

# Forbidden response key patterns — must never appear in agent-visible DTOs.
_FORBIDDEN_RESPONSE_KEYS = (
    "endpoint", "host", "ip", "port", "capability_token",
    "network_alias", "token", "secret", "capability_header",
)


def _validate_response_no_leaks(result: dict[str, Any]) -> None:
    """Reject any response that contains forbidden key patterns."""
    for key in result:
        key_lower = key.lower()
        for forbidden in _FORBIDDEN_RESPONSE_KEYS:
            if forbidden in key_lower:
                raise RPCError(
                    f"response contains forbidden key {key!r}",
                    "forbidden_response_key",
                )


class TaskHandle:
    """Handle to a delegated task. Agent code never sees endpoints or tokens."""

    def __init__(self, task_id: str, rpc: "RPCClient") -> None:
        self.task_id = task_id
        self._rpc = rpc

    def events(self, after_sequence: int = 0) -> list[dict[str, Any]]:
        """List events for this task after the given sequence number."""
        result = self._rpc.call("list_task_events", {
            "task_id": self.task_id,
            "after_sequence": after_sequence,
        })
        _validate_response_no_leaks(result)
        return result.get("events", [])

    def result(self, timeout_s: float | None = None) -> dict[str, Any] | None:
        """Poll for the task result. Returns None if not yet terminal.
        timeout_s is a stub for T05 wait/wake.
        """
        result = self._rpc.call("get_task", {"task_id": self.task_id})
        _validate_response_no_leaks(result)
        status = result.get("status")
        if status in ("SUCCEEDED", "FAILED", "CANCELLED", "EXPIRED", "DENIED"):
            return result
        return None

# --- v0.3 safety bounds (Block 27) -------------------------------------------

_PHASE_MAX = 128
_STR_ITEM_MAX = 1024
_LIST_MAX = 50
_ARTIFACT_MAX = 32
_ARTIFACT_PATH_MAX = 512
_ARTIFACT_SEGMENTS_MAX = 8


def _has_control_chars(s: str) -> bool:
    """Return True if s contains ASCII control chars (U+0000-U+001F or U+007F)."""
    return any(ord(c) < 0x20 or ord(c) == 0x7F for c in s)


def _check_str_list(
    values: list[str] | None,
    *,
    name: str,
    max_items: int,
    max_item_len: int,
) -> list[str]:
    """Validate a string list and return a fresh list (never a shared mutable)."""
    if values is None:
        return []
    if not isinstance(values, list):
        raise ProgressError(f"{name} must be a list", "INVALID_PROGRESS")
    if len(values) > max_items:
        raise ProgressError(
            f"{name} exceeds max {max_items} entries", "INVALID_PROGRESS",
        )
    out: list[str] = []
    for item in values:
        if not isinstance(item, str):
            raise ProgressError(
                f"{name} entries must be strings", "INVALID_PROGRESS",
            )
        if len(item.encode("utf-8")) > max_item_len:
            raise ProgressError(
                f"{name} entry exceeds {max_item_len} bytes", "INVALID_PROGRESS",
            )
        if _has_control_chars(item):
            raise ProgressError(
                f"{name} entry contains control characters", "INVALID_PROGRESS",
            )
        out.append(item)
    return out


def _validate_artifact_ref(path: str) -> None:
    """Lexical validation of a single artifact reference path."""
    if not isinstance(path, str) or not path:
        raise ArtifactRejected("artifact_reference cannot be empty", "ARTIFACT_REJECTED")
    if len(path) > _ARTIFACT_PATH_MAX:
        raise ArtifactRejected(
            f"artifact_reference exceeds {_ARTIFACT_PATH_MAX} chars", "ARTIFACT_REJECTED",
        )
    if "\\" in path:
        raise ArtifactRejected("artifact_reference cannot contain backslashes", "ARTIFACT_REJECTED")
    if path.startswith("/"):
        raise ArtifactRejected("artifact_reference cannot be absolute", "ARTIFACT_REJECTED")
    segments = path.split("/")
    if len(segments) > _ARTIFACT_SEGMENTS_MAX:
        raise ArtifactRejected(
            f"artifact_reference exceeds {_ARTIFACT_SEGMENTS_MAX} segments", "ARTIFACT_REJECTED",
        )
    import re
    seg_re = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
    for seg in segments:
        if not seg or seg == "." or seg == "..":
            raise ArtifactRejected(
                "artifact_reference has empty or dot segment", "ARTIFACT_REJECTED",
            )
        if not seg_re.match(seg):
            raise ArtifactRejected(
                f"artifact_reference segment '{seg}' is invalid", "ARTIFACT_REJECTED",
            )


class Agent:
    def __init__(self) -> None:
        self._invoke_handler: InvokeHandler | None = None
        self._rpc: RPCClient | None = None
        self._mcp_tools: dict[str, Callable[[dict[str, Any]], dict[str, Any]]] = {}

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

    def llm(self, prompt: str, model: str | None = None, **kwargs: Any) -> dict[str, Any]:
        params: dict[str, Any] = {"prompt": prompt}
        if model is not None:
            params["model"] = model
        params.update(kwargs)
        return self._call("llm", params)

    # ---- normalized envelope (B29-T02) -------------------------------------

    def messages(
        self,
        messages: list[dict[str, Any]],
        model: str | None = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        """Buffered multi-role call over the normalized model-call envelope.

        ``messages`` is a list of ``{"role": ..., "content": ...}`` dicts.
        Returns a single complete dict, like :meth:`llm`.
        """
        if not isinstance(messages, list) or not messages:
            raise RPCError("messages must be a non-empty list", "INVALID_ENVELOPE")
        params: dict[str, Any] = {"messages": list(messages)}
        if model is not None:
            params["model"] = model
        params.update(kwargs)
        return self._call("llm", params)

    def llm_stream(
        self,
        prompt: str | None = None,
        messages: list[dict[str, Any]] | None = None,
        model: str | None = None,
        **kwargs: Any,
    ):
        """Additive streaming method over the normalized call envelope.

        Exactly one of ``prompt`` or ``messages`` must be supplied. Returns an
        iterator yielding :class:`StreamEvent` objects governed by the harness
        streaming adapter (guardrail mode, incremental usage/budget,
        backpressure, cancellation). If the connected harness does not support
        streaming, raises :class:`StreamingNotSupported` eagerly (before the
        first yield). Input validation errors also raise eagerly.

        Event kinds (minimally):
          response_started; output_delta; tool_call_delta; usage_update;
          response_completed; response_failed.
        """
        if (prompt is None) == (messages is None):
            raise RPCError(
                "exactly one of prompt or messages must be supplied",
                "INVALID_ENVELOPE",
            )
        params: dict[str, Any] = {}
        if prompt is not None:
            params["prompt"] = prompt
        if messages is not None:
            if not isinstance(messages, list) or not messages:
                raise RPCError(
                    "messages must be a non-empty list", "INVALID_ENVELOPE",
                )
            params["messages"] = list(messages)
        if model is not None:
            params["model"] = model
        params.update(kwargs)

        if self._rpc is None:
            # No connected harness: streaming is not supported. Fail closed
            # with the typed error before any transport attempt.
            raise StreamingNotSupported(
                "connected harness does not support streaming",
                "streaming_not_supported",
            )

        # Only RPC clients that expose call_stream can stream. A plain call()
        # RPC (no streaming transport) is treated as not supported. This check
        # is eager so callers see StreamingNotSupported before iterating.
        call_stream = getattr(self._rpc, "call_stream", None)
        if not callable(call_stream):
            raise StreamingNotSupported(
                "connected harness does not support streaming",
                "streaming_not_supported",
            )

        return self._iter_stream(call_stream, params)

    def _iter_stream(self, call_stream, params: dict[str, Any]):
        """Generator backing llm_stream. Yields StreamEvent objects."""
        try:
            for raw in call_stream("llm_stream", params):
                yield StreamEvent.from_rpc(raw)
        except StreamingNotSupported:
            raise
        except RPCError as exc:
            # A harness RPC error with code streaming_not_supported is surfaced
            # as the typed StreamingNotSupported; other RPC errors propagate.
            if exc.code == "streaming_not_supported":
                raise StreamingNotSupported(str(exc), exc.code)
            raise

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

    # ---- delegation (B32-T03) ------------------------------------------------

    def delegate(
        self,
        capability: str,
        message: dict[str, Any] | list[Any],
        *,
        idempotency_key: str | None = None,
        operation: str = "",
        data_class: str = "internal",
    ) -> TaskHandle:
        """Delegate a task to another agent by logical capability name.

        Agent code NEVER provides or receives endpoints, addresses, ports,
        or capability tokens. The trusted harness/gateway resolves the
        logical identity to a real endpoint.

        Args:
            capability: Logical binding name from the signed workflow
                (e.g. \"report.verify\").
            message: A dict with \"role\"/\"parts\" keys or a simple dict
                that is coerced to a user text part.
            idempotency_key: Optional stable key for idempotent delegation.
            operation: Optional operation qualifier.
            data_class: Data classification level (public, internal,
                confidential, restricted). Default \"internal\".

        Returns:
            A TaskHandle for polling events and results.
        """
        if not isinstance(capability, str) or not capability.strip():
            raise RPCError("capability must be a non-empty string", "invalid_params")
        if " " in capability or "\n" in capability or "\x00" in capability:
            raise RPCError("capability contains forbidden characters", "invalid_params")

        # Normalize message.
        normalized_message = self._normalize_delegate_message(message)
        if normalized_message is None:
            raise RPCError(
                "message must be a dict with 'role'/'parts' or a simple dict",
                "invalid_message",
            )

        # Coerce idempotency_key from message if not provided.
        if idempotency_key is None:
            idempotency_key = normalized_message.get("idempotency_key")
        if not idempotency_key:
            import uuid
            idempotency_key = uuid.uuid4().hex

        result = self._call("delegate_task", {
            "capability": capability,
            "operation": operation,
            "message": normalized_message,
            "idempotency_key": idempotency_key,
            "data_class": data_class,
        })
        _validate_response_no_leaks(result)
        task_id = result.get("task_id")
        if not task_id:
            raise RPCError("delegate_task response missing task_id", "invalid_response")
        # _rpc is guaranteed non-None because _call would have raised.
        assert self._rpc is not None
        return TaskHandle(task_id, self._rpc)

    @staticmethod
    def _normalize_delegate_message(
        message: dict[str, Any] | list[Any],
    ) -> dict[str, Any] | None:
        """Normalize a delegate message to the canonical {role, parts} form."""
        if isinstance(message, dict):
            if "role" in message and "parts" in message:
                # Already canonical.
                return message
            # Coerce simple dict to user text part.
            text = message.get("text") or message.get("content") or ""
            if text and isinstance(text, str):
                return {
                    "role": "user",
                    "parts": [{"kind": "text", "text": text}],
                }
            # Fallback: JSON part.
            import json as _json
            return {
                "role": "user",
                "parts": [{"kind": "json", "json": _json.dumps(message)}],
            }
        if isinstance(message, list):
            # List of parts — wrap as user message.
            return {"role": "user", "parts": list(message)}
        return None

    # ---- progress (B27) -----------------------------------------------------

    def progress(
        self,
        phase: str,
        *,
        completed_work: list[str] | None = None,
        remaining_work: list[str] | None = None,
        artifact_references: list[str] | None = None,
        last_committed_action: str | None = None,
        safe_to_resume: bool = False,
    ) -> dict[str, Any]:
        """Report semantic progress to the runtime.

        Every call is a heartbeat.  When *safe_to_resume* is True the runtime
        creates a durable checkpoint provided *last_committed_action* is set and
        *completed_work* is non-empty.

        Example::

            resume = agent.progress(phase="starting").get("resume_checkpoint")
            if resume:
                restore_explicit_state(resume)

            agent.progress(
                phase="themes_complete",
                completed_work=["theme analysis"],
                remaining_work=["write report"],
                artifact_references=["themes.json"],
                last_committed_action="wrote themes.json",
                safe_to_resume=True,
            )

        Returns a dict with: ``recorded``, ``workflow_id``, ``node_id``,
        ``run_id``, ``attempt_id``, ``checkpoint_id``, ``lease_expires_at``,
        and optionally ``resume_checkpoint`` / ``resume_reason``.
        """
        # --- validate phase ---
        if not isinstance(phase, str):
            raise ProgressError("phase must be a string", "INVALID_PROGRESS")
        if not phase:
            raise ProgressError("phase must not be empty", "INVALID_PROGRESS")
        if len(phase.encode("utf-8")) > _PHASE_MAX:
            raise ProgressError(
                f"phase exceeds {_PHASE_MAX} bytes", "INVALID_PROGRESS",
            )
        if _has_control_chars(phase):
            raise ProgressError(
                "phase contains control characters", "INVALID_PROGRESS",
            )

        # --- validate collections (normalize None → empty list, fresh copy) ---
        cw = _check_str_list(
            completed_work, name="completed_work",
            max_items=_LIST_MAX, max_item_len=_STR_ITEM_MAX,
        )
        rw = _check_str_list(
            remaining_work, name="remaining_work",
            max_items=_LIST_MAX, max_item_len=_STR_ITEM_MAX,
        )

        art_refs: list[str] = []
        if artifact_references is not None:
            if not isinstance(artifact_references, list):
                raise ArtifactRejected(
                    "artifact_references must be a list", "ARTIFACT_REJECTED",
                )
            if len(artifact_references) > _ARTIFACT_MAX:
                raise ArtifactRejected(
                    f"artifact_references exceeds {_ARTIFACT_MAX} entries",
                    "ARTIFACT_REJECTED",
                )
            for ref in artifact_references:
                _validate_artifact_ref(ref)
                art_refs.append(ref)

        # --- validate last_committed_action ---
        if last_committed_action is not None:
            if not isinstance(last_committed_action, str):
                raise ProgressError(
                    "last_committed_action must be a string", "INVALID_PROGRESS",
                )
            if len(last_committed_action.encode("utf-8")) > _STR_ITEM_MAX:
                raise ProgressError(
                    f"last_committed_action exceeds {_STR_ITEM_MAX} bytes",
                    "INVALID_PROGRESS",
                )
            if _has_control_chars(last_committed_action):
                raise ProgressError(
                    "last_committed_action contains control characters",
                    "INVALID_PROGRESS",
                )

        # --- validate safe_to_resume constraints ---
        if safe_to_resume:
            if not last_committed_action:
                raise ProgressError(
                    "safe_to_resume=True requires last_committed_action",
                    "INVALID_PROGRESS",
                )
            # Spec: "at least one non-empty completed_work entry"
            has_non_empty = any(entry != "" for entry in cw)
            if not has_non_empty:
                raise ProgressError(
                    "safe_to_resume=True requires at least one non-empty completed_work entry",
                    "INVALID_PROGRESS",
                )

        # --- validate safe_to_resume type ---
        if not isinstance(safe_to_resume, bool):
            raise ProgressError(
                "safe_to_resume must be a boolean", "INVALID_PROGRESS",
            )

        # --- build RPC params ---
        event_id = uuid.uuid4().hex
        params: dict[str, Any] = {
            "event_id": event_id,
            "phase": phase,
            "completed_work": cw,
            "remaining_work": rw,
            "artifact_references": art_refs,
            "safe_to_resume": safe_to_resume,
        }
        if last_committed_action is not None:
            params["last_committed_action"] = last_committed_action

        return self._call("progress", params)

    def _call(self, method: str, params: dict[str, Any]) -> dict[str, Any]:
        if self._rpc is None:
            raise RPCError("SDK RPC is not connected", "rpc_not_connected")
        result = self._rpc.call(method, params)
        if isinstance(result, dict):
            return result
        return {"result": result}

    # ---- MCP tool registration (B33-T02) -----------------------------------

    def mcp_tool(self, name: str):
        """Decorator that registers a callable as an MCP tool.

        Raises RPCError at registration time if the name is invalid,
        empty, or already registered.
        """
        import re

        if not isinstance(name, str) or not name:
            raise RPCError("tool name must be a non-empty string", "invalid_tool_name")

        if not re.match(r"^[a-zA-Z][a-zA-Z0-9_.-]*$", name):
            raise RPCError(
                f"invalid tool name {name!r} (must match [a-zA-Z][a-zA-Z0-9_.-]*)",
                "invalid_tool_name",
            )

        if name in self._mcp_tools:
            raise RPCError(
                f"duplicate tool registration for {name!r}",
                "duplicate_tool",
            )

        def decorator(fn: Callable[[dict[str, Any]], dict[str, Any]]):
            self._mcp_tools[name] = fn
            return fn

        return decorator

    def list_mcp_tools(self) -> list[str]:
        """Return sorted list of registered MCP tool names.

        Returns a copy, never the internal mutable dict keys view.
        """
        return sorted(self._mcp_tools.keys())

    def call_mcp_tool(self, name: str, arguments: Any) -> dict[str, Any]:
        """Dispatch an MCP tool call by name.

        Raises RPCError (tool_error) if the tool raises, with bounded
        error message (no traceback).

        Args:
            name: Registered tool name.
            arguments: Must be a JSON object (dict). Rejects list/str/None.

        Returns:
            The tool's return value as a dict.
        """
        import json as _json

        fn = self._mcp_tools.get(name)
        if fn is None:
            raise RPCError(
                f"tool {name!r} is not registered", "unknown_tool",
            )

        if not isinstance(arguments, dict):
            raise RPCError(
                "arguments must be a JSON object (dict), got " + type(arguments).__name__,
                "invalid_arguments",
            )

        # Reject reserved control fields in arguments.
        for key in arguments:
            if key.startswith("__agentpaas_"):
                raise RPCError(
                    f"arguments contain reserved key {key!r}", "reserved_key",
                )

        try:
            result = fn(arguments)
        except Exception as exc:
            # Map to bounded error without traceback leakage.
            msg = str(exc)
            # Redact common secret patterns.
            import re as _re
            msg = _re.sub(r'sk-[a-zA-Z0-9]{16,}', '[REDACTED]', msg)
            msg = _re.sub(r'[A-Za-z0-9+/]{40,}={0,2}', '[REDACTED]', msg)
            msg = _re.sub(r'AKIA[0-9A-Z]{16}', '[REDACTED]', msg)
            raise RPCError(f"tool {name!r} error: {msg}", "tool_error")

        if not isinstance(result, dict):
            raise RPCError(
                "tool result must be a dict, got " + type(result).__name__,
                "invalid_result",
            )

        # Validate JSON serializability.
        try:
            _json.dumps(result, separators=(",", ":"))
        except (TypeError, ValueError) as e:
            raise RPCError(
                f"tool result is not JSON-serializable: {e}", "invalid_result",
            )

        # Reject reserved control fields in result.
        for key in result:
            if key.startswith("__agentpaas_"):
                raise RPCError(
                    f"result contains reserved key {key!r}", "reserved_key",
                )
        _validate_response_no_leaks(result)

        return result

    def validate_declared_tools(self, declared: list[str]) -> str | None:
        """Validate exact set equality: every declared tool must be registered
        AND every registered tool must be declared.

        Returns None if valid, or an error message string describing the
        mismatch.
        """
        registered = set(self._mcp_tools.keys())
        declared_set = set(declared)

        missing = declared_set - registered
        if missing:
            return f"declared tools not registered: {sorted(missing)}"

        extra = registered - declared_set
        if extra:
            return f"registered tools not declared: {sorted(extra)}"

        return None

    def get_max_concurrency(self) -> int:
        """Return the declared max_concurrency from env, or 1.

        Capped to [1, 32].
        """
        import os as _os

        raw = _os.environ.get("AGENTPAAS_MCP_MAX_CONCURRENCY", "")
        if raw:
            try:
                val = int(raw)
                if val < 1:
                    return 1
                if val > 32:
                    return 32
                return val
            except ValueError:
                pass
        return 1


agent = Agent()