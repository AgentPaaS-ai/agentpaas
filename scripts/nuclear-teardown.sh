#!/usr/bin/env bash
# AgentPaaS Nuclear Teardown — removes EVERYTHING for a genuine zero state.
# Usage: bash scripts/nuclear-teardown.sh [hermes-profile]
# Customer tenant token and customer secrets are purged.
# The founder's Cloudflare API token (agentpaas-cloudflare-api-token) is kept because it is the founder's infra credential for building agentpaas.
set -euo pipefail

PROFILE="${1:-ap-testing}"
CONFIG="$HOME/.hermes/profiles/$PROFILE/config.yaml"

echo "=== 1. Stopping processes ==="
pkill -f agentpaasd 2>/dev/null || true
sleep 1
pkill -9 -f agentpaasd 2>/dev/null || true
colima stop --force 2>/dev/null || true
sleep 1
pkill -9 -f limactl 2>/dev/null || true
pkill -9 -f colima 2>/dev/null || true

echo "=== 2. Uninstalling brew packages + untap ==="
brew uninstall --cask agentpaas 2>/dev/null || brew uninstall agentpaas 2>/dev/null || true
brew uninstall docker 2>/dev/null || true
brew uninstall colima 2>/dev/null || true
brew untap agentpaas-ai/tap 2>/dev/null || true

echo "=== 3. Removing residual binaries ==="
rm -f /opt/homebrew/bin/agentpaas* 2>/dev/null || true
rm -f ~/.local/bin/agentpaas* 2>/dev/null || true
sudo rm -f /usr/local/bin/agentpaas* 2>/dev/null || true

echo "=== 4. Removing state directories ==="
rm -rf ~/.agentpaas ~/.agentpaas-testing ~/.colima ~/agentpaas ~/weather-agent ~/golden-weather-agent 2>/dev/null || true

echo "=== 5. Purging Keychain entries ==="
# Purge customer tenant token + customer secrets + daemon. The founder's
# Cloudflare API token (agentpaas-cloudflare-api-token) is deliberately kept.
for svc in \
  "ai.agentpaas.secrets.3b7a4be2064eedc6" \
  "ai.agentpaas.secrets.741d0c70f8cb08ab" \
  "agentpaas-daemon" \
  "agentpaas-cloud-api-token"; do
  while security delete-generic-password -s "$svc" 2>/dev/null; do :; done
done

echo "=== 6. Stripping $PROFILE profile ==="
P="$HOME/.hermes/profiles/$PROFILE"
rm -rf "$P/plugins/agentpaas" 2>/dev/null || true
rm -rf "$P/skills/agentpaas" "$P/skills/agentpaas-build" "$P/skills/agentpaas-lifecycle" 2>/dev/null || true
rm -rf "$P/skills/devops/agentpaas-setup" "$P/skills/devops/agentpaas-acceptance-testing" "$P/skills/devops/agentpaas-autonomous-testing" "$P/skills/devops/agentpaas-manual-testing-setup" 2>/dev/null || true
rm -f "$P/SOUL.md" 2>/dev/null || true
rm -rf "$P/pastes" "$P/state.db" "$P/.hermes_history" "$P/cache" 2>/dev/null || true

echo "=== 7. Cleaning test artifacts ==="
rm -rf /tmp/test-* ~/test-share-* ~/test-fork-* ~/test-policy-* 2>/dev/null || true

echo "=== 8. Patching $PROFILE config.yaml ==="
if [ -f "$CONFIG" ]; then
  sed -i '' '/^    - agentpaas$/d' "$CONFIG"
  python3 - "$CONFIG" <<'PY'
import re, sys
p = sys.argv[1]
s = open(p).read()
s = re.sub(r'plugins:\n  enabled:\n(    - agentpaas\n|    \[\]\n)', 'plugins:\n  enabled: []\n', s, count=1)
s = re.sub(r'  entries:\n    agentpaas:\n      allow_tool_override: false\n', '  entries: {}\n', s, count=1)
open(p, 'w').write(s)
PY
fi

echo ""
echo "=== VERIFICATION ==="
PASS=true
which agentpaas agentpaasd docker colima 2>/dev/null && PASS=false || echo "binaries: gone"
ls -d ~/.agentpaas ~/.agentpaas-testing ~/.colima 2>/dev/null && PASS=false || echo "state dirs: gone"
ps aux | grep -E 'agentpaasd|limactl|colima' | grep -v grep && PASS=false || echo "processes: none"
brew tap 2>/dev/null | grep -qi agentpaas && { echo "taps: WARN - agentpaas tap remains"; PASS=false; } || echo "taps: clean"
grep -qi agentpaas "$CONFIG" 2>/dev/null && { echo "config: WARN - agentpaas refs remain"; PASS=false; } || echo "config: clean"

if [ "$PASS" = true ]; then
  echo ""
  echo "✓ Clean slate verified. Ready for testing."
else
  echo ""
  echo "⚠ Some items remain — check above."
fi
