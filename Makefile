.PHONY: build build-harness-linux build-all test proto lint race osv e2e-network redteam-smoke

build:
	go build ./...

build-harness-linux:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o bin/agentpaas-harness-linux ./cmd/harness

build-all: build build-harness-linux
	go build -o bin/agentpaas ./cmd/agent
	go build -o bin/agentpaasd ./cmd/agentpaasd
	go build -o bin/agentpaas-harness ./cmd/harness

test:
	go test ./...

proto:
	buf generate

lint:
	golangci-lint run --timeout 5m

race:
	go test -race ./...

osv:
	osv-scanner scan -r .

e2e-network: build
	@echo "==> Running E2E network tests (positive path + canary probes + host/bridge probes)"
	# Run the canary probe tests with a short timeout for fast failure.
	# AGENTPAAS_DOCKER_TESTS=1 is required to run Docker integration tests.
	# Tests: E2E_Network_PositivePath (canary: direct external blocked, DNS blocked,
	#        positive: gateway egress works, agent→gateway internal path works)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestE2E_Network_PositivePath' ./internal/runtime/... -timeout 120s
	# Run B5-T04a adversary tests (bypass, timeout, DNS redirect, etc.)
	# These document expected adversary behaviour for the gateway topology.
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestAdversaryB5T04a' ./internal/runtime/... -timeout 120s
	# Run B5-T04b host/bridge probe tests (host.docker.internal, bridge gateway,
	# gateway container IP probing, daemon ports)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestE2E_HostBridgeProbes' ./internal/runtime/... -timeout 120s
	# Run B5-T04b adversary tests (host bypass, loopback, gateway port scan, socket discovery)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestAdversaryB5T04b' ./internal/runtime/... -timeout 180s
	# Run B5-T04c protocol bypass probe tests (IPv6, UDP, ICMP, raw socket, CONNECT tunnel, namespace)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestE2E_ProtocolBypassProbes' ./internal/runtime/... -timeout 120s
	# Run B5-T04c adversary tests (IPv6 bypass, UDP tunneling, ICMP covert channel, CAP_NET_RAW, namespace sharing)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestAdversaryB5T04c' ./internal/runtime/... -timeout 180s
	# Run B5-T04d topology inspect tests (deep Docker inspect assertions, restart preserves membership)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestE2E_TopologyInspect' ./internal/runtime/... -timeout 120s
	# Run B5-T04d partial create cleanup tests (failure-injection leaves zero orphans)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestE2E_PartialCreateCleanup' ./internal/runtime/... -timeout 120s
	# Run B5-T04d adversary tests (orphan leaks, restart bypass, double-removal, cross-run isolation)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestAdversaryB5T04d' ./internal/runtime/... -timeout 180s
	# Run B5-T05 crash reconciliation tests (agent without gateway killed, agent with gateway kept)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestE2E_CrashReconciliation' ./internal/runtime/... -timeout 120s
	# Run B5-T05 secret-free debug output tests (no raw secrets in inspect/logs/config dumps)
	AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run 'TestE2E_SecretFreeDebugOutput' ./internal/runtime/... -timeout 120s
	@echo "✓ e2e-network gate: PASS"

redteam-smoke:
	@echo "==> Running P1 red-team smoke gate"
	AGENTPAAS_DOCKER_TESTS=1 DOCKER_HOST="unix://$$HOME/.colima/default/docker.sock" go test ./test/redteam/... -v -timeout 15m

.PHONY: block1-gate
block1-gate: proto build test lint
	@echo "Block 1 gate: PASS"

.PHONY: block2-gate block3-gate block4-gate block5-gate block6-gate block7-gate block8-gate block9-gate block10-gate block11-gate block12-gate block13-gate block14-gate block14a0-gate block14a-gate block14b-gate block14c-gate block15-gate block16-gate

block2-gate: build test lint race
	@echo "Verifying Block 2 packages..."
	go test ./internal/home/... -race -count=1
	go test ./internal/daemon/... -race -count=1
	go test ./internal/cli/... -count=1
	go test ./internal/service/... -count=1
	go test ./internal/doctor/... -race -count=1
	go test ./internal/logging/... -race -count=1
	@echo "Block 2 gate: PASS"

