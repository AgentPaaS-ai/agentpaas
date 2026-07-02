# Worker: B15-T01 MC6 — Update make block15-gate + integration test, checkpoint

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc6` (create from main).
MC1-MC5 are merged.

## Scope (ONE micro-chunk)
1. Update the `block15-gate` Makefile target — it currently says "manual/docs-only gate" but B15 is now the P1 completion block
2. Update `block16-gate` — it currently says "P1 completion" but B16 is now manual testing
3. Update the gates help text
4. Write the B15-T01 checkpoint doc

## Files to edit

### 1. Makefile — update block15-gate and block16-gate

Current (lines 254-258):
```makefile
block16-gate:
	@echo "Error: block16-gate not yet implemented. See execution plan §16 (BLOCK 16)." && exit 1

block15-gate:
	@echo "Error: block15-gate is a manual/docs-only gate. See execution plan §15.2 use-case matrix." && exit 1
```

Change `block15-gate` to:
```makefile
block15-gate: build lint
	@echo "==> Running Block 15 gate: P1 completion items"
	@echo "  T01: credential onboarding (secret add/list/remove/rotate/test)"
	go test -race -count=1 ./internal/secrets/... ./internal/cli/...
	@echo "  Plugin: secret onboarding tools"
	cd integrations/hermes-plugin && python3 -m unittest discover -s tests -t . -v 2>&1 | tail -5
	@echo "==> Block 15 gate passed (T01-T08 to be added as blocks complete)"
```

Note: For now, the gate only runs T01 (credential onboarding). As T02-T08
are completed in later sessions, their tests will be added here.

Change `block16-gate` to:
```makefile
block16-gate:
	@echo "Error: block16-gate (manual use-case assessment) runs after block15-gate passes. See execution plan §16." && exit 1
```

Update the gates help text (lines 281-282):
```
	@echo "  block15-gate - P1 completion: LLM, credentials, policy, hardening, release"
	@echo "  block16-gate - Manual use-case assessment (runs AFTER B15)"
```

### 2. Write `docs/b15-checkpoint-01.md`

```markdown
# B15 Session Checkpoint — 01

**Date:** 2026-06-30
**Branch:** main
**Goal:** 15-T01 Credential Onboarding — complete all 5 secret CLI commands + plugin tools + docs

## Completed This Session
- MC1: secret add/remove with aliases (commit 0633252) — renamed set→add, rm→remove
- MC2: secret rotate command (commit 0212fe0) — atomic credential replacement via stdin
- MC3: secret test command + provider adapters (commit 4c72682) — OpenAI/Anthropic/xAI validation
- MC5: credential onboarding docs + Hermes skill (commit b09ea0a)
- MC4: Hermes plugin 5 secret tools (pending merge)
- MC6: block15-gate Makefile update + this checkpoint

## Key Facts
- Provider adapters use package-level endpoint vars (openAIEndpoint, anthropicEndpoint, xaiEndpoint)
  with SetTestEndpoints() for test override
- secret test auto-detects provider from name: "openai"/"gpt"→openai, "anthropic"/"claude"→anthropic, "xai"/"grok"→xiai
- _run_cli_with_stdin helper added to plugin tools.py for stdin-based secret value passing
- Secret values NEVER appear in: CLI stdout/stderr, audit trail, container env, process args

## Next Session Start
- Immediate next action: start 15-T02 (LLM provider integration via unified gateway egress)
- File to read first: agentpaas-execution-plan-v1.md, search "15-T02"
- Block: B15, Subtask: T02, Micro-chunk: LLM provider adapter + agent.llm() sugar

## 15-T01 Verification Status
1. `agentpaas secret add <name>` — DONE, stores in Keychain
2. `agentpaas secret list` — DONE, shows labels only, never values
3. `agentpaas secret test <name>` — DONE, validates via provider HTTP call
4. `agentpaas secret rotate <name>` — DONE, atomic replace
5. `agentpaas secret remove <name>` — DONE, deletes from Keychain
6. Secret value never in container env, logs, audit, CLI output — VERIFIED by tests
7. Unit tests for all 5 commands + provider adapter tests — DONE (21+ Go tests, 11 Python tests)
8. Hermes plugin: 5 secret tools — DONE (MC4)
```

## Constraints
- Do NOT change any Go source files.
- Run `make block15-gate` after updating the Makefile — it should pass.
- Run `make test` and `make lint` — both must still pass.

## Commit
`chore: update block15-gate for T01 + write B15-T01 checkpoint (B15-T01 MC6)`

Do NOT push.