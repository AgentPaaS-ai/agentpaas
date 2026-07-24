# B33-T09 — block33-gate + adversary matrix

Workdir: `/Users/pms88/projects/ap-b33-t09`
Branch: `feat/b33-t09-block-gate` @ `b4476cb`

LOCAL CI ONLY. No GitHub Actions.

## 1. Makefile `block33-gate`

Add after `block32-gate`:

```make
.PHONY: block33-gate
block33-gate: block32-gate
	@echo "==> Running Block 33 gate: MCP container services"
	@echo "  T01-T08 packages race"
	@go test ./internal/mcpmanager/... ./internal/harness/... ./internal/daemon/... -count=1 -race
	@go test ./internal/runtime/... ./internal/routedrun/... ./internal/policy/... ./internal/pack/... -count=1 -race
	@echo "  Python SDK"
	@python3 -m unittest discover -s python/agentpaas_sdk/tests -v
	@echo "  MCP container e2e (docker)"
	@$(MAKE) mcp-container-e2e
	@echo "  B33 adversary matrix"
	@go test ./internal/mcpmanager/... ./internal/harness/... -count=1 -race -run 'TestAdversary_B33|TestAdversaryT07|TestMCP_|TestE2E_Neg'
	@echo "  vet"
	@go vet ./...
	@echo "  lint (project only — clear cache)"
	@rm -rf ~/Library/Caches/golangci-lint && golangci-lint run --timeout 5m ./...
	@echo "  govulncheck"
	@govulncheck ./...
	@echo "  golden-fast"
	@$(MAKE) golden-fast
	@echo "Block 33 gate: PASS"
```

Also add `block33-gate` to the help list and `.PHONY` line near other block gates.

Note: `golangci-lint ./...` may still scan sibling worktrees if module replace paths exist — if lint fails only on `../ap-*` paths, document and use:
`golangci-lint run --timeout 5m ./internal/... ./cmd/... ./python/... ./test/...`
or fix config. Prefer full pass.

**Pragmatic split if block32-gate is multi-hour:**
Implement full target as above, plus:
```make
block33-gate-fast:  # skip block32 chain for iterative T09 work
```
Orchestrator will run full `block33-gate` once; worker validates `block33-gate-fast` + mcp-container-e2e first.

## 2. Adversary matrix inventory + gap fills

Create `internal/mcpmanager/adversary_b33_matrix_test.go` that:

A) **Documents** each T09 matrix row with a subtest that either:
- Calls existing coverage (table of test names / package functions that prove the property), OR
- Implements a minimal regression assertion if missing.

Matrix rows from b33-summary T09:

| # | Threat | Existing coverage (use if present) | Gap action |
|---|--------|-----------------------------------|------------|
| 1 | Synthetic no-router success in production | TestMCP_NoRouter_FailsClosedInProduction, TestMCP_ManagedBinding_RejectsSyntheticSuccess | assert still pass |
| 2 | Raw endpoint/IP/DNS/capability from worker | managed_resolver never takes endpoint from payload | Add TestAdversary_B33_WorkerSuppliedEndpointIgnored if missing |
| 3 | Generic HTTP/raw socket bypass | TestE2E_Neg_HTTPBypassNoCapability | ok |
| 4 | Cross-workflow / stale capability | TestE2E_Neg_CrossWorkflowIsolation | Add TestAdversary_B33_StaleCapabilityRejected (wrong/old cap → 401) |
| 5 | Service registers undeclared tool | tool set equality / readiness | harness or pack test; e2e mock |
| 6 | Caller undeclared tool | TestE2E_Neg_UndeclaredTool | ok |
| 7 | Caller credential inherited by service | Add TestAdversary_B33_ServiceEnvNoCallerSecrets — createServiceContainer env must not contain caller secret env keys |
| 8 | Capability in Python/logs/audit/error | StripCapability, redact tests, bridge tests | Add TestAdversary_B33_CapabilityNotInAgentErrorPaths if thin gap |
| 9 | Oversized/deep request | TestMCP_InputSizeExceeded, bounds tests | ok |
| 10 | Fixed 5s timeout on managed path | mcpCallContext / EffectiveCallDeadline | Add TestAdversary_B33_ManagedPathNotHardcoded5s — managed resolver / router uses ctx deadline >5s or lease-aware |
| 11 | Concurrency exhaustion | TestMCP_CallerConcurrency_MaxPlusOneRejected | ok |
| 12 | Late result after lease revoke | T07 terminal monotonicity + Fence | ok |
| 13 | Daemon restart replays tool | MarkInFlightUnknown never SUCCEEDED | ok |
| 14 | Service/network orphan | DiscoverOrphans, e2e cleanup | ok |

Implement only **real gaps** (2, 4 stale, 7, 10) as concrete tests. Others can be thin wrappers that re-invoke existing unit logic or `t.Run` documentation with `// covered by X` and a one-line compile-time link (call a helper shared with existing tests).

Prefer **not** duplicating full docker e2e inside matrix file — unit/regression only; e2e already in mcp-container-e2e.

## 3. Acceptance

```bash
cd /Users/pms88/projects/ap-b33-t09
go test ./internal/mcpmanager/... ./internal/harness/... -count=1 -race -run 'TestAdversary_B33|TestMCP_NoRouter|TestMCP_Managed'
go test ./internal/mcpmanager/... ./internal/harness/... ./internal/daemon/... -count=1 -race
go test ./internal/runtime/... ./internal/routedrun/... ./internal/policy/... ./internal/pack/... -count=1 -race
python3 -m unittest discover -s python/agentpaas_sdk/tests -v
AGENTPAAS_DOCKER_TESTS=1 make mcp-container-e2e
go vet ./...
# lint as feasible
govulncheck ./... || true  # report honestly if fails on known deps
make golden-fast
```

Then attempt `make block33-gate-fast` if defined, and full `make block33-gate` if time allows (block32 chain is long).

## 4. Docs
- `docs/owa-records/b33-t09.md` with matrix coverage table + gate evidence
- Update `docs/execution/current-state.md` only after orch merges

Commits:
1. `build: add make block33-gate`
2. `test(mcp): B33 adversary matrix inventory + gap regressions`
3. `docs: B33-T09 OWA record`

Do not merge to main (orch merges after full gate).
