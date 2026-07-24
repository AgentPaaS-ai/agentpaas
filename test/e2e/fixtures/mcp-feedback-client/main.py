"""
MCP Feedback Client — invokes the feedback service via MCP.

This client agent calls the MCP-enabled feedback service at invoke time
and returns the service's response. The feedback service is discovered
via the ManagedServiceResolver (or sidecar) mechanism.
"""
from agentpaas_sdk import agent


@agent.on_invoke
def invoke(payload):
    """Invoke the MCP feedback service and return its response."""
    try:
        result = agent.mcp("feedback", "lookup_feedback", {"query": "test"})
        return {
            "status": "OK",
            "mcp_result": result,
            "marker": result.get("marker", "mcp-call-succeeded"),
        }
    except Exception as e:
        return {
            "status": "FAILED",
            "error": str(e),
        }
