# Block 13 — Hermes Integration Plugin

**Status:** COMPLETE — `make block13-gate` passes
**Gate:** make block13-gate
**Date:** 2026-06-25
**Block scope:** Hermes integration plugin, e2e governance verification, demo matrix fixtures

## Scope

Block 13 delivers the Hermes integration plugin (plugin.yaml + 18 tool handlers + schemas + tests),
solves the e2e deploy/invoke gap (BUG 7d — daemon now auto-invokes agents via docker exec),
and establishes the full deploy → govern → audit lifecycle with real Docker containers on
isolated networks.

## Subtasks Completed

| Task | Title | Status | Key Findings |
|------|-------|--------|-------------|
| T01 | Hermes Plugin Skeleton and Tool Manifest | MERGED (0dd8322) | 18 tools registered, 19/19 tests pass. Adversary: 3 breaks (args=None crash, AGENTPAAS_CLI injection, JSONDecodeError) — all fixed. |
| T02 | Reconcile Agent from Source Code | MERGED | pack.InitFromCode() + reconcile CLI. Path validation rejects symlinks/traversal. |
| T03 | Policy Init and Validation | MERGED | policy init with 4 templates (deny-all, allow-http, allow-llm, allow-mcp). Pack-time validation. |
| T04 | E2E Deploy Acceptance (BUG 7d) | MERGED (13e489c) | Added Docker Exec to runtime. Daemon auto-invokes agent after container start. Full lifecycle: pack→run→invoke→audit. |
| daemon-fix | Nil-pointer panic in runDaemonStart() | MERGED (247181c) | Replaced time.Sleep+ProcessState.Exited() with goroutine+select. Fixed env var overwrite bug. |
| T05-T09 | Slash commands, SKILL.md, demo fixtures, gate | MERGED | /agentpaas-* slash commands, after-install.md, demo agents, block13-gate. |

## Block-End Verification

VERIFY PASS:
- block13-gate: build + test + lint all clean
- Python plugin tests: 109+ passing
- Docker e2e: pack→run→invoke→stop→audit chain verified with real containers
- Audit chain hash-chained and verified: pack, run_start, egress_denied, run_stop
- The invoke gap (BUG 7d) was the final blocker — solved by adding Exec to RuntimeDriver
  interface and auto-invoking via docker exec after polling /readyz

## Key Architecture Decisions

1. **Hermes Plugin, not MCP Server** — Single plugin wraps 17+ Go CLI tools as Hermes
   tool handlers. MCP server deferred to P2.
2. **Local-first mode** — No GitHub issues/PRs mid-build. Merge locally, checkpoint push at block end.
3. **ctx.dispatch_tool** — Plugin uses Hermes dispatch mechanism, not direct CLI calls for cross-tool workflows.
4. **No MCP in P1** — MCP support is P2. P1 is Hermes-only.

## Risk Analysis Summary

**HIGH RISK identified:** No gateway container — egress "denial" was network isolation, not
policy enforcement. The daemon created internal-only Docker networks but had no gateway
sidecar or policy enforcement layer. This was the single biggest gap between product vision
and implementation. (Resolved in Block 14B.)

**Other risks:**
- Docker endpoint resolution didn't read Docker context store (Colima) — fixed with internal/dockerclient
- Harness binary not found by daemon — fixed with resolveHarnessBinary()
- Private key parse failure (SEC1 vs PKCS8) — fixed with fallback parser
- cosign --tlog-upload=false deprecated — fixed with signing-config JSON

## Commits

- 0dd8322: T01 plugin skeleton merged
- 13e489c: BUG 7d fix — docker Exec + auto-invoke
- 247181c: daemon startup nil-pointer fix
- Multiple T02-T09 commits for slash commands, SKILL.md, demo fixtures

## Full Details

- [Decisions and Learnings](../archive/session-history/b13-session-history.md) — 24KB of architecture decisions, bug series, and build log
- [Risk Analysis](../archive/session-history/b13-session-history.md) — 13KB risk analysis
- [OWA Records](../archive/owa-records/b13-owa-records.md) — Per-subtask worker/adversary/verifier records
