"""AgentPaaS Hermes plugin — registers 30 operator-contract tools + slash commands."""
import json
import logging
import os
from pathlib import Path
from . import schemas, tools

logger = logging.getLogger(__name__)


_POINTER_NAME = "agentpaas-build"  # deliberately != "agentpaas-deploy" (slash cmd collision)

# Marker for SOUL upsert (must stay stable).
_SOUL_MARK_BEGIN = "# AgentPaaS Onboarding Rule"
_SOUL_MARK_END = "# End AgentPaaS Rules"


def _full_skill_content() -> str:
    """Return the full deploy SKILL.md with frontmatter for the local skill index.

    Bug 017: skill_view("agentpaas:deploy") fails on fresh plugin installs
    (plugin skill registry not always available). Writing the FULL skill into
    the profile skills index as agentpaas-build makes onboarding loadable via
    the normal available_skills path.
    """
    skill_path = Path(__file__).resolve().parent / "SKILL.md"
    body = skill_path.read_text(encoding="utf-8") if skill_path.is_file() else ""
    # Strip a leading H1 if present; frontmatter provides the title.
    if body.startswith("# "):
        body = body.split("\n", 1)[1] if "\n" in body else ""
    return (
        f"---\n"
        f"name: {_POINTER_NAME}\n"
        f"description: >\n"
        f"  Build, deploy, package, run, and govern AI agents. Load this skill\n"
        f"  BEFORE writing agent code. Contains mandatory onboarding (LLM provider,\n"
        f"  model, secret add, hostname confirm — no ports in chat), @agent.on_invoke\n"
        f"  pattern, pack/run, and anti-fabrication rules.\n"
        f"---\n\n"
        f"# AgentPaaS Deploy\n\n"
        f"**MANDATORY:** Follow Build-Time Onboarding below BEFORE packing.\n"
        f"Ask one short question at a time. Never show ports to the user.\n"
        f"Never copy demo agents without completing onboarding with the user.\n\n"
        f"{body}"
    )


def _soul_snippet() -> str:
    return f"""
{_SOUL_MARK_BEGIN}

CRITICAL: When the user asks to build, create, deploy, pack, run, or
govern any agent, you MUST load the full skill FIRST by calling
skill_view(name="{_POINTER_NAME}") BEFORE writing any code or calling
any agentpaas tools. That skill contains onboarding (LLM provider/model,
secret add in the user terminal, hostname confirm without ports),
@agent.on_invoke requirements, and pack gates.

Do NOT skip onboarding. Do NOT pre-fill openrouter/model/secrets without
asking. Do NOT copy demo/weather-agent as a substitute for onboarding.
Do NOT use wildcard egress without the user explicitly requesting it.

BEFORE packing any agent, you MUST verify:
1. Egress policy lists every external hostname (ports are write-time only).
2. Every credential is stored in Keychain (user ran `agentpaas secret add <name>`).
3. If the agent uses an LLM: agent.yaml has llm:, secret exists (secret_list),
   and the LLM provider hostname is in the egress policy.
If any are missing, do NOT pack — ask the user to resolve first.

NEVER call agentpaas_secret_add with the key value as a tool parameter.
The user MUST run `agentpaas secret add <name>` in their terminal and
paste the key via stdin. Hermes only verifies via agentpaas_secret_list.

# AgentPaaS Anti-Fabrication Rule

NEVER fabricate agent output. If agentpaas_run, agentpaas_trigger_invoke,
or any agentpaas tool returns an error, empty response, or a result you
don't understand, report the error honestly. Do NOT invent weather or
API values. After invoke, call agentpaas_status and read the real
invoke_response. If result.status is ERROR, report failure — do not
scrape numbers from an error body and claim success. Weather+LLM agents
need egress_allowed for BOTH the weather host AND the LLM provider.

{_SOUL_MARK_END}
"""


def _upsert_soul(soul_path: Path, snippet: str) -> None:
    """Insert or replace the AgentPaaS SOUL block (force-update on every register)."""
    existing = soul_path.read_text(encoding="utf-8") if soul_path.exists() else ""
    begin = _SOUL_MARK_BEGIN
    end = _SOUL_MARK_END
    if begin in existing:
        # Replace from begin through end marker (inclusive).
        pre, rest = existing.split(begin, 1)
        if end in rest:
            _, post = rest.split(end, 1)
            new = pre.rstrip() + "\n" + snippet.strip() + "\n" + post.lstrip("\n")
        else:
            # Old snippet without end marker: drop from begin to EOF, re-append.
            new = pre.rstrip() + "\n" + snippet.strip() + "\n"
    else:
        if existing and not existing.endswith("\n"):
            existing += "\n"
        new = existing + snippet
    soul_path.write_text(new if new.endswith("\n") else new + "\n", encoding="utf-8")


