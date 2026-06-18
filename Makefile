.PHONY: build test proto lint race osv e2e-network redteam-smoke

build:
	go build ./...

test:
	go test ./...

proto:
	buf generate

lint:
	golangci-lint run

race:
	go test -race ./...

osv:
	osv-scanner scan source -r .

e2e-network:
	@echo "Error: e2e-network is not implemented until later blocks" && exit 1

redteam-smoke:
	@echo "Error: redteam-smoke is not implemented until Block 12" && exit 1

.PHONY: block1-gate
block1-gate: proto build test lint
	@echo "Block 1 gate: PASS"

.PHONY: block2-gate block3-gate block4-gate block5-gate block6-gate block7-gate block8-gate block9-gate block10-gate block11-gate block12-gate block13-gate block14-gate block15-gate

block2-gate:
	@echo "Error: block2-gate is not implemented until Block 2" && exit 1

block3-gate:
	@echo "Error: block3-gate is not implemented until Block 3" && exit 1

block4-gate:
	@echo "Error: block4-gate is not implemented until Block 4" && exit 1

block5-gate:
	@echo "Error: block5-gate is not implemented until Block 5" && exit 1

block6-gate:
	@echo "Error: block6-gate is not implemented until Block 6" && exit 1

block7-gate:
	@echo "Error: block7-gate is not implemented until Block 7" && exit 1

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
	@echo "  block2-gate  - Daemon skeleton, CLI plumbing (not implemented)"
	@echo "  block3-gate  - Identity service, audit hash chain (not implemented)"
	@echo "  block4-gate  - Policy engine (not implemented)"
	@echo "  block5-gate  - Secrets broker (not implemented)"
	@echo "  block6-gate  - Agent harness (not implemented)"
	@echo "  block7-gate  - Runtime driver, Docker integration (not implemented)"
	@echo "  block8-gate  - Gateway sidecar, network enforcement (not implemented)"
	@echo "  block9-gate  - Trigger API, event bus (not implemented)"
	@echo "  block10-gate - OTel pipeline, dashboard (not implemented)"
	@echo "  block11-gate - Hermes operator contract (not implemented)"
	@echo "  block12-gate - Red-team smoke gate (not implemented)"
	@echo "  block13-gate - Hermes integration plugin (not implemented)"
	@echo "  block14-gate - Install path, docs, demo, release (not implemented)"
	@echo "  block15-gate - Sequencing, founder calendar (not implemented)"