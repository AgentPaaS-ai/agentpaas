#!/usr/bin/env bash
# Backward-compatible name for the automated founder-cold cloud golden path.
set -euo pipefail
exec "$(dirname "${BASH_SOURCE[0]}")/golden-path-founder-cold.sh" "$@"