def _ensure_local_skill_pointer():
    """Install full deploy skill into the profile skill index + upsert SOUL.md.

    Bug 017 root cause: a tiny pointer told the agent to skill_view
    ("agentpaas:deploy"), which fails on fresh installs; the agent then
    skipped onboarding and copied the demo weather agent with pre-filled
    OpenRouter config and no secret. Writing the full SKILL.md as
    agentpaas-build makes onboarding loadable from available_skills.
    """
    try:
        from hermes_constants import get_hermes_home
        profile_dir = get_hermes_home()

        from hermes_cli.profiles import get_active_profile_name
        profile = get_active_profile_name()
        if profile not in ("default", "custom"):
            if profile_dir.name != profile:
                profile_dir = profile_dir / "profiles" / profile

        # 1. Full skill body (not a broken pointer to agentpaas:deploy)
        skills_dir = profile_dir / "skills" / "agentpaas"
        skills_dir.mkdir(parents=True, exist_ok=True)
        (skills_dir / "SKILL.md").write_text(_full_skill_content(), encoding="utf-8")

        # 2. Force-upsert SOUL rules every register (not append-only)
        _upsert_soul(profile_dir / "SOUL.md", _soul_snippet())

        logger.info(
            "AgentPaaS full skill + SOUL.md installed in profile %s (%s)",
            profile,
            profile_dir,
        )
    except Exception as e:
        logger.warning("Could not create skill pointer: %s", e)


def _ensure_toolset():
    """Ensure 'agentpaas' is in platform_toolsets.cli for the active profile.

    Called from register() at session load. If the toolset is missing,
    runs ensure-toolset.py to add it to config.yaml. This is needed
    because `hermes plugins install --enable` does NOT add the toolset
    automatically — without it, the agent can see slash commands but
    cannot call agentpaas_* tools. Doing it here (instead of relying
    on the agent to run after-install.md) makes the first-session
    experience work without the agent having to read after-install.md.
    """
    try:
        import subprocess, os, sys
        profile_dir = os.environ.get("HERMES_HOME", "")
        if not profile_dir:
            return
        # Determine profile name from the path
        profile_name = os.path.basename(profile_dir.rstrip("/"))
        script = os.path.join(os.path.dirname(__file__), "scripts", "ensure-toolset.py")
        if not os.path.exists(script):
            # Fallback: look in repo root
            script = os.path.join(os.path.dirname(__file__), "..", "..", "scripts", "ensure-toolset.py")
            script = os.path.normpath(script)
        if os.path.exists(script):
            subprocess.run(
                [sys.executable, script, profile_name],
                capture_output=True, timeout=10, text=True,
            )
            logger.debug("ensure-toolset.py ran for profile %s", profile_name)
    except Exception as e:
        logger.warning("Could not ensure toolset: %s", e)


