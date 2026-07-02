# Secrets & Credential Handling

AgentPaaS uses a **brokered credential model** — raw secrets never reach the
agent container. The gateway sidecar mediates all credential access on the
agent's behalf.

## Architecture

- **Gateway sidecar injection.** Credentials are injected into the gateway
  sidecar, not the agent container. When an agent makes an API call that
  requires authentication, the gateway attaches the appropriate credential to
  the outbound request.
- **macOS Keychain storage.** On the host, credentials are stored in the macOS
  Keychain via the `security` framework. The daemon reads them at startup and
  passes them to the gateway over a local Unix socket.
- **No env passthrough.** Raw secrets never appear in container environment
  variables or the container filesystem. The agent code has no path to
  accidentally log, echo, or exfiltrate a secret.

## Policy Controls

The policy file (`policy.yaml`) declares which credentials an agentpaas $1 may
access. The gateway enforces this: if a credential is not listed in the policy
for that run, the gateway refuses to attach it to any outbound request. This
gives you fine-grained control — a weather-agent gets the OpenWeather API key,
not your GitHub token.

## Audit Trail

Every credential access is recorded in the signed audit chain:
- Which credential was used (by label, never by value)
- Which outbound request it was attached to
- Whether the policy allowed or denied the access
- Timestamp and run identifier

This gives you a complete, verifiable log of how your secrets were used during
each agentpaas $1.