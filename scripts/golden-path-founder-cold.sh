#!/usr/bin/env bash
# Automated founder-cold golden path: install/build/cloud/undeploy equivalent.
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
API="${AGENTPAAS_CLOUD_API_URL:-https://cloud.agentpaas.ai}"
PROJECT="${1:-$REPO_ROOT/demo/weather-agent}"
BODY='{"query":"What is the weather in Folsom?"}'
DEP=""
SECOND_DEP=""
MAIN_DEP=""
RUN=""
FINAL_OUTPUT=""
STATUS="NO-GO"
EVIDENCE_DIR="$REPO_ROOT/docs/owa-records"
TIMESTAMP=$(date +%Y%m%d-%H%M)
EVIDENCE="$EVIDENCE_DIR/golden-founder-cold-$TIMESTAMP.md"

section() { printf '\n== %s ==\n' "$1"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

cleanup() {
  if [[ -n "$SECOND_DEP" ]]; then
    agentpaas cloud undeploy "$SECOND_DEP" >/dev/null 2>&1 || true
  fi
  if [[ -n "$DEP" ]]; then
    agentpaas cloud undeploy "$DEP" >/dev/null 2>&1 || true
  fi
}
write_evidence() {
  mkdir -p "$EVIDENCE_DIR"
  EXCERPT=$(printf '%s' "$FINAL_OUTPUT" | tr '\n' ' ' | cut -c1-500)
  {
    printf '# Founder-cold automated golden path\n\n'
    printf -- '- Result: %s\n- API: %s\n- Project: %s\n- Deployment: %s\n- Run ID: %s\n- Final output excerpt: %s\n' "$STATUS" "$API" "$PROJECT" "$MAIN_DEP" "$RUN" "$EXCERPT"
  } > "$EVIDENCE"
  printf '\nEvidence: %s\n' "$EVIDENCE"
}

trap 'cleanup; write_evidence' EXIT

command -v agentpaas >/dev/null 2>&1 || fail "agentpaas is not on PATH; install the brew agentpaas binary first"
AGENTPAAS_BIN=$(command -v agentpaas)
[[ "$PATH" == *"$(dirname "$AGENTPAAS_BIN")"* ]] || fail "PATH does not include the agentpaas binary directory"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"

section "version + doctor"
agentpaas version
agentpaas doctor

section "whoami"
WHOAMI=$(agentpaas cloud whoami)
printf '%s\n' "$WHOAMI"
grep -qi 'Tenant' <<<"$WHOAMI" || fail "cloud whoami did not show Tenant"

section "prerequisites"
agentpaas secret list | grep -q 'openrouter-key' || fail "local secret openrouter-key is missing"
agentpaas identity show >/dev/null || fail "identity is not initialized"
if command -v docker >/dev/null 2>&1; then
  docker info >/dev/null 2>&1 || fail "Docker is not running (start Docker or colima)"
elif command -v colima >/dev/null 2>&1; then
  colima status | grep -q 'Running' || fail "colima is not running"
else
  fail "Docker or colima is required"
fi

section "project"
[[ -d "$PROJECT" ]] || fail "project directory does not exist: $PROJECT"
[[ -f "$PROJECT/main.py" ]] || fail "project has no main.py: $PROJECT"
grep -q 'on_invoke' "$PROJECT/main.py" || fail "main.py has no on_invoke handler: $PROJECT"
printf 'Project: %s\nAPI: %s\n' "$PROJECT" "$API"

section "pack"
cd "$PROJECT"
PACK_OUT=$(agentpaas pack . --target linux/amd64)
printf '%s\n' "$PACK_OUT"
LOCK=$(awk -F': ' '/^Lock:/{print $2; exit}' <<<"$PACK_OUT")
if [[ -z "$LOCK" || ! -f "$LOCK" ]]; then
  NAME=$(awk -F: '/^name:/{gsub(/[[:space:]]/, "", $2); print $2; exit}' agent.yaml)
  [[ -n "$NAME" ]] || NAME=weather-agent
  LOCK="$HOME/.agentpaas/state/agents/$NAME/agent.lock"
fi
[[ -f "$LOCK" ]] || fail "agent lock not found: $LOCK"
printf 'Lock: %s\n' "$LOCK"

grep -qi 'linux/amd64\|amd64' "$LOCK" || fail "lock does not record linux/amd64"

section "push (tenant-only)"
env -u CLOUDFLARE_API_TOKEN -u CF_API_TOKEN agentpaas cloud push --lock "$LOCK"

section "images"
IMAGES=$(agentpaas cloud images)
printf '%s\n' "$IMAGES"
grep -qi 'admitted' <<<"$IMAGES" || fail "cloud images did not show an admitted image"

section "deploy latest"
DEP_OUT=$(agentpaas cloud deploy latest 2>&1) || {
  if grep -q 'no_slot_capacity' <<<"$DEP_OUT"; then
    printf '%s\n' "$DEP_OUT"
    OLD_DEP=$(agentpaas cloud deployments | awk '/dep[_-]/{print $1; exit}')
    [[ -n "$OLD_DEP" ]] || fail "no_slot_capacity and no deployment available to undeploy"
    agentpaas cloud undeploy "$OLD_DEP"
    DEP_OUT=$(agentpaas cloud deploy latest)
  else
    printf '%s\n' "$DEP_OUT" >&2
    exit 1
  fi
}
printf '%s\n' "$DEP_OUT"
DEP=$(awk '/Deployment created:/{print $3; exit}' <<<"$DEP_OUT")
[[ -n "$DEP" ]] || fail "could not parse deployment id"
printf 'Deployment: %s\n' "$DEP"

section "secrets + bindings"
agentpaas cloud secrets push openrouter-key
agentpaas cloud secrets bind "$DEP" openrouter-key --as bearer --host openrouter.ai
BINDINGS=$(agentpaas cloud secrets bindings "$DEP")
printf '%s\n' "$BINDINGS"
grep -q 'openrouter-key' <<<"$BINDINGS" || fail "deployment bindings are empty or missing openrouter-key"

section "invoke"
agentpaas cloud invoke-token "$DEP" >/dev/null
INV_OUT=$(agentpaas cloud invoke "$DEP" --body "$BODY")
printf '%s\n' "$INV_OUT"
RUN=$(awk '/^Run ID:/{print $3; exit}' <<<"$INV_OUT")
[[ -n "$RUN" ]] || fail "could not parse run id"
printf 'Run: %s\n' "$RUN"

section "result"
RESULT_OUT=$(agentpaas cloud result "$RUN")
printf '%s\n' "$RESULT_OUT"
grep -q '^Status: succeeded$' <<<"$RESULT_OUT" || fail "cloud result did not succeed"
FINAL_OUTPUT=$(awk '/^Final output:/{seen=1; next} /^Artifacts:/{seen=0} seen {print}' <<<"$RESULT_OUT" | sed '/^[[:space:]]*$/d')
[[ -n "$FINAL_OUTPUT" && "$FINAL_OUTPUT" != "null" ]] || fail "cloud result final_output is empty or null"

section "logs (best-effort)"
agentpaas cloud logs "$RUN" || printf 'NOTE: cloud logs unavailable; continuing\n'

section "usage"
agentpaas cloud usage >/dev/null

section "undeploy"
MAIN_DEP="$DEP"
agentpaas cloud undeploy "$DEP"
DEPLOYMENTS=$(agentpaas cloud deployments)
if grep -q "$DEP" <<<"$DEPLOYMENTS"; then
  fail "undeployed deployment remains listed: $DEP"
fi
DEP=""

section "slot reuse (optional proof)"
SECOND_OUT=$(agentpaas cloud deploy latest 2>&1) || {
  if grep -q 'no_slot_capacity' <<<"$SECOND_OUT"; then
    printf 'NOTE: optional slot-reuse proof skipped: no_slot_capacity\n'
  else
    printf '%s\n' "$SECOND_OUT" >&2
    exit 1
  fi
}
if [[ -n "${SECOND_OUT:-}" && "$SECOND_OUT" != *no_slot_capacity* ]]; then
  printf '%s\n' "$SECOND_OUT"
  SECOND_DEP=$(awk '/Deployment created:/{print $3; exit}' <<<"$SECOND_OUT")
  [[ -n "$SECOND_DEP" ]] || fail "could not parse optional deployment id"
  agentpaas cloud undeploy "$SECOND_DEP"
  SECOND_DEP=""
fi

STATUS="GO"
printf 'GO founder-cold golden path\n'
