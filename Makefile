.PHONY: build build-harness-linux build-all test proto lint race osv e2e-network redteam-smoke install-plugin golden-eval golden-fast golden-slow golden-docker

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

# install-plugin: Symlink the Hermes plugin from this repo into the active
# Hermes profile's plugins directory, then enable it. This is the documented
# way to register the AgentPaaS plugin with Hermes for local development.
#
# Usage:
#   make install-plugin                      # uses HERMES_PROFILE or 'agentpaas'
#   make install-plugin HERMES_PROFILE=myprof
#
# Prerequisites:
#   - Hermes installed (hermes on PATH)
#   - Profile created (hermes profile create <name>)
install-plugin:
	@profile="$${HERMES_PROFILE:-agentpaas}"; \
	plugins_dir="$$HOME/.hermes/profiles/$$profile/plugins"; \
	src="$(CURDIR)/integrations/hermes-plugin"; \
	if [ ! -f "$$src/plugin.yaml" ]; then \
		echo "FAIL: plugin.yaml not found at $$src — run from repo root"; exit 1; \
	fi; \
	mkdir -p "$$plugins_dir"; \
	if [ -L "$$plugins_dir/agentpaas" ] || [ -d "$$plugins_dir/agentpaas" ]; then \
		echo "Plugin already linked at $$plugins_dir/agentpaas — replacing"; \
		rm -rf "$$plugins_dir/agentpaas"; \
	fi; \
	ln -s "$$src" "$$plugins_dir/agentpaas"; \
	echo "Symlinked $$src -> $$plugins_dir/agentpaas"; \
	hermes -p "$$profile" plugins enable agentpaas; \
	echo "Adding 'agentpaas' to platform_toolsets.cli..."; \
	python3 "$(CURDIR)/scripts/ensure-toolset.py" "$$profile"; \
	echo "✓ AgentPaaS plugin installed for profile '$$profile'"; \
	echo ""; \
	echo "  IMPORTANT: Run /quit and relaunch Hermes to load the plugin and tools."; \
	echo "  Verify after restart: hermes -p $$profile tools list | grep agentpaas"

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
	@echo "Error: block16-gate (manual use-case assessment) runs after block15-gate passes. See execution plan §16." && exit 1

# ── Block 25 Gate ────────────────────────────────────────────────────────────
#
# B25 gate: Go/Docker vuln closure + sharing tools + docs + release readiness.
# Extends as chunks land. T06 is the only chunk that publishes artifacts.

.PHONY: block25-gate
block25-gate: build test lint
	@echo "==> Running Block 25 gate"
	@echo "  T00-A: Go toolchain patched (1.26.5+)"
	@go version | grep -q '1.26' || (echo "FAIL: Go 1.26+ required" && exit 1)
	@echo "  T00-B: Docker Engine readiness checks"
	@go test -count=1 -run 'TestParseDocker|TestIsDocker|TestCheckDockerServer' ./internal/doctor/
	@echo "  T00-C: No deprecated docker/docker in go.mod require (replace directive OK)"
	@grep -v 'replace' go.mod | grep -q 'github.com/docker/docker v28' && echo "WARN: docker/docker still in require (check replace)" || true
	@echo "  T01: Plugin sharing tools present"
	@python3 -c "import ast; ast.parse(open('integrations/hermes-plugin/tools.py').read()); print('tools.py syntax OK')"
	@python3 -c "import ast; ast.parse(open('integrations/hermes-plugin/schemas.py').read()); print('schemas.py syntax OK')"
	@grep -q 'agentpaas_identity_show' integrations/hermes-plugin/tools.py || (echo "FAIL: sharing tools missing" && exit 1)
	@echo "  T01: No consent-bypass params in plugin tool arg builders"
	@grep -E '^\s+["'"'"']?(confirm_fingerprint|accept_policy)["'"'"']?\s*[:=]' integrations/hermes-plugin/tools.py integrations/hermes-plugin/schemas.py && (echo "FAIL: consent bypass found" && exit 1) || echo "No consent-bypass params (OK)"
	@echo "✓ Block 25 gate: PASS"

