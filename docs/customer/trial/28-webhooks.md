# Webhooks

Three webhooks. The agent never POSTs them. The AgentPaaS control plane does. Configure them in Hermes or via the API. The 0.4.0 brew cask does not add a `cloud webhook` command.

## Ingress

A signed POST to AgentPaaS starts a run. Bad signature is 401 and creates no run. This starts work. It does not mean the job finished.

## Completion

When the run reaches a terminal state, AgentPaaS POSTs a receipt to your HTTPS URL: run id and status. HMAC header present.

## Delivery

When the run succeeds, AgentPaaS POSTs declared final_output only. No logs, no secret values, no artifact dump.

Destination must be public HTTPS. Do not use your laptop as the destination. Do not paste webhook secrets into Hermes chat.
