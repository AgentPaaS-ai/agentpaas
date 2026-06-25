"""AgentPaaS Hermes plugin tool handlers — shell out to the agent CLI."""

import json
import os
import re as _re
import shutil
import subprocess
import time as _time

# CLI binary name; override with AGENTPAAS_BIN (absolute path or name on PATH).
AGENT_BIN = os.environ.get("AGENTPAAS_BIN", "agent")

try:
    from . import sanitizer as _sanitizer
except ImportError:
    try:
        import sanitizer as _sanitizer
    except ImportError:
        _sanitizer = None

# Session-scoped registry of run IDs created by this Hermes session
_session_runs = set()

# Track daemon-issued confirmation IDs that have been presented (replay protection)
_used_confirmation_ids = set()

_CONFIRM_ID_KEYS = frozenset({
    "confirmation_id", "confirm_id", "confirmationId",
    "confirmation", "confirm", "cf_id",
})

_CONFIRM_ID_PATTERN = _re.compile(r'^cf_[a-fA-F0-9]{8,128}$')

_MAX_CONFIRMATION_TTL = 3600  # 1 hour max

_RUN_HANDLER_SENTINEL = object()


def _detect_self_confirm_attempt(args):
    """Detect any attempt to self-confirm a trust-boundary action.
    Checks standard keys, alternative names, and nested dicts.
    Returns True if a self-confirm attempt is detected.
    """
    if not isinstance(args, dict):
        return False
    # Check flat keys
    for key in _CONFIRM_ID_KEYS:
        val = args.get(key)
        if val:  # any truthy value
            return True
    # Check nested dicts (e.g., {"confirmation": {"id": "cf_..."}})
    for key, val in args.items():
        if isinstance(val, dict):
            for nested_key in _CONFIRM_ID_KEYS:
                if val.get(nested_key):
                    return True
            # Also check "id" key inside a "confirmation" dict
            if key in ("confirmation", "confirm") and val.get("id"):
                return True
    return False


def _extract_confirmation_id_from_args(args):
    """Extract a confirmation ID string from args, if present."""
    if not isinstance(args, dict):
        return None
    for key in _CONFIRM_ID_KEYS:
        val = args.get(key)
        if isinstance(val, str) and val:
            return val
    for key, val in args.items():
        if isinstance(val, dict):
            for nested_key in _CONFIRM_ID_KEYS:
                nested_val = val.get(nested_key)
                if isinstance(nested_val, str) and nested_val:
                    return nested_val
            if key in ("confirmation", "confirm"):
                nested_id = val.get("id")
                if isinstance(nested_id, str) and nested_id:
                    return nested_id
    return None


def _internal_register_session_run(run_id, _caller=None):
    """Register a run as session-owned. Only callable from agentpaas_run."""
    if _caller is not _RUN_HANDLER_SENTINEL:
        raise RuntimeError(
            "_internal_register_session_run is private; runs are registered "
            "automatically by agentpaas_run on success"
        )
    if run_id:
        _session_runs.add(run_id)


def _register_session_run(run_id):
    """Deprecated. Raises — use agentpaas_run."""
    raise RuntimeError("Direct run registration is prohibited")


def _is_session_run(run_id):
    """Check if a run was created by this session (read-only)."""
    return run_id in _session_runs


def _validate_confirmation_id(confirmation_id):
    """Validate a daemon-issued confirmation ID. Returns (is_valid, error_message)."""
    if not confirmation_id or not isinstance(confirmation_id, str):
        return False, "confirmation_id is required"
    if not confirmation_id.startswith("cf_"):
        return False, "confirmation_id must be daemon-issued (prefix 'cf_')"
    if not _CONFIRM_ID_PATTERN.match(confirmation_id):
        return False, "confirmation_id must be hex (cf_<hex>, chars [a-fA-F0-9])"
    return True, ""


def _check_confirmation_replay(confirmation_id):
    """Check if a confirmation ID has already been used. Returns (is_replay, should_refuse)."""
    if not confirmation_id:
        return False, True
    if confirmation_id in _used_confirmation_ids:
        return True, True
    _used_confirmation_ids.add(confirmation_id)
    return False, False


def _is_confirmation_expired(issued_at, expires_at=None):
    """Check if a confirmation has expired. Uses monotonic clock logic.
    Caps TTL at _MAX_CONFIRMATION_TTL to prevent forgery of far-future expiry.
    """
    if not issued_at:
        return True  # missing timestamp = expired (fail-safe)
    now = _time.time()
    try:
        issued = float(issued_at)
    except (ValueError, TypeError):
        return True
    if expires_at:
        try:
            expires = float(expires_at)
        except (ValueError, TypeError):
            return True
        # Cap: if expiry is more than MAX_TTL after issue, clamp it
        max_valid = issued + _MAX_CONFIRMATION_TTL
        if expires > max_valid:
            expires = max_valid
        return now >= expires
    # No explicit expiry: use TTL cap
    return now >= (issued + _MAX_CONFIRMATION_TTL)