block3-gate: build test race lint
	@echo "==> Running Block 3 gate: identity + audit tests"
	go test ./internal/identity/... ./internal/audit/... -race -count=1
	@echo "✓ Block 3 gate passed"

# block4-gate runs unit tests (with race detection) plus fuzz targets.
# Fuzz targets use -fuzztime=20s for CI practicality (~100K-500K executions).
# For the full 1M-execution gate, run locally with block4-gate-full.
# See B4-T05 spec: parser fuzzing + block4-gate.
block4-gate: build lint
	@echo "==> Running Block 4 gate: policy engine (unit + race + vet + fuzz)"
	go vet ./internal/policy/...
	go test -race -count=1 ./internal/policy/...
	go test -race -fuzz=FuzzParsePolicy -fuzztime=20s -count=1 ./internal/policy/...
	go test -race -fuzz=FuzzCanonicalize -fuzztime=20s -count=1 ./internal/policy/...
	go test -race -fuzz=FuzzDigest -fuzztime=20s -count=1 ./internal/policy/...
	@echo "✓ Block 4 gate passed"

# block4-gate-full runs the full 1M+ execution fuzz gate with -fuzztime=5m.
# Use this locally before merging to achieve ~1M+ executions per target.
block4-gate-full: build lint
	@echo "==> Running Block 4 FULL gate: policy engine (unit + race + vet + fuzz 5m)"
	go vet ./internal/policy/...
	go test -race -count=1 ./internal/policy/...
	go test -race -fuzz=FuzzParsePolicy -fuzztime=5m -count=1 ./internal/policy/...
	go test -race -fuzz=FuzzCanonicalize -fuzztime=5m -count=1 ./internal/policy/...
	go test -race -fuzz=FuzzDigest -fuzztime=5m -count=1 ./internal/policy/...
	@echo "✓ Block 4 FULL gate passed"

block5-gate: build test race lint osv
	@echo "==> Running Block 5 gate: container runtime + network topology + canary probes"
	# B5-T01/T02/T03 unit + integration tests (runtime driver, containers, hardening)
	go test -race -count=1 ./internal/runtime/...
	# e2e-network: positive path + canary probes + adversary tests (needs Docker)
	AGENTPAAS_DOCKER_TESTS=1 $(MAKE) e2e-network
	@echo "✓ Block 5 gate passed"

block6-gate: build test race lint osv
	@echo "==> Running Block 6 gate: agent harness HTTP lifecycle"
	go test -race -count=1 ./internal/harness/...
	@echo "✓ Block 6 gate passed"

block7-gate: build test race lint osv
	@echo "==> Running Block 7 gate: secret store"
	go test -race -count=1 ./internal/secrets/...
	@echo "Block 7 gate: PASS"

block8-gate: build test race lint osv
	@echo "==> Running Block 8 gate: packaging pipeline (agent pack)"
	# B8-T01..T06 unit + integration tests (detect, build, scan, lock, immutable, advisory)
	go test -race -count=1 ./internal/pack/...
	# Adversary regression tests (B8-T02..T05) - security breaks resolved
	go test -tags=adversary -race -count=1 ./internal/pack/...
	@echo "✓ Block 8 gate passed"

block9-gate: build test race lint osv
	@echo "==> Running Block 9 gate: trigger API, event bus, webhooks, cron"
	# B9-T01..T09 unit + integration tests
	go test -race -count=1 ./internal/trigger/...
	# B9 adversary tests
	go test -tags=adversary -race -count=1 ./internal/trigger/...
	@echo "✓ Block 9 gate passed"

block10-gate: build test race lint osv
	@echo "==> Running Block 10 gate: OTel pipeline, dashboard"
	# B10-T01..T07 unit + integration tests
	go test -race -count=1 ./internal/dashboard/... ./internal/otel/...
	# B10 adversary tests
	go test -tags=adversary -race -count=1 ./internal/dashboard/... ./internal/otel/...
	@echo "✓ Block 10 gate passed"

