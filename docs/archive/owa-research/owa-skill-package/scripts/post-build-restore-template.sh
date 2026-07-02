#!/usr/bin/env bash
# post-build-restore-template.sh
# Restore skills, toolsets, and MCP servers disabled by
# pre-build-prune-template.sh. Run AFTER the block checkpoint completes.
#
# CUSTOMIZE: Mirror the lists in pre-build-prune-template.sh.

set -euo pipefail

PROFILE="<YOUR_ORCHESTRATOR_PROFILE>"
ADVERSARY_PROFILE="<YOUR_ADVERSARY_PROFILE>"
VERIFIER_PROFILE="<YOUR_VERIFIER_PROFILE>"
SKILLS_DIR="<YOUR_SKILLS_DIR>"
HOLDING_DIR="<YOUR_HOLDING_DIR>"

echo "=== Post-Build Restore ==="

# --- 1. Re-enable toolsets on the orchestrator profile ---
IRRELEVANT_TOOLSETS=(
  vision video image_gen video_gen tts browser computer_use
  x_search moa session_search clarify cronjob spotify homeassistant yuanbao
)

echo "Re-enabling toolsets on $PROFILE..."
for ts in "${IRRELEVANT_TOOLSETS[@]}"; do
  <your-enable-command> "$PROFILE" "$ts" 2>/dev/null || true
done

# --- 2. Re-enable toolsets on specialist profiles ---
for p in "$ADVERSARY_PROFILE" "$VERIFIER_PROFILE"; do
  ALL_TOOLSETS=(web browser vision video image_gen video_gen x_search tts \
    computer_use moa session_search clarify cronjob delegation \
    code_execution todo memory context_engine skills spotify homeassistant)
  for ts in "${ALL_TOOLSETS[@]}"; do
    <your-enable-command> "$p" "$ts" 2>/dev/null || true
  done
done

# --- 3. Re-add MCP servers ---
echo "Re-adding MCP servers..."
<your-mcp-add-command> "$PROFILE" github 2>/dev/null || true
<your-mcp-add-command> "$ADVERSARY_PROFILE" github 2>/dev/null || true
<your-mcp-add-command> "$VERIFIER_PROFILE" github 2>/dev/null || true

# --- 4. Restore skills from holding directory ---
echo "Restoring skills from holding..."
if [ -d "$HOLDING_DIR" ]; then
  for p in "$PROFILE" "$ADVERSARY_PROFILE" "$VERIFIER_PROFILE"; do
    if ls "$HOLDING_DIR"/* > /dev/null 2>&1; then
      mv "$HOLDING_DIR"/* "$SKILLS_DIR/" 2>/dev/null || true
    fi
  done
fi

echo "=== Restore Complete ==="
