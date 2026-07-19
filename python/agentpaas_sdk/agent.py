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

InvokeHandler = Callable[[dict[str, Any]], dict[str, Any]]

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

        Exactly one of ``prompt`` or ``messages`` must be supplied. Yields
        versioned events. If the connected harness does not support streaming,
        raises :class:`StreamingNotSupported`.

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
        # The streaming transport is introduced in T04; until the harness
        # advertises streaming support, fail closed with a typed error.
        raise StreamingNotSupported(
            "connected harness does not support streaming",
            "streaming_not_supported",
        )

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


agent = Agent()