block11-gate: build test race lint osv
	@echo "==> Running Block 11 gate: Hermes operator contract"
	# B11-T01..T07 unit + integration tests
	go test -race -count=1 ./internal/operator/... ./internal/daemon/... ./internal/cli/... ./internal/pack/...
	# B11 golden flow test
	go test -race -count=1 -run TestGoldenFlow_B11T07 ./internal/daemon/...
	# B11 adversary tests
	go test -tags=adversary -race -count=1 ./internal/daemon/... ./internal/cli/... ./internal/pack/...
	@echo "Block 11 gate PASSED: golden flow green"

block12-gate: build test race lint osv
	@echo "==> Running Block 12 gate: P1 red-team smoke gate"
	$(MAKE) redteam-smoke

block13-gate: build lint
	@echo "==> Running Block 13 gate: Hermes integration plugin + e2e governance"
	# B13 unit tests: harness audit appender, daemon handlers, runtime Exec
	go test -race -count=1 ./internal/harness/... ./internal/daemon/... ./internal/runtime/...
	# B8 immutable redeploy path (prompt-change → distinct digests)
	go test -race -count=1 -run TestImmutablePromptUpdatePath ./internal/pack/...
	# Verify Hermes plugin files exist
	@test -f integrations/hermes-plugin/plugin.yaml || (echo "FAIL: plugin.yaml missing" && exit 1)
	@test -f integrations/hermes-plugin/__init__.py || (echo "FAIL: __init__.py missing" && exit 1)
	@test -f integrations/hermes-plugin/tools.py || (echo "FAIL: tools.py missing" && exit 1)
	@test -f integrations/hermes-plugin/SKILL.md || (echo "FAIL: SKILL.md missing" && exit 1)
	# Verify demo matrix fixtures exist
	@test -d demo/governed-weather || (echo "FAIL: demo/governed-weather missing" && exit 1)
	@test -d demo/secret-saas || (echo "FAIL: demo/secret-saas missing" && exit 1)
	@test -d demo/repair-loop || (echo "FAIL: demo/repair-loop missing" && exit 1)
	# Verify plugin Python syntax
	@python3 -c "import ast; ast.parse(open('integrations/hermes-plugin/__init__.py').read()); print('plugin __init__.py syntax OK')"
	@python3 -c "import ast; ast.parse(open('integrations/hermes-plugin/tools.py').read()); print('plugin tools.py syntax OK')"
	@echo "✓ Block 13 gate passed: plugin + demos + e2e governance verified"

block14a0-gate: build lint
	@echo "==> Running Block 14A0 gate: B13 correctness fixes"
	# Run status tracking, invoke/Stop sync, orphan reconciliation tests
	go test -race -count=1 ./internal/daemon/...
	# Immutable redeploy path
	go test -race -count=1 -run TestImmutablePromptUpdatePath ./internal/pack/...
	# Docker e2e (skips gracefully if Docker not available)
	@if [ "$$AGENTPAAS_DOCKER_TESTS" = "1" ]; then \
		AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run TestE2E_PackRunInvokeStopAudit ./internal/daemon/... -timeout 300s; \
	else \
		echo "(skipping Docker e2e — set AGENTPAAS_DOCKER_TESTS=1 to run)"; \
	fi
	@echo "✓ Block 14A0 gate passed: B13 correctness fixes verified"

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

block14b-gate: build lint
	@echo "==> Running Block 14B gate: gateway container, policy enforcement, egress, stats, trigger server"
	@go test ./internal/runtime/... -race -count=1 -run "TestStats"
	@go test ./internal/runtime/... -race -count=1 -run "TestInspectContainerIP"
	@go test ./internal/daemon/... -race -count=1 -run "TestRun_CreatesGateway"
	@go test ./internal/daemon/... -race -count=1 -run "TestRun_DefaultDeny"
	@go test ./internal/daemon/... -race -count=1 -run "TestRun_SetsProxyEnv"
	@go test ./internal/daemon/... -race -count=1 -run "TestRun_OmitsProxyEnv"
	@go test ./internal/daemon/... -race -count=1 -run "TestTriggerServer"
	@go test ./internal/daemon/... -race -count=1 -run "TestTriggerService"
	@go test ./internal/daemon/... -race -count=1 -run "TestAdversaryT02"
	@go test ./internal/daemon/... -race -count=1 -run "TestAuditTailer"
	@echo "✓ Block 14B gate passed: gateway topology, policy enforcement, real-time egress, Stats, trigger server verified"

