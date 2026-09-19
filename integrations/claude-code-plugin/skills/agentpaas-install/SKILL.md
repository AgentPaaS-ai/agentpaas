---
name: agentpaas-install
description: >
  Install from https://github.com/AgentPaaS-ai/agentpaas/tree/main/install
  Use when the user pastes that GitHub install URL or says install AgentPaaS
  in Claude Code.
---

# Install AgentPaaS in Claude Code

If `agentpaas version` already shows 0.5.x, leave the CLI.

```
claude mcp add -s user agentpaas -- "$(brew --prefix)/bin/agentpaas-mcp"
```

Restart the session. Then MCP `doctor`. Secrets stay in Terminal.
