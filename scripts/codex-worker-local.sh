#!/usr/bin/env bash
#
# codex-worker-local.sh — Local-mode Codex OWA worker dispatch
#
# Usage: codex-worker-local.sh <branch> <issue_number> <task_prompt_file> [worktree_dir]
#
# Local-first build: no GitHub PRs, no remote push, no CI round-trip.
# Worker commits to local branch only. Orchestrator merges locally.
# GitHub checkpoint happens at block completion via block-checkpoint.sh.
#
# This script:
#   1. Creates a git worktree on a fresh branch from local main
#   2. Launches Codex CLI (GPT-5.5) with the LOCAL worker prompt
#   3. Waits for Codex to finish
#   4. Captures structured JSON output
#   5. Prints the worktree path (for adversary/verifier to use)

set -euo pipefail

BRANCH="${1:?Usage: codex-worker-local.sh <branch> <issue_number> <task_prompt_file> [worktree_dir]}"
ISSUE="${2:?Missing issue number}"
TASK_PROMPT_FILE="${3:?Missing task prompt file}"
WORKTREE_DIR="${4:-/tmp/agentpaas-codex-$(date +%s)}"

REPO_DIR="$HOME/projects/agentpaas"
WORKER_PROMPT="$REPO_DIR/docs/codex-owa-worker-local.md"
SCHEMA_FILE="$REPO_DIR/scripts/codex-worker-schema.json"
OUTPUT_FILE="$WORKTREE_DIR/codex-output.json"

echo "=== Codex OWA Worker Dispatch (LOCAL MODE) ==="
echo "Branch:     $BRANCH"
echo "Issue:      #$ISSUE"
echo "Worktree:   $WORKTREE_DIR"
echo "Model:      gpt-5.5"
echo "Mode:       LOCAL (no PR, no push, no CI)"
echo ""

# 1. Create worktree from local main (no fetch — we're local-first)
echo "[1/4] Creating git worktree..."
cd "$REPO_DIR"
git worktree add -b "$BRANCH" "$WORKTREE_DIR" main
echo "Worktree created at $WORKTREE_DIR"
echo ""

# 2. Build the full prompt
echo "[2/4] Launching Codex CLI..."
FULL_PROMPT="$(cat "$WORKER_PROMPT")

---

## TASK SPECIFICS

Issue: #$ISSUE

$(cat "$TASK_PROMPT_FILE")

---

Remember: commit early and often, run local gate (go test + golangci-lint),
and output the structured JSON summary as your final message.
DO NOT create a PR. DO NOT push to remote. Local commits only."

# 3. Run Codex CLI
cd "$WORKTREE_DIR"
codex exec \
  --sandbox danger-full-access \
  -m gpt-5.5 \
  -C "$WORKTREE_DIR" \
  --output-last-message "$OUTPUT_FILE" \
  --output-schema "$SCHEMA_FILE" \
  "$FULL_PROMPT"

EXIT_CODE=$?
echo ""
echo "[3/4] Codex exited with code $EXIT_CODE"
echo ""

# 4. Print results
echo "[4/4] Worker output:"
echo "---"
if [ -f "$OUTPUT_FILE" ]; then
  cat "$OUTPUT_FILE"
else
  echo '{"status":"blocked","blocker":"Codex did not produce output file","summary":"No output captured","branch":"'"$BRANCH"'","pr":0,"files_changed":[],"tests_added":0,"commands_run":[],"acceptance_criteria":[],"known_risks":["no output file generated"]}'
fi
echo ""
echo "---"
echo ""
echo "WORKTREE=$WORKTREE_DIR"
echo "OUTPUT=$OUTPUT_FILE"
echo "To clean up: git worktree remove --force $WORKTREE_DIR"
