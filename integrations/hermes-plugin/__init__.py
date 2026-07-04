"""AgentPaaS Hermes plugin — registers 30 operator-contract tools + slash commands."""
import json
import logging
import os
from pathlib import Path
from . import schemas, tools

logger = logging.getLogger(__name__)


_POINTER_NAME = "agentpaas-build"  # deliberately != "agentpaas-deploy" (slash cmd collision)

_SKILL_POINTER_CONTENT = f"""---
name: {_POINTER_NAME}
description: >
  Build, deploy, package, run, and govern AI agents. Use when the user
  asks to build, create, deploy, pack, or run any agent. You MUST load
  the full skill with skill_view(name="agentpaas:deploy") for onboarding
  instructions, code structure requirements (@agent.on_invoke SDK
  pattern), egress policy rules, credential onboarding, and LLM
  configuration.
---

# AgentPaaS Deploy Pointer

When the user asks to build, create, deploy, pack, run, or govern any
agent, you MUST load the real skill immediately:

skill_view(name="agentpaas:deploy")

This pointer exists because plugin skills do not appear in the
available_skills index. The full instructions (agent code structure
with @agent.on_invoke, egress confirmation, credential onboarding,
LLM configuration, pack/run workflow) live in the plugin skill at
agentpaas:deploy. Load it BEFORE writing any agent code or calling
agentpaas tools. Do NOT skip this step.
"""


def _ensure_local_skill_pointer():
    """Create a local skill pointer in the active profile so it appears
    in <available_skills>. Plugin skills are opt-in (skill_view only)
    and never appear in the system prompt index. This pointer bridges
    that gap deterministically at plugin registration time — no reliance
    on after-install.md being shown to the agent.

    Also writes a SOUL.md snippet (BUG 8 fix) that is always injected
    into the system prompt, ensuring the agent loads the deploy skill
    before building any agent — even if it ignores the available_skills
    hint.
    """
    try:
        # Resolve the active profile directory. Hermes activates a profile
        # by setting a HERMES_HOME override that points directly at
        # ``~/.hermes/profiles/<name>``. We must use ``get_hermes_home()``
        # (which honors the override) — NOT ``os.environ["HERMES_HOME"]``,
        # which is the *parent* process env and does not reflect the -p flag.
        from hermes_constants import get_hermes_home
        profile_dir = get_hermes_home()

        # When running without an override (HERMES_HOME == ~/.hermes),
        # get_active_profile_name() tells us which sub-profile to target.
        from hermes_cli.profiles import get_active_profile_name
        profile = get_active_profile_name()
        if profile not in ("default", "custom"):
            # Only descend if profile_dir is still the root ~/.hermes.
            # If the override already pointed at profiles/<name>, the
            # profile name will not appear as a trailing dir component.
            if profile_dir.name != profile:
                profile_dir = profile_dir / "profiles" / profile

        # 1. Create skill pointer (appears in available_skills index)
        skills_dir = profile_dir / "skills" / "agentpaas"
        skills_dir.mkdir(parents=True, exist_ok=True)
        pointer = skills_dir / "SKILL.md"
        pointer.write_text(_SKILL_POINTER_CONTENT, encoding="utf-8")

        # 2. Write SOUL.md snippet (always injected into system prompt)
        # This ensures the agent loads the deploy skill even if it ignores
        # the available_skills hint. SOUL.md is loaded fresh every turn.
        soul_path = profile_dir / "SOUL.md"
        soul_snippet = """
# AgentPaaS Onboarding Rule

CRITICAL: When the user asks to build, create, deploy, pack, run, or
govern any agent, you MUST load the agentpaas:deploy skill FIRST by
calling skill_view(name="agentpaas:deploy") BEFORE writing any code
or calling any agentpaas tools. This skill contains essential
instructions for:
- Agent code structure (@agent.on_invoke SDK pattern)
- Egress policy confirmation (ask user, write specific domains)
- Credential and LLM configuration
- Pack/run workflow and verification

Do NOT skip this step. Do NOT write agent code without loading the
skill first. Do NOT use wildcard egress policies without asking the
user for specific domains.
"""

        existing_soul = ""
        if soul_path.exists():
            existing_soul = soul_path.read_text(encoding="utf-8")

        # Only append if the snippet isn't already there (idempotent)
        if "AgentPaaS Onboarding Rule" not in existing_soul:
            if existing_soul and not existing_soul.endswith("\n"):
                existing_soul += "\n"
            soul_path.write_text(existing_soul + soul_snippet, encoding="utf-8")

        logger.debug("AgentPaaS skill pointer + SOUL.md created in profile %s", profile)
    except Exception as e:
        logger.warning("Could not create skill pointer: %s", e)


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
    """`/agentpaas-list` — list all AgentPaaS runs (active and recent)."""
    result = json.loads(tools.agentpaas_list_runs({}))
    err = _format_error(result, "List failed")
    if err:
        return err
    runs = result.get("runs", []) if isinstance(result, dict) else (result if isinstance(result, list) else [])
    if not runs:
        return "No AgentPaaS runs found."
    lines = [f"AgentPaaS runs ({len(runs)} total):"]
    for r in runs:
        if isinstance(r, dict):
            run_id = r.get("run_id", r.get("id", "?"))
            agent = r.get("agent_name", "?")
            status = r.get("status", "?")
            lines.append(f"  {run_id}: {agent} — {status}")
        else:
            lines.append(f"  {r}")
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

    logger.debug("AgentPaaS plugin registered %d tools + %d slash commands",
                 len(schemas.TOOL_NAMES), len(_SLASH_COMMANDS))
