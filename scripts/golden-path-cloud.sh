#!/usr/bin/env bash
# Backward-compatible name for the automated cold-start cloud golden path.
set -euo pipefail
exec "$(dirname "${BASH_SOURCE[0]}")/golden-path-cold-start.sh" "$@"
