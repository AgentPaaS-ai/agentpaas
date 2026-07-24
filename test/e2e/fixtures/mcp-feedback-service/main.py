"""
MCP Feedback Service — provides feedback lookup via MCP tool.

This is a pure MCP service agent with no LLM or HTTP egress.
It exposes a single tool: `lookup_feedback` which returns a
distinctive marker for callers to verify.
"""
from agentpaas_sdk import agent


@agent.mcp_tool("lookup_feedback")
def lookup_feedback(args):
    """Look up feedback for a given query. Returns a distinctive marker."""
    query = args.get("query", "")
    return {
        "query": query,
        "result": "feedback-found",
        "score": 42,
        "marker": "mcp-feedback-service-S1c-fixture",
    }
