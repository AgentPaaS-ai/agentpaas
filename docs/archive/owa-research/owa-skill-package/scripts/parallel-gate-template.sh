#!/usr/bin/env bash
# parallel-gate-template.sh
# Run gate components in PARALLEL instead of sequentially.
# Collapses 3-5 min sequential → ~1 min parallel (slowest component).
#
# CUSTOMIZE: Replace the make targets / commands with your actual gate
# commands. This template uses a Makefile pattern (make build, make test,
# etc.) but you can substitute any command.

set -uo pipefail

WORKDIR="${1:-.}"
LOG_DIR="/tmp/owa-gate-$$"
mkdir -p "$LOG_DIR"

echo "=== Parallel Gate Execution ==="
echo "Workdir: $WORKDIR"
echo "Logs: $LOG_DIR"
echo ""

# --- 1. Clear all caches (MANDATORY — stale cache = false green) ---
echo "Clearing tool caches..."
rm -rf <YOUR_LINTER_CACHE_DIR> 2>/dev/null || true   # e.g. ~/Library/Caches/golangci-lint
rm -rf <YOUR_BUILD_CACHE_DIR> 2>/dev/null || true     # e.g. .cache/tsc
rm -rf <YOUR_TEST_CACHE_DIR> 2>/dev/null || true      # if your test runner caches
# NOTE: linter cache clearing is the most critical. A stale linter cache
# produces false-green lint runs — the gate "passes" on yesterday's results.
# In our measured run, this hid 5 real errcheck issues in one incident.

# --- 2. Launch all gate components in parallel ---
# Each component logs to its own file. We use tmux for portability.
# If you don't have tmux, use nohup + & (but tmux survives disconnects).

declare -a COMPONENTS=(build test race lint secscan)
declare -A COMMANDS=(
  [build]="<YOUR_BUILD_COMMAND>"       # e.g. make build
  [test]="<YOUR_TEST_COMMAND>"         # e.g. make test
  [race]="<YOUR_RACE_COMMAND>"         # e.g. go test -race ./... (or skip if N/A)
  [lint]="<YOUR_LINT_COMMAND>"         # e.g. make lint / golangci-lint run
  [secscan]="<YOUR_SECSCAN_COMMAND>"   # e.g. make osv / npm audit / cargo audit
)

for comp in "${COMPONENTS[@]}"; do
  echo "Launching: $comp"
  tmux new-session -d -s "gate-$comp" \
    "cd '$WORKDIR' && ${COMMANDS[$comp]} 2>&1 | tee '$LOG_DIR/$comp.log'; echo \"EXIT=\$?\" >> '$LOG_DIR/$comp.log'"
done

# --- 3. Poll for completion ---
echo ""
echo "Waiting for all components to finish..."
ALL_DONE=false
while [ "$ALL_DONE" = false ]; do
  ALL_DONE=true
  for comp in "${COMPONENTS[@]}"; do
    if tmux has-session -t "gate-$comp" 2>/dev/null; then
      ALL_DONE=false
    fi
  done
  sleep 5
done

# --- 4. Collect results ---
echo ""
echo "=== Gate Results ==="
OVERALL_PASS=true
for comp in "${COMPONENTS[@]}"; do
  EXIT_CODE=$(grep "^EXIT=" "$LOG_DIR/$comp.log" | tail -1 | cut -d= -f2)
  if [ "$EXIT_CODE" = "0" ]; then
    echo "  $comp: PASS"
  else
    echo "  $comp: FAIL (exit $EXIT_CODE)"
    echo "    See: $LOG_DIR/$comp.log"
    OVERALL_PASS=false
  fi
done

echo ""
if [ "$OVERALL_PASS" = true ]; then
  echo "=== GATE PASS ==="
  exit 0
else
  echo "=== GATE FAIL ==="
  echo "Dispatch a fix worker for the failing component(s)."
  exit 1
fi