block14c-gate: build lint
	@echo "==> Running Block 14C gate: install path, docs, demo, release"
	@test -f .goreleaser.yaml || (echo "FAIL: .goreleaser.yaml missing" && exit 1)
	@test -f Formula/agentpaas.rb || (echo "FAIL: Formula/agentpaas.rb missing" && exit 1)
	@test -f .github/workflows/release.yml || (echo "FAIL: release.yml missing" && exit 1)
	@test -f README.md || (echo "FAIL: README.md missing" && exit 1)
	@test -f docs/quickstart.md || (echo "FAIL: docs/quickstart.md missing" && exit 1)
	@test -f docs/how-enforcement-works.md || (echo "FAIL: docs/how-enforcement-works.md missing" && exit 1)
	@test -f docs/known-limitations.md || (echo "FAIL: docs/known-limitations.md missing" && exit 1)
	@test -f docs/policy-reference.md || (echo "FAIL: docs/policy-reference.md missing" && exit 1)
	@test -f docs/audit-export.md || (echo "FAIL: docs/audit-export.md missing" && exit 1)
	@grep -q "brew install agentpaas/tap/agentpaas" README.md || (echo "FAIL: README missing install command" && exit 1)
	@grep -q "Zero telemetry" README.md || (echo "FAIL: README missing zero telemetry statement" && exit 1)
	@echo "✓ Block 14C gate passed: goreleaser, formula, CI, README, docs verified"
	@echo "  NOTE: volunteer clean-machine test (2 users <15 min) is a manual gate — see execution plan §14C"

block14-gate: block14a0-gate block14a-gate block14b-gate block14c-gate
	@echo "==> All Block 14 sub-segment gates passed (14A0 → 14A → 14B → 14C)"

block16-gate:
	@echo "Error: block16-gate not yet implemented. See execution plan §16 (BLOCK 16)." && exit 1

block15-gate:
	@echo "Error: block15-gate is a manual/docs-only gate. See execution plan §15.2 use-case matrix." && exit 1

.PHONY: gates
gates: ## List all available gate targets
	@echo "Available gates:"
	@echo "  block1-gate  - Repo bootstrap, proto contracts, CI skeleton (ACTIVE)"
	@echo "  block2-gate  - Daemon skeleton, CLI plumbing (ACTIVE)"
	@echo "  block3-gate  - Identity service, audit hash chain (ACTIVE)"
	@echo "  block4-gate  - Policy engine (ACTIVE)"
	@echo "  block5-gate  - Runtime driver, Docker integration (ACTIVE)"
	@echo "  block6-gate  - Agent harness (ACTIVE)"
	@echo "  block7-gate  - Secret store (ACTIVE)"
	@echo "  block8-gate  - Packaging pipeline, agent pack (ACTIVE)"
	@echo "  block9-gate  - Trigger API, event bus (ACTIVE)"
	@echo "  block10-gate - OTel pipeline, dashboard (not implemented)"
	@echo "  block11-gate - Hermes operator contract (golden flow)"
	@echo "  block12-gate - P1 red-team smoke gate"
	@echo "  block13-gate - Hermes integration plugin + e2e governance (ACTIVE)"
	@echo "  block14-gate  - Post-B13 consolidated: correctness + security + egress + release"
	@echo "  block14a0-gate - B13 correctness fixes: run status, orphan reconciliation, sync, e2e"
	@echo "  block14a-gate - Security remediation (B13.1, 8 tasks: T01-T08) (ACTIVE)"
	@echo "  block14b-gate - Real-time egress timeline (B13.5)"
	@echo "  block14c-gate - Install path, docs, demo, v0.1.0 release"
	@echo "  block15-gate - Manual use-case assessment (docs-only gate)"
	@echo "  block16-gate - P1 completion: LLM, credentials, policy, hardening, release"
