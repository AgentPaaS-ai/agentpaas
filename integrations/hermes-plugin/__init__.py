"""AgentPaaS Hermes plugin — registers 17 operator-contract tools + slash commands."""
import json
import logging
from . import schemas, tools

logger = logging.getLogger(__name__)


# --- Slash command handlers ---

def _cmd_deploy(args_str, ctx=None):
    """`/agentpaas deploy <path>` — pack → run, return run_id."""
    path = (args_str or "").strip()
    if not path:
        return "Usage: /agentpaas deploy <project_path>"
    pack_result = json.loads(tools.agentpaas_pack({"project_dir": path}))
    if "error" in pack_result:
        return f"Pack failed: {pack_result['error']}"
    agent_name = pack_result.get("agent_name", "")
    run_result = json.loads(tools.agentpaas_run({"image_or_project": agent_name}))
    if "error" in run_result:
        return f"Run failed: {run_result['error']}"
    run_id = run_result.get("run_id", "?")
    return f"Deployed {agent_name}: run_id={run_id}"


def _cmd_status(args_str, ctx=None):
    """`/agentpaas status` — show active runs."""
    result = json.loads(tools.agentpaas_status({}))
    if "error" in result:
        return f"Status failed: {result['error']}"
    runs = result.get("runs", [])
    if not runs:
        return "No active runs."
    lines = ["Active runs:"]
    for r in runs:
        lines.append(f"  {r.get('run_id','?')}: {r.get('agent_name','?')} — {r.get('status','?')}")
    return "\n".join(lines)


def _cmd_logs(args_str, ctx=None):
    """`/agentpaas logs [run_id]` — tail logs for a run."""
    run_id = (args_str or "").strip()
    if not run_id:
        return "Usage: /agentpaas logs <run_id>"
    result = json.loads(tools.agentpaas_logs({"run_id": run_id, "tail": 50}))
    if "error" in result:
        return f"Logs failed: {result['error']}"
    return result.get("logs", "(no output)")


def _cmd_audit(args_str, ctx=None):
    """`/agentpaas audit [run_id]` — show audit events."""
    run_id = (args_str or "").strip()
    result = json.loads(tools.agentpaas_audit_query({"run_id": run_id} if run_id else {}))
    if "error" in result:
        return f"Audit query failed: {result['error']}"
    entries = result.get("entries", [])
    if not entries:
        return "No audit events."
    lines = [f"Audit ({len(entries)} events):"]
    for e in entries[-10:]:
        lines.append(f"  [{e.get('event_type','?')}] {e.get('timestamp','?')}")
    return "\n".join(lines)


def register(ctx):
    """Register all AgentPaaS operator tools and slash commands."""
    # Register 17 operator-contract tools
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
    slash_commands = {
        "agentpaas deploy": _cmd_deploy,
        "agentpaas status": _cmd_status,
        "agentpaas logs": _cmd_logs,
        "agentpaas audit": _cmd_audit,
    }
    for cmd_name, handler in slash_commands.items():
        if hasattr(ctx, "register_command"):
            ctx.register_command(cmd_name, handler)

    # Register bundled SKILL.md
    if hasattr(ctx, "register_skill"):
        from pathlib import Path
        skill_path = Path(__file__).resolve().parent / "SKILL.md"
        if skill_path.is_file():
            ctx.register_skill("deploy", skill_path, description="AgentPaaS deploy workflow")

    logger.debug("AgentPaaS plugin registered %d tools + %d slash commands",
                 len(schemas.TOOL_NAMES), len(slash_commands))
