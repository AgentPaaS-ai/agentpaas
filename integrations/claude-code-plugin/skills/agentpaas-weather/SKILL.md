---
name: agentpaas-weather
description: >
  Build a weather agent that uses an LLM, and responds in a friendly
  demeanour. Use when the user asks to build a weather agent, a friendly
  weather assistant, or the AgentPaaS weather golden path.
---

# AgentPaaS weather agent

This is an AgentPaaS agent, not a standalone HTTP app.

1. Call MCP `init` in the current folder if `agent.yaml` is missing.
2. Keep `from agentpaas_sdk import agent` and `@agent.on_invoke`.
3. Live weather via `agent.http("GET", "https://wttr.in/{city}?format=j1")`.
4. Friendly summary via `agent.llm(prompt)` which returns `{"text": "..."}`.
5. Payload `{"query":"What's the weather in Folsom?"}`.
6. `agentpaas pack` then local run. For cloud, `pack --target linux/amd64` then cloud push/deploy/invoke.
7. If a key is needed, tell the user to run `agentpaas secret add openrouter-key` in Terminal.

The harness injects the LLM credential. Do not open `/usr` SDK sources. Do not POST to OpenRouter from `requests`.
