Block 27 (SDK Progress, Checkpoint, and Artifact Protocol) is COMPLETE.

ALL TASKS DONE:
- T01: Python SDK progress contract. Merged a751ad7.
- T02: Harness progress RPC and authenticated journal (HMAC-SHA-256, monotonic sequence, dedup). Merged 8219eed.
- T03: Daemon journal ingestion and checkpoint persistence (tailer, verify, store). Merged bfa7adf.
- T04: Bounded artifact workspace and metadata (path validation, quota, digest). Merged 0d254eb.
- T05: Resume checkpoint delivery (digest verification, run/attempt relationship). Merged 30fbea7.
- T06: Reference worker pattern and Hermes authoring fixture. Merged 7865a8b.
- T07: Block gate + adversary review. Merged 7e6ff8c, 34916bd, 672bad9, 148ba39, 3e629ed.

BLOCK GATE RESULTS:
- go build ./...: PASS — 0 errors
- go test ./... -count=1: ALL PASS (29 packages)
- go test -race ./...: ALL PASS — 0 data races
- go vet ./...: PASS — 0 issues
- golangci-lint (fresh cache): 0 issues
- govulncheck: 5 pre-existing (docker/moby v28.5.2, no fix available, go.mod unchanged)
- Adversary tests: 14/14 PASS (all with real assertions after verifier-driven fixes)
- Handler tests: 6/6 PASS (new — control chars, empty completed_work, secret sentinels, resume)
- Python SDK tests: 68/68 PASS (was 64, added 4 control char / non-empty completed_work tests)
- v0.2.3 compat fixtures: ALL PASS
- CI: green (29670356486)
- Release Verify: green (29670356481)
- Block Gates: pre-existing failures in Block 5 (Docker e2e) and Block 8 (publisher keystore) — NOT B27 regressions

VERIFIER (grok-composer-2.5-fast, 2 passes):
- Pass 1: VERIFY FAIL — 19 findings. All in-scope fixed.
- Pass 2: VERIFY FAIL — 1 residual (control chars in last_committed_action in Python SDK). Fixed.
- All gates green after fixes.

KEY FIXES (verifier-driven):
- Control chars rejected in phase, completed_work, remaining_work, last_committed_action (Go + Python)
- safe_to_resume requires at least one NON-EMPTY completed_work entry
- Hard links rejected (Nlink > 1) in artifact workspace
- Resume checkpoint/resume_reason populated in RPC response via SetProgressMetadata
- Safe checkpoints require non-empty digest
- Secret sentinels rejected in checkpoint content (lexical)
- go.mod replace directive restored (docker→moby, was reversed in 6b3ba2a)
- Hermes authoring guidance added (progress-resume-pattern.md)

DEFERRED (current B28–B30/B39 scope):
- B28 maps live progress/journal/artifact wiring onto the portable runtime,
  state, event, artifact, identity, and lease ports.
- B29 makes external events durable and reconnectable.
- B30 owns the long-running watchdog/live-container path; B39 owns routed
  continuation. The completed B27 invariants must not be reimplemented.

NEXT BLOCK: B28 (Runtime Portability and Managed-PaaS Feasibility Gate). Read
docs/execution/blocks/b28-summary.md, then the approved D34–D65 decision
register and current roadmap before implementation.

REPO: ~/projects/agentpaas, main branch (3e629ed).
