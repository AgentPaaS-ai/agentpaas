#!/bin/bash
# check-codegen-reproducible.sh
# Verifies that generated Go code from proto files is byte-identical
# between consecutive buf generate runs.
#
# Usage: ./scripts/check-codegen-reproducible.sh
#   Exit 0: generated code is reproducible (byte-identical)
#   Exit 1: codegen drift detected

set -euo pipefail

# Ensure buf is available
if ! command -v buf &>/dev/null; then
    echo "ERROR: buf not found in PATH"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SCRIPT_DIR"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Save current generated .go files to temp directory
echo "Saving current generated Go files..."
find api -name '*.pb.go' -o -name '*_grpc.pb.go' -o -name '*.pb.gw.go' | while read -r f; do
    mkdir -p "$TMPDIR/$(dirname "$f")"
    cp "$f" "$TMPDIR/$f"
done

# Regenerate
echo "Regenerating with buf generate..."
buf generate

# Compare saved files with regenerated output
echo "Comparing generated files..."
DRIFT=0
if [ -d "$TMPDIR/api" ]; then
    for f in $(find "$TMPDIR/api" -name '*.go' -type f); do
        rel="${f#$TMPDIR/}"
        if [ ! -f "$rel" ]; then
            echo "FILE REMOVED: $rel"
            DRIFT=1
        elif ! diff -q "$f" "$rel" >/dev/null 2>&1; then
            echo "DIFFERENCE: $rel"
            diff "$f" "$rel" || true
            DRIFT=1
        fi
    done
    # Check for new files that didn't exist before
    for f in $(find api -name '*.go' -type f); do
        if [ ! -f "$TMPDIR/$f" ]; then
            echo "NEW FILE: $f"
            DRIFT=1
        fi
    done
fi

if [ "$DRIFT" -eq 0 ]; then
    echo "OK: Codegen is reproducible — generated files are byte-identical"
    exit 0
else
    echo "ERROR: Codegen drift detected — generated files differ from committed versions"
    exit 1
fi