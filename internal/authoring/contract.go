package authoring

// MCPInstructions is returned on MCP initialize. Hosts (Claude Code, Cursor,
// Grok) get this from the server. The customer does not write a markdown file.
const MCPInstructions = `AgentPaaS host rules:
- Call MCP tools (init, pack, doctor). Do not clone GitHub. Do not build a standalone HTTP service.
- Do not read agentpaas_sdk sources. Do not /add-dir /usr or /usr/local/python.
- Do not POST to OpenRouter yourself. The harness injects the key.
- Agent code: from agentpaas_sdk import agent; @agent.on_invoke; agent.llm(prompt) returns {"text": "..."}; agent.http("GET", url) for real data (wttr.in).
- Secrets stay in Terminal: agentpaas secret add openrouter-key. Never paste keys in chat.
- Weather golden: payload {"query":"What's the weather in Folsom?"}; fetch wttr.in; summarize with agent.llm.
- Prefer agentpaas init. The scaffold already has the SDK calls.
`
