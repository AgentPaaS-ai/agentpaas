# Troubleshooting

## `agentpaas` killed with exit 137 after brew install

Cask is not notarized. Clear quarantine on all three binaries before any
command:

```bash
xattr -cr /opt/homebrew/bin/agentpaas /opt/homebrew/bin/agentpaasd /opt/homebrew/bin/agentpaas-harness-linux
```

## Daemon won't start (checkpoint key error)

After upgrades or a clean state reset:

```bash
rm -f ~/.agentpaas/state/audit-checkpoint-key.der
agentpaas daemon start
```

## No `agentpaas_*` tools in Hermes

Toolset not registered:

```bash
python3 ~/.hermes/profiles/<profile>/plugins/agentpaas/scripts/ensure-toolset.py <profile>
```

Then `/quit` and `hermes -p <profile>`.

## Pack fails: "agentpaas-sdk was not found"

The SDK is injected automatically. Do not list `agentpaas-sdk` in
requirements.txt. Only list the agent's own deps.

## Agent returns "agentpaas fake llm response"

LLM is not configured. Set the `llm:` block in agent.yaml, or ask Hermes
to configure it.

## Agent fails: "credential is not declared"

Credentials must appear in policy.yaml, not only in Keychain:

```yaml
credentials:
  - id: my-api-key
    type: header
    header: Authorization
```

More cases: [manual testing guide](manual-testing.md).
