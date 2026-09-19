# AgentPaaS host install

This directory is the AgentPaaS install entry for host tools.

## Claude Code

If `agentpaas version` already shows 0.5.x, leave the CLI. Add the MCP server:

```
claude mcp add -s user agentpaas -- agentpaas-mcp
```

Restart Claude Code. Then use MCP `init`, `pack`, `doctor`. Agent code uses `from agentpaas_sdk import agent`, `@agent.on_invoke`, `agent.llm(prompt)` (returns `{"text": "..."}`), and `agent.http("GET", url)` for live data such as `https://wttr.in/Folsom?format=j1`. Secrets stay in Terminal (`agentpaas secret add openrouter-key`).

## Hermes

Paste this GitHub URL in Hermes. Hermes installs the plugin and local tooling (CLI + Colima/Docker). Restart the Hermes session when asked.
