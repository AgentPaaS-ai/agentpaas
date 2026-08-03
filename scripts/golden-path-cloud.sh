#!/usr/bin/env bash
# Terminal half of Cloud golden path (orch/self-test). Not the Hermes NL loop.
# Usage:
#   export AGENTPAAS_CLOUD_API_URL=https://agentpaas-cloud-api….workers.dev
#   export CLOUDFLARE_API_TOKEN=…   # for push
#   ./scripts/golden-path-cloud.sh [path-to-weather-project]
set -euo pipefail

export PATH="/opt/homebrew/bin:${PATH}"
API="${AGENTPAAS_CLOUD_API_URL:?set AGENTPAAS_CLOUD_API_URL}"
PROJECT="${1:-$HOME/projects/agentpaas/demo/weather-agent}"
BODY='{"query":"What is the weather in Folsom?"}'

echo "== version =="
agentpaas version
echo "== doctor =="
agentpaas doctor
echo "== whoami =="
agentpaas cloud whoami

echo "== pack =="
cd "$PROJECT"
PACK_OUT=$(agentpaas pack . --target linux/amd64)
echo "$PACK_OUT"
LOCK=$(echo "$PACK_OUT" | awk -F': ' '/^Lock:/{print $2}')
if [[ -z "${LOCK}" || ! -f "${LOCK}" ]]; then
  # fallback conventional path
  NAME=$(python3 -c "import yaml,sys; print(yaml.safe_load(open('agent.yaml'))['name'])" 2>/dev/null || echo weather-agent)
  LOCK="$HOME/.agentpaas/state/agents/${NAME}/agent.lock"
fi
echo "LOCK=$LOCK"
test -f "$LOCK"

echo "== push =="
: "${CLOUDFLARE_API_TOKEN:?set CLOUDFLARE_API_TOKEN for registry push}"
agentpaas cloud push --lock "$LOCK"

echo "== deploy latest =="
set +e
DEP_OUT=$(agentpaas cloud deploy latest 2>&1)
DEP_RC=$?
set -e
echo "$DEP_OUT"
DEP=$(echo "$DEP_OUT" | awk '/Deployment created:/{print $3}')
if [ -z "$DEP" ] || [ "$DEP_RC" -ne 0 ]; then
  if echo "$DEP_OUT" | grep -q no_slot_capacity; then
    echo "no_slot_capacity — using existing ready deployment"
    DEP=$(agentpaas cloud deployments 2>/dev/null | awk 'NR>1 && $3=="ready"{print $1; exit}')
  fi
fi
test -n "$DEP"
echo "DEP=$DEP"

echo "== secrets push + bind =="
agentpaas cloud secrets push openrouter-key
agentpaas cloud secrets bind "$DEP" openrouter-key --as bearer --host openrouter.ai
agentpaas cloud secrets bindings "$DEP"

echo "== invoke =="
agentpaas cloud invoke-token "$DEP" >/dev/null || true
INV=$(agentpaas cloud invoke "$DEP" --body "$BODY")
echo "$INV"
RUN=$(echo "$INV" | awk '/^Run ID:/{print $3}')
# Some CLI builds omit Run ID line on success; fall back to status list
if [[ -z "${RUN}" ]]; then
  RUN=$(agentpaas cloud status 2>/dev/null | awk 'NR==2{print $1}')
fi
echo "RUN=$RUN"
test -n "$RUN"

echo "== result =="
agentpaas cloud result "$RUN"
echo "OK golden-path-cloud DEP=$DEP RUN=$RUN API=$API"
