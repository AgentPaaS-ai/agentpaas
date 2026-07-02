"""Demo 3: Agentic repair loop.

Agent has a simulated code defect and missing egress policy.
The agent makes an LLM call, hits a failure, records iterations,
and attempts egress that gets denied. The MCP next_action flow
would recommend: fix_code (if defect is detectable) or
propose_policy (if egress is legitimately needed).

This fixture exercises:
  - agent.llm() — budget-tracked LLM call
  - agent.record_iteration() — iteration accounting
  - agent.http() — egress attempt (denied by isolation)
  - agent.mcp() — MCP tool call (denied if not declared)
"""
from agentpaas_sdk import agent


@agent.on_invoke
def invoke(payload):
    results = {}

    # Step 1: LLM call for reasoning (budget-tracked)
    try:
        llm_resp = agent.llm("Analyze this error: connection refused to database")
        results["llm"] = {"status": "ok", "text": llm_resp.get("text", "")[:200]}
    except Exception as e:
        results["llm"] = {"status": "failed", "error": str(e)}

    # Step 2: Record iteration for budget accounting
    try:
        agent.record_iteration()
        results["iteration"] = {"status": "ok"}
    except Exception as e:
        results["iteration"] = {"status": "failed", "error": str(e)}

    # Step 3: Attempt egress (missing policy — denied by isolation)
    try:
        resp = agent.http("GET", "https://registry.npmjs.org/express")
        results["egress"] = {"status": "ok"}
    except Exception as e:
        results["egress"] = {"status": "denied", "error": str(e)}

    # Step 4: MCP tool call (undeclared — denied by harness)
    try:
        result = agent.mcp("package-registry", "lookup", {"name": "express"})
        results["mcp"] = {"status": "ok", "result": result}
    except Exception as e:
        results["mcp"] = {"status": "denied", "error": str(e)}

    return {"scenario": "repair-loop", "results": results}
