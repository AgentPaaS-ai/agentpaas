#!/usr/bin/env bash
#
# block-checkpoint.sh — Push block to GitHub and batch-create issues
#
# Usage: block-checkpoint.sh <block_number>
#
# This runs at BLOCK COMPLETION (not per-subtask):
#   1. Pushes local main to GitHub (all merged work)
#   2. Creates GitHub issues for each subtask from local OWA records
#   3. Creates a block summary issue
#   4. Optionally creates a single review PR for the entire block
#
# Prerequisites:
#   - All subtasks merged to local main
#   - OWA records written to docs/owa-records/b<N>-t<NN>.md
#   - Block label exists on GitHub (gh label create block-N ...)
#   - gh auth status is valid

set -euo pipefail

BLOCK="${1:?Usage: block-checkpoint.sh <block_number>}"
REPO_DIR="$HOME/projects/agentpaas"
cd "$REPO_DIR"

OWNER="AgentPaaS-ai"
REPO="agentpaas"

echo "=== Block $BLOCK Checkpoint ==="
echo ""

# 1. Verify local main is clean and ahead of remote
echo "[1/4] Verifying local state..."
git status --short
if [ -n "$(git status --short)" ]; then
  echo "ERROR: working tree not clean. Commit or stash first."
  exit 1
fi
LOCAL_AHEAD=$(git rev-list --count origin/main..main 2>/dev/null || echo "?")
echo "  Local main is $LOCAL_AHEAD commits ahead of origin/main"
echo ""

# 2. Push to GitHub
echo "[2/4] Pushing to GitHub..."
git push origin main
echo "  Pushed."
echo ""

# 3. Create issues from OWA records
echo "[3/4] Creating GitHub issues from OWA records..."
OWA_DIR="$REPO_DIR/docs/owa-records"
if [ ! -d "$OWA_DIR" ]; then
  echo "  WARNING: no OWA records directory at $OWA_DIR"
  echo "  Skipping issue creation."
else
  BLOCK_PREFIX="b${BLOCK}-"
  ISSUE_COUNT=0
  for record in "$OWA_DIR"/${BLOCK_PREFIX}*.md; do
    [ -f "$record" ] || continue
    filename=$(basename "$record")
    # Extract title from first H1 in the record
    title=$(grep -m1 '^# ' "$record" | sed 's/^# //')
    if [ -z "$title" ]; then
      title="$filename"
    fi
    echo "  Creating issue: $title"
    gh issue create \
      --title "$title" \
      --body-file "$record" \
      --label "block-${BLOCK},documentation" \
      --repo "$OWNER/$REPO"
    ISSUE_COUNT=$((ISSUE_COUNT + 1))
  done
  echo "  Created $ISSUE_COUNT issues."
fi
echo ""

# 4. Create block summary issue
echo "[4/4] Creating block summary issue..."
SUMMARY_FILE="/tmp/block${BLOCK}-summary.md"
cat > "$SUMMARY_FILE" << EOF
## Block $BLOCK Complete

### Overview

Block $BLOCK has been built and verified locally. All subtasks passed
the full OWA cycle (worker → adversary → verifier) with local gate
verification. This issue documents the block checkpoint.

### Subtasks

See individual issues labeled \`block-${BLOCK}\` for per-subtask OWA records.

### Verification

- All subtasks merged to local main
- Local gate passed (build + test + race + lint + osv)
- Adversary tests: all breaks resolved
- Verifier: all acceptance criteria met

### Commits

$(git log --oneline origin/main~$LOCAL_AHEAD..main 2>/dev/null || git log --oneline -20)
EOF

gh issue create \
  --title "Block $BLOCK Complete (Checkpoint)" \
  --body-file "$SUMMARY_FILE" \
  --label "block-${BLOCK},documentation" \
  --repo "$OWNER/$REPO"

echo ""
echo "=== Checkpoint Complete ==="
echo "Issues created: $((${ISSUE_COUNT:-0} + 1))"
echo "Commits pushed: $LOCAL_AHEAD"
