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

# --- Step 2: Remove plugin symlinks ---
rm -f "$PROFILE_DIR/plugins/agentpaas"
log "Removed agentpaas plugin symlink"

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
if hermes -p "$PROFILE" sessions list 2>/dev/null | grep -qE '^\s*[a-f0-9-]{36}'; then
    # Extract session IDs and delete each
    SESSION_IDS=$(hermes -p "$PROFILE" sessions list 2>/dev/null | grep -oE '[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}')
    for sid in $SESSION_IDS; do
        hermes -p "$PROFILE" sessions delete "$sid" --yes 2>/dev/null || true
        log "Deleted session: $sid"
    done
else
    log "No sessions to delete"
fi

# --- Step 7: Verify ---
log "Verification:"
echo "  plugins.enabled: $(python3 -c "import yaml,pathlib; cfg=yaml.safe_load(pathlib.Path('$PROFILE_DIR/config.yaml').read_text()); print(cfg.get('plugins',{}).get('enabled'))")"
echo "  platform_toolsets.cli: $(python3 -c "import yaml,pathlib; cfg=yaml.safe_load(pathlib.Path('$PROFILE_DIR/config.yaml').read_text()); print(cfg.get('platform_toolsets',{}).get('cli'))")"
echo "  memories/: $(ls "$PROFILE_DIR/memories/" 2>/dev/null | wc -l | tr -d ' ') files"
echo "  plugins/ dir: $(ls "$PROFILE_DIR/plugins/" 2>/dev/null | wc -l | tr -d ' ') entries"
echo "  SOUL.md: $([[ -f "$PROFILE_DIR/SOUL.md" ]] && echo 'EXISTS' || echo 'absent')"

echo ""
log "✓ Profile '$PROFILE' is now a clean baseline."
log "  Start a fresh session: hermes -p $PROFILE"
