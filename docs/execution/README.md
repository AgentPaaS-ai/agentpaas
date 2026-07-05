# AgentPaaS Execution Documentation

Consolidated build docs for the AgentPaaS project. 220 scattered files
reorganized into 22 active + 27 archive files for fast LLM navigation.

## Quick Start (for a new session)

1. **What are we building?** → `planning/prd-v4-master.md` (the WHY)
2. **How are we building it?** → `planning/execution-plan-v1.md` (the HOW)
3. **Where did we leave off?** → `resume-prompts/b16-resume-prompt.md` (latest)
4. **What happened in each block?** → `blocks/b*-summary.md` (12 files)
5. **What remains?** → `../roadmap.md` (in parent docs/ directory)

## Directory Structure

```
docs/execution/
├── README.md                    ← You are here. Index + navigation guide.
│
├── planning/                    (3 files — the source of truth)
│   ├── prd-v4-master.md         Product spec: what, why, for whom
│   ├── execution-plan-v1.md     Build plan: block-by-block tasks, tests, gates
│   └── subtask-decomposition-v1.md  PR-sized task breakdown with acceptance criteria
│
├── blocks/                      (12 files — one per block, concise)
│   ├── b6-summary.md            Harness HTTP lifecycle, budget, PID 1, SDK, failures
│   ├── b7-summary.md            Egress gateway, DNS, network isolation, topology
│   ├── b8-summary.md            Pack pipeline, Docker build, signed images, SBOM
│   ├── b9-summary.md             Trigger API, events, webhooks, cron
│   ├── b10-summary.md           OTLP observability, SQLite store, redaction
│   ├── b11-summary.md            Operator contract: 17 CLI commands, path validation
│   ├── b12-summary.md           P1 red-team smoke gate
│   ├── b13-summary.md           Hermes plugin, e2e deploy, BUG 7d (auto-invoke)
│   ├── b14-summary.md           Security remediation, gateway, policy, release gate
│   ├── b15-summary.md           Credentials, LLM, policy authoring, production hardening
│   └── b16-summary.md           Manual testing, bug fixes, open-source prep
│
├── resume-prompts/              (4 files — latest state per phase)
│   ├── b13-resume-prompt.md     Block 13 resume (latest: turn 40)
│   ├── b14-resume-prompt.md     Block 14 resume (latest: B14E)
│   ├── b15-resume-prompt.md     Block 15 resume (latest: session 07)
│   └── b16-resume-prompt.md     Block 16 resume (latest: session 6 — MOST RECENT)
│
├── reference/                   (3 files — operational guidance)
│   ├── credential-onboarding.md How to store/test/rotate API keys in Keychain
│   ├── enterprise-follow-up.md P2 enterprise patterns (managed vault, remote broker)
│   └── e2e-test-plan.md         Manual lifecycle test cases LC-01..LC-05
│
└── archive/                     (historical — not needed for active work)
    ├── owa-records/             (8 files — per-subtask worker/adversary/verifier records)
    │   ├── b6-owa-records.md    All B6 subtask OWA records consolidated
    │   ├── b7-owa-records.md    All B7+B7m subtask OWA records consolidated
    │   ├── b8-owa-records.md    ...through b13
    │   └── ...
    │
    ├── session-history/         (4 files — all checkpoints, resume prompts, worker prompts)
    │   ├── b13-session-history.md  Deploy e2e checkpoints v1-v5, turn snapshots, build prompts
    │   ├── b14-session-history.md  Sub-segment checkpoints + 50+ worker dispatch prompts
    │   ├── b15-session-history.md  Checkpoints 01-05, resume prompts 02-06, MC worker prompts
    │   └── b16-session-history.md  Fix round resume prompts, session 3-5 resume prompts
    │
    ├── pre-build-planning.md    14 early planning checkpoints (June 11-12, 2026)
    ├── owa-research/             OWA skill package (SKILL.md + references + templates)
    ├── owa-templates.md         Issue + PR templates for OWA workflow
    ├── owa-worker-prompts.md     Codex OWA worker prompts (remote + local mode)
    ├── audit-remediation-2026-06-18.md  Plan audit findings + fixes
    ├── b13-cosign-coverage-fix.md  Cosign signing coverage gap plan
    └── paper-additions-owa-metrics.md  Research paper notes on OWA metrics
```

## Navigation Guide

### "I'm starting a new session — what's the state?"

1. Read this README for orientation
2. Read `resume-prompts/b16-resume-prompt.md` (most recent resume prompt)
3. Read `blocks/b16-summary.md` (what was last completed)
4. Read `../roadmap.md` (what remains to be done)

### "I need to understand the product vision"

→ `planning/prd-v4-master.md` — 77KB, covers product definition, personas,
wedge, security model, and the full P1/P2 scope.

### "I need to find what a specific block delivered"

→ `blocks/bN-summary.md` — concise summary with subtask table, verification
results, risk analysis, and key commits.

### "I need the detailed OWA record for a specific subtask"

→ `archive/owa-records/bN-owa-records.md` — full worker/adversary/verifier
records for every subtask in the block, with TOC.

### "I need to understand a specific bug or decision"

→ `archive/session-history/bN-session-history.md` — all checkpoints, resume
prompts, and worker dispatch prompts in chronological order.

### "I need to set up credentials for an LLM provider"

→ `reference/credential-onboarding.md` — Keychain commands, provider
adapters, validation flow.

### "I need the execution plan for a specific block"

→ `planning/execution-plan-v1.md` — search for "BLOCK N" to find the
task specs, test plans, and success gates.

### "I need the subtask decomposition (PR-sized work items)"

→ `planning/subtask-decomposition-v1.md` — 93 tasks with acceptance
criteria, file scopes, and gate commands.

## File Counts

| Location | Files | Purpose |
|----------|-------|---------|
| planning/ | 3 | Source of truth (PRD, execution plan, decomposition) |
| blocks/ | 12 | Concise per-block summaries |
| resume-prompts/ | 4 | Latest state per build phase |
| reference/ | 3 | Operational guidance |
| archive/owa-records/ | 8 | Consolidated OWA records (from 63 files) |
| archive/session-history/ | 4 | Consolidated session history (from ~100 files) |
| archive/ (other) | 15 | Research, templates, standalone docs |
| **Total active** | **22** | What an LLM reads on restart |
| **Total archive** | **27** | Historical reference |
| **Grand total** | **49** | Down from 220 scattered files |
