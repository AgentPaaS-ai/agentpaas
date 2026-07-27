# B34.5 Scope Lock — residual close (no creep)

**Date:** 2026-07-26  
**Parent tip:** 8eedf9c  

## Tracks (parallel)

| ID | Branch | Deliverable |
|----|--------|-------------|
| A | feat/b345-cancel | CancelWorkflow RPC + MCP WorkflowTerminal |
| B | feat/b345-043 | Durable start testability + thorough tests + gap fixes |
| C | feat/b345-pipeline | Register pipeline on admit + RuntimeStageLauncher when Docker; skip startDurableRun for pipeline |

## Explicit non-goals

- B35 child spawn
- Pause/resume/restart RPC enable (unless required by cancel)
- Full multi-agent packed product demo with LLMs
- GitHub Actions (local CI only)
- Flipping pipeline default-on

## Merge order

A → B → C (or A∥B then C if makefile conflict). Local: build, scoped lint, race tests, block34-gate, block345-gate, golden-fast.
