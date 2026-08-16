# Hermes plugin setup

1. Install Hermes: https://hermes-agent.nousresearch.com/docs
2. In Hermes paste:  
   `Install from https://github.com/AgentPaaS-ai/agentpaas`
3. If tools do not appear: type `/quit` and reopen Hermes.
4. Confirm tools exist (doctor / agentpaas tools).

Hermes must **not** block on cloud login. You run login in a normal terminal.

To compose more than one agent, tell Hermes:

`Build a support workflow. Classify the ticket as refund, escalate, or close, then run only the matching specialist.`

## Route on a signed menu (`kind: "choice"`)

A choice stage does not run an agent. After a classifier stage succeeds, the platform reads one declared key from that committed result and starts exactly one already-signed child workflow from the menu you published.

- **Signed.** Routes are part of the workflow you created. The classifier cannot add a branch.
- **Sandboxed.** The chosen child is its own workflow, with its own isolation.
- **Policy-controlled.** Only listed child workflows can start. An unknown, missing, or non-string value ends the run. There is no default branch and no human override.
- **Audited.** A match records the value and the child that started. A miss records the value and the allowed set.

Create the branch workflows first, then the parent. Classify as refund, escalate, or close — anything else fails closed.
