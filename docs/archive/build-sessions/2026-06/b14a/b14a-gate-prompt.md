# Task: 14A-Gate — Implement make block14a-gate target

## Branch
You are on branch `feat/b14a-t08`. Do NOT create a new branch.

## What to implement

Replace the stub `block14a-gate` target in `Makefile` (currently at line 199-200) with a real gate that runs all 14A security remediation tests.

### Current stub (lines 199-200):
```makefile
block14a-gate:
	@echo "Error: block14a-gate (security remediation) is not implemented until Block 14A" && exit 1
```

### Replace with:
```makefile
block14a-gate: build lint
	@echo "==> Running Block 14A gate: Security remediation (T01-T08)"
	# Go security tests: hash chain verification, harness audit ingestion
	go test -race -count=1 ./internal/daemon/... -run TestVerifyHarnessChain
	go test -race -count=1 ./internal/daemon/... -run TestIngestHarnessAudit
	# Go pack tests: cosign signing, key import, path validation
	go test -race -count=1 ./internal/pack/...
	# Go harness tests: file appender hash chain
	go test -race -count=1 ./internal/harness/...
	# Go audit tests: no regressions
	go test -race -count=1 ./internal/audit/...
	# Python plugin tests: path allow-list, binary verification, output cap,
	# thread-safe confirmation, sanitizer, socket check (166 tests)
	cd integrations/hermes-plugin && python3 -m unittest discover -s tests -t . -v
	# Cosign integration test (only if real tools enabled)
	@if [ "$$AGENTPAAS_PACK_REAL_TOOLS" = "1" ]; then \
		AGENTPAAS_PACK_REAL_TOOLS=1 go test -tags=integration -count=1 -run TestSignImage_RealCosign ./internal/pack/ -timeout 5m; \
	else \
		echo "(skipping cosign integration test — set AGENTPAAS_PACK_REAL_TOOLS=1 to run)"; \
	fi
	@echo "✓ Block 14A gate passed: security remediation T01-T08 verified"
```

### Also update the help text (line 232):
Change:
```
@echo "  block14a-gate - Security remediation (B13.1, 9 tasks)"
```
To:
```
@echo "  block14a-gate - Security remediation (B13.1, 8 tasks: T01-T08) (ACTIVE)"
```

## Verification

```bash
cd /Users/pms88/projects/agentpaas
make block14a-gate
```

The gate must pass. All tests must succeed. Do NOT set AGENTPAAS_PACK_REAL_TOOLS=1 for the verification — the cosign test should skip.

## Commit message
```
feat(14a): implement block14a-gate target with all security remediation tests

Gate runs: Go daemon hash chain tests, Go pack cosign tests, Go harness
audit tests, Go audit tests, Python plugin tests (166 tests), and optional
cosign integration test (gated on AGENTPAAS_PACK_REAL_TOOLS=1).
```