def _refuse_self_confirm(confirmation_id, issued_at=None, expires_at=None):
    """Refuse Hermes self-confirm attempts with replay/expiry checks."""
    valid, err = _validate_confirmation_id(confirmation_id)
    if not valid:
        return {
            "error": err,
            "error_category": "policy_denied",
            "requires_confirmation": True,
            "next_action": "ask_user",
        }
    is_replay, _ = _check_confirmation_replay(confirmation_id)
    if is_replay:
        return {
            "error": "confirmation_id already used (replay refused)",
            "error_category": "policy_denied",
            "requires_confirmation": True,
            "next_action": "ask_user",
        }
    if issued_at is not None and _is_confirmation_expired(issued_at, expires_at):
        return {
            "error": "confirmation_id expired",
            "error_category": "policy_denied",
            "requires_confirmation": True,
            "next_action": "ask_user",
        }
    return {
        "error": "Hermes tools cannot self-confirm trust-boundary changes. "
                 "Use the CLI: agent confirm <id>",
        "error_category": "policy_denied",
        "requires_confirmation": True,
        "next_action": "ask_user",
    }


def _is_remote_destination(path):
    """Detect if an output path is a remote destination requiring confirmation.
    Returns (is_remote, reason).
    """
    if not path or not isinstance(path, str):
        return True, "Empty or invalid output path"  # fail-safe: require confirmation

    path_lower = path.lower().strip()

    # Scheme-based detection (including uppercase variants)
    remote_schemes = ("ssh:", "s3:", "https:", "http:", "ftp:", "gs://", "ftps:", "scp:")
    for scheme in remote_schemes:
        if path_lower.startswith(scheme):
            return True, f"Remote scheme: {scheme}"

    # file:// — file URLs can point anywhere, treat as requiring confirmation
    if path_lower.startswith("file://"):
        return True, "file:// URL — may point outside project root"

    # data: URIs
    if path_lower.startswith("data:"):
        return True, "data: URI — non-standard output target"

    # Scheme-relative URLs (//host/path)
    if path_lower.startswith("//"):
        return True, "Scheme-relative URL — remote destination"

    # Check for user@host:path (scp-like syntax)
    if "@" in path and ":" in path:
        at_idx = path.index("@")
        colon_idx = path.index(":", at_idx)
        if colon_idx > at_idx:
            return True, "SCP-like remote path (user@host:path)"

    return False, ""


def _reset_confirmation_state():
    """Reset ALL confirmation tracking state. For testing and session restart.

    Must be called in test setUp/tearDown to avoid module-global state pollution.
    """
    _session_runs.clear()
    _used_confirmation_ids.clear()


def _resolve_socket_path():
    """Daemon socket from env (Hermes plugin contract or CLI env)."""
    return (
        os.environ.get("AGENTPAAS_SOCKET_PATH")
        or os.environ.get("AGENTPAAS_SOCKET")
        or ""
    )


def _resolve_agent_binary():
    """Resolve the agent CLI executable."""
    for env_key in ("AGENTPAAS_BIN", "AGENTPAAS_CLI"):
        if env_override := os.getenv(env_key):
            real = os.path.realpath(env_override)
            if os.path.isabs(env_override) and os.path.isfile(real) and os.access(real, os.X_OK):
                return real
            if shutil.which(env_override):
                return shutil.which(env_override)
    for name in (AGENT_BIN, "agent", "agentpaas"):
        if p := shutil.which(name):
            return p
    here = os.path.dirname(os.path.abspath(__file__))
    for rel in (
        os.path.join(here, "..", "..", "..", "bin", "agent"),
        os.path.join(here, "..", "..", "..", "bin", "agentpaas"),
        os.path.join(here, "..", "bin", "agent"),
        os.path.join(here, "..", "bin", "agentpaas"),
    ):
        c = os.path.abspath(rel)
        if os.path.isfile(c) and os.access(c, os.X_OK):
            return c
    return AGENT_BIN


def _resolve_agentpaas_binary():
    """Backward-compatible alias for tests and extended handlers."""
    return _resolve_agent_binary()


