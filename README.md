# AgentPaaS

AgentPaaS is a governed, local-first runtime for packaging, running, and
observing AI-generated agents with policy enforcement, auditability, identity,
secrets handling, and a path to Hermes-first operator workflows.

This repository is currently in planning/decomposition mode. The active source
of truth is:

- [agentpaas-prd-v4-master.md](agentpaas-prd-v4-master.md) - product and
  technical definition.
- [agentpaas-execution-plan-v1.md](agentpaas-execution-plan-v1.md) - block by
  block implementation plan and gates.
- [docs/agentpaas-subtask-decomposition-v1.md](docs/agentpaas-subtask-decomposition-v1.md)
  - coding-agent sized issue breakdown for the orchestrator.

## Repository Layout

```text
agentpaas/
├── agentpaas-prd-v4-master.md
├── agentpaas-execution-plan-v1.md
├── docs/
│   ├── agentpaas-subtask-decomposition-v1.md
│   └── archive/
│       ├── checkpoints/
│       ├── landing-page-legacy/
│       └── prd-history/
└── landing-page/
    ├── README.md
    └── index.html
```

## Archive Policy

Superseded PRDs, checkpoints, and unused landing-page generation assets are
kept under `docs/archive/` for provenance. They are not active implementation
inputs unless a task explicitly references them.

## Landing Page

The current static landing page remains in `landing-page/`. See
[landing-page/README.md](landing-page/README.md) for deploy notes.
