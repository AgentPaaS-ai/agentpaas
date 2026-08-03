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

## Providers
- openai: models like gpt-4o, gpt-4o-mini
- anthropic: models like claude-sonnet-4, claude-3-5-sonnet
- xai: models like grok-beta

## Security
- The credential arg is a Keychain secret NAME (label), never the value
- Secret values are never in agent.yaml, container env, or audit trail
