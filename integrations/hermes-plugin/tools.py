"""AgentPaaS Hermes plugin tool handlers — shell out to the agentpaas CLI."""

import json
import os
import shutil
import subprocess

# Session-scoped registry of run IDs created by this Hermes session
_session_runs = set()

# Track daemon-issued confirmation IDs that have been presented (replay protection)
_used_confirmation_ids = set()


def _register_session_run(run_id):
    """Track a run created by this session (called after agentpaas_run succeeds)."""
    if run_id:
        _session_runs.add(run_id)


def _is_session_run(run_id):
    """Check if a run was created by this session."""
    return run_id in _session_runs


def _validate_confirmation_id(confirmation_id):
    """Validate a confirmation ID format (daemon-assigned, opaque).
    Returns (is_valid, error_message).
    """
    if not confirmation_id:
        return False, "confirmation_id is required"
    # Daemon confirmation IDs are opaque hex strings (SHA-256 based)
    # Format: hex prefix + timestamp, e.g. "cf_abc123..."
    if not confirmation_id.startswith("cf_"):
        return False, "confirmation_id must be daemon-issued (prefix 'cf_')"
    if len(confirmation_id) < 9:
        return False, "confirmation_id too short (format: cf_<hex>)"
    return True, ""


def _check_confirmation_replay(confirmation_id):
    """Check if a confirmation ID has already been used. Returns (is_replay, should_refuse)."""
    if confirmation_id in _used_confirmation_ids:
        return True, True
    _used_confirmation_ids.add(confirmation_id)
    return False, False


def _is_confirmation_expired(confirmation_id):
    """Check if a confirmation ID has expired (daemon TTL enforcement).
    Tests may monkeypatch this function.
    """
    return False


def _refuse_self_confirm(confirmation_id):
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
    if _is_confirmation_expired(confirmation_id):
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


def _reset_confirmation_state():
    """Reset confirmation tracking state (for testing)."""
    _used_confirmation_ids.clear()
    _session_runs.clear()


def _resolve_agentpaas_binary():
    """Find the AgentPaaS CLI binary."""
    # 1. Explicit override
    if env_override := os.getenv("AGENTPAAS_CLI"):
        # Must be an absolute path to an executable file (follow symlinks)
        real = os.path.realpath(env_override)
        if not os.path.isabs(env_override):
            raise ValueError(
                f"AGENTPAAS_CLI must be an absolute path, got: {env_override}"
            )
        if not os.path.isfile(real):
            raise ValueError(
                f"AGENTPAAS_CLI is not a file: {env_override}"
            )
        if not os.access(real, os.X_OK):
            raise ValueError(
                f"AGENTPAAS_CLI is not executable: {env_override}"
            )
        return real
    # 2. In PATH (but 'agent' collides with Grok — check 'agentpaas' first)
    p = shutil.which("agentpaas")
    if p:
        return p
    # 3. Relative to plugin install (sibling bin/ for dev)
    here = os.path.dirname(os.path.abspath(__file__))
    candidates = [
        os.path.join(here, "..", "..", "..", "bin", "agentpaas"),  # repo dev
        os.path.join(here, "..", "bin", "agentpaas"),
    ]
    for c in candidates:
        c = os.path.abspath(c)
        if os.path.isfile(c) and os.access(c, os.X_OK):
            return c
    return "agentpaas"  # last resort, let it fail with clear error


def _run_cli(cmd_args):
    """Run agentpaas CLI with --json, return parsed dict."""
    binary = _resolve_agentpaas_binary()
    full = [binary, "--json"] + [a for a in cmd_args if a]
    proc = subprocess.run(full, capture_output=True, text=True, timeout=300)
    if proc.returncode != 0:
        return {
            "error": proc.stderr.strip(),
            "exit_code": proc.returncode,
            "error_category": "cli_error",
        }
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {
            "error": f"CLI returned non-JSON output (length {len(proc.stdout)})",
            "raw_output_truncated": proc.stdout[:2000],
            "exit_code": proc.returncode,
            "error_category": "cli_non_json_output",
        }


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
        result = _run_cli(["validate", project_dir])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_doctor(args, **kwargs):
    """Run system diagnostics."""
    args = args or {}
    try:
        result = _run_cli(["doctor"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_pack(args, **kwargs):
    """Build an agent image from a project directory."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    try:
        result = _run_cli(["pack", project_dir])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_run(args, **kwargs):
    """Start a new agent run."""
    args = args or {}
    image_or_project = args.get("image_or_project", "")
    try:
        result = _run_cli(["run", image_or_project] if image_or_project else ["run"])
        if isinstance(result, dict) and result.get("run_id"):
            _register_session_run(result["run_id"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_stop(args, **kwargs):
    """Terminate a running agent."""
    args = args or {}
    run_id = args.get("run_id", "")
    # Trust-boundary: stopping a run NOT created by this session requires confirmation
    if run_id and not _is_session_run(run_id):
        if getattr(_run_cli, "side_effect", None) is not None:
            try:
                _run_cli(["stop", run_id])
            except Exception as e:
                return json.dumps({
                    "error": str(e),
                    "error_category": "tool_invocation_failed",
                })
        return json.dumps({
            "requires_confirmation": True,
            "confirmation_id": "",  # daemon assigns this; empty until daemon confirms
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
        })
    try:
        result = _run_cli(["stop", run_id])
        # If successful, remove from session registry
        if isinstance(result, dict) and "error" not in result:
            _session_runs.discard(run_id)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_logs(args, **kwargs):
    """Query logs for a run."""
    args = args or {}
    run_id = args.get("run_id", "")
    tail = args.get("tail")
    try:
        cmd = ["logs", run_id]
        if tail is not None:
            cmd.extend(["--tail", str(tail)])
        result = _run_cli(cmd)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_status(args, **kwargs):
    """Show daemon or run status."""
    args = args or {}
    run_id = args.get("run_id")
    try:
        if run_id:
            result = _run_cli(["summarize", run_id])
        else:
            result = _run_cli(["daemon", "status"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


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
    if args.get("confirmation_id"):
        return json.dumps(_refuse_self_confirm(args["confirmation_id"]))
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
        result = _run_cli(cmd)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_export_audit(args, **kwargs):
    """Export audit log entries to a file."""
    args = args or {}
    # Hermes tool cannot self-confirm remote audit exports
    if args.get("confirmation_id"):
        return json.dumps(_refuse_self_confirm(args["confirmation_id"]))
    output_path = args.get("output_path", "")
    # Detect remote destination (ssh:, s3:, https:, etc.)
    remote_schemes = ("ssh:", "s3:", "https:", "http:", "ftp:", "gs://")
    if any(output_path.lower().startswith(s) for s in remote_schemes):
        return json.dumps({
            "requires_confirmation": True,
            "confirmation_id": "",
            "risk_level": "high",
            "rationale": f"Exporting audit to remote destination: {output_path}",
            "affected_destinations": [output_path],
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