def _ensure_daemon_running():
    """Best-effort: start the AgentPaaS daemon if its socket is missing.

    Called from register() at session load. If the daemon socket doesn't
    exist, spawn `agentpaas daemon start` as a detached background process.
    This removes the UX friction where a user who just installed the plugin
    and restarted has to separately discover and run the daemon-start command.

    Handles three known recurring pitfalls:
    1. Checkpoint key corruption after a binary upgrade — removes the stale
       key so the daemon generates a fresh one.
    2. Stale socket file from an unclean shutdown (e.g. pkill -9, kernel
       OOM) — removes it so the daemon can bind.
    3. Socket file exists but daemon is DEAD (the file was never cleaned up
       by the killed process). A mere os.path.exists() check is NOT enough
       — it returns True for a stale socket, so the function returns early
       and the daemon stays down. We must verify liveness by actually
       connecting to the socket.

    Then waits up to 15s for the socket to appear before returning.

    Failures are logged at debug level and never raised — plugin registration
    must not fail because the daemon is down. The agent can still call
    agentpaas_doctor to diagnose, and the hint in error messages guides
    manual start.
    """
    try:
        import os
        import socket
        import subprocess
        import time

        # Resolve the socket path the same way tools.py does.
        socket_path = os.environ.get("AGENTPAAS_SOCKET_PATH")
        if not socket_path:
            socket_path = os.path.expanduser("~/.agentpaas/daemon.sock")

        def _socket_is_live(path):
            """Return True iff a Unix socket at path accepts a connection."""
            if not os.path.exists(path):
                return False
            try:
                s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                s.settimeout(0.5)
                s.connect(path)
                s.close()
                return True
            except OSError:
                return False

        # Fast path: socket exists AND is live → daemon is already running.
        if _socket_is_live(socket_path):
            return

        # Pre-flight cleanup: stale checkpoint key and stale socket are the
        # two reasons the daemon crashes immediately after spawn. The
        # checkpoint key format changes between versions, leaving a stale
        # key the new binary can't decrypt. Removing it lets the daemon
        # generate a fresh one. (Recurring pitfall — see SKILL.md.)
        state_dir = os.path.expanduser("~/.agentpaas/state")
        stale_key = os.path.join(state_dir, "audit-checkpoint-key.der")
        if os.path.exists(stale_key):
            try:
                os.remove(stale_key)
            except OSError:
                pass
        if os.path.exists(socket_path):
            try:
                os.remove(socket_path)
            except OSError:
                pass

        # Find the agentpaas binary. Prefer the repo binary if present,
        # fall back to PATH resolution.
        binary = None
        candidates = [
            "/usr/local/bin/agentpaas",
            os.path.expanduser("~/projects/agentpaas/bin/agentpaas"),
        ]
        for c in candidates:
            if os.path.isfile(c) and os.access(c, os.X_OK):
                binary = c
                break
        if not binary:
            binary = "agentpaas"  # rely on PATH

        # Spawn detached. start_new_session=True (POSIX) detaches into a
        # new session so the daemon survives this process exiting. Output
        # goes to the daemon's own log file, not our pipes.
        subprocess.Popen(
            [binary, "daemon", "start"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            stdin=subprocess.DEVNULL,
            start_new_session=True,
        )

        # Wait for the socket to be live (up to 15s). The daemon needs time
        # to generate its checkpoint key, bind the socket, and become ready.
        # We check liveness (connect), not just file existence — a crash-
        # looping daemon can leave a stale socket file. If it doesn't come
        # up, we don't raise; the agent can still call agentpaas_doctor
        # to diagnose.
        for _ in range(30):
            time.sleep(0.5)
            if _socket_is_live(socket_path):
                logger.debug("AgentPaaS daemon auto-started (socket ready)")
                return
        logger.debug("AgentPaaS daemon auto-started but socket not live after 15s")
    except Exception as e:
        logger.debug("AgentPaaS daemon auto-start skipped: %s", e)


# ---------------------------------------------------------------------------
# Slash command handlers
#
# Each handler is a thin orchestrator over the plugin's own tool functions.
# Handler signature: fn(raw_args: str) -> str | None
# ---------------------------------------------------------------------------

def _format_error(result, error_prefix="Command failed"):
    """Format an error dict from a tool result into a human-readable string.

    Surfaces the ``hint`` field (e.g. "Run: agentpaas daemon start") when
    the tool provides one, so the user knows how to fix the problem instead
    of seeing a bare error message.
    """
    if not isinstance(result, dict) or "error" not in result:
        return None
    msg = f"{error_prefix}: {result['error']}"
    hint = result.get("hint")
    if hint:
        msg += f"\nHint: {hint}"
    return msg


def _parse_result(result_json, error_prefix="Command failed"):
    """Parse a tool result JSON string and return a human-readable summary."""
    try:
        result = json.loads(result_json)
    except (json.JSONDecodeError, TypeError):
        return str(result_json)
    if isinstance(result, dict) and "error" in result:
        return _format_error(result, error_prefix)
    return result


def _cmd_deploy(args_str, ctx=None):
    """`/agentpaas-deploy <path>` — pack → run, return run_id."""
    path = (args_str or "").strip()
    if not path:
        return "Usage: /agentpaas-deploy <project_path>"
    pack_result = json.loads(tools.agentpaas_pack({"project_dir": path}))
    err = _format_error(pack_result, "Pack failed")
    if err:
        return err
    agent_name = pack_result.get("agent_name", "")
    image_digest = pack_result.get("image_digest", "")
    target = image_digest or agent_name or path
    run_result = json.loads(tools.agentpaas_run({"image_or_project": target}))
    err = _format_error(run_result, "Run failed")
    if err:
        return err
    run_id = run_result.get("run_id", "?")
    return f"Deployed {agent_name or path}: run_id={run_id}"


def _cmd_status(args_str, ctx=None):
    """`/agentpaas-status` — show daemon status and active runs."""
    result = json.loads(tools.agentpaas_status({}))
    err = _format_error(result, "Status failed")
    if err:
        return err
    # daemon status returns a flat dict (no "runs" key)
    lines = []
    if isinstance(result, dict):
        if result.get("ready") is not None:
            lines.append(f"Daemon: {'ready' if result.get('ready') else 'not ready'}")
            if result.get("daemon_version"):
                lines.append(f"  Version: {result['daemon_version']}")
            if result.get("os_arch"):
                lines.append(f"  Platform: {result['os_arch']}")
        # If called with a run_id, result is a run summary dict
        if result.get("run_id"):
            lines.append(f"Run: {result.get('run_id')}: {result.get('agent_name','?')} — {result.get('status','?')}")
    if not lines:
        return json.dumps(result, indent=2)
    return "\n".join(lines)


def _cmd_list(args_str, ctx=None):
    """`/agentpaas-list` — list AgentPaaS runs, split by status.

    Shows a "Running" section (active containers) and a "Recent" section
    (completed/failed history, last 10), so the user can tell at a glance
    what's active vs done.
    """
    result = json.loads(tools.agentpaas_list_runs({}))
    err = _format_error(result, "List failed")
    if err:
        return err
    runs = result.get("runs", []) if isinstance(result, dict) else (result if isinstance(result, list) else [])
    if not runs:
        return "No AgentPaaS runs found."

    def _status(r):
        return str(r.get("status", "?")).lower() if isinstance(r, dict) else ""

    def _fmt(r):
        if isinstance(r, dict):
            run_id = r.get("run_id", r.get("id", "?"))
            agent = r.get("agent_name", "?")
            status = r.get("status", "?")
            ts = r.get("started_at", "")
            if hasattr(ts, "get"):
                ts = ""  # protobuf timestamp — skip for compact display
            line = f"  {run_id}: {agent} — {status}"
            if ts:
                line += f"  ({ts})"
            return line
        return f"  {r}"

    running = [r for r in runs if _status(r) in ("running", "pending", "RUN_STATUS_RUNNING", "RUN_STATUS_PENDING")]
    recent = [r for r in runs if _status(r) not in ("running", "pending", "RUN_STATUS_RUNNING", "RUN_STATUS_PENDING")]
    recent = recent[:10]  # cap history

    lines = []
    if running:
        lines.append(f"=== Running ({len(running)}) ===")
        lines.extend(_fmt(r) for r in running)
    if recent:
        if lines:
            lines.append("")
        lines.append(f"=== Recent ({len(recent)} shown, {len(runs) - len(running)} total completed) ===")
        lines.extend(_fmt(r) for r in recent)
    if not lines:
        return "No AgentPaaS runs found."
    return "\n".join(lines)


def _cmd_logs(args_str, ctx=None):
    """`/agentpaas-logs <run_id>` — tail logs for a run."""
    run_id = (args_str or "").strip()
    if not run_id:
        return "Usage: /agentpaas-logs <run_id>"
    result = json.loads(tools.agentpaas_logs({"run_id": run_id, "tail": 50}))
    err = _format_error(result, "Logs failed")
    if err:
        return err
    return result.get("logs", "(no output)") if isinstance(result, dict) else str(result)


def _cmd_audit(args_str, ctx=None):
    """`/agentpaas-audit [run_id]` — show audit events."""
    run_id = (args_str or "").strip()
    result = json.loads(tools.agentpaas_audit_query({"run_id": run_id} if run_id else {}))
    err = _format_error(result, "Audit query failed")
    if err:
        return err
    entries = result.get("entries", []) if isinstance(result, dict) else (result if isinstance(result, list) else [])
    if not entries:
        return "No audit events."
    lines = [f"Audit ({len(entries)} events):"]
    for e in entries[-10:]:
        if isinstance(e, dict):
            lines.append(f"  [{e.get('event_type','?')}] {e.get('timestamp','?')}")
        else:
            lines.append(f"  {e}")
    return "\n".join(lines)


def _cmd_doctor(args_str, ctx=None):
    """`/agentpaas-doctor` — run system diagnostics."""
    result = json.loads(tools.agentpaas_doctor({}))
    err = _format_error(result, "Doctor failed")
    if err:
        return err
    checks = result.get("checks", []) if isinstance(result, dict) else []
    if not checks:
        return json.dumps(result, indent=2)
    lines = ["Diagnostics:"]
    for c in checks:
        name = c.get("name", "?")
        status = c.get("status", "?")
        msg = c.get("message", "")
        mark = "✓" if status == "ok" else "✗"
        line = f"  {mark} {name}: {status}"
        if msg:
            line += f" ({msg})"
        lines.append(line)
    overall = result.get("overall_status", "")
    if overall:
        lines.append(f"Overall: {overall}")
    return "\n".join(lines)


def _cmd_stop(args_str, ctx=None):
    """`/agentpaas-stop <run_id>` — stop a running agent."""
    run_id = (args_str or "").strip()
    if not run_id:
        return "Usage: /agentpaas-stop <run_id>"
    result = json.loads(tools.agentpaas_stop({"run_id": run_id}))
    if isinstance(result, dict) and result.get("requires_confirmation"):
        return (f"Stopping run '{run_id}' requires confirmation (not created by "
                f"this session).\nUse the CLI: agent confirm <id>")
    err = _format_error(result, "Stop failed")
    if err:
        return err
    return f"Stopped run {run_id}."


def _cmd_timeline(args_str, ctx=None):
    """`/agentpaas-timeline <run_id>` — show chronological timeline for a run."""
    run_id = (args_str or "").strip()
    if not run_id:
        return "Usage: /agentpaas-timeline <run_id>"
    result = json.loads(tools.agentpaas_get_run_timeline({"run_id": run_id}))
    err = _format_error(result, "Timeline failed")
    if err:
        return err
    events = result.get("events", []) if isinstance(result, dict) else (result if isinstance(result, list) else [])
    if not events:
        return f"No timeline events for run {run_id}."
    lines = [f"Timeline for {run_id} ({len(events)} events):"]
    for e in events[-15:]:
        if isinstance(e, dict):
            ts = e.get("timestamp", "?")
            etype = e.get("event_type", "?")
            detail = e.get("detail", e.get("message", ""))
            line = f"  [{ts}] {etype}"
            if detail:
                line += f" — {detail}"
            lines.append(line)
        else:
            lines.append(f"  {e}")
    return "\n".join(lines)


def _cmd_init(args_str, ctx=None):
    """`/agentpaas-init <path>` — create a new agent project scaffold."""
    path = (args_str or "").strip()
    if not path:
        return "Usage: /agentpaas-init <project_path>"
    result = json.loads(tools.agentpaas_init_project({"project_dir": path}))
    err = _format_error(result, "Init failed")
    if err:
        return err
    files = result.get("files_created", "scaffold created") if isinstance(result, dict) else "scaffold created"
    return f"Project initialized at {path}.\nFiles: {files}"


def _cmd_pack(args_str, ctx=None):
    """`/agentpaas-pack <path>` — build a signed agent image."""
    path = (args_str or "").strip()
    if not path:
        return "Usage: /agentpaas-pack <project_path>"
    result = json.loads(tools.agentpaas_pack({"project_dir": path}))
    err = _format_error(result, "Pack failed")
    if err:
        return err
    agent_name = result.get("agent_name", "?") if isinstance(result, dict) else "?"
    digest = result.get("image_digest", "?") if isinstance(result, dict) else "?"
    return f"Packed {agent_name}.\n  Image digest: {digest}"


def _cmd_run(args_str, ctx=None):
    """`/agentpaas-run <image_or_project>` — start a governed agent run."""
    target = (args_str or "").strip()
    if not target:
        return "Usage: /agentpaas-run <image_digest_or_project_path>"
    result = json.loads(tools.agentpaas_run({"image_or_project": target}))
    err = _format_error(result, "Run failed")
    if err:
        return err
    run_id = result.get("run_id", "?") if isinstance(result, dict) else "?"
    status = result.get("status", "?") if isinstance(result, dict) else "?"
    return f"Run started: run_id={run_id}, status={status}"


def _cmd_secret_list(args_str, ctx=None):
    """`/agentpaas-secret-list` — list stored credentials by label."""
    result = json.loads(tools.agentpaas_secret_list({}))
    err = _format_error(result, "Secret list failed")
    if err:
        return err
    # CLI returns a bare list of credential labels
    secrets = result if isinstance(result, list) else result.get("secrets", [])
    if not secrets:
        return "No stored credentials."
    lines = [f"Stored credentials ({len(secrets)}):"]
    for s in secrets:
        if isinstance(s, str):
            lines.append(f"  {s}")
        elif isinstance(s, dict):
            name = s.get("name", "?")
            provider = s.get("provider", "")
            line = f"  {name}"
            if provider:
                line += f" ({provider})"
            lines.append(line)
    return "\n".join(lines)


def _cmd_cron_list(args_str, ctx=None):
    """`/agentpaas-cron-list` — list all cron schedules."""
    result = json.loads(tools.agentpaas_cron_list({}))
    err = _format_error(result, "Cron list failed")
    if err:
        return err
    # CLI returns a bare list of schedules
    schedules = result if isinstance(result, list) else result.get("schedules", [])
    if not schedules:
        return "No cron schedules."
    lines = [f"Cron schedules ({len(schedules)}):"]
    for s in schedules:
        if isinstance(s, dict):
            sid = s.get("id", s.get("schedule_id", "?"))
            agent = s.get("agent_name", "?")
            expr = s.get("expr", s.get("cron_expr", "?"))
            lines.append(f"  {sid}: {agent} — {expr}")
        else:
            lines.append(f"  {s}")
    return "\n".join(lines)


def _cmd_policy_show(args_str, ctx=None):
    """`/agentpaas-policy-show [project_dir|run_id]` — show active policy."""
    target = (args_str or "").strip()
    if not target:
        result = json.loads(tools.agentpaas_policy_show({}))
    else:
        # Try as project_dir first
        result = json.loads(tools.agentpaas_policy_show({"project_dir": target}))
        if isinstance(result, dict) and "error" in result:
            # Fall back to run_id
            result = json.loads(tools.agentpaas_policy_show({"run_id": target}))
    err = _format_error(result, "Policy show failed")
    if err:
        return err
    return json.dumps(result, indent=2)


def _cmd_summarize(args_str, ctx=None):
    """`/agentpaas-summarize <run_id>` — summarize a completed or failed run."""
    run_id = (args_str or "").strip()
    if not run_id:
        return "Usage: /agentpaas-summarize <run_id>"
    result = json.loads(tools.agentpaas_summarize_run({"run_id": run_id}))
    err = _format_error(result, "Summarize failed")
    if err:
        return err
    return json.dumps(result, indent=2)


def _cmd_explain_failure(args_str, ctx=None):
    """`/agentpaas-explain-failure <run_id>` — diagnose a failed run."""
    run_id = (args_str or "").strip()
    if not run_id:
        return "Usage: /agentpaas-explain-failure <run_id>"
    result = json.loads(tools.agentpaas_explain_failure({"run_id": run_id}))
    err = _format_error(result, "Explain failure failed")
    if err:
        return err
    root_cause = result.get("root_cause", "") if isinstance(result, dict) else ""
    evidence = result.get("evidence", []) if isinstance(result, dict) else []
    lines = [f"Failure analysis for {run_id}:"]
    if root_cause:
        lines.append(f"  Root cause: {root_cause}")
    if evidence:
        lines.append(f"  Evidence ({len(evidence)} items):")
        for ev in evidence[-5:]:
            lines.append(f"    - {ev}")
    if len(lines) == 1:
        return json.dumps(result, indent=2)
    return "\n".join(lines)


def _cmd_trigger(args_str, ctx=None):
    """`/agentpaas-trigger <agent_name>` — invoke an agent via trigger API."""
    agent_name = (args_str or "").strip()
    if not agent_name:
        return "Usage: /agentpaas-trigger <agent_name>"
    result = json.loads(tools.agentpaas_trigger_invoke({"agent_name": agent_name}))
    err = _format_error(result, "Trigger failed")
    if err:
        return err
    run_id = result.get("run_id", "?") if isinstance(result, dict) else "?"
    status = result.get("status", "?") if isinstance(result, dict) else "?"
    invoke_response = result.get("invoke_response", "") if isinstance(result, dict) else ""
    msg = f"Trigger invoked {agent_name}: run_id={run_id}, status={status}"
    if invoke_response:
        msg += f"\nInvoke Response:\n{invoke_response}"
    return msg


def _cmd_export(args_str, ctx=None):
    """`/agentpaas-export <project_path>` — export an agent project as a signed bundle."""
    path = (args_str or "").strip()
    if not path:
        return "Usage: /agentpaas-export <project_path>"
    result = json.loads(tools.agentpaas_export({"project_dir": path}))
    err = _format_error(result, "Export failed")
    if err:
        return err
    if isinstance(result, dict):
        lines = []
        if result.get("bundle_path"):
            lines.append(f"Bundle: {result['bundle_path']}")
        if result.get("digest"):
            lines.append(f"Digest: {result['digest']}")
        if result.get("publisher_fingerprint"):
            fp = result["publisher_fingerprint"]
            lines.append(f"Fingerprint: {fp}")
            lines.append(
                f"Read your fingerprint {fp} to the receiver over another "
                f"channel (phone, Signal, etc.) so they can verify the bundle."
            )
        if result.get("instruction"):
            lines.append(result["instruction"])
        if lines:
            return "\n".join(lines)
    return json.dumps(result, indent=2)


def _cmd_inspect(args_str, ctx=None):
    """`/agentpaas-inspect <bundle_path>` — inspect a signed agent bundle."""
    bundle_path = (args_str or "").strip()
    if not bundle_path:
        return "Usage: /agentpaas-inspect <bundle_path>"
    result = json.loads(tools.agentpaas_bundle_inspect({"bundle_path": bundle_path}))
    err = _format_error(result, "Inspect failed")
    if err:
        return err
    return json.dumps(result, indent=2)


def _cmd_install(args_str, ctx=None):
    """`/agentpaas-install <bundle_path>` — install a signed agent bundle.

    CRITICAL: This command NEVER approves consent. It instructs the user
    to run the install in their terminal where they can follow prompts.
    """
    bundle_path = (args_str or "").strip()
    if not bundle_path:
        return "Usage: /agentpaas-install <bundle_path>"
    result = json.loads(tools.agentpaas_install({"bundle_path": bundle_path}))
    err = _format_error(result, "Install failed")
    if err:
        instruction = result.get("terminal_instruction") if isinstance(result, dict) else ""
        if instruction:
            return f"{err}\n\n{instruction}"
        return err
    return json.dumps(result, indent=2)


def _cmd_installed(args_str, ctx=None):
    """`/agentpaas-installed` — list installed agents."""
    result = json.loads(tools.agentpaas_installed_list({}))
    err = _format_error(result, "List installed failed")
    if err:
        return err
    agents = result if isinstance(result, list) else result.get("agents", [])
    if not agents:
        return "No installed agents."
    lines = [f"Installed agents ({len(agents)}):"]
    for a in agents:
        if isinstance(a, dict):
            ref = a.get("ref", a.get("digest", "?"))
            alias = a.get("alias", "")
            fp = a.get("fingerprint", "")
            line = f"  {ref}"
            if alias:
                line += f"  (alias: {alias})"
            if fp:
                line += f"  [{fp}]"
            lines.append(line)
        else:
            lines.append(f"  {a}")
    return "\n".join(lines)


def _cmd_fork(args_str, ctx=None):
    """`/agentpaas-fork <ref> <target_dir>` — fork an installed agent to a local project."""
    parts = (args_str or "").strip().split(None, 1)
    if len(parts) < 2:
        return "Usage: /agentpaas-fork <ref> <target_dir>"
    ref, target_dir = parts[0], parts[1]
    result = json.loads(tools.agentpaas_fork({"installed_ref": ref, "target_dir": target_dir}))
    err = _format_error(result, "Fork failed")
    if err:
        return err
    return f"Forked {ref} to {target_dir}"


def _cmd_provenance(args_str, ctx=None):
    """`/agentpaas-provenance <ref>` — show provenance chain for an installed agent."""
    ref = (args_str or "").strip()
    if not ref:
        return "Usage: /agentpaas-provenance <ref>"
    result = json.loads(tools.agentpaas_provenance_show({"installed_ref": ref}))
    err = _format_error(result, "Provenance failed")
    if err:
        return err
    return json.dumps(result, indent=2)


def _cmd_trust(args_str, ctx=None):
    """`/agentpaas-trust` — list trusted publishers."""
    result = json.loads(tools.agentpaas_trust_list({}))
    err = _format_error(result, "Trust list failed")
    if err:
        return err
    publishers = result if isinstance(result, list) else result.get("publishers", [])
    if not publishers:
        return "No trusted publishers."
    lines = [f"Trusted publishers ({len(publishers)}):"]
    for p in publishers:
        if isinstance(p, dict):
            fp = p.get("fingerprint", p.get("id", "?"))
            name = p.get("name", "")
            line = f"  {fp}"
            if name:
                line += f"  ({name})"
            lines.append(line)
        else:
            lines.append(f"  {p}")
    return "\n".join(lines)


def _cmd_identity(args_str, ctx=None):
    """`/agentpaas-identity` — show current publisher identity."""
    result = json.loads(tools.agentpaas_identity_show({}))
    err = _format_error(result, "Identity show failed")
    if err:
        return err
    if isinstance(result, dict):
        fp = result.get("fingerprint", result.get("publisher", "?"))
        name = result.get("name", "")
        lines = [f"Publisher identity:"]
        lines.append(f"  Fingerprint: {fp}")
        if name:
            lines.append(f"  Name: {name}")
        return "\n".join(lines)
    return json.dumps(result, indent=2)


# ---------------------------------------------------------------------------
# Slash command registry
#
# Each entry: (command_name, handler, description, args_hint)
# The command_name uses spaces — register_command normalizes to hyphens.
# ---------------------------------------------------------------------------

_SLASH_COMMANDS = {
    "agentpaas deploy": (_cmd_deploy, "Pack and run an agent from a project directory", "<project_path>"),
    "agentpaas status": (_cmd_status, "Show daemon status and active runs", ""),
    "agentpaas list": (_cmd_list, "List all AgentPaaS runs (active and recent)", ""),
    "agentpaas logs": (_cmd_logs, "Tail logs for a run", "<run_id>"),
    "agentpaas audit": (_cmd_audit, "Show audit events", "[run_id]"),
    "agentpaas doctor": (_cmd_doctor, "Run system diagnostics", ""),
    "agentpaas stop": (_cmd_stop, "Stop a running agent", "<run_id>"),
    "agentpaas timeline": (_cmd_timeline, "Show timeline events for a run", "<run_id>"),
    "agentpaas init": (_cmd_init, "Create a new agent project scaffold", "<project_path>"),
    "agentpaas pack": (_cmd_pack, "Build a signed agent image", "<project_path>"),
    "agentpaas run": (_cmd_run, "Start a governed agent run", "<image_or_project>"),
    "agentpaas secret-list": (_cmd_secret_list, "List stored credentials by label", ""),
    "agentpaas cron-list": (_cmd_cron_list, "List all cron schedules", ""),
    "agentpaas policy-show": (_cmd_policy_show, "Show active policy", "[project_dir|run_id]"),
    "agentpaas summarize": (_cmd_summarize, "Summarize a completed or failed run", "<run_id>"),
    "agentpaas explain-failure": (_cmd_explain_failure, "Diagnose a failed run", "<run_id>"),
    "agentpaas trigger": (_cmd_trigger, "Invoke an agent via trigger API", "<agent_name>"),
    "agentpaas export": (_cmd_export, "Export an agent project as a signed bundle", "<project_path>"),
    "agentpaas inspect": (_cmd_inspect, "Inspect a signed agent bundle", "<bundle_path>"),
    "agentpaas install": (_cmd_install, "Install a signed agent bundle (NEVER approves consent — requires terminal)", "<bundle_path>"),
    "agentpaas installed": (_cmd_installed, "List installed agents", ""),
    "agentpaas fork": (_cmd_fork, "Fork an installed agent to a local project", "<ref> <target_dir>"),
    "agentpaas provenance": (_cmd_provenance, "Show provenance chain for an installed agent", "<ref>"),
    "agentpaas trust": (_cmd_trust, "List trusted publishers", ""),
    "agentpaas identity": (_cmd_identity, "Show current publisher identity", ""),
}


def register(ctx):
    """Register all AgentPaaS operator tools and slash commands."""
    # Register operator-contract tools
    for tool_name in schemas.TOOL_NAMES:
        schema = getattr(schemas, tool_name.upper())
        handler = getattr(tools, tool_name)
        ctx.register_tool(
            name=tool_name,
            toolset="agentpaas",
            schema=schema,
            handler=handler,
        )

    # Register slash commands (thin orchestrators over the plugin's own tools)
    for cmd_name, (handler, description, args_hint) in _SLASH_COMMANDS.items():
        if hasattr(ctx, "register_command"):
            ctx.register_command(cmd_name, handler, description=description, args_hint=args_hint)

    # Register bundled SKILL.md as a plugin skill (accessible via
    # skill_view("agentpaas:deploy") but NOT in available_skills index).
    if hasattr(ctx, "register_skill"):
        from pathlib import Path
        skill_path = Path(__file__).resolve().parent / "SKILL.md"
        if skill_path.is_file():
            ctx.register_skill("deploy", skill_path, description="AgentPaaS deploy workflow")

    # Create a local skill pointer that WILL appear in <available_skills>.
    # Plugin skills registered above are opt-in (skill_view only) and never
    # appear in the system prompt index. Without this pointer, the agent
    # in future sessions has no idea the onboarding instructions exist and
    # will write plain Python instead of using the @agent.on_invoke SDK
    # pattern. This is deterministic — no reliance on after-install.md.
    _ensure_local_skill_pointer()

    # Ensure 'agentpaas' is in platform_toolsets.cli. This is normally
    # done by after-install.md, but if the agent didn't run it (or the
    # plugin was installed via git clone), the toolset won't be present
    # and the agent can't call agentpaas_* tools. Running it here makes
    # the first session work regardless.
    _ensure_toolset()

    # Best-effort: ensure the AgentPaaS daemon is running. A user who just
    # installed the plugin and restarted should not have to separately know
    # about `agentpaas daemon start`. If the socket is missing, spawn the
    # daemon as a detached background process. Failures are logged but never
    # block plugin registration — the agent can still call agentpaas_doctor
    # to diagnose, and the hint in the error messages guides manual start.
    _ensure_daemon_running()

    logger.debug("AgentPaaS plugin registered %d tools + %d slash commands",
                 len(schemas.TOOL_NAMES), len(_SLASH_COMMANDS))
