# Block 13 — OWA Records

## Table of Contents

- [B13-T01: Hermes Plugin Skeleton and Tool Manifest](#b13-t01)
- [B13-T02: Schema-Generated Tool Wrappers + Contract-Parity Gate](#b13-t02)
- [B13-T03: Confirmation Protocol Handling](#b13-t03)
- [B13-T04: Prompt-Injection Boundary in Integration Responses](#b13-t04)
- [Daemon Startup Bug Fix (Pre-T05)](#verification) — verification record

---

# B13-T01: Hermes Plugin Skeleton and Tool Manifest

**Block:** 13 — Hermes Integration Plugin/Skill
**Subtask:** T01
**Date:** 2026-06-24
**Status:** MERGED to main (commit 0dd8322)

## Scope

Create the Hermes plugin package (`plugin.yaml` + `__init__.py` + `schemas.py`
+ `tools.py`) with 17 required P1 tool names registered via `ctx.register_tool`.

## Worker

- Model: grok-composer-2.5-fast via Grok CLI ($0)
- Commits: 2697de7 (skeleton), 0d4f9c6 (adversary fix hardening)
- Files: plugin.yaml, __init__.py, schemas.py, tools.py, tests/test_plugin_skeleton.py
- Result: 18 tools (init + reconcile are separate per spec), all shell out to
  `bin/agentpaas` CLI with --json via dynamic binary resolver. 8/8 tests pass.

## Adversary

- Model: grok-4.3 via agentpaas-adversary profile ($0)
- Result: 3 breaks found (2 HIGH, 1 MEDIUM), all real:
  1. HIGH: `args=None` crashes handlers (AttributeError before try/except)
  2. HIGH: `AGENTPAAS_CLI` env var accepted with zero validation (binary injection)
  3. MEDIUM: non-JSON CLI output returned raw_output (not structured error); schemas
     missing "required" arrays
- Fix worker addressed all 3: `args = args or {}` guard, realpath+isfile+executable
  validation on AGENTPAAS_CLI, structured error on JSONDecodeError, required arrays
  added to schemas.

## Gate

- `python3 -m unittest discover -s integrations/hermes-plugin/tests` → 19/19 PASS
  (8 skeleton + 11 adversary regression tests)
- This is a Python block — local-gate.sh (Go-centric) not applicable per-subtask.

## Verifier

Deferred to block-end verification (see docs/owa-records/b13-block-end.md).

## Notes

- `validate` CLI subcommand does NOT yet support `--json` (B11 gap). The handler
  passes --json globally; validate may return text until B11 CLI parity. Tracked
  for B13-T02 (contract parity gate).
- `agent` on PATH is Grok's binary (collision). Plugin resolves AgentPaaS binary
  via AGENTPAAS_CLI env → `which agentpaas` → repo dev `bin/agentpaas` → last resort.

---

# B13-T02: Schema-Generated Tool Wrappers + Contract-Parity Gate

**Block:** 13 — Hermes Integration Plugin/Skill
**Subtask:** T02
**Date:** 2026-06-24
**Status:** MERGED to main

## Scope

Generate/schema-test tool wrappers against B11 contracts. CI fails if an operator
method lacks a wrapper, returns fields outside the schema, or drops evidence
refs/error categories.

## Worker

- Model: grok-composer-2.5-fast via Grok CLI ($0)
- Commits: 9d69c3b (contracts + parity tests)
- Files: contracts.py (B11 schema fixtures), tests/test_contract_parity.py (17 tests,
  5 classes: OperatorCoverage, ResponseFieldParity, EvidenceRefIntegrity,
  SchemaVersionParity, ErrorEnvelopeParity)
- Result: 36/36 tests pass after worker pass.

## Adversary

- Model: grok-4.3 via agentpaas-adversary profile ($0)
- Result: 3 breaks found (1 HIGH, 2 MEDIUM), all real:
  1. HIGH: CONTRACT DRIFT — RESPONSE_CONTRACTS only modeled top-level fields; nested
     struct fields (ValidationIssue, TimelineEvent, RedactedExcerpt, EvidenceRef,
     ConfirmationRequirement) not in contract. 30+ Go json fields missing.
  2. MEDIUM: SCHEMA VERSION MISMATCH — gate uses regex extraction; adversary proved it
     works but flagged fragility.
  3. MEDIUM: TRUSTED/UNTRUSTED LEAKAGE — TRUSTED_CONTROL_FIELDS and
     UNTRUSTED_EVIDENCE_FIELDS defined but never referenced by any test.

## Fix Worker

- Commits: 73cce53 (address all 3 adversary breaks)
- Fixes:
  - Added NESTED_CONTRACTS for all 5 nested struct types, linked via "nested" keys
  - Added NestedFieldParityTests (6 tests) and TrustBoundaryClassificationTests (4 tests)
  - Converted adversary break-tests to regression tests
- Result: 52/52 tests pass (19 T01 + 17 T02 + 9 adversary T02 + 7 new).

## Gate

- `python3 -m unittest discover -s integrations/hermes-plugin/tests` → 52/52 PASS

## Verifier

Deferred to block-end verification (see docs/owa-records/b13-block-end.md).

---

# B13-T03: Confirmation Protocol Handling

**Block:** 13 — Hermes Integration Plugin/Skill
**Subtask:** T03
**Date:** 2026-06-24
**Status:** MERGED to main

## Scope

Trust-boundary actions return requires_confirmation/confirmation_id/risk_level.
Hermes cannot self-confirm; only the daemon/UI/CLI confirmation path can apply changes.

## Worker

- Model: grok-composer-2.5-fast via Grok CLI ($0)
- Commit: e6f078c
- Files: tools.py (+136 lines: session tracking, self-confirm refusal, replay/expiry
  protection), test_confirmation_protocol.py (12 tests, 4 classes)
- Result: 64/64 tests pass.

## Adversary

- Model: grok-4.3 via agentpaas-adversary ($0)
- Result: 3 breaks (2 HIGH, 1 MEDIUM) + 3 confirmed MEDIUM weaknesses:
  1. HIGH: Self-confirm refusal only checks "confirmation_id" key; alternative keys
     (confirm_id, nested dicts) bypass it entirely.
  2. HIGH: _register_session_run callable directly to bypass stop confirmation.
  3. MEDIUM: Remote export detection misses file://, data:, //host/path, empty/None.
  4. MEDIUM: Confirmation ID validation accepts malicious input ("cf_'; rm -rf /").
  5. MEDIUM: Expiry forgery — future timestamps always valid.
  6. MEDIUM: Global state pollution (module-level sets, not thread-safe).

## Fix Worker

- Commit: fee35d9
- Fixes:
  - _detect_self_confirm_attempt: checks 6 key variants + nested dicts
  - _internal_register_session_run with sentinel guard; _register_session_run raises
  - _is_remote_destination: file://, data:, //host, scp syntax, empty/None fail-safe
  - Confirmation ID regex: ^cf_[a-fA-F0-9]{8,128}$
  - _is_confirmation_expired with 1-hour TTL cap
  - Documented global state + _reset_confirmation_state for test isolation
- Result: 80/80 tests pass (19 T01 + 33 T02 + 16 T03 + 12 adversary T03).

## Gate

- `python3 -m unittest discover -s integrations/hermes-plugin/tests` → 80/80 PASS

## Verifier

Deferred to block-end verification.

---

# B13-T04: Prompt-Injection Boundary in Integration Responses

**Block:** 13 — Hermes Integration Plugin/Skill
**Subtask:** T04
**Date:** 2026-06-24
**Status:** MERGED to main

## Scope

Separate trusted control fields from untrusted evidence. Negative tests prove hostile
instructions in agent source/logs/comments/traces/tool output/remote payloads cannot
alter policy, reveal secrets, delete audit, disable gates, or stop unrelated runs.

## Worker

- Model: grok-composer-2.5-fast via Grok CLI ($0)
- Commit: a4fbffa
- Files: sanitizer.py (injection detection, response sanitizer, trusted-field integrity
  validator), test_prompt_injection_boundary.py (25 tests, 4 classes)
- Result: 99/99 tests pass (pre-existing binary test needed worktree build).

## Adversary

- Model: grok-4.3 via agentpaas-adversary ($0)
- Result: 5 breaks (4 HIGH, 1 MEDIUM):
  1. HIGH: Sanitizer never called from tools.py handlers — boundary is documentation only
  2. HIGH: Injection patterns evadable via abbreviations ("rm audit"), indirect phrasing,
     URL-encoding ("%64isable policy")
  3. HIGH: Summary injection reaches model as usable evidence (flagged but not marked)
  4. HIGH: Evidence JSON control-channel tampering — `{"next_action": "rerun"}` in evidence
  5. MEDIUM: evidence_refs subfields beyond content/detail/source not scanned

## Fix Worker

- Commit: 4df7484
- Fixes:
  - _run_cli now calls sanitize_response() on every CLI JSON response (enforcement, not docs)
  - URL-decode step before pattern matching; added abbreviation + indirect phrasing patterns
  - Full evidence subfield scanning (all string values in list items)
  - _untrusted_fields marker on responses with injection warnings
  - _detect_structural_injection for JSON-like control fields in evidence
  - All 7 adversary tests converted to regression tests
- Result: 109/109 tests pass.

## Gate

- `python3 -m unittest discover -s integrations/hermes-plugin/tests` → 109/109 PASS

## Verifier

Deferred to block-end verification.

---

# Daemon Startup Bug Fix (Pre-T05)

**Block:** 13 (pre-T05 housekeeping)
**Date:** 2026-06-24
**Status:** MERGED to main (247181c)

## Scope

Fix nil-pointer panic in `internal/cli/daemon.go` `runDaemonStart()`: the code
called `cmdDaemon.ProcessState.Exited()` after `Start()`, but `ProcessState` is
nil until `Wait()` returns. This panicked whenever the daemon exited before the
500ms sleep completed (crash, bad config, missing binary).

## Worker

- Model: grok-composer-2.5-fast via Grok CLI ($0)
- Commits: 131d523, 4510133
- Files: internal/cli/daemon.go, internal/cli/cli_test.go

### Changes

1. Replaced `time.Sleep` + `ProcessState.Exited()` with goroutine+select pattern:
   - `Wait()` in goroutine, race against 500ms timeout
   - Early exit → error with exit code (nil-safe ProcessState access)
   - Survives 500ms → success
2. Extracted `resolveDaemonBinary()` into injectable package var for testing
3. Fixed env var overwrite bug (found by adversary): second
   `append(os.Environ(),...)` was clobbering AGENTPAAS_HOME when both --home and
   socket path were set. Now builds cumulatively.

## Adversary

- Model: grok-4.3 via agentpaas-adversary ($0)
- 6 breaks reported:
  1. **HIGH — Env var overwrite** (pre-existing, lines 232-237). REAL. Fixed.
  2. **MEDIUM — Goroutine leak** (success path). Benign — CLI process exits shortly after, reaping goroutine. Not fixed (standard CLI spawner pattern).
  3. **MEDIUM/HIGH — Race on waitCh/ProcessState**. FALSE POSITIVE — race was in adversary's own test code (captureStdout from goroutine), not in daemon.go. Verified clean under -race.
  4. **LOW — Exit code 0 during grace = error**. Correct behavior — a daemon shouldn't exit in 500ms.
  5. **MEDIUM — Success-path test gap**. REAL. Fixed (TestDaemonStart_StaysAlive_Success).
  6. **MEDIUM — Global resolver state fragility** under t.Parallel(). Valid fragility, not a current break. Deferred.

## Gate

- `go build ./...` — PASS
- `go vet ./internal/cli/...` — PASS
- `go test -race -count=1 ./internal/cli/...` — PASS (5 daemon tests, including 2 new regression tests)

## Tests Added

- `TestDaemonStart_ExitImmediate_NoPanic` — fake daemon exits 1, verifies error (not panic)
- `TestDaemonStart_StaysAlive_Success` — fake daemon sleeps 2s, verifies success + PID cleanup

## Verifier

Deferred to block-end verification.