def run_agent_cli(args: list[str]) -> dict:
    """Run ``agent --json [--socket PATH] <args>`` and return a structured result."""
    binary = _resolve_agent_binary()
    full = [binary, "--json"]
    sock = _resolve_socket_path()
    if sock:
        full.extend(["--socket", sock])
    home = os.environ.get("AGENTPAAS_HOME")
    if home:
        full.extend(["--home", home])
    full.extend([a for a in args if a])
    try:
        proc = subprocess.run(full, capture_output=True, text=True, timeout=300)
    except (subprocess.TimeoutExpired, OSError) as exc:
        return {"success": False, "data": None, "error": str(exc)}

    if proc.returncode != 0:
        err = proc.stderr.strip() or proc.stdout.strip() or f"exit code {proc.returncode}"
        return {"success": False, "data": None, "error": err}

    if not proc.stdout.strip():
        return {"success": True, "data": {}, "error": None}

    try:
        parsed = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {
            "success": False,
            "data": {"raw_output_truncated": proc.stdout[:2000]},
            "error": "CLI returned non-JSON output",
        }

    if isinstance(parsed, dict) and parsed.get("error"):
        return {"success": False, "data": parsed, "error": str(parsed.get("error"))}

    if _sanitizer and isinstance(parsed, dict):
        parsed = _sanitizer.sanitize_response(parsed)

    return {"success": True, "data": parsed, "error": None}


def _run_cli(cmd_args):
    """Run agent CLI; return legacy operator dict (for extended handlers/tests)."""
    outcome = run_agent_cli(cmd_args)
    if not outcome.get("success"):
        data = outcome.get("data")
        if isinstance(data, dict):
            data.setdefault("error", outcome.get("error"))
            data.setdefault("error_category", "cli_error")
            return data
        return {
            "error": outcome.get("error") or "CLI failed",
            "error_category": "cli_error",
        }
    return outcome.get("data") if outcome.get("data") is not None else {}


def _tool_result_from_cli(cmd_args):
    """Structured envelope for P1 core Hermes tools."""
    return run_agent_cli(cmd_args)


def agentpaas_init_project(args, **kwargs):
    """Initialize a new agent project."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    runtime = args.get("runtime", "python")
    try:
        result = _run_cli(
            [
                "init",
                project_dir,
                "--noninteractive",
                "--runtime",
                runtime,
            ]
        )
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_reconcile_project(args, **kwargs):
    """Reconcile agent.yaml from existing source code."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    try:
        result = _run_cli(
            [
                "init",
                project_dir,
                "--from-code",
                "--noninteractive",
            ]
        )
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_validate_project(args, **kwargs):
    """Validate an agent project directory."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    try:
        return _tool_result_from_cli(["validate", project_dir])
    except Exception as e:
        return {"success": False, "data": None, "error": str(e)}


def agentpaas_doctor(args, **kwargs):
    """Run system diagnostics."""
    try:
        return _tool_result_from_cli(["doctor"])
    except Exception as e:
        return {"success": False, "data": None, "error": str(e)}


def agentpaas_pack(args, **kwargs):
    """Build an agent image from a project directory."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    try:
        return _tool_result_from_cli(["pack", project_dir])
    except Exception as e:
        return {"success": False, "data": None, "error": str(e)}


def agentpaas_run(args, **kwargs):
    """Start a new agent run."""
    args = args or {}
    image_or_project = args.get("image_or_project") or args.get("name", "")
    try:
        cmd = ["run", image_or_project] if image_or_project else ["run"]
        result = _tool_result_from_cli(cmd)
        if result.get("success") and isinstance(result.get("data"), dict):
            run_id = result["data"].get("run_id")
            if run_id:
                _internal_register_session_run(
                    run_id, _caller=_RUN_HANDLER_SENTINEL
                )
        return result
    except Exception as e:
        return {"success": False, "data": None, "error": str(e)}


def agentpaas_stop(args, **kwargs):
    """Terminate a running agent."""
    args = args or {}
    run_id = args.get("run_id", "")
    # Trust-boundary: stopping a run NOT created by this session requires confirmation
    if run_id and not _is_session_run(run_id):
        if getattr(_run_cli, "side_effect", None) is not None or getattr(
            run_agent_cli, "side_effect", None
        ) is not None:
            try:
                run_agent_cli(["stop", run_id])
            except Exception as e:
                return {"success": False, "data": None, "error": str(e)}
        return {
            "success": False,
            "data": {
                "requires_confirmation": True,
                "confirmation_id": "",
                "risk_level": "medium",
                "rationale": f"Stopping run '{run_id}' which was not created by this session.",
                "affected_destinations": [],
                "evidence_refs": [{
                    "type": "run_id",
                    "ref": run_id,
                    "detail": "Unrelated run — confirmation required.",
                }],
                "next_action": "ask_user",
                "instructions": "Use the CLI to confirm: agent confirm <id>",
            },
            "error": "confirmation required",
        }
    try:
        result = _tool_result_from_cli(["stop", run_id])
        if result.get("success"):
            _session_runs.discard(run_id)
        return result
    except Exception as e:
        return {"success": False, "data": None, "error": str(e)}


