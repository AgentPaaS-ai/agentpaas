# Policy Generation

Scaffold a `policy.yaml` from one of four egress templates to govern agent network access.

## When to use
Use when the user needs a `policy.yaml` for their agent project — either a new project or an existing one that lacks policy.

## How to ask the user
Present the four template options with a brief description of each:

| Template   | Description |
|------------|-------------|
| deny-all   | Blocks all network egress (most secure, default) |
| allow-http | Allows general HTTPS outbound (any domain, port 443) |
| allow-llm  | Allows OpenAI, Anthropic, and xAI on port 443 |
| allow-mcp  | Allows local MCP server (localhost, port 8080) |

Ask: **"Which egress policy fits your agent?"**

## After the user chooses

Before calling `agentpaas_policy_init`, identify the exact hostnames that the
chosen template would permit and show them to the user. Ask:

`Approve egress to <hosts>? [y/N]`

Wait for an affirmative answer. Never auto-approve egress. The user approves
every host. If the user has not approved, STOP and ask. Only then call
`agentpaas_policy_init` with the chosen template and project directory.

## Custom egress
If the user has custom domain or port requirements beyond the four templates,
start with `allow-http` as a base, then list the exact resulting hostnames and
ask `Approve egress to <hosts>? [y/N]`. Wait for affirmative approval before
writing or manually editing `policy.yaml`; never auto-approve a custom host.
