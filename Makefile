.PHONY: build build-harness-linux build-harness-linux-amd64 build-all test soak-test proto lint race osv install-plugin clean fmt vet

# LDFLAGS_VERSION stamps the dev version into all binaries when building without
# a release tag. goreleaser overrides these at tag time with the actual version.
LDFLAGS_VERSION := -X github.com/AgentPaaS-ai/agentpaas/internal/daemon.CLIVersion=0.4.0-dev
LDFLAGS_VERSION += -X github.com/AgentPaaS-ai/agentpaas/internal/daemon.DaemonVersion=0.4.0-dev
LDFLAGS_VERSION += -X github.com/AgentPaaS-ai/agentpaas/internal/daemon.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

LDFLAGS_HARNESS := $(LDFLAGS_VERSION)
LDFLAGS_HARNESS += -X github.com/AgentPaaS-ai/agentpaas/internal/harness.HarnessVersion=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

BUILD_FLAGS := -trimpath -ldflags "$(LDFLAGS_VERSION)"
BUILD_FLAGS_HARNESS := -trimpath -ldflags "$(LDFLAGS_HARNESS) -s -w"

build:
	mkdir -p bin
	go build $(BUILD_FLAGS) -o bin/agentpaas ./cmd/agent
	go build $(BUILD_FLAGS) -o bin/agentpaasd ./cmd/agentpaasd
	go build $(BUILD_FLAGS_HARNESS) -o bin/agentpaas-harness ./cmd/harness

build-harness-linux:
	mkdir -p bin
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(BUILD_FLAGS_HARNESS) -o bin/agentpaas-harness-linux ./cmd/harness

build-harness-linux-amd64:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS_HARNESS) -o bin/agentpaas-harness-linux-amd64 ./cmd/harness

build-all: build build-harness-linux build-harness-linux-amd64

test:
	go test ./...

# Long-running operator soak tests (TestSoak_*, TestOperatorSoak_*) are gated
# behind the `soak` build tag and are NOT part of `make test`. They need Docker
# plus a real daemon and run 30+ minutes each. Run them explicitly with:
#   AGENTPAAS_DOCKER_TESTS=1 go test -tags=soak ./internal/operator/ -run TestSoak -v
soak-test:
	AGENTPAAS_DOCKER_TESTS=1 go test -tags=soak ./internal/operator/ -v

proto:
	buf generate

lint:
	golangci-lint run --timeout 5m

race:
	go test -race ./...

osv:
	osv-scanner scan -r .

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf bin/

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
