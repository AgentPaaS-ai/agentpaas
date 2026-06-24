"""AgentPaaS Hermes plugin — registers 17 operator-contract tools."""
import logging
from . import schemas, tools

logger = logging.getLogger(__name__)


def register(ctx):
    """Register all AgentPaaS operator tools."""
    for tool_name in schemas.TOOL_NAMES:
        schema = getattr(schemas, tool_name.upper())
        handler = getattr(tools, tool_name)
        ctx.register_tool(
            name=tool_name,
            toolset="agentpaas",
            schema=schema,
            handler=handler,
        )
    logger.debug("AgentPaaS plugin registered %d tools", len(schemas.TOOL_NAMES))