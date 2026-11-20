# MCP and tools

MCP servers and tools are components. Pack them, push them, deploy them. They are not workflows.

| Kind | Flag | What it is |
|------|------|------------|
| MCP | `--type mcp` | A hosted MCP server inside an AgentPaaS container |
| Tool | `--type tool` | Deterministic worker. No LLM. Same input, same output. |

An agent can call a deployed MCP or tool. That still is not a workflow. A workflow is only when you compose more than one step.

The console Component Registry lists what you pushed. Deployments are the live ones that take slots.
