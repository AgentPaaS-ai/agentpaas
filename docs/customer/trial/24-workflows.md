# Workflows

A workflow is a signed recipe. It names components you already packed. It is not a deployment and it does not take a slot.

Ready means every member component is already deployed. That status lives on the Workflows card. There is no deploy-the-workflow object.

## Path

1. In Hermes, describe the job. Hermes shows the graph and waits for yes.
2. Create child workflows first, then the parent.
3. Start once.
4. Watch the run under Runs. The signed trail is under Logs.

Weather is not this path. Weather is pack, deploy, invoke. One agent.

## Shapes in 0.4

| Kind | When | Edge |
|------|------|------|
| Linear | A then B | A writes a work order and exits. B runs. |
| Fan-out | N copies of one child | Join all. Parent shows each child answer, not only ids. |
| Choice | Closed menu after a classifier | No default route. Undeclared value fails closed. |
| Phone-call | Living A must stay up | Depth 1. Off-list denied. Stop A cancels those children. |

Not in 0.4: native HITL, join-any, for-each, wait/delay, spawn deeper than 1.

CLI (after brew 0.4.0): `agentpaas cloud workflow create` then `agentpaas cloud workflow start`. Inspect with `workflow instance`.
