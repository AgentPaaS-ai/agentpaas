# Workflow kinds and edges

Use this page when you already know you need more than one step. A single agent is none of these: deploy it and invoke it.

## Linear

Default. A then B. A writes a work order and exits. B runs. A does not need to stay up.

## Fan-out

N copies of one child workflow. `join: all` only. Parent result must contain each child's answer, not only ids. Join-any is not shipped.

## Choice

Closed menu. Routes map a string to a child workflow. No default route. If the value is missing, not a string, or not declared, the run fails closed. The classifier cannot invent a branch. Create the branch workflows first.

## Phone-call

Only when a living A is required. A stays up and pays. A may call only the signed callee list. A call outside the list is denied. Stop A cancels that A's children. Depth is 1. Standalone A cannot call agents. Mixing choice or fan-out with callees is rejected.

## Not in 0.4

Native HITL, for-each, wait/delay, join-any, spawn deeper than 1, in-envelope stage jumps.
