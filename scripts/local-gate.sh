#!/usr/bin/env bash
#
# local-gate.sh — Run gate verification for a specific package or block
#
# Usage:
#   local-gate.sh <package>     — test + lint a single package
#   local-gate.sh block <N>     — run make blockN-gate
#   local-gate.sh full           — build + test + race + lint + osv (no Docker)
#   local-gate.sh e2e            — e2e-network tests (needs Docker)
#
# Docker: if you use Colima, export DOCKER_HOST before running, e.g.
#   export DOCKER_HOST="unix://$HOME/.colima/default/docker.sock"
#
# Exits 0 on pass, non-zero on fail.
# Prints a compact summary at the end.

set -euo pipefail

# Resolve repo dir from script location (portable across machines).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_DIR"

# Ensure homebrew binaries are on PATH if present (no-op on non-macOS).
if [ -d /opt/homebrew/bin ]; then
  export PATH="/opt/homebrew/bin:$PATH"
fi
export PATH="$(go env GOPATH 2>/dev/null || echo "$HOME/go")/bin:$PATH"

TARGET="${1:?Usage: local-gate.sh <package|block N|full|e2e>}"
GATE_EXIT=0

run_check() {
  local label="$1"
  shift
  echo -n "  $label ... "
  if output=$("$@" 2>&1); then
    echo "PASS"
    return 0
  else
    echo "FAIL"
    echo "$output" | tail -15 | sed 's/^/    /'
    return 1
  fi
}

echo "=== Local Gate: $TARGET ==="
echo ""

case "$TARGET" in
  block*)
    BLOCK_NUM="${TARGET#block}"
    BLOCK_NUM="${BLOCK_NUM# }"
    if [ "$BLOCK_NUM" = "5" ]; then
      # Block 5 gate needs Docker
      export AGENTPAAS_DOCKER_TESTS=1
      run_check "build" make build || GATE_EXIT=1
      run_check "test" make test || GATE_EXIT=1
      run_check "race" make race || GATE_EXIT=1
      run_check "lint" make lint || GATE_EXIT=1
      run_check "osv" make osv || GATE_EXIT=1
      run_check "runtime-race" go test -race -count=1 ./internal/runtime/... || GATE_EXIT=1
      echo "  e2e-network (split into batches)..."
      # Run e2e-network test groups (unique Docker net names = safe parallel)
      for pattern in TestE2E_Network_PositivePath TestAdversaryB5T04a TestE2E_HostBridgeProbes TestAdversaryB5T04b TestE2E_ProtocolBypassProbes TestAdversaryB5T04c TestE2E_TopologyInspect TestE2E_PartialCreateCleanup TestAdversaryB5T04d TestE2E_CrashReconciliation TestE2E_SecretFreeDebugOutput; do
        run_check "$pattern" go test -count=1 -run "$pattern" ./internal/runtime/... -timeout 180s || GATE_EXIT=1
      done
    else
      echo "  Running make block${BLOCK_NUM}-gate..."
      if make "block${BLOCK_NUM}-gate" 2>&1 | tail -20; then
        echo "  block${BLOCK_NUM}-gate: PASS"
      else
        echo "  block${BLOCK_NUM}-gate: FAIL"
        GATE_EXIT=1
      fi
    fi
    ;;
  full)
    run_check "build" make build || GATE_EXIT=1
    run_check "test" make test || GATE_EXIT=1
    run_check "race" make race || GATE_EXIT=1
    run_check "lint" make lint || GATE_EXIT=1
    run_check "osv" make osv || GATE_EXIT=1
    ;;
  e2e)
    export AGENTPAAS_DOCKER_TESTS=1
    echo "  e2e-network tests (sequential, Docker required)..."
    for pattern in TestE2E_Network_PositivePath TestAdversaryB5T04a TestE2E_HostBridgeProbes TestAdversaryB5T04b TestE2E_ProtocolBypassProbes TestAdversaryB5T04c TestE2E_TopologyInspect TestE2E_PartialCreateCleanup TestAdversaryB5T04d TestE2E_CrashReconciliation TestE2E_SecretFreeDebugOutput; do
      run_check "$pattern" go test -count=1 -run "$pattern" ./internal/runtime/... -timeout 180s || GATE_EXIT=1
    done
    ;;
  *)
    # Assume it's a package path (e.g. ./internal/harness/...)
    run_check "build" go build ./... || GATE_EXIT=1
    run_check "test-race" go test -race -count=1 "$TARGET" || GATE_EXIT=1
    run_check "lint" golangci-lint run "$TARGET" || GATE_EXIT=1
    ;;
esac

echo ""
if [ "$GATE_EXIT" -eq 0 ]; then
  echo "=== GATE: PASS ==="
else
  echo "=== GATE: FAIL ==="
fi
exit "$GATE_EXIT"
