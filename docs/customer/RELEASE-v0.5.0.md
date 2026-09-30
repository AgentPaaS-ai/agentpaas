# AgentPaaS v0.5.0 release notes

The OSS repository has tag `v0.5.0`. These notes cover the local CLI, daemon, harness, policy compiler, and Hermes path shipped at that tag. AgentPaaS Cloud has no matching public release tag, so Cloud behavior belongs in the Cloud documentation release notes.

## Added

- LLM budgets in `policy.yaml` through `llm_budget.max_tokens`, `llm_budget.max_tokens_per_request`, and `llm_budget.max_cost_usd`.
- PII guardrails through `guardrails.pii`, with `mask` and `reject` actions, five built-in detector names, and custom patterns.
- Policy inspection through `agentpaas policy show` and `agentpaas policy validate`.
- Policy lineage output for the compiled contract.

The built-in PII detector names are:

```text
Email
Ssn
DriversLicense
CreditCard
Key
```

Phone numbers use a custom pattern in this release. There is no `Phone` built-in.

## Changed

- `agentpaas pack` carries an LLM credential reference from `agent.yaml` into the compiled signed policy. The secret value is not copied into the bundle or container.
- PII inspection buffers a stream or fails closed when a policy requires inspection.
- The documented customer path remains Hermes plus the AgentPaaS CLI. Other host integrations are customer customization paths, not verified 0.5.0 integrations.

## Fixed

- LLM completion handling remains on the documented Hermes and CLI path.
- Policy inspection and pack use the same LLM credential compilation behavior.
- The CLI persists the last successful Cloud API URL for later commands. `AGENTPAAS_CLOUD_API_URL` still takes precedence, and logout clears the saved value.

## Security

- PII values are masked before the audit event is persisted when `action: mask` is active.
- PII detector, audit, and budget failures fail closed.
- The preview vault remains a preview backend. This release does not make OpenBao-grade isolation claims.

## Known issues

- Host-specific guides for Cursor, Claude Code, and Grok Build are not part of this release. Hermes is the documented host.
- A provider-specific Nous Research OAuth configuration is not published until the production token endpoint, client registration, and scopes are verified. The generic `oauth_llm` schema is documented in the Cloud docs.

## How to use the new policy inspection commands

```bash
agentpaas policy show
agentpaas policy validate
```

## Links

- [Cloud v0.5.0 release notes](https://docs.agentpaas.ai/releases/v0.5.0)
- [Policy reference](../policy-reference.md)
- [Quickstart](../quickstart.md)
- [Known limitations](../known-limitations.md)
