# AgentPaaS Checkpoint

Date/time: 2026-06-12 00:03:18 PDT
Workspace: `/Users/pms88/projects/agentpaas`

## Purpose

This checkpoint captures the Block 15 sequencing review. The review converted
the old rough sequencing note into a founder calendar and execution-control
block, renumbered the execution plan into 15 blocks, and aligned the PRD with
the aggressive P1/P2 timing.

The work remained in planning/spec review mode; no implementation code has
been built yet.

Primary files edited:
- `/Users/pms88/projects/agentpaas/agentpaas-execution-plan-v1.md`
- `/Users/pms88/projects/agentpaas/agentpaas-prd-v4-master.md`
- `/Users/pms88/projects/agentpaas/checkpoints/agentpaas-checkpoint-2026-06-12_00-03-18_PDT.md`

Latest committed checkpoint entering this review:
- `249c23f docs: close block 13 release scope review`

## Block 15 Review Outcome

Block 15 is now the founder calendar and execution-control block. It is not a
product feature; it governs build order, parallelism, timing, and what cannot
be cut once implementation starts.

Decisions locked:
- The execution plan now has 15 blocks: 14 product/release build blocks plus
  Block 15 as the calendar/control block.
- Former Block 10.5 is now Block 11: Agentic operator contract.
- Former Block 11 is now Block 12: P1 red-team smoke gate.
- Former Block 12 is now Block 13: MCP server + Claude Code plugin + Hermes
  skill.
- Former Block 13 is now Block 14: install path, docs, demo, and v0.1.0
  release.
- P1 target is week 4/5.
- P2 customer-facing release track target is four additional weeks after P1.
- Once implementation starts, P1 blocks do not silently slip. If the calendar
  becomes impossible, stop and explicitly rescope before continuing.
- No Block 13 integration items are skipped for P1.
- Extra demo recordings beyond the minimum launch video are launch-asset
  prioritization, not skipped product functionality.
- CrewAI is an input framework, not an AgentPaaS product feature. P1 support
  means generated CrewAI Python projects pack/run through the generic Python
  harness; AgentPaaS does not build CrewAI authoring, orchestration, or a
  custom CrewAI adapter in P1.
- Node SDK remains deferred and is not part of the P1 gate.

## Founder Calendar

P1 rough founder calendar:
- **Week 1:** Blocks 1-3 green. Repo/protos/CI, daemon/CLI skeleton, identity
  and audit spine.
- **Week 2:** Blocks 4-8 green. Policy compiler, macOS Docker Desktop/Colima
  fenced runtime, harness/Python SDK, secrets broker, packaging.
- **Week 3:** Blocks 9-12 green. Trigger API/events/cron, dashboard, operator
  contract, redteam-smoke.
- **Week 4:** Blocks 13-14 green. MCP server, Claude Code plugin, Hermes
  native MCP skill, install/docs/demo/release path.
- **Week 5:** P1 release buffer only: bug fixes, volunteer clean-machine
  verification, offline bundle verification, video/asciinema polish, v0.1.0
  tag.

P2 rough calendar:
- **Week 6:** Linux certification track.
- **Week 7:** Customer-facing control-plane foundations.
- **Week 8:** Commercial observability and opt-in telemetry.
- **Week 9:** P2 customer release hardening.

## PRD Alignment

The PRD now mirrors the Block 15 decisions:
- GTM sequence uses Weeks 1-4/5 for P1 and Weeks 6-9 for P2.
- P1 is described as macOS-first OSS/demo delivery, not the full
  customer-facing release.
- P2 is described as the customer-facing release track.
- The MCP integration reference points to Block 13 and uses `agentpaas_*` tool
  names.
- CrewAI is described as a generated Python input shape handled by the generic
  Python harness, not a custom adapter.
- Phase 1 success criteria require a CrewAI-generated Python project, a
  LangGraph project, and a plain-Python agent to pack/run through the generic
  Python harness.

## What Is Left

Planning/spec work remaining before implementation:
1. Decide whether to start Block 1 immediately or do one final whole-plan
   consistency pass.
2. Create/confirm the GitHub repo and Project once the private repo exists.
3. Convert the 15 blocks into implementation issues with acceptance criteria.
4. Confirm local development prerequisites for P1 execution: macOS host,
   Docker Desktop and/or Colima, Homebrew, cosign, Go toolchain, and browser
   test tooling.

Implementation work remaining:
1. Build Blocks 1-14 in order under Block 15's founder calendar.
2. Keep every block gated by its Makefile command and negative/security tests.
3. Produce P1 release artifacts, docs, demos, offline bundle, volunteer
   clean-machine validation, and v0.1.0 tag.
4. Begin P2 only after P1 ships or is explicitly rescoped.

## How To Continue In A Fresh Session

Paste this:

"Continue from the AgentPaaS checkpoint dated 2026-06-12 00:03:18 PDT. We
finished the planning/spec block review. Git is initialized and checkpoints
are committed. Latest commit should be the checkpoint closeout after Block 15.
P1 is macOS-first OSS/demo delivery with zero telemetry and a week 4/5 target;
P2 is the first customer-facing release track over the following four weeks.
Next, decide whether to start Block 1 implementation immediately or run one
final whole-plan consistency pass."

## Verification Notes

No code tests were run because this session only edited markdown planning
documents. `git diff --check` passed before this checkpoint was created.
