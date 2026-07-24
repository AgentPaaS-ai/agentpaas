# Compaction Checkpoint — Golden Dataset + Context Engineering, Session 2026-07-05

## Key Decisions

1. Built golden dataset regression suite (test/golden/) — 43 tasks, 3 tiers, pass^k measurement
2. Added plugin-built marker (.agentpaas-built-via) to plugin's agentpaas_pack tool
3. Added installed-state reference checker (scripts/verify-installed-state.py)
4. Created context-engineering skill (hermes-agent/context-engineering)
5. Pruned memory from 96% → 36%, user profile from 99% → 50%
6. Created golden-dataset-regression skill documenting the system

## Files Changed

- test/golden/golden_tasks.yaml — 45 tasks (G01-G45), 3 tiers, sourced from real failures
- test/golden/runner.go — Runner with pass^k, tiered execution, regression thresholds
- test/golden/graders.go — Fast-tier graders (18 tasks: G01-G16, G44-G45)
- test/golden/docker_graders.go — Slow + docker tier graders (27 tasks)
- test/golden/dataset.go — YAML loader + validator + stats
- test/golden/golden_test.go — Test entry point (TestGoldenSuite)
- Makefile — golden-fast, golden-slow, golden-docker, golden-eval targets
- integrations/hermes-plugin/tools.py — _write_build_marker() in agentpaas_pack
- scripts/verify-installed-state.py — 11-check filesystem reference state checker
- .hermes/profiles/agentpaas/skills/hermes-agent/context-engineering/SKILL.md — new skill
- .hermes/profiles/agentpaas/skills/agentpaas/golden-dataset-regression/SKILL.md — new skill
- .hermes/profiles/agentpaas/skills/software-development/agentpaas-build-rhythm/references/compaction-checkpoint.md — new reference

## Commits

- 3b7026f: Add golden dataset regression suite with pass^k measurement
- 8320fd8: Add plugin-built marker + installed-state verification to golden dataset

## Current State

- Fast tier: 18/18 tasks PASS at pass^k=3 (2.8s total runtime)
- Slow tier: 9 tasks, graders implemented but not verified (needs built binary + Docker)
- Docker tier: 15 tasks, graders implemented but not verified (needs Docker + running daemon)
- Plugin marker: implemented, verified via G45 golden task
- Installed-state checker: 11/11 checks pass on active agentpaas profile
- Memory: 36% (803/2200 chars) — healthy
- User profile: 50% (698/1375 chars) — healthy

## Next Steps (Priority #3: Spec-Driven Development)

1. Write a spec.md template skill — for each block, formal spec before code
2. The spec is the source of truth; code is generated from it
3. When code diverges from spec, update the spec first
4. Add "spec drift" check to risk analysis
5. Use spec acceptance criteria as input to golden dataset tasks

## Open Questions

- Slow + docker tier golden tasks need end-to-end verification (need Docker running)
- G41-G43 (LLM-as-judge tasks) not implemented yet — need deepseek-v4-flash integration
- Should we add a GitHub Actions workflow for golden-fast as a CI gate?
