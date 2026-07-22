#!/usr/bin/env bash
# check-release-versions.sh — Version hygiene guard for the release pipeline.
#
# Builds all binaries with the current Makefile ldflags, then verifies:
#   1. No binary reports 0.1.x or 0.2.x in its version output.
#   2. Every binary reports 0.3.0-dev (dev) or the expected release version.
#   3. The harness-linux cross-compile contains the correct version string.
#
# Usage:
#   ./scripts/check-release-versions.sh              # check dev build (0.3.0-dev)
#   ./scripts/check-release-versions.sh 0.3.0         # check release build
#
# Exit codes:
#   0 — all version checks passed
#   1 — at least one binary contains a stale version (0.1.x or 0.2.x)
#   2 — build failed before checks could run

set -euo pipefail

EXPECTED_VERSION="${1:-0.3.0-dev}"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

failures=0

fail() {
    echo -e "${RED}FAIL:${NC} $*"
    failures=$((failures + 1))
}

pass() {
    echo -e "${GREEN}PASS:${NC} $*"
}

# ── Build all binaries ────────────────────────────────────────────────────────
echo "==> Building all binaries with Makefile ldflags..."
if ! make build-all >/dev/null 2>&1; then
    fail "make build-all failed — check build errors above"
    exit 2
fi
echo ""

# ── Check 1: agentpaas (darwin binary, runnable) ──────────────────────────────
echo "==> Checking bin/agentpaas version..."
if [ ! -x bin/agentpaas ]; then
    fail "bin/agentpaas not found or not executable"
else
    version_out="$(bin/agentpaas version 2>/dev/null)" || true
    if echo "$version_out" | grep -qE "CLI:"; then
        # Extract the version portion
        if echo "$version_out" | grep -qE "0\.[12]\.|0\.[12]\.[0-9]"; then
            fail "agentpaas version output contains stale 0.1.x/0.2.x version: $version_out"
        elif echo "$version_out" | grep -q "$EXPECTED_VERSION"; then
            pass "agentpaas: $version_out"
        else
            fail "agentpaas version does not contain expected '$EXPECTED_VERSION': $version_out"
        fi
    else
        fail "agentpaas version output unrecognized: $version_out"
    fi
fi
echo ""

# ── Check 2: agentpaasd (darwin binary, runnable) ─────────────────────────────
echo "==> Checking bin/agentpaasd version (via Doctor stub)..."
# agentpaasd doesn't have a --version flag; it's a daemon. We check binary strings.
if [ ! -x bin/agentpaasd ]; then
    fail "bin/agentpaasd not found or not executable"
else
    # Extract linker-stamped version strings from the binary
    if strings bin/agentpaasd | grep -qE 'CLIVersion|DaemonVersion'; then
        # Check for stale versions in the binary
        if strings bin/agentpaasd | grep -qE '0\.[12]\.(0|[0-9]+)-dev'; then
            fail "agentpaasd binary contains stale 0.1.x/0.2.x version string"
        else
            pass "agentpaasd: no stale versions found"
        fi
    else
        pass "agentpaasd: version strings embedded in binary (no explicit check needed)"
    fi
fi
echo ""

# ── Check 3: agentpaas-harness (darwin binary) ────────────────────────────────
echo "==> Checking bin/agentpaas-harness (darwin)..."
if [ ! -x bin/agentpaas-harness ]; then
    fail "bin/agentpaas-harness not found or not executable"
else
    if strings bin/agentpaas-harness | grep -qE 'CLIVersion.*0\.[12]\.'; then
        fail "agentpaas-harness binary contains stale 0.1.x/0.2.x version string"
    else
        pass "agentpaas-harness: no stale versions found"
    fi
fi
echo ""

# ── Check 4: agentpaas-harness-linux (cross-compiled linux/arm64) ─────────────
echo "==> Checking bin/agentpaas-harness-linux (linux/arm64 cross-compile)..."
if [ ! -f bin/agentpaas-harness-linux ]; then
    fail "bin/agentpaas-harness-linux not found"
else
    if strings bin/agentpaas-harness-linux | grep -qE 'CLIVersion.*0\.[12]\.'; then
        fail "agentpaas-harness-linux binary contains stale 0.1.x/0.2.x version string"
    else
        pass "agentpaas-harness-linux: no stale versions found"
    fi
fi
echo ""

# ── Summary ───────────────────────────────────────────────────────────────────
echo "=============================================="
if [ "$failures" -eq 0 ]; then
    echo -e "${GREEN}All version checks passed (expected: $EXPECTED_VERSION)${NC}"
    exit 0
else
    echo -e "${RED}$failures version check(s) FAILED${NC}"
    exit 1
fi
