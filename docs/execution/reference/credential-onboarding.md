# AgentPaaS Credential Onboarding

## Overview

AgentPaaS stores credentials (API keys, tokens) in the macOS Keychain. The
`agentpaas secret` commands manage these credentials. Credentials are never
written to disk in plaintext, never appear in container environment variables,
and are never logged.

## Keychain Service Name Convention

Each AgentPaaS home directory (`AGENTPAAS_HOME`) has its own Keychain
namespace. The service name is derived from the home directory path:

```
ai.agentpaas.secrets.<sha256(homeDir)[:8]>
```

For example, if `AGENTPAAS_HOME` is `~/.agentpaas`, the service name is
`ai.agentpaas.secrets.<first 8 hex chars of sha256("~/.agentpaas")>`.

This means:
- Each AgentPaaS profile/installation has its own isolated credential set
- Credentials from one home directory are not visible to another
- The `agentpaas secret` commands automatically use the correct service name

### Verifying the Service Name

To see the service name for your installation:

```bash
# From Go (if you have the repo):
go run ./cmd/agentpaas secret list
# The service name is used internally; you can verify a specific key with:
security find-generic-password -s "ai.agentpaas.secrets.<hash>" -a <secret-name>
```

## Commands

### `agentpaas secret add <name>`

Stores a credential in macOS Keychain. Reads the value from stdin (not argv,
to avoid leaking it in the process list).

```bash
echo "sk-..." | agentpaas secret add openai-api-key
# Or interactively (will prompt):
agentpaas secret add openai-api-key
```

Alias: `secret set` (backward compat).

### `agentpaas secret list`

Lists stored credentials by label. Never shows the value.

```bash
agentpaas secret list
# Output:
# NAME                CREATED_AT          UPDATED_AT          LAST_USED_AT        REFERENCED_BY
# openai-api-key      2026-06-30 14:00   2026-06-30 14:00   2026-06-30 14:05   -
```

### `agentpaas secret remove <name>`

Deletes a credential from Keychain.

```bash
agentpaas secret remove openai-api-key
```

Alias: `secret rm` (backward compat).

### `agentpaas secret rotate <name>`

Replaces a credential with a new value from stdin. Atomic: if the new value
fails validation, the old value is preserved unchanged.

```bash
echo "sk-new..." | agentpaas secret rotate openai-api-key
```

### `agentpaas secret test <name> [--provider <provider>]`

Validates a credential by making a trivial authenticated HTTP call to the
provider, OUTSIDE the container, before pack/run. Fail fast with a clear error
if the key is wrong, the provider is unreachable, or the destination is not
recognized.

```bash
agentpaas secret test openai-api-key
# secret "openai-api-key": openai test OK (https://api.openai.com/v1/chat/completions, HTTP 200)

agentpaas secret test openai-api-key --provider anthropic
# secret "openai-api-key": anthropic test OK (https://api.anthropic.com/v1/messages, HTTP 200)
```

Supported providers:
- `openai` — POST to api.openai.com/v1/chat/completions with "say OK"
- `anthropic` — POST to api.anthropic.com/v1/messages with "say OK"
- `xiai` — POST to api.x.ai/v1/chat/completions with "say OK"

If `--provider` is omitted, the provider is auto-detected from the secret name:
- Names containing "openai" or "gpt" → openai
- Names containing "anthropic" or "claude" → anthropic
- Names containing "xai" or "grok" → xiai
- Default → openai

## Security Rules

1. **Secret values never appear in:**
   - Container environment variables
   - Daemon logs
   - Audit trail (only "injected" events, never the value)
   - CLI stdout/stderr (list shows labels only)
   - Process arguments (values go through stdin, not argv)

2. **Secret names** must not contain whitespace, control characters, or
   invisible format characters (enforced by `secrets.ValidateSecretName`).
   Max length: 64KB (`secrets.MaxSecretValueSize`).

3. **Credential access** is brokered. The agent never reads the Keychain
   directly. The secrets broker (`internal/secrets/broker.go`) injects
   credentials as HTTP headers at call time, with revocation checking and
   audit events.

## Credential Naming Convention

Use descriptive names with provider prefix:
- `openai-api-key` — OpenAI
- `anthropic-api-key` — Anthropic
- `xai-api-key` — xAI (Grok)
- `openweather-api-key` — OpenWeatherMap
- `stripe-secret-key` — Stripe
- `github-token` — GitHub API

The `secret test` command auto-detects the provider from the name. Override
with `--provider` if the name doesn't match the provider.

## Policy Binding

Credentials are bound to egress rules in `policy.yaml`:

```yaml
credentials:
  - id: "openai-api-key"
    type: brokered
    header: "Authorization"

egress:
  - domain: "api.openai.com"
    ports: [443]
    methods: ["POST"]
    credential: "openai-api-key"
```

The secrets broker resolves the credential ID to the Keychain secret at call
time, injects it as the configured header (default: Authorization), and
records an audit event.