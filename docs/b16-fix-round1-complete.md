# B16 Fix Round 1 — Resume Prompt (after all fixes)

**Date:** 2026-07-03
**Status:** ALL 6 FIXES COMPLETE, MERGED, PUSHED, v0.1.0 TAG MOVED

## WHAT WAS DONE

All 6 manual test fixes from `b16-fix-round1-resume-prompt.md` are complete:

| Fix | Priority | Description | Files Changed |
|-----|----------|-------------|---------------|
| T3f | P0 | Bundle Python SDK in container image + bridge app() entry point | build.go, runner.py |
| T3a | BLOCKER | Sanitize log messages to valid UTF-8 before proto marshal | control_handlers.go |
| T3b | BLOCKER | Add `agentpaas status <run-id>` CLI subcommand | control.go, root.go |
| T3c | BLOCKER | Trigger invoke inline JSON payload handling (plugin-side) | tools.py |
| T3d | — | Surface failure reason in run_stop timeline event | control_handlers.go, operator_handlers.go, stub_handlers.go |
| T3e | — | Add `agentpaas run list` CLI + agentpaas_list_runs plugin tool | proto, control.go, tools.py, plugin.yaml, stub_handlers.go |

## VERIFICATION RESULTS

- `go build ./...` — PASS
- `golangci-lint run ./...` — 0 issues (after removing unused setRunFailed)
- `go test ./internal/pack/... ./internal/daemon/... ./internal/cli/... ./internal/harness/... -short` — ALL PASS
- Plugin tests: 208/208 PASS
- Pushed to origin/main (HEAD: 0c90399)
- v0.1.0 tag moved to 0c90399

## NEXT STEPS

1. Reset agentpaas-test profile: `cd ~/projects/agentpaas && ./scripts/reset-agentpaas-test.sh`
2. Re-run manual tests in agentpaas-test profile:
   - LC-01: plugin install (from GitHub)
   - LC-02: deploy + invoke (the critical test — agents should now actually run)
3. Verify the full lifecycle works end-to-end per the test plan in the original resume prompt

## OWA MODEL USED

- Orchestrator: z-ai/glm-5.2 (z.ai direct API)
- Workers: grok-composer-2.5-fast (Grok CLI, $0 subscription)
- All 6 fixes dispatched as parallel Grok workers via tmux

## KEY ARCHITECTURAL CHANGES

### T3f (P0) — SDK Bundling
- `BuildConfig.SDKDir` field auto-detects SDK location relative to harness binary
- `collectSDKFiles()` walks python/ dir, skips __pycache__/.pyc, sorts for determinism
- Dockerfile now has `COPY --chown=64000:64000 python/ /app/python/`
- `runner.py` now bridges `app()` callable when no invoke handler is registered

### T3e — Run Listing
- New proto messages: RunInfo, ListRunsRequest, ListRunsResponse
- New RPC: ListRuns (GET /v1/control/runs)
- trackedRun now has AgentName and StartedAt fields
- CLI: `agentpaas run list` (or `agentpaas ps`)
- Plugin: `agentpaas_list_runs` tool registered in plugin.yaml

### T3d — Failure Reasons
- trackedRun.FailReason field added
- invokeFailReason(err) helper extracts reason from errors
- FailReason set directly at failure points (lines 420-422, 462-464)
- fail_reason included in run_stop audit event when status=failed
- SummarizeRun includes fail_reason in summary text
