#!/usr/bin/env bash
#
# codex-worker-dispatch.sh — Orchestrator's dispatch script for Codex OWA workers
#
# Usage: codex-worker-dispatch.sh <branch> <issue_number> <task_prompt_file> [worktree_dir]
#
# This script:
#   1. Creates a git worktree on a fresh branch from main
#   2. Launches Codex CLI (GPT-5.5) with the OWA worker prompt + task specifics
#   3. Waits for Codex to finish
#   4. Captures the structured output (JSON) from --output-last-message
#   5. Prints the result for the orchestrator to parse
#
# The orchestrator then:
#   - Reviews the PR diff
#   - Calls kanban_complete with the worker's metadata
#   - Dispatches adversary/verifier as usual

set -euo pipefail

BRANCH="${1:?Usage: codex-worker-dispatch.sh <branch> <issue_number> <task_prompt_file> [worktree_dir]}"
ISSUE="${2:?Missing issue number}"
TASK_PROMPT_FILE="${3:?Missing task prompt file}"
WORKTREE_DIR="${4:-/tmp/agentpaas-codex-$(date +%s)}"

REPO_DIR="$HOME/projects/agentpaas"
WORKER_PROMPT="$REPO_DIR/docs/codex-owa-worker.md"
SCHEMA_FILE="$REPO_DIR/scripts/codex-worker-schema.json"
OUTPUT_FILE="$WORKTREE_DIR/codex-output.json"

echo "=== Codex OWA Worker Dispatch ==="
echo "Branch:     $BRANCH"
echo "Issue:      #$ISSUE"
echo "Worktree:   $WORKTREE_DIR"
echo "Model:      gpt-5.5"
echo ""

# 1. Create worktree from main
echo "[1/4] Creating git worktree..."
cd "$REPO_DIR"
git fetch origin main
git worktree add -b "$BRANCH" "$WORKTREE_DIR" origin/main
echo "Worktree created at $WORKTREE_DIR"
echo ""

# 2. Build the full prompt
echo "[2/4] Launching Codex CLI..."
FULL_PROMPT="$(cat "$WORKER_PROMPT")

---

## TASK SPECIFICS

GitHub Issue: #$ISSUE

$(cat "$TASK_PROMPT_FILE")

---

Remember: commit early and often, push your branch, create a PR with 'gh pr create',
and output the structured JSON summary as your final message."

# 3. Run Codex CLI
# --sandbox danger-full-access: worktree .git metadata lives in the parent
#   repo's .git/worktrees/ dir, outside the workspace-write sandbox. Without
#   full access, git add/commit/push fail. Safety comes from: explicit workdir,
#   clean git status, narrow task prompts, orchestrator review before merge.
# --output-last-message: capture structured summary
# --output-schema: enforce JSON schema
# -m gpt-5.5: use GPT-5.5
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
  echo '{"status":"blocked","blocker":"Codex did not produce output file","summary":"No output captured","branch":"'$BRANCH'","files_changed":[],"tests_added":0,"commands_run":[],"acceptance_criteria":{},"known_risks":["no output file generated"]}'
fi
echo ""
echo "---"
echo ""
echo "Worktree preserved at: $WORKTREE_DIR"
echo "To clean up: git worktree remove $WORKTREE_DIR"
