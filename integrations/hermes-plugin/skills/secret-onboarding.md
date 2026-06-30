---
name: agentpaas-secret-onboarding
description: Guide users through adding credentials to AgentPaaS Keychain
version: 1.0.0
---

# AgentPaaS Secret Onboarding

## When to Use

- User needs to add an API key for an agent (OpenAI, Anthropic, xAI, weather, etc.)
- User needs to validate a credential before deployment
- User needs to rotate or remove a credential

## Adding a Credential

1. Ask the user for the credential name (e.g. "openai-api-key")
2. Ask the user for the credential value (API key)
3. Call `agentpaas_secret_add` with name and value
4. Verify it was stored: call `agentpaas_secret_list`

## Validating a Credential (Pre-Deployment)

Before packaging an agent that uses a credential, ALWAYS validate it:

1. Call `agentpaas_secret_test` with the credential name
2. If it fails, tell the user the error and ask them to re-add the credential
3. Only proceed to `agentpaas_pack` after all credentials pass validation

This catches:
- Wrong API key (provider returns 401)
- Provider unreachable (network error)
- Key with insufficient permissions

## Rotating a Credential

1. Ask the user for the new value
2. Call `agentpaas_secret_rotate` with name and new value
3. Call `agentpaas_secret_test` to verify the new value works

## Removing a Credential

1. Call `agentpaas_secret_remove` with the name
2. Verify it's gone: call `agentpaas_secret_list`

## Security Rules

- NEVER print or log the secret value
- NEVER pass the secret value as a command-line argument (it goes through stdin)
- The `agentpaas_secret_list` command shows labels only, never values
- If a credential test fails, do NOT show the key in the error message
- Credentials are stored in macOS Keychain, scoped to the AGENTPAAS_HOME namespace

## Credential Naming Convention

Use descriptive names with provider prefix:
- `openai-api-key` — for OpenAI
- `anthropic-api-key` — for Anthropic
- `xai-api-key` — for xAI
- `openweather-api-key` — for OpenWeatherMap
- `stripe-secret-key` — for Stripe

The `secret test` command auto-detects the provider from the name. You can
override with the `provider` parameter: `openai`, `anthropic`, `xiai`.