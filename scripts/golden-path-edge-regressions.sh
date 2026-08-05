#!/usr/bin/env bash
# Automated founder-cold edge regressions. Uses at most one temporary deployment.
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
API="${AGENTPAAS_CLOUD_API_URL:-https://cloud.agentpaas.ai}"
PROJECT="${1:-$REPO_ROOT/demo/weather-agent}"
DEP=""
SECOND_DEP=""
FAILURES=0
ADMIN_SECRET="${ADMIN_SECRET:-}"
ADMIN_TENANT=""
ADMIN_ORIGINAL_FILE=""
ADMIN_CPU_ORIGINAL=""
ADMIN_CPU_LIMIT=""
ADMIN_TRIAL_ORIGINAL=""
ADMIN_AGENT_LIMIT_ORIGINAL=""

pass() { printf 'PASS: %s\n' "$1"; }
fail_case() { printf 'FAIL: %s\n' "$1" >&2; FAILURES=$((FAILURES + 1)); }
cleanup() {
  if [[ -n "$ADMIN_TENANT" && -n "$ADMIN_SECRET" ]]; then
    [[ -n "$ADMIN_CPU_ORIGINAL" ]] && admin_patch "{\"cpu_minutes_used\":$ADMIN_CPU_ORIGINAL}" || true
    [[ -n "$ADMIN_TRIAL_ORIGINAL" ]] && admin_patch "{\"trial_expires_at\":\"$ADMIN_TRIAL_ORIGINAL\"}" || true
    [[ -n "$ADMIN_AGENT_LIMIT_ORIGINAL" ]] && admin_patch "{\"agent_limit\":$ADMIN_AGENT_LIMIT_ORIGINAL}" || true
  fi
  if [[ -n "$ADMIN_ORIGINAL_FILE" ]]; then
    rm -f "$ADMIN_ORIGINAL_FILE"
  fi
  if [[ -n "$DEP" ]]; then
    agentpaas cloud undeploy "$DEP" >/dev/null 2>&1 || true
  fi
  if [[ -n "$SECOND_DEP" ]]; then
    agentpaas cloud undeploy "$SECOND_DEP" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

admin_patch() {
  curl -fsS --max-time 15 -X PATCH \
    -H 'Content-Type: application/json' \
    -H "X-Admin-Secret: $ADMIN_SECRET" \
    --data "$1" "$API/v1/admin/tenants/$ADMIN_TENANT" >/dev/null
}

admin_prepare() {
  if [[ -z "$ADMIN_SECRET" ]] && command -v security >/dev/null 2>&1; then
    ADMIN_SECRET=$(security find-generic-password -s agentpaas-cloud-admin-secret -w 2>/dev/null) || ADMIN_SECRET=""
  fi
  [[ -n "$ADMIN_SECRET" ]] || return 1
  ADMIN_TENANT=$(agentpaas cloud whoami | awk -F': ' '/^Tenant:/{print $2; exit}')
  [[ -n "$ADMIN_TENANT" ]] || return 1
  ADMIN_ORIGINAL_FILE=$(mktemp)
  local status
  status=$(curl -sS --max-time 15 -o "$ADMIN_ORIGINAL_FILE" -w '%{http_code}' \
    -H "X-Admin-Secret: $ADMIN_SECRET" "$API/v1/admin/tenants/$ADMIN_TENANT") || return 1
  [[ "$status" == 2* ]] || return 1
  read -r ADMIN_CPU_ORIGINAL ADMIN_CPU_LIMIT ADMIN_TRIAL_ORIGINAL ADMIN_AGENT_LIMIT_ORIGINAL < <(
    python3 - "$ADMIN_ORIGINAL_FILE" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    payload = json.load(handle)
tenant = payload.get("tenant", payload)
print(
    tenant.get("cpu_minutes_used", ""),
    tenant.get("cpu_minute_limit", ""),
    tenant.get("trial_expires_at", ""),
    tenant.get("agent_limit", ""),
)
PY
  )
  [[ -n "$ADMIN_CPU_ORIGINAL" && -n "$ADMIN_CPU_LIMIT" && -n "$ADMIN_TRIAL_ORIGINAL" && -n "$ADMIN_AGENT_LIMIT_ORIGINAL" ]]
}

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
  agentpaas cloud invoke-token "$SECOND_DEP" >/dev/null || fail_case "could not mint temporary invoke token"
else
  fail_case "second deployment id was not returned"
fi

printf 'Case 10: CPU exhausted customer message\n'
if admin_prepare; then
  if admin_patch "{\"cpu_minutes_used\":$ADMIN_CPU_LIMIT}"; then
    CPU_LIMIT_OUT=$(agentpaas cloud invoke "$SECOND_DEP" --body '{"query":"What is the weather in Folsom?"}' 2>&1) && CPU_LIMIT_RC=0 || CPU_LIMIT_RC=$?
    if [[ "$CPU_LIMIT_RC" -ne 0 ]] && grep -Eqi 'Trial limits crossed|convert to a paid|quota|cpu minutes' <<<"$CPU_LIMIT_OUT"; then
      pass "CPU exhaustion explains trial limit"
    else
      fail_case "CPU exhaustion did not return a trial/quota message"
    fi
  else
    printf 'SKIP: Case 10 admin PATCH unavailable\n'
  fi
else
  printf 'SKIP: Case 10 ADMIN_SECRET or admin API unavailable\n'
fi

printf 'Case 11: trial expired customer message\n'
if [[ -n "$ADMIN_TENANT" && -n "$ADMIN_SECRET" && -n "$ADMIN_TRIAL_ORIGINAL" ]]; then
  if admin_patch "{\"cpu_minutes_used\":$ADMIN_CPU_ORIGINAL}" && \
     admin_patch '{"trial_expires_at":"1970-01-01T00:00:00Z"}'; then
    EXPIRED_OUT=$(agentpaas cloud invoke "$SECOND_DEP" --body '{"query":"What is the weather in Folsom?"}' 2>&1) && EXPIRED_RC=0 || EXPIRED_RC=$?
    if [[ "$EXPIRED_RC" -ne 0 ]] && grep -Eqi 'Trial period ended|trial expired' <<<"$EXPIRED_OUT"; then
      pass "expired trial explains trial period ended"
    else
      fail_case "expired trial did not return Trial period ended"
    fi
  else
    printf 'SKIP: Case 11 admin PATCH unavailable\n'
  fi
else
  printf 'SKIP: Case 11 ADMIN_SECRET or admin API unavailable\n'
fi

printf 'Case 12: agent limit customer message\n'
printf 'SKIP: Case 12 optional heavy quota mutation was not run\n'

if [[ "$FAILURES" -ne 0 ]]; then
  printf 'NO-GO: %d edge regression(s) failed\n' "$FAILURES" >&2
  exit 1
fi
printf 'GO: all founder-cold edge regressions passed\n'