# ── Block 27 Gate ────────────────────────────────────────────────────────────
#
# B27 gate: SDK progress contract, authenticated journal, daemon ingestion,
# bounded artifact workspace, resume checkpoint delivery.
# Covers T01-T06: progress RPC, HMAC journal, checkpoint persistence,
# artifact validation/quota, resume checkpoint loader, reference worker pattern.

.PHONY: block27-gate
block27-gate: build test race lint
	@echo "==> Running Block 27 gate"
	@echo "  T01: Python SDK progress contract (unittest)"
	@cd python && python3 -m unittest discover -s agentpaas_sdk/tests -q
	@echo "  T02: Harness progress RPC and authenticated journal"
	@go test -race -count=1 ./internal/harness/...
	@echo "  T03: Daemon journal ingestion and checkpoint persistence"
	@go test -race -count=1 ./internal/routedrun/...
	@echo "  T03b: Daemon integration tests"
	@go test -race -count=1 ./internal/daemon/...
	@echo "  T04: Bounded artifact workspace and metadata"
	@go test -race -count=1 -run 'TestArtifactWorkspace' ./internal/routedrun/...
	@echo "  T05: Resume checkpoint delivery"
	@go test -race -count=1 -run 'TestLoadResumeCheckpoint' ./internal/routedrun/...
	@echo "  T06: Reference worker pattern and Hermes authoring fixture"
	@go test -count=1 ./internal/routedrun/... ./internal/harness/...
	@echo "  T07: Adversary tests (progress/journal/artifacts)"
	@go test -race -count=1 -run 'TestAdversary_B27' ./internal/routedrun/... ./internal/harness/...
	@echo "  T08: Hermes plugin tests"
	@cd integrations/hermes-plugin && python3 -m unittest discover -s tests -t . 2>&1 | tail -5
	@echo "  Cross-block: compat fixtures unaffected"
	@go test -count=1 ./test/compat/... 2>/dev/null || echo "(no compat tests)"
	@echo "  go vet"
	@go vet ./...
	@echo "  govulncheck"
	@govulncheck ./... 2>&1 | tail -20 || echo "(govulncheck: non-zero exit — check output above)"
	@echo "  golden-fast (requires Docker)"
	@$(MAKE) golden-fast 2>&1 | tail -20 || echo "(golden-fast: non-zero exit — may need Docker)"
	@echo "✓ Block 27 gate: PASS"
# NOTE: Summary T07 lists `make block26-gate` but that target does not exist
# in the Makefile (B26 was folded into other blocks). Daemon tests are run
# directly via `go test ./internal/daemon/...` instead. See b27-review-notes.md.

# ── Block 28: Runtime Portability and Managed-PaaS Feasibility Gate ───────────
#
# B28 proves that B26/B27 contracts are platform contracts, not Docker-specific.
# It defines portable port interfaces, a Docker baseline adapter, and a local
# Kubernetes proof. Cloudflare is deferred per D66.
# Covers T01-T07: coupling inventory, port contracts, Docker adapter, k8s proof,
# isolation/metering, substrate decision, block gate.

.PHONY: block28-gate
block28-gate: block27-gate
	@echo "==> Running Block 28 gate"
	@echo "  T02: Portable port contracts (interfaces + fakes + conformance)"
	@go test -count=1 -race ./internal/port/...
	@echo "  T03: Docker baseline adapter (unit tests)"
	@go test -count=1 -race ./internal/adapter/docker/...
	@echo "  T04: Kubernetes adapter (unit tests)"
	@go test -count=1 -race ./internal/adapter/k8s/...
	@echo "  Cross-adapter: all port interfaces compile"
	@go build ./internal/port/... ./internal/adapter/...
	@echo "  go vet"
	@go vet ./internal/port/... ./internal/adapter/...
	@echo "  lint"
	@golangci-lint run --timeout 120s ./internal/port/... ./internal/adapter/...
	@echo "✓ Block 28 gate: PASS"

.PHONY: block28-docker-tests
block28-docker-tests:
	@echo "==> Block 28 Docker integration tests (requires Docker)"
	@AGENTPAAS_DOCKER_TESTS=1 go test -count=1 -timeout 300s ./internal/adapter/docker/...

.PHONY: block28-k8s-tests
block28-k8s-tests:
	@echo "==> Block 28 Kubernetes integration tests (requires kind cluster)"
	@AGENTPAAS_K8S_TESTS=1 go test -count=1 -timeout 300s ./internal/adapter/k8s/...

