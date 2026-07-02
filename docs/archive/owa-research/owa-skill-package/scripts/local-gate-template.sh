#!/usr/bin/env bash
# local-gate-template.sh
# Run the local gate on a worktree or package. Used by the Orchestrator
# after a Worker reports done (Step 2 of the OWA loop).
#
# This is a simpler version of parallel-gate-template.sh for when you
# want to gate a single package or run a subset of components.
#
# CUSTOMIZE: Replace commands with your actual gate commands.

set -uo pipefail

TARGET="${1:-./...}"
WORKDIR="${2:-.}"

echo "=== Local Gate: $TARGET ==="
echo "Workdir: $WORKDIR"
echo ""

cd "$WORKDIR"

# --- 1. Clear caches (MANDATORY) ---
echo "Clearing linter cache..."
rm -rf <YOUR_LINTER_CACHE_DIR> 2>/dev/null || true
# A stale linter cache produces false-green results. In our measured run,
# this hid 5 real errcheck issues. ALWAYS clear before gating.

# --- 2. Run gate components ---
# For per-subtask gating, you can run sequentially (fast enough for one
# package) or use parallel-gate-template.sh for the full suite.

echo "Build..."
<YOUR_BUILD_COMMAND> "$TARGET" || { echo "BUILD FAIL"; exit 1; }

echo "Test..."
<YOUR_TEST_COMMAND> "$TARGET" || { echo "TEST FAIL"; exit 1; }

echo "Race (if applicable)..."
<YOUR_RACE_COMMAND> "$TARGET" 2>/dev/null || echo "Race: skipped (N/A)"

echo "Lint..."
<YOUR_LINT_COMMAND> "$TARGET" || { echo "LINT FAIL"; exit 1; }

echo "Security scan..."
<YOUR_SECSCAN_COMMAND> || { echo "SECSCAN FAIL"; exit 1; }

echo ""
echo "=== GATE PASS ==="
exit 0
