#!/usr/bin/env bash
# reset-agentpaas-test.sh — Restore agentpaas-test profile to a clean baseline
#
# Usage:
#   ./scripts/reset-agentpaas-test.sh          # reset from baseline
#   ./scripts/reset-agentpaas-test.sh --save   # save current state as new baseline
#
# What this does:
#   1. Restores config.yaml from test/profile-baselines/agentpaas-test-clean.yaml
#   2. Removes plugin symlinks (agentpaas)
#   3. Removes SOUL.md, memories, checkpoints, cron
#   4. Stops the gateway
#   5. Deletes all sessions from state.db
#
# After running this, the profile is a blank slate ready for acceptance testing.

set -euo pipefail

PROFILE="agentpaas-test"
PROFILE_DIR="$HOME/.hermes/profiles/$PROFILE"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BASELINE="$REPO_ROOT/test/profile-baselines/agentpaas-test-clean.yaml"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[reset]${NC} $1"; }
warn() { echo -e "${YELLOW}[warn]${NC} $1"; }
err()  { echo -e "${RED}[error]${NC} $1"; }

# --- Save mode ---
if [[ "${1:-}" == "--save" ]]; then
    log "Saving current $PROFILE config as new baseline..."
    cp "$PROFILE_DIR/config.yaml" "$BASELINE"
    log "Saved to $BASELINE"
    exit 0
fi

# --- Preflight ---
if [[ ! -f "$BASELINE" ]]; then
    err "Baseline not found: $BASELINE"
    err "Run: $0 --save  (from a known-clean state)"
    exit 1
fi

if [[ ! -d "$PROFILE_DIR" ]]; then
    err "Profile directory not found: $PROFILE_DIR"
    exit 1
fi

log "Resetting profile: $PROFILE"
log "Baseline: $BASELINE"

# --- Step 1: Restore config.yaml ---
cp "$BASELINE" "$PROFILE_DIR/config.yaml"
log "Restored config.yaml from baseline"

# --- Step 2: Remove plugin (symlink or directory from GitHub clone) ---
if [[ -d "$PROFILE_DIR/plugins/agentpaas" ]]; then
    rm -rf "$PROFILE_DIR/plugins/agentpaas"
    log "Removed agentpaas plugin directory"
elif [[ -L "$PROFILE_DIR/plugins/agentpaas" ]]; then
    rm -f "$PROFILE_DIR/plugins/agentpaas"
    log "Removed agentpaas plugin symlink"
fi

# --- Step 3: Remove SOUL.md ---
rm -f "$PROFILE_DIR/SOUL.md"
log "Removed SOUL.md"

# --- Step 4: Clear memories, checkpoints, cron ---
rm -f "$PROFILE_DIR/memories/"*.md "$PROFILE_DIR/memories/"*.lock 2>/dev/null || true
rm -rf "$PROFILE_DIR/checkpoints/"* 2>/dev/null || true
rm -rf "$PROFILE_DIR/cron/"* 2>/dev/null || true
log "Cleared memories, checkpoints, cron"

# --- Step 5: Stop gateway ---
hermes -p "$PROFILE" gateway stop 2>/dev/null || true
log "Stopped gateway"

# --- Step 6: Delete all sessions from state.db ---
# Sessions live in SQLite, not the sessions/ directory.
# Use hermes CLI directly (handles both UUID and date-based IDs).
SESSION_IDS=$(hermes -p "$PROFILE" sessions list 2>/dev/null | grep -oE '[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}') || true
for sid in $SESSION_IDS; do
    hermes -p "$PROFILE" sessions delete "$sid" --yes 2>/dev/null || true
    log "Deleted session: $sid"
done
if [[ -z "$SESSION_IDS" ]]; then
    log "No sessions to delete"
fi

# --- Step 7: Clean up leftover agent project dirs and deployed agent state ---
# These accumulate from prior test runs and cause the test agent to reuse
# stale code instead of building fresh.
LEFTOVER_DIRS=(
    "$HOME/weather-agent"
    "/tmp/weather-test-agent"
    "/tmp/e2e-final-test"
    "/tmp/deps-test"
    "/tmp/scaffold-test"
    "/tmp/egress-denial-test"
    "/tmp/test-abs"
)
for d in "${LEFTOVER_DIRS[@]}"; do
    if [[ -d "$d" ]]; then
        rm -rf "$d"
        log "Removed leftover agent dir: $d"
    fi
done

# Clean deployed agent state and run artifacts from AgentPaaS home
if [[ -d "$HOME/.agentpaas/state/agents" ]]; then
    rm -rf "$HOME/.agentpaas/state/agents"/*
    log "Cleared deployed agent state"
fi
if [[ -d "$HOME/.agentpaas/state/runs" ]]; then
    rm -rf "$HOME/.agentpaas/state/runs"/*
    log "Cleared run state"
fi
rm -f "$HOME/.agentpaas/state/audit-checkpoint-key.der" 2>/dev/null || true

# --- Step 8: Verify ---
log "Verification:"
echo "  plugins.enabled: $(python3 -c "import yaml,pathlib; cfg=yaml.safe_load(pathlib.Path('$PROFILE_DIR/config.yaml').read_text()); print(cfg.get('plugins',{}).get('enabled'))")"
echo "  platform_toolsets.cli: $(python3 -c "import yaml,pathlib; cfg=yaml.safe_load(pathlib.Path('$PROFILE_DIR/config.yaml').read_text()); print(cfg.get('platform_toolsets',{}).get('cli'))")"
echo "  memories/: $(ls "$PROFILE_DIR/memories/" 2>/dev/null | wc -l | tr -d ' ') files"
echo "  plugins/ dir: $(ls "$PROFILE_DIR/plugins/" 2>/dev/null | wc -l | tr -d ' ') entries"
echo "  SOUL.md: $([[ -f "$PROFILE_DIR/SOUL.md" ]] && echo 'EXISTS' || echo 'absent')"

echo ""
log "✓ Profile '$PROFILE' is now a clean baseline."
log "  Start a fresh session: hermes -p $PROFILE"