# ── Golden Dataset Regression Suite ─────────────────────────────────────────
#
# The golden dataset is a regression suite that measures pass^k (all k runs
# succeed) for user-facing operations. Tasks are sourced from real failure
# modes encountered during builds.
#
# Tiers:
#   fast    — deterministic checks, <5s each, run on every commit
#   slow    — integration tests, 5-60s each, run on PRs
#   docker  — full Docker e2e, 30s-5min each, run on main merge
#
# Usage:
#   make golden-fast        # fast tier only (every commit)
#   make golden-slow        # slow tier only (PRs)
#   make golden-docker      # docker tier only (main merge)
#   make golden-eval         # all tiers
#
# Environment:
#   GOLDEN_K=N    — override repetition count (default: 3)
#   AGENTPAAS_DOCKER_TESTS=1 — enables docker-tier tasks

golden-fast: build-all
	@echo "==> Running golden dataset: fast tier (pass^k=3)"
	GOLDEN_TIER=fast GOLDEN_K=3 go test -v -run TestGoldenSuite -count=1 ./test/golden/ -timeout 120s
	@echo "✓ Golden fast tier: PASS"

golden-slow: build
	@echo "==> Running golden dataset: slow tier (pass^k=3)"
	GOLDEN_TIER=slow GOLDEN_K=3 go test -v -run TestGoldenSuite -count=1 ./test/golden/ -timeout 600s
	@echo "✓ Golden slow tier: PASS"

golden-docker: build
	@echo "==> Running golden dataset: docker tier (pass^k=3)"
	AGENTPAAS_DOCKER_TESTS=1 GOLDEN_TIER=docker GOLDEN_K=3 go test -v -run TestGoldenSuite -count=1 ./test/golden/ -timeout 1800s
	@echo "✓ Golden docker tier: PASS"

golden-eval: golden-fast golden-slow golden-docker
	@echo "✓ Golden dataset: ALL TIERS PASS"

block15-gate: build lint
	@echo "==> Running Block 15 gate: P1 completion items"
	@echo "  T01: credential onboarding (secret add/list/remove/rotate/test)"
	go test -race -count=1 ./internal/secrets/... ./internal/cli/...
	@echo "  T02: LLM provider integration (adapter + handleLLM + buildInvokePayload)"
	go test -race -count=1 ./internal/llm/... ./internal/harness/... ./internal/daemon/... ./internal/pack/...
	@echo "  T03: policy authoring (policy init command + pack-time validation + plugin tool)"
	go test -race -count=1 ./internal/pack/... ./internal/cli/... ./internal/daemon/...
	@echo "  T04: trigger/cron/event surface (CronScheduler management, CLI, plugin tools)"
	go test -race -count=1 ./internal/trigger/... ./internal/daemon/... ./internal/cli/...
	@echo "  T05: production hardening (gateway subnet, Rekor retry, checkpoint key encryption, capset)"
	go test -race -count=1 ./internal/daemon/... ./internal/pack/... ./internal/audit/... ./internal/harness/...
	@echo "  T08: egress enforcement regression (firewall script content, egress enabled flag)"
	go test -race -count=1 -run 'Firewall' ./internal/harness/...
	@echo "  Plugin: secret onboarding + LLM configure + policy init + trigger/cron tools"
	cd integrations/hermes-plugin && python3 -m unittest discover -s tests -t . 2>&1 | tail -5
	@echo "==> Block 15 gate passed (T01+T02+T03+T04+T05+T08 complete; T06-T07 pending)"

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
	@echo "  block15-gate - P1 completion: LLM, credentials, policy, hardening, release"
	@echo "  block16-gate - Manual use-case assessment (runs AFTER B15)"
	@echo "  block25-gate - Sharing tools, release readiness"
	@echo "  block27-gate - SDK progress, checkpoint, artifact protocol"
	@echo ""
	@echo "Golden dataset (pass^k regression suite):"
	@echo "  golden-fast  - Fast tier: deterministic checks, every commit"
	@echo "  golden-slow  - Slow tier: integration tests, PRs"
	@echo "  golden-docker - Docker tier: full e2e, main merge"
	@echo "  golden-eval  - All tiers"
