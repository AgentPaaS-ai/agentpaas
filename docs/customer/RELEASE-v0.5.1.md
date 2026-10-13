# AgentPaaS v0.5.1 release notes

These notes cover the local CLI, daemon, harness, and policy compiler in AgentPaaS v0.5.1. Cloud behavior is documented in the [Cloud release notes](https://docs.agentpaas.ai/releases/v0.5.1).

## Changed

PII guardrails accept built-in detectors only. Custom patterns are refused. There is no phone-number detection, and there is no `Phone` built-in.

The built-in detector names are:

```text
CreditCard
Ssn
Email
DriversLicense
Key
```

`agentpaas pack` stamps `llm_budget` and `guardrails.pii` into the signed lock. The secret value is not copied into the bundle or container.

## Fixed

- A cost ceiling is refused before egress. When recorded spend reaches `llm_budget.max_cost_usd`, the call stops before an egress allow.
- Model ids that include a provider slash are priced, so the cost ceiling applies to those calls.
- Virtual-model failover runs when the primary model returns HTTP 429 or HTTP 5xx.
- An identical invoke retried while you are at the concurrency limit returns the run that is already waiting. It no longer returns HTTP 500.
- A workflow wait fails with one of these exact errors: `live_call_wait_expired` when the parent time budget ends first, and `seat_wait_timeout` when a child waits too long for a seat.
- The Logs view can filter by event type.
- Reasoning effort is sent on the model call, including `max`.
- A daemon pipeline is registered before invoke returns, and orphan reconciliation no longer stops a live pipeline stage.
- Telemetry egress is hardened. Telemetry host aliases are rejected, workload telemetry environment variables are removed, and a models probe is recorded as egress.

## Security

- Custom PII patterns are refused by policy validate and pack. An agent already deployed with custom patterns fails closed, with no upstream call, until you repack it with 0.5.1.
- The CLI includes Go toolchain and library updates for published security fixes.

## Upgrade notes

The 0.5.0 CLI installed with Homebrew already accepts policy version 1.1 with `llm_budget.max_cost_usd` set to `0.00`. It stamps that lock the same way, and Cloud enforces the cost ceiling the same way. That stamp does not need a repack.

Custom-pattern agents fail closed until you repack with 0.5.1 and use the built-in detectors only.

## Links

- [Cloud v0.5.1 release notes](https://docs.agentpaas.ai/releases/v0.5.1)
- [Cloud v0.5.0 release notes](https://docs.agentpaas.ai/releases/v0.5.0)
- [Policy reference](../policy-reference.md)
- [Quickstart](../quickstart.md)
- [Known limitations](../known-limitations.md)