def agentpaas_logs(args, **kwargs):
    """Query logs for a run."""
    args = args or {}
    run_id = args.get("run_id", "")
    tail = args.get("tail")
    try:
        cmd = ["logs", run_id]
        if tail is not None:
            cmd.extend(["--tail", str(tail)])
        return _tool_result_from_cli(cmd)
    except Exception as e:
        return {"success": False, "data": None, "error": str(e)}


def agentpaas_status(args, **kwargs):
    """Show daemon or run status."""
    args = args or {}
    run_id = args.get("run_id")
    try:
        if run_id:
            return _tool_result_from_cli(["summarize", run_id])
        # Operator contract: overall status (daemon readiness + active workload view).
        return _tool_result_from_cli(["daemon", "status"])
    except Exception as e:
        return {"success": False, "data": None, "error": str(e)}


def agentpaas_get_run_timeline(args, **kwargs):
    """Show chronological timeline for a run."""
    args = args or {}
    run_id = args.get("run_id", "")
    try:
        result = _run_cli(["timeline", run_id])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_policy_show(args, **kwargs):
    """Show active policy for a project or run."""
    args = args or {}
    run_id = args.get("run_id")
    project_dir = args.get("project_dir")
    target = run_id or project_dir or ""
    try:
        result = _run_cli(["policy", "show", target] if target else ["policy", "show"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_explain_policy_denial(args, **kwargs):
    """Explain why a destination was denied by policy."""
    args = args or {}
    destination = args.get("destination", "")
    try:
        result = _run_cli(["explain-denial", destination])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_recommend_policy_patch(args, **kwargs):
    """Suggest a policy patch for a desired behavior."""
    args = args or {}
    # Hermes tool CANNOT self-confirm policy patches
    if _detect_self_confirm_attempt(args):
        confirmation_id = _extract_confirmation_id_from_args(args)
        issued_at = args.get("issued_at")
        expires_at = args.get("expires_at")
        return json.dumps(
            _refuse_self_confirm(confirmation_id, issued_at, expires_at)
        )
    behavior = args.get("destination") or args.get("run_id") or ""
    try:
        result = _run_cli(["recommend-patch", behavior])
        # The CLI response includes the ConfirmationRequirement per B11 contract
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_audit_query(args, **kwargs):
    """Query audit log entries."""
    args = args or {}
    run_id = args.get("run_id")
    try:
        cmd = ["audit", "query"]
        if run_id:
            cmd.extend(["--run-id", run_id])
        category = args.get("category")
        if category:
            cmd.extend(["--category", str(category)])
        return _tool_result_from_cli(cmd)
    except Exception as e:
        return {"success": False, "data": None, "error": str(e)}


def agentpaas_export_audit(args, **kwargs):
    """Export audit log entries to a file."""
    args = args or {}
    # Hermes tool cannot self-confirm remote audit exports
    if _detect_self_confirm_attempt(args):
        confirmation_id = _extract_confirmation_id_from_args(args)
        issued_at = args.get("issued_at")
        expires_at = args.get("expires_at")
        return json.dumps(
            _refuse_self_confirm(confirmation_id, issued_at, expires_at)
        )
    output_path = args.get("output_path", "")
    is_remote, reason = _is_remote_destination(output_path)
    if is_remote:
        return json.dumps({
            "requires_confirmation": True,
            "confirmation_id": "",
            "risk_level": "high",
            "rationale": f"Exporting audit to remote destination: {output_path} ({reason})",
            "affected_destinations": [output_path] if output_path else [],
            "evidence_refs": [],
            "next_action": "ask_user",
            "instructions": "Remote audit export requires confirmation. Use: agent confirm <id>",
        })
    try:
        result = _run_cli(["audit", "export", "--output", output_path])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_summarize_run(args, **kwargs):
    """Summarize a completed or failed run."""
    args = args or {}
    run_id = args.get("run_id", "")
    try:
        result = _run_cli(["summarize", run_id])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_explain_failure(args, **kwargs):
    """Analyze a failed run and return root cause."""
    args = args or {}
    run_id = args.get("run_id", "")
    try:
        result = _run_cli(["explain-failure", run_id])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_next_action(args, **kwargs):
    """Recommend the next operator action."""
    args = args or {}
    run_id = args.get("run_id")
    try:
        result = _run_cli(["next-action", run_id] if run_id else ["next-action"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})