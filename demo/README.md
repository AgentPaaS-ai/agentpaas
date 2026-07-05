# Weather Agent Demo

A single demo that showcases AgentPaaS governing an LLM-powered agent.

## What It Does

1. User asks a natural-language question: "What's the weather in Folsom?"
2. LLM (via OpenRouter) extracts the city name from the question
3. Agent fetches weather data from wttr.in (policy-controlled egress)
4. LLM analyzes the raw weather data and produces a friendly summary

Two LLM calls = real reasoning, not just a web API call wrapped in an agent.

## What It Demonstrates

- **LLM governance**: Both LLM calls are budget-tracked and audited
- **Egress policy**: Only wttr.in is allowed — all other domains are denied
- **Signed audit chain**: Every action (LLM call, HTTP egress, invoke) is
  recorded in the tamper-evident audit log
- **Policy-controlled**: The agent cannot exfiltrate data — only wttr.in egress

## Prerequisites

1. AgentPaaS installed (brew install agentpaas-ai/tap/agentpaas)
2. Docker (Colima or Docker Desktop) running
3. An OpenRouter API key (free tier works): https://openrouter.ai/keys

## Quick Start (through Hermes)

The easiest way to run this demo is through Hermes Agent, which handles
the full lifecycle: build, pack, run, invoke.

1. Install the AgentPaaS Hermes plugin:
   ```sh
   hermes plugins install https://github.com/AgentPaaS-ai/agentpaas/tree/main/integrations/hermes-plugin --enable
   /quit  # restart Hermes to load the plugin
   ```

2. Store your OpenRouter API key:
   ```sh
   agentpaas secret add openrouter-key
   # paste your OpenRouter key when prompted
   ```

3. In Hermes, ask it to build and run the weather agent:
   ```
   Build and run the weather agent demo from demo/weather-agent/.
   Use OpenRouter with the deepseek/deepseek-v4-flash model.
   My OpenRouter key is stored as "openrouter-key".
   Then invoke it with the question "What's the weather in Folsom?"
   ```

4. Hermes will:
   - Initialize the agent project (agentpaas_init_project)
   - Configure the LLM provider (agentpaas_llm_configure)
   - Validate the project (agentpaas_validate_project)
   - Pack the agent into a Docker image (agentpaas_pack)
   - Run the agent (agentpaas_run)
   - Invoke it with your question (agentpaas_trigger_invoke)
   - Show you the timeline and audit trail (agentpaas_get_run_timeline)

## Quick Start (CLI only)

If you prefer the CLI directly:

```sh
# 1. Store your OpenRouter key (pipe it in — never on the command line)
echo -n "sk-or-v1-YOUR_KEY_HERE" | agentpaas secret add openrouter-key

# 2. Configure LLM in the agent project
cd demo/weather-agent
agentpaas llm configure --provider openrouter --model deepseek/deepseek-v4-flash --credential openrouter-key

# 3. Validate, pack, and run
agentpaas validate-project .
agentpaas pack .
agentpaas run weather-agent

# 4. Invoke the agent (payload must be a file path)
echo '{"query": "What is the weather in Folsom?"}' > /tmp/payload.json
agentpaas trigger invoke weather-agent --payload /tmp/payload.json --wait

# 5. Check the timeline and audit trail
agentpaas status <run-id>
agentpaas audit query --run-id <run-id>
```

## Expected Output

The agent returns something like:

```json
{
  "scenario": "weather-agent",
  "results": {
    "city": "Folsom",
    "weather_fetched": true,
    "llm_summary": true
  },
  "answer": "It's currently 72°F and sunny in Folsom. The weather is pleasant with clear skies. It's a great day to be outside!"
}
```

The exact answer varies — the LLM generates it from the live weather data.

## Audit Trail

After running, check the audit trail to see the governance in action:

```sh
agentpaas audit query --run-id <run-id>
```

You'll see:
- `llm_call` events (2x — city extraction + weather summary)
- `egress_allowed` for wttr.in
- `egress_denied` for any other domain the agent tries to reach
- Budget tracking (tokens used, time elapsed)
- Signed hash chain linking all events

## Try Variations

```sh
# Different city
echo '{"query": "How is the weather in Tokyo?"}' > /tmp/payload.json
agentpaas trigger invoke weather-agent --payload /tmp/payload.json --wait

# Vague question — LLM has to reason about what city to use
echo '{"query": "Is it raining where Apple is headquartered?"}' > /tmp/payload.json
agentpaas trigger invoke weather-agent --payload /tmp/payload.json --wait
```
