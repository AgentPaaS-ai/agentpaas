# Weather Agent Demo

A single demo that showcases AgentPaaS governing an LLM-powered agent.

## What It Does

1. User provides a city name as input
2. Agent fetches real weather data from wttr.in (HTTP, policy-controlled)
3. LLM summarizes the raw weather data into a friendly response

Real data first, LLM for presentation — never ask an LLM to "look up"
weather (they fabricate values).

## What It Demonstrates

- **Egress policy**: Only wttr.in:443 and openrouter.ai:443 are allowed
- **Credential brokering**: LLM API key stored in Keychain, injected at runtime
- **Signed audit chain**: Every action (LLM call, HTTP egress, invoke) is
  recorded in the tamper-evident audit log
- **Real data**: Weather values match wttr.in — not LLM-fabricated

## Prerequisites

1. **Hermes Agent**: `brew install nousresearch/tap/hermes-agent`
2. **Docker**: `brew install colima && colima start`
3. **AgentPaaS**: `brew install agentpaas-ai/tap/agentpaas`
4. **Clear quarantine** (brew cask not notarized):
   ```bash
   xattr -cr /opt/homebrew/bin/agentpaas
   ```
5. **OpenRouter API key** (free tier): https://openrouter.ai/keys

## Quick Start (through Hermes)

### 1. Install the AgentPaaS plugin

In Hermes, tell it:

> Install the AgentPaaS plugin from github https://github.com/AgentPaaS-ai/agentpaas

Hermes installs the plugin and registers the toolset. Restart when prompted:
```
/quit
hermes
```

### 2. Store your OpenRouter API key

In a separate terminal (NOT in Hermes — the key never enters the
conversation):

```bash
agentpaas secret add openrouter-key
# paste your OpenRouter key when prompted
```

### 3. Build and run the weather agent

In Hermes:

> Build a weather agent that takes a city name as input, uses an LLM
> to look up the weather, and returns the current conditions.

When Hermes asks about the LLM:

> Yes, use openrouter deepseek flash, key is in openrouter-key

### 4. Invoke the agent

> Invoke the weather agent with city=Folsom

### 5. Check the audit trail

> Show me the audit trail for the last run

You'll see:
- `egress_allowed` for wttr.in (weather data fetch)
- `egress_allowed` for openrouter.ai (LLM call, with credential_id)
- 0 policy denials

## Quick Start (CLI only)

If you prefer the CLI directly:

```bash
# 1. Start the daemon
agentpaas daemon start

# 2. Store your OpenRouter key
agentpaas secret add openrouter-key

# 3. Build the agent from this demo directory
cd demo/weather-agent
agentpaas llm configure --provider openrouter --model deepseek/deepseek-v4-flash --credential openrouter-key
agentpaas validate-project .
agentpaas pack .
agentpaas run weather-agent

# 4. Invoke the agent
agentpaas trigger invoke weather-agent --payload '{"city": "Folsom"}' --wait

# 5. Check the audit trail
agentpaas audit query
```

## Expected Output

The agent returns something like:

```json
{
  "status": "OK",
  "city": "Folsom",
  "conditions": "Currently in Folsom it's 90°F (32°C) and sunny.
  Humidity is 21% with a light SW breeze at 8 mph."
}
```

The exact answer varies — the LLM generates it from the live weather data.

## Verify Real Data

Cross-check the agent's output against wttr.in directly:

```bash
curl -s "https://wttr.in/Folsom?format=j1" | python3 -c "
import json, sys
d = json.load(sys.stdin)
cur = d['current_condition'][0]
print(f'{cur[\"temp_F\"]}F, {cur[\"weatherDesc\"][0][\"value\"]}, {cur[\"humidity\"]}% humidity')
"
```

The agent's response should match these real values.

## Try Variations

```bash
# Different city
agentpaas trigger invoke weather-agent --payload '{"city": "Tokyo"}' --wait

# Different question style
agentpaas trigger invoke weather-agent --payload '{"city": "Paris"}' --wait
```

## Troubleshooting

See the [main README troubleshooting section](../README.md#troubleshooting)
and the [manual testing guide](../docs/manual-testing.md).
