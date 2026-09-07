# Troubleshooting

## `agentpaas` killed with exit 137 after brew install

Reinstall with Homebrew so post-install can clear macOS quarantine, then
run `agentpaas doctor`:

```bash
brew tap AgentPaaS-ai/homebrew-tap
brew reinstall agentpaas
agentpaas doctor
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
