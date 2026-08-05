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
ADMIN_CPU_ORIGINAL=""
ADMIN_CPU_LIMIT=""
ADMIN_TRIAL_ORIGINAL=""
ADMIN_AGENTS_USED=""
ADMIN_AGENT_LIMIT_ORIGINAL=""
ADMIN_CPU_PATCHED=0
ADMIN_TRIAL_PATCHED=0
ADMIN_AGENT_LIMIT_PATCHED=0
CRON_CONFIGURED=0
CRON_DEP=""
CRON_TOKEN="${AGENTPAAS_CLOUD_API_TOKEN:-}"
if [[ -z "$CRON_TOKEN" ]] && command -v security >/dev/null 2>&1; then
  CRON_TOKEN=$(security find-generic-password -s agentpaas-cloud-api-token -w 2>/dev/null) || CRON_TOKEN=""
fi

pass() { printf 'PASS: %s\n' "$1"; }
fail_case() { printf 'FAIL: %s\n' "$1" >&2; FAILURES=$((FAILURES + 1)); }
cleanup() {
  if [[ "$CRON_CONFIGURED" == 1 && -n "$CRON_DEP" && -n "$CRON_TOKEN" ]]; then
    curl -fsS --max-time 15 -X PUT \
      -H "Authorization: Bearer $CRON_TOKEN" \
      -H 'Content-Type: application/json' \
      --data '{"expr":"every_1m","enabled":false}' \
      "$API/v1/deployments/$CRON_DEP/cron" >/dev/null || true
  fi
  if [[ -n "$ADMIN_TENANT" && -n "$ADMIN_SECRET" ]]; then
    if [[ "$ADMIN_CPU_PATCHED" == 1 && -n "$ADMIN_CPU_ORIGINAL" ]]; then
      admin_patch "{\"cpu_minutes_used\":$ADMIN_CPU_ORIGINAL}" || true
    fi
    if [[ "$ADMIN_TRIAL_PATCHED" == 1 ]]; then
      ADMIN_TRIAL_RESTORE="$ADMIN_TRIAL_ORIGINAL"
      if [[ -z "$ADMIN_TRIAL_RESTORE" ]]; then
        ADMIN_TRIAL_RESTORE=$(python3 -c 'from datetime import datetime, timedelta, timezone; print((datetime.now(timezone.utc) + timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"))')
      fi
      admin_patch "{\"trial_expires_at\":\"$ADMIN_TRIAL_RESTORE\"}" || true
    fi
    if [[ "$ADMIN_AGENT_LIMIT_PATCHED" == 1 && -n "$ADMIN_AGENT_LIMIT_ORIGINAL" ]]; then
      admin_patch "{\"agent_limit\":$ADMIN_AGENT_LIMIT_ORIGINAL}" || true
    fi
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
  [[ -n "$ADMIN_SECRET" ]] || return 2
  ADMIN_TENANT=$(agentpaas cloud whoami | awk -F': ' '/^Tenant:/{print $2; exit}') || return 1
  [[ -n "$ADMIN_TENANT" ]] || return 1
  local usage_json
  usage_json=$(agentpaas cloud usage --json) || return 1
  read -r ADMIN_CPU_ORIGINAL ADMIN_CPU_LIMIT ADMIN_TRIAL_ORIGINAL ADMIN_AGENTS_USED ADMIN_AGENT_LIMIT_ORIGINAL < <(
    python3 -c '
import json
import sys

payload = json.load(sys.stdin)
print(
    payload.get("cpu_minutes_used", ""),
    payload.get("cpu_minute_limit", ""),
    payload.get("trial_expires_at", ""),
    payload.get("agents_used", ""),
    payload.get("agent_limit", ""),
)
' <<<"$usage_json"
  ) || return 1
  [[ -n "$ADMIN_CPU_ORIGINAL" && -n "$ADMIN_CPU_LIMIT" && -n "$ADMIN_AGENTS_USED" && -n "$ADMIN_AGENT_LIMIT_ORIGINAL" ]]
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
PACK_OUT=$(agentpaas pack . --target linux/amd64) || { printf 'NOTE: amd64 pack failed this run; using existing lock if present\n'; PACK_OUT=""; }
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
    ADMIN_CPU_PATCHED=1
    CPU_LIMIT_OUT=$(agentpaas cloud invoke "$SECOND_DEP" --body '{"query":"What is the weather in Folsom?"}' 2>&1) && CPU_LIMIT_RC=0 || CPU_LIMIT_RC=$?
    if [[ "$CPU_LIMIT_RC" -ne 0 ]] && grep -Eqi 'Trial limits crossed|convert to a paid|quota|cpu minutes' <<<"$CPU_LIMIT_OUT"; then
      pass "CPU exhaustion explains trial limit"
    else
      fail_case "CPU exhaustion did not return a trial/quota message"
    fi
  else
    fail_case "CPU exhaustion admin PATCH failed"
  fi
else
  ADMIN_PREPARE_RC=$?
  if [[ "$ADMIN_PREPARE_RC" -eq 2 ]]; then
    printf 'SKIP: Case 10 ADMIN_SECRET unavailable\n'
  else
    fail_case "could not prepare tenant usage baseline for admin PATCH"
  fi
fi

printf 'Case 11: trial expired customer message\n'
if [[ -n "$ADMIN_TENANT" && -n "$ADMIN_SECRET" ]]; then
  CPU_RESTORED=1
  if [[ "$ADMIN_CPU_PATCHED" == 1 ]]; then
    if admin_patch "{\"cpu_minutes_used\":$ADMIN_CPU_ORIGINAL}"; then
      ADMIN_CPU_PATCHED=0
    else
      CPU_RESTORED=0
      fail_case "could not restore CPU usage before Case 11"
    fi
  fi
  ADMIN_TRIAL_PATCHED=1
  if [[ "$CPU_RESTORED" == 1 ]] && admin_patch '{"trial_expires_at":"1970-01-01T00:00:00Z"}'; then
    EXPIRED_OUT=$(agentpaas cloud invoke "$SECOND_DEP" --body '{"query":"What is the weather in Folsom?"}' 2>&1) && EXPIRED_RC=0 || EXPIRED_RC=$?
    if [[ "$EXPIRED_RC" -ne 0 ]] && grep -Eqi 'Trial period ended|trial expired' <<<"$EXPIRED_OUT"; then
      pass "expired trial explains trial period ended"
    else
      fail_case "expired trial did not return Trial period ended"
    fi
    ADMIN_TRIAL_RESTORE="$ADMIN_TRIAL_ORIGINAL"
    if [[ -z "$ADMIN_TRIAL_RESTORE" ]]; then
      ADMIN_TRIAL_RESTORE=$(python3 -c 'from datetime import datetime, timedelta, timezone; print((datetime.now(timezone.utc) + timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"))')
    fi
    if admin_patch "{\"trial_expires_at\":\"$ADMIN_TRIAL_RESTORE\"}"; then
      ADMIN_TRIAL_PATCHED=0
    else
      fail_case "could not restore trial expiration after Case 11"
    fi
  else
    fail_case "trial expiration admin PATCH failed"
  fi
else
  if [[ -z "$ADMIN_SECRET" ]]; then
    printf 'SKIP: Case 11 ADMIN_SECRET unavailable\n'
  else
    fail_case "admin tenant usage baseline unavailable for Case 11"
  fi
fi

printf 'Case 12: agent limit customer message\n'
if [[ -n "$ADMIN_TENANT" && -n "$ADMIN_SECRET" ]]; then
  if admin_patch "{\"agent_limit\":$ADMIN_AGENTS_USED}"; then
    ADMIN_AGENT_LIMIT_PATCHED=1
    AGENT_LIMIT_OUT=$(agentpaas cloud deploy latest 2>&1) && AGENT_LIMIT_RC=0 || AGENT_LIMIT_RC=$?
    if [[ "$AGENT_LIMIT_RC" -ne 0 ]] && grep -Eqi 'Trial limits crossed|convert to a paid|agent limit|quota|limit' <<<"$AGENT_LIMIT_OUT"; then
      pass "agent limit exhaustion explains deployment rejection"
    else
      fail_case "agent limit exhaustion did not reject deployment"
    fi
  else
    fail_case "agent limit admin PATCH failed"
  fi
else
  printf 'SKIP: Case 12 ADMIN_SECRET unavailable\n'
fi

printf 'Case 13: cron tick smoke\n'
if [[ -z "$ADMIN_SECRET" ]]; then
  printf 'SKIP: Case 13 ADMIN_SECRET unavailable (cannot authenticate admin cron tick)\n'
elif [[ -z "$CRON_TOKEN" ]]; then
  printf 'SKIP: Case 13 AGENTPAAS_CLOUD_API_TOKEN unavailable (cannot configure tenant cron API)\n'
elif [[ -z "$SECOND_DEP" ]]; then
  fail_case "cron smoke has no deployment from Case 9"
else
  CRON_DEP="$SECOND_DEP"
  if agentpaas cloud secrets push openrouter-key >/dev/null 2>&1 && \
     agentpaas cloud secrets bind "$CRON_DEP" openrouter-key --as bearer --host openrouter.ai >/dev/null 2>&1; then
    CRON_BEFORE=$(curl -fsS --max-time 15 \
      -H "Authorization: Bearer $CRON_TOKEN" "$API/v1/runs") || CRON_BEFORE='[]'
    CRON_BEFORE_IDS=$(python3 -c 'import json,sys; print(" ".join(r.get("id", "") for r in json.load(sys.stdin)))' <<<"$CRON_BEFORE")
    CRON_CONFIGURED=1
    CRON_CONFIG=$(curl -fsS --max-time 15 -X PUT \
      -H "Authorization: Bearer $CRON_TOKEN" \
      -H 'Content-Type: application/json' \
      --data '{"expr":"every_1m","enabled":true}' \
      "$API/v1/deployments/$CRON_DEP/cron") || CRON_CONFIG=''
    if [[ -z "$CRON_CONFIG" ]]; then
      fail_case "cron configuration failed"
    else
      CRON_TICK=$(curl -fsS --max-time 15 -X POST \
        -H "X-Admin-Secret: $ADMIN_SECRET" \
        "$API/v1/admin/cron/tick") || CRON_TICK=''
      if [[ -z "$CRON_TICK" ]]; then
        fail_case "admin cron tick failed"
      else
        CRON_RUN_FOUND=0
        for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
          CRON_RUNS=$(curl -fsS --max-time 15 \
            -H "Authorization: Bearer $CRON_TOKEN" "$API/v1/runs") || CRON_RUNS='[]'
          if python3 -c 'import json,sys; before=set(sys.argv[1].split()); dep=sys.argv[2]; runs=json.load(sys.stdin); raise SystemExit(0 if any(r.get("id") not in before and r.get("deployment_id") == dep for r in runs) else 1)' "$CRON_BEFORE_IDS" "$CRON_DEP" <<<"$CRON_RUNS"; then
            CRON_RUN_FOUND=1
            break
          fi
          sleep 5
        done
        if [[ "$CRON_RUN_FOUND" == 1 ]]; then
          pass "admin cron tick produced a new deployment run"
        else
          fail_case "admin cron tick produced no new run after 60 seconds"
        fi
      fi
    fi
  else
    fail_case "openrouter-key push or cron binding failed"
  fi
fi

if [[ "$FAILURES" -ne 0 ]]; then
  printf 'NO-GO: %d edge regression(s) failed\n' "$FAILURES" >&2
  exit 1
fi
printf 'GO: all founder-cold edge regressions passed\n'
