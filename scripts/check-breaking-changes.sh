#!/bin/bash
# check-breaking-changes.sh
# Checks proto files for breaking changes against the last committed state.
#
# Usage: ./scripts/check-breaking-changes.sh [--against BRANCH]
#   Default: checks against the 'main' branch
#   Pass --against <ref> to check against a different git ref

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SCRIPT_DIR"

AGAINST="${2:-main}"

echo "Checking breaking changes against '${AGAINST}'..."
echo

if buf breaking --against ".git#branch=${AGAINST}" 2>&1; then
    echo
    echo "OK: No breaking changes detected against '${AGAINST}'"
    exit 0
else
    EXIT_CODE=$?
    echo
    echo "BREAKING CHANGE DETECTED against '${AGAINST}'"
    exit "${EXIT_CODE}"
fi