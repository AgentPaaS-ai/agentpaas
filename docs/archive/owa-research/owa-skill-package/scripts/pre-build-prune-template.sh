#!/usr/bin/env bash
# pre-build-prune-template.sh
# Context pruning before an OWA block. Run BEFORE dispatching the first
# subtask. This reduces system prompt size by ~20KB/call, saving ~250K
# tokens per 5-subtask block.
#
# CUSTOMIZE: Replace the toolset lists, skill categories, and MCP server
# names with those relevant to your stack. This template is for a backend
# build (Go/Rust/TypeScript). For a frontend build, keep vision/browser.

set -euo pipefail

PROFILE="<YOUR_ORCHESTRATOR_PROFILE>"
ADVERSARY_PROFILE="<YOUR_ADVERSARY_PROFILE>"
VERIFIER_PROFILE="<YOUR_VERIFIER_PROFILE>"
SKILLS_DIR="<YOUR_SKILLS_DIR>"  # e.g. ~/.hermes/profiles/<profile>/skills
HOLDING_DIR="<YOUR_HOLDING_DIR>"  # e.g. ~/.hermes/profiles/<profile>/.skills-holding

echo "=== Pre-Build Pruning ==="

# --- 1. Disable irrelevant toolsets on the orchestrator profile ---
# These are toolsets the backend build NEVER uses. Every disabled toolset
# removes its schema from the system prompt on every API call.
IRRELEVANT_TOOLSETS=(
  vision
  video
  image_gen
  video_gen
  tts
  browser
  computer_use
  x_search       # or your social-media search toolset
  moa            # mixture-of-agents (not needed for OWA)
  session_search
  clarify
  cronjob
  spotify        # or your music toolset
  homeassistant  # or your home-automation toolset
  yuanbao        # or any non-English model toolset
)

echo "Disabling toolsets on $PROFILE..."
for ts in "${IRRELEVANT_TOOLSETS[@]}"; do
  <your-disable-command> "$PROFILE" "$ts" 2>/dev/null || true
done

# --- 2. Strip adversary and verifier to terminal + file ONLY ---
echo "Stripping specialist profiles to terminal + file..."
for p in "$ADVERSARY_PROFILE" "$VERIFIER_PROFILE"; do
  ALL_TOOLSETS=(web browser vision video image_gen video_gen x_search tts \
    computer_use moa session_search clarify cronjob delegation \
    code_execution todo memory context_engine skills spotify homeassistant)
  for ts in "${ALL_TOOLSETS[@]}"; do
    <your-disable-command> "$p" "$ts" 2>/dev/null || true
  done
done

# --- 3. Remove MCP servers the build doesn't call ---
# If your checkpoint script uses a CLI (e.g., gh) instead of an MCP server,
# remove the MCP server — it injects ~4KB of schema per call for nothing.
echo "Removing unnecessary MCP servers..."
<your-mcp-remove-command> "$PROFILE" github --yes 2>/dev/null || true
<your-mcp-remove-command> "$ADVERSARY_PROFILE" github --yes 2>/dev/null || true
<your-mcp-remove-command> "$VERIFIER_PROFILE" github --yes 2>/dev/null || true
# Add other MCP servers you don't need during the build here.

# --- 4. Move irrelevant skills to a holding directory (reversible) ---
# Keep only the skill categories the build needs. Everything else is
# loaded into the system prompt and paid for on every call.
IRRELEVANT_CATEGORIES=(
  apple creative gaming media messaging smart-home social-media
  finance note-taking productivity research email mlops data-science
  # Add any other categories your build doesn't use
)

mkdir -p "$HOLDING_DIR"

for p in "$PROFILE" "$ADVERSARY_PROFILE" "$VERIFIER_PROFILE"; do
  P_SKILLS="$SKILLS_DIR"
  for cat in "${IRRELEVANT_CATEGORIES[@]}"; do
    if [ -d "$P_SKILLS/$cat" ] && [ ! -d "$HOLDING_DIR/$cat" ]; then
      mv "$P_SKILLS/$cat" "$HOLDING_DIR/"
      echo "  Moved $cat to holding"
    fi
  done
  # Adversary/verifier: also move devops, autonomous-ai-agents, mcp
  if [ "$p" != "$PROFILE" ]; then
    for cat in devops autonomous-ai-agents mcp; do
      if [ -d "$P_SKILLS/$cat" ] && [ ! -d "$HOLDING_DIR/$cat" ]; then
        mv "$P_SKILLS/$cat" "$HOLDING_DIR/"
        echo "  Moved $cat to holding (specialist)"
      fi
    done
  fi
done

echo "=== Pruning Complete ==="
echo "Run scripts/post-build-restore-template.sh after the block to restore."
