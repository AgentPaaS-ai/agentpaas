#!/usr/bin/env bash
# block-checkpoint-template.sh
# Push local main to remote + batch-create issues at block completion.
# Run AFTER the block-end Verifier passes and the post-build audit is clean.
#
# This is the ONLY time during the block that code goes to the remote.
# All subtasks were merged locally; this push sends them all at once.
#
# CUSTOMIZE: Replace issue-creation logic with your tracker (GitHub
# Issues, Jira, Linear, etc.). This template uses GitHub Issues via gh CLI.

set -euo pipefail

BLOCK_NUM="${1:?Usage: block-checkpoint-template.sh <block-number>}"
REPO_DIR="${2:-.}"
OWA_RECORDS_DIR="$REPO_DIR/docs/owa-records"
REPO="<YOUR_REPO>"  # e.g. owner/repo

cd "$REPO_DIR"

echo "=== Block $BLOCK_NUM Checkpoint ==="

# --- 1. Verify clean tree ---
if [ -n "$(git status --short)" ]; then
  echo "ERROR: Working tree is not clean. Commit or stash before checkpoint."
  git status --short
  exit 1
fi

# --- 2. Push local main to remote ---
echo "Pushing local main to remote..."
git push origin main

# --- 3. Batch-create issues from OWA records (optional, if using tracker) ---
# Skip if issues already exist (check for duplicates by title).
echo "Checking for existing block-$BLOCK_NUM issues..."
EXISTING=$(gh issue list --repo "$REPO" --label "block-$BLOCK_NUM" --state all --json title --jq '.[].title' 2>/dev/null || echo "")

if [ -d "$OWA_RECORDS_DIR" ]; then
  for record in "$OWA_RECORDS_DIR"/b"$BLOCK_NUM"-t*.md; do
    [ -f "$record" ] || continue
    TITLE=$(head -1 "$record" | sed 's/^# //')

    # Skip if an issue with this title already exists
    if echo "$EXISTING" | grep -qF "$TITLE"; then
      echo "  Skipping (exists): $TITLE"
      continue
    fi

    echo "  Creating issue: $TITLE"
    gh issue create --repo "$REPO" \
      --title "$TITLE" \
      --label "block-$BLOCK_NUM" \
      --body-file "$record" 2>/dev/null || echo "  (failed, continue)"
  done
fi

# --- 4. Create block summary issue ---
SUMMARY_TITLE="Block $BLOCK_NUM Summary"
if echo "$EXISTING" | grep -qF "$SUMMARY_TITLE"; then
  echo "  Skipping summary (exists)"
else
  echo "  Creating block summary issue..."
  SUBTASK_COUNT=$(ls "$OWA_RECORDS_DIR"/b"$BLOCK_NUM"-t*.md 2>/dev/null | wc -l | tr -d ' ')
  gh issue create --repo "$REPO" \
    --title "$SUMMARY_TITLE" \
    --label "block-$BLOCK_NUM" \
    --body "Block $BLOCK_NUM completed. $SUBTASK_COUNT subtasks. See docs/owa-records/b${BLOCK_NUM}-*.md for details." 2>/dev/null || true
fi

# --- 5. Wait for CI (if using self-hosted runner as safety net) ---
echo ""
echo "Waiting for CI on remote main..."
sleep 10  # let CI trigger
# Check CI status (adapt to your CI system)
# gh run watch <run-id> --exit-status  # blocks until CI completes

# --- 6. Post-checkpoint cleanup ---
echo ""
echo "=== Post-Checkpoint Cleanup ==="

# Close stale PRs from prior GitHub-mode sessions (if any)
echo "Closing stale PRs..."
gh pr list --repo "$REPO" --state open --json number,title --jq '.[] | "\(.number) \(.title)"' 2>/dev/null | while read -r num title; do
  echo "  Closing PR #$num: $title"
  gh pr close "$num" --repo "$REPO" --comment "Closed: local-first mode, work merged locally and pushed at checkpoint." 2>/dev/null || true
done

# Delete stale local branches (AFTER confirming all work is on origin/main)
echo "Deleting stale local branches..."
git branch | grep -v 'main' | xargs git branch -D 2>/dev/null || true

echo ""
echo "=== Checkpoint Complete ==="
echo "Next: close all block-$BLOCK_NUM issues with closure comments."
echo "Then: run scripts/post-build-restore-template.sh to restore pruned skills/toolsets."
