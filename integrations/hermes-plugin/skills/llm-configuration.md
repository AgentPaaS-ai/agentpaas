# LLM Configuration

Configure the LLM provider for an agent by writing the llm: section in agent.yaml.

## Steps

At build/pack time, after the user has chosen the project and LLM provider:

1. Tell the user to run `agentpaas secret add <name>` in a separate
   terminal and paste the key when prompted. Wait for the user to confirm it
   is stored; never ask for or receive the key in this conversation.
2. Validate it works: call `agentpaas_secret_test` with the credential name
3. Configure the agent: call `agentpaas_llm_configure` with project_dir,
   provider, model, and credential

## Providers for the cold weather demo
- openrouter (recommended first): simple API key, `openrouter-key`
- openai: simple API key, `openai-key` (or user label)
- anthropic: simple API key, `anthropic-key` (or user label)

## OpenRouter model defaults (P2-16b)
Do **not** offer stale IDs like `deepseek/deepseek-chat-v3-0324` or
`deepseek/deepseek-r1-0528:free` (they fail). Prefer:
- `deepseek/deepseek-chat` (default cheap)
- `openai/gpt-4o-mini` (cheap OpenAI via OpenRouter)

At most two choices. If the user has no preference, use `deepseek/deepseek-chat`.

Do not offer Nous token-exchange or xAI OAuth in the first-time provider
picker. Keep the list to these 2-3 API-key choices; other providers can be
configured explicitly later.

## Security
- The credential arg is a Keychain secret NAME (label), never the value
- Secret values are never in agent.yaml, container env, or audit trail
