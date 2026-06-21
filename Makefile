.PHONY: build test proto lint race osv e2e-network redteam-smoke

build:
	go build ./...

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
	@echo "Error: redteam-smoke is not implemented until Block 12" && exit 1

.PHONY: block1-gate
block1-gate: proto build test lint
	@echo "Block 1 gate: PASS"

.PHONY: block2-gate block3-gate block4-gate block5-gate block6-gate block7-gate block8-gate block9-gate block10-gate block11-gate block12-gate block13-gate block14-gate block15-gate

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

block8-gate:
	@echo "Error: block8-gate is not implemented until Block 8" && exit 1

block9-gate:
	@echo "Error: block9-gate is not implemented until Block 9" && exit 1

block10-gate:
	@echo "Error: block10-gate is not implemented until Block 10" && exit 1

block11-gate:
	@echo "Error: block11-gate is not implemented until Block 11" && exit 1

block12-gate:
	@echo "Error: block12-gate is not implemented until Block 12" && exit 1

block13-gate:
	@echo "Error: block13-gate is not implemented until Block 13" && exit 1

block14-gate:
	@echo "Error: block14-gate is not implemented until Block 14" && exit 1

block15-gate:
	@echo "Error: block15-gate is not implemented until Block 15" && exit 1

.PHONY: gates
gates: ## List all available gate targets
	@echo "Available gates:"
	@echo "  block1-gate  - Repo bootstrap, proto contracts, CI skeleton (ACTIVE)"
	@echo "  block2-gate  - Daemon skeleton, CLI plumbing (ACTIVE)"
	@echo "  block3-gate  - Identity service, audit hash chain (ACTIVE)"
	@echo "  block4-gate  - Policy engine (ACTIVE)"
	@echo "  block5-gate  - Runtime driver, Docker integration (ACTIVE)"
	@echo "  block6-gate  - Agent harness (ACTIVE)"
	@echo "  block7-gate  - Runtime driver, Docker integration (not implemented)"
	@echo "  block8-gate  - Gateway sidecar, network enforcement (not implemented)"
	@echo "  block9-gate  - Trigger API, event bus (not implemented)"
	@echo "  block10-gate - OTel pipeline, dashboard (not implemented)"
	@echo "  block11-gate - Hermes operator contract (not implemented)"
	@echo "  block12-gate - Red-team smoke gate (not implemented)"
	@echo "  block13-gate - Hermes integration plugin (not implemented)"
	@echo "  block14-gate - Install path, docs, demo, release (not implemented)"
	@echo "  block15-gate - Sequencing, founder calendar (not implemented)"
