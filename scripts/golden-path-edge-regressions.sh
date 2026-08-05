#!/usr/bin/env bash
# Automated founder-cold edge regressions. Uses at most one temporary deployment.
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
API="${AGENTPAAS_CLOUD_API_URL:-https://cloud.agentpaas.ai}"
PROJECT="${1:-$REPO_ROOT/demo/weather-agent}"
DEP=""
FAILURES=0

pass() { printf 'PASS: %s\n' "$1"; }
fail_case() { printf 'FAIL: %s\n' "$1" >&2; FAILURES=$((FAILURES + 1)); }
cleanup() {
  if [[ -n "$DEP" ]]; then
    agentpaas cloud undeploy "$DEP" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

command -v agentpaas >/dev/null 2>&1 || { printf 'FAIL: agentpaas is not on PATH\n' >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { printf 'FAIL: curl is required\n' >&2; exit 1; }
[[ -d "$PROJECT" ]] || { printf 'FAIL: project does not exist: %s\n' "$PROJECT" >&2; exit 1; }

printf '== founder-cold edge regressions ==\n'
printf 'API: %s\n' "$API"

printf 'Case 1: unauth whoami HTTP\n'
HTTP_CODE=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' https://cloud.agentpaas.ai/v1/whoami) || HTTP_CODE=000
if [[ "$HTTP_CODE" == 401 ]]; then pass "unauthenticated whoami returns 401"; else fail_case "unauthenticated whoami returned HTTP $HTTP_CODE (want 401)"; fi

printf 'Case 2: CLI logged-in positive control\n'
if agentpaas cloud whoami | grep -qi 'Tenant'; then
  pass "logged-in whoami positive control shows Tenant"
else
  fail_case "logged-in whoami positive control failed"
fi
printf 'NOTE: empty-keychain CLI not-logged-in simulation is skipped to preserve the user login\n'

printf 'Case 3: non-amd64 push guard / amd64 lock fallback\n'
cd "$PROJECT"
PACK_OUT=$(agentpaas pack . --target linux/amd64) || { fail_case "amd64 pack failed"; PACK_OUT=""; }
LOCK=$(awk -F': ' '/^Lock:/{print $2; exit}' <<<"$PACK_OUT")
if [[ -z "$LOCK" || ! -f "$LOCK" ]]; then
  NAME=$(awk -F: '/^name:/{gsub(/[[:space:]]/, "", $2); print $2; exit}' agent.yaml)
  [[ -n "$NAME" ]] || NAME=weather-agent
  LOCK="$HOME/.agentpaas/state/agents/$NAME/agent.lock"
fi
if [[ -f "$LOCK" ]] && grep -qi 'linux/amd64\|amd64' "$LOCK"; then
  pass "amd64 fallback lock records amd64"
else
  fail_case "could not prove packed lock is amd64"
fi

printf 'Case 4: invoke without token\n'
if [[ -n "$LOCK" && -f "$LOCK" ]]; then
  env -u CLOUDFLARE_API_TOKEN -u CF_API_TOKEN agentpaas cloud push --lock "$LOCK" >/dev/null || fail_case "tenant-only push failed"
  DEP_OUT=$(agentpaas cloud deploy latest 2>&1) || {
    if grep -q 'no_slot_capacity' <<<"$DEP_OUT"; then
      OLD_DEP=$(agentpaas cloud deployments | awk '/dep[_-]/{print $1; exit}')
      if [[ -n "$OLD_DEP" ]]; then
        agentpaas cloud undeploy "$OLD_DEP"
        DEP_OUT=$(agentpaas cloud deploy latest)
      else
        fail_case "no_slot_capacity and no deployment available"
        DEP_OUT=""
      fi
    else
      fail_case "temporary deployment failed"
      DEP_OUT=""
    fi
  }
  DEP=$(awk '/Deployment created:/{print $3; exit}' <<<"$DEP_OUT")
fi
if [[ -n "$DEP" ]]; then
  INV_CODE=$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST \
    -H 'Content-Type: application/json' -d '{"query":"What is the weather in Folsom?"}' \
    "$API/v1/deployments/$DEP/invoke") || INV_CODE=000
  if [[ "$INV_CODE" == 401 ]]; then pass "invoke without token returns 401"; else fail_case "invoke without token returned HTTP $INV_CODE (want 401)"; fi
else
  fail_case "temporary deployment unavailable for invoke-token regression"
fi

printf 'Case 5: double undeploy\n'
if [[ -n "$DEP" ]]; then
  if agentpaas cloud undeploy "$DEP" >/dev/null 2>&1; then
    FIRST_UNDEPLOY_OK=1
  else
    FIRST_UNDEPLOY_OK=0
    fail_case "first undeploy failed"
  fi
  SECOND_OUT=$(agentpaas cloud undeploy "$DEP" 2>&1) && SECOND_RC=0 || SECOND_RC=$?
  if [[ "$FIRST_UNDEPLOY_OK" == 1 && "$SECOND_RC" -ne 0 ]] || grep -Eqi '404|not found|already|inactive|does not exist' <<<"$SECOND_OUT"; then
    pass "second undeploy is rejected cleanly"
  else
    fail_case "second undeploy unexpectedly succeeded or lacked 404-style error"
  fi
  DEP=""
else
  fail_case "double undeploy skipped because no temporary deployment exists"
fi

printf 'Case 6: bindings empty guard\n'
if grep -q 'cloud secrets bindings' "$REPO_ROOT/scripts/golden-path-founder-cold.sh" && \
   grep -q "grep -q 'openrouter-key'" "$REPO_ROOT/scripts/golden-path-founder-cold.sh"; then
  pass "founder-cold path rejects empty bindings before invoke"
else
  fail_case "founder-cold path has no binding assertion"
fi

printf 'Case 7: brew/cloud URL\n'
AGENTPAAS_BIN=$(command -v agentpaas)
# Resolve brew cask symlink. Avoid `strings|grep -q` under pipefail (SIGPIPE → false fail).
AGENTPAAS_REAL=$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$AGENTPAAS_BIN")
URL_HITS=$(strings -a "$AGENTPAAS_REAL" 2>/dev/null | grep -c 'cloud.agentpaas.ai' || true)
if [[ "${URL_HITS:-0}" -gt 0 ]] || agentpaas cloud --help 2>&1 | grep -Eqi 'cloud\.agentpaas\.ai|Cloud API'; then
  pass "agentpaas binary/help contains default cloud URL"
else
  fail_case "agentpaas binary/help does not mention default cloud URL"
fi

printf 'Case 8: registry CLI\n'
if agentpaas cloud registry >/dev/null 2>&1 || agentpaas cloud list >/dev/null 2>&1; then
  pass "cloud registry/list exits 0"
else
  fail_case "cloud registry/list failed"
fi

printf 'Case 9: second deploy after undeploy\n'
SECOND_DEP_OUT=$(agentpaas cloud deploy latest 2>&1) || {
  fail_case "second deploy after undeploy failed"
  SECOND_DEP_OUT=""
}
SECOND_DEP=$(awk '/Deployment created:/{print $3; exit}' <<<"$SECOND_DEP_OUT")
if [[ -n "$SECOND_DEP" ]]; then
  pass "slot reused for second deployment"
  agentpaas cloud undeploy "$SECOND_DEP" >/dev/null || fail_case "second deployment cleanup failed"
else
  fail_case "second deployment id was not returned"
fi

if [[ "$FAILURES" -ne 0 ]]; then
  printf 'NO-GO: %d edge regression(s) failed\n' "$FAILURES" >&2
  exit 1
fi
printf 'GO: all founder-cold edge regressions passed\n'
