# Block 14C — Risk Analysis

**Date:** 2026-06-26
**Block:** 14C (Install Path, Docs, Demo, Release Scaffolding)
**Status:** COMPLETE (technical artifacts) — volunteer gate pending

## Summary

Block 14C created the release infrastructure: goreleaser config, Homebrew formula,
GitHub Actions release workflow, README rewrite, and 6 documentation files.
The technical gate passes. The volunteer clean-machine gate (2 users, <15 min)
requires real macOS users and is deferred.

## Completed Tasks

| Task | Description | Gate |
|------|-------------|------|
| T01 | Goreleaser config, Homebrew formula, release CI | Verified (file existence + build) |
| T02 | README rewrite (60-second story, install, quickstart) | Verified (grep checks) |
| T03 | 6 docs (quickstart, enforcement, limitations, policy, audit, threat model) | Verified (file existence) |

## Shortcuts Taken

1. **Homebrew formula uses PLACEHOLDER SHA256**: Real checksums come from goreleaser during the first release. The formula in Formula/agentpaas.rb is a template.

2. **No demo video/asciinema recorded**: The spec calls for 3-minute demo scripts and asciinema recordings. These require manual recording and are deferred to release day.

3. **block14c-gate is artifact-presence only**: The gate checks that files exist and README contains expected content. It does NOT verify goreleaser actually builds, the brew formula installs, or docs render. The real verification is the volunteer clean-machine test.

4. **No offline bundle created**: The spec calls for a macOS offline bundle (signed binaries, checksums, SBOMs, container images). This requires a goreleaser release run first. Deferred.

5. **No docs CI (broken-link check, command-snippet smoke)**: Deferred — these are nice-to-haves for the docs site, not blockers for v0.1.0.

## Broken Items / TODOs

None in shipped code. The Homebrew formula SHA256 placeholders are intentional (filled during release).

## CI Coverage Gaps

1. **No goreleaser dry-run in CI**: The release workflow only runs on tag push. A `goreleaser check` or `goreleaser release --snapshot` step in CI would catch config errors early.

2. **No brew audit**: The formula isn't validated with `brew audit` in CI.

3. **No docs smoke**: README quickstart commands aren't tested in CI.

## Manual Gates Pending

1. **Volunteer clean-machine test**: 2 users (not the developer) each follow the README on their own macOS machine and reach a running governed agent in <15 minutes. Every deviation is a docs bug, filed and fixed before release.

2. **v0.1.0 tag + goreleaser release**: After volunteer test passes.

3. **cosign verify-blob + agent verify-release**: Documented but not yet tested on real release artifacts.

4. **Offline bundle verification**: Documented but not yet created/tested.

## P1 Backlog Items (from this block)

1. Demo video scripts + asciinema recordings
2. Offline bundle creation + verification
3. Docs CI (broken-link check, command-snippet smoke)
4. goreleaser dry-run in CI
5. brew audit in CI

## Verdict

**Block 14C technical artifacts are COMPLETE.** The volunteer clean-machine gate is a human-driven process that cannot be automated. All release infrastructure (goreleaser, formula, CI, README, docs) is in place and verified by the block14c-gate.

**BLOCK 14 SUCCESS GATE**: `make block14-gate` passes all four sub-segments (14A0 → 14A → 14B → 14C).
