#!/usr/bin/env bash
# Create Block 5 OWA kanban chains on agentpaas-build.
# Run from orchestrator profile after issues #56-#64 exist.
# See references/block4-execution-notes.md for the Block 4 pattern.
set -euo pipefail
BOARD=agentpaas-build
SK=(--skill kanban-worker --skill agentpaas-owa-build-orchestration)

mk() {
  local title="$1" assignee="$2" body="$3" branch="$4" parent="${5:-}"
  local idempotency="${6:-}"
  local args=(--assignee "$assignee" --body "$body" \
    --workspace worktree --branch "$branch" --max-runtime 2h \
    "${SK[@]}" --json)
  [[ -n "$parent" ]] && args+=(--parent "$parent")
  [[ -n "$idempotency" ]] && args+=(--idempotency-key "$idempotency")
  hermes kanban --board "$BOARD" create "$title" "${args[@]}" \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])"
}

# ── B4-GATE-FIX (no adversary needed — Makefile-only change) ──
# This must complete before B5-T01 can use `make block4-gate`
FIX_BODY='ISSUE: #56
GOAL: Fix block4-gate fuzz test context deadline timeout on Go 1.26.4.
SCOPE: Edit Makefile only. Change three -fuzztime=30s to -fuzztime=20s in block4-gate target.
NON-GOALS: No code changes. No block4-gate-full changes (keep at 5m).
ACCEPTANCE: make block4-gate exits 0. All three fuzz targets complete without "context deadline exceeded".
REPO: ~/projects/agentpaas
BRANCH: fix/b4-gate-fuzz-timeout
Commit early/often; PR closes #56; golangci-lint 0 issues before kanban_complete.
NOTE: This is NOT an adversary task — Makefile config change only.'

FIX_V_BODY='B4-GATE-FIX VERIFIER #56. Run: make block4-gate. Must exit 0.'

FIXW=$(mk "B4-GATE-FIX: Fuzz test timeout fix" agentpaas-worker "$FIX_BODY" fix/b4-gate-fuzz-timeout "" "b4-gate-fix-worker")
FIXV=$(mk "B4-GATE-FIX VERIFIER: Gate check" agentpaas-verifier "$FIX_V_BODY" fix/b4-gate-fuzz-timeout "$FIXW" "b4-gate-fix-verifier")

# ── B5-T01: RuntimeDriver Interface and Docker Naming ──
W1_BODY='ISSUE: #57
GOAL: Define RuntimeDriver interface (Create, Start, Stop, Remove, Status, Stats, Logs) and Docker implementation shell with deterministic AgentPaaS labels/names.
SCOPE: internal/runtime/driver.go (interface), internal/runtime/docker.go (Docker impl shell), internal/runtime/naming.go (deterministic names).
- Reconciliation discovers only owned resources via AgentPaaS labels.
- Naming: agentpaas-agent-<id>, agentpaas-gateway-<id>, agentpaas-net-internal-<id>, agentpaas-net-egress-<id>
NON-GOALS: No actual container creation (stub/shell). No network bypass suite.
ACCEPTANCE: Unit tests cover naming, labels, cleanup selection, logs/stats stubs. go test ./internal/runtime/... -race -count=1 passes.
GATE: go test ./internal/runtime/... -race -count=1
REPO: ~/projects/agentpaas
BRANCH: feat/b5-t01-runtimedriver-interface
Commit early/often; PR closes #57; golangci-lint 0 issues before kanban_complete.
Docker integration tests need AGENTPAAS_DOCKER_TESTS=1 guard (like AGENTPAAS_KEYCHAIN_TESTS pattern from B3).'

A1_BODY='B5-T01 ADVERSARY (issue #57). Attack naming/labels/ownership/reconciliation.
Focus: name collision/determinism, label injection/bypass, resource ownership confusion, reconciliation safety (non-AgentPaaS containers not touched).
Write adversary tests under internal/runtime/adversary_t01_test.go.'

V1_BODY='B5-T01 VERIFIER #57. go test ./internal/runtime/... -race -count=1; golangci-lint run ./internal/runtime/...'

# ── B5-T02: Dual-Container Gateway-Only Network Topology ──
W2_BODY='ISSUE: #58
GOAL: Create per-agent internal bridge and AgentPaaS egress network with gateway sidecar dual-homed and agent isolated.
SCOPE: Actual Docker container + network creation. Per-agent internal:true bridge, dedicated egress network, gateway dual-homed, agent never shares gateway namespace, agent never attached to egress.
NON-GOALS: No secrets/SDK behavior. No hardening flags (T03). No bypass probes (T04a-d).
ACCEPTANCE: Docker inspect assertions prove network membership. Positive path reaches harness only through gateway.
GATE: go test ./internal/runtime/... -race -count=1 (with AGENTPAAS_DOCKER_TESTS=1)
REPO: ~/projects/agentpaas
BRANCH: feat/b5-t02-gateway-only-topology
Commit early/often; PR closes #58; golangci-lint 0 issues before kanban_complete.'

A2_BODY='B5-T02 ADVERSARY #58. Attack network isolation: namespace sharing bypass, unauthorized egress, direct agent-to-host communication.'
V2_BODY='B5-T02 VERIFIER #58. Run e2e-network positive path and inspect assertions.'

# ── B5-T03: Container Hardening ──
W3_BODY='ISSUE: #59
GOAL: Enforce hardening flags on agent containers.
SCOPE: Non-root uid 64000, read-only rootfs, tmpfs /tmp, cap-drop ALL, no-new-privileges, default seccomp, pids-limit 256, mem/cpu limits from agent.yaml, IPv6 disabled.
NON-GOALS: No advanced AppArmor/seccomp custom profile.
ACCEPTANCE: Inspect/resource tests prove each flag is applied.
GATE: go test ./internal/runtime/... -race -count=1 (with AGENTPAAS_DOCKER_TESTS=1)
REPO: ~/projects/agentpaas
BRANCH: feat/b5-t03-container-hardening
Commit early/often; PR closes #59; golangci-lint 0 issues before kanban_complete.'

A3_BODY='B5-T03 ADVERSARY #59. Attack: privilege escalation, capability bypass, resource limit circumvention, seccomp escape.'
V3_BODY='B5-T03 VERIFIER #59. Run hardening assertions. Inspect each flag.'

# ── B5-T04a: Positive Path and External Canary Probes ──
W4A_BODY='ISSUE: #60
GOAL: Prove allowed gateway path works while direct external canaries fail.
SCOPE: Agent invoke through gateway ingress; agent outbound through gateway egress; curl https://1.1.1.1 fails fast (<=2s); DNS to 8.8.8.8 unreachable.
NON-GOALS: No full Block 12 red-team. No host-local/protocol bypass suite (T04b/c).
ACCEPTANCE: E2E prints allowed path PASS. 1.1.1.1 and 8.8.8.8 canaries BLOCKED without hanging.
GATE: make e2e-network (positive path subset)
REPO: ~/projects/agentpaas
BRANCH: feat/b5-t04a-canary-probes
Commit early/often; PR closes #60; golangci-lint 0 issues before kanban_complete.'

A4A_BODY='B5-T04a ADVERSARY #60. Attack: canary bypass, timeout bypass, DNS redirect, hidden egress path.'
V4A_BODY='B5-T04a VERIFIER #60. Run e2e-network positive path and canary target.'

# ── B5-T04b: Host, Loopback, and Docker Bridge Probes ──
W4B_BODY='ISSUE: #61
GOAL: Prove agent containers cannot use host or Docker bridge shortcuts.
SCOPE: host.docker.internal unreachable, Docker bridge gateway IP unreachable, gateway container IP probing blocked, daemon ports unreachable.
NON-GOALS: No IPv6/UDP/ICMP/raw socket bypass suite (T04c).
ACCEPTANCE: Each host/loopback/bridge probe BLOCKED with bounded timeout.
GATE: make e2e-network (host-probe subset)
REPO: ~/projects/agentpaas
BRANCH: feat/b5-t04b-host-bridge-probes
Commit early/often; PR closes #61; golangci-lint 0 issues before kanban_complete.'

A4B_BODY='B5-T04b ADVERSARY #61. Attack: host access bypass, bridge hopping, port scanning, host.docker.internal alternatives.'
V4B_BODY='B5-T04b VERIFIER #61. Run host-probe subset of make e2e-network.'

# ── B5-T04c: Protocol and Namespace Bypass Probes ──
W4C_BODY='ISSUE: #62
GOAL: Prove protocol-level bypass attempts and namespace sharing are blocked.
SCOPE: IPv6 disabled (AAAA + v6 literals dead), UDP non-DNS blocked, ICMP blocked, raw-socket blocked, CONNECT tunnel blocked. Docker inspect proves no host networking, no shared netns.
NON-GOALS: No cleanup/reconciliation behavior (T04d).
ACCEPTANCE: IPv6, UDP, ICMP, raw socket, CONNECT all BLOCKED. Inspect proves no shared namespace.
GATE: make e2e-network (protocol-bypass subset)
REPO: ~/projects/agentpaas
BRANCH: feat/b5-t04c-protocol-bypass-probes
Commit early/often; PR closes #62; golangci-lint 0 issues before kanban_complete.'

A4C_BODY='B5-T04c ADVERSARY #62. Attack: IPv6 re-enable, raw socket creation, CONNECT tunnel bypass, namespace escape.'
V4C_BODY='B5-T04c VERIFIER #62. Run protocol-bypass subset of make e2e-network.'

# ── B5-T04d: Topology Inspect, Restart, and Partial-Create Cleanup ──
W4D_BODY='ISSUE: #63
GOAL: Prove Docker topology assertions remain true through restart and partial failure cleanup.
SCOPE: Inspect proves agent has no default route, no egress attachment, no host networking, no shared gateway namespace. Restart preserves membership. Partial create failure leaves zero orphans.
NON-GOALS: No protocol bypass probing (T04c).
ACCEPTANCE: Inspect assertions pass before/after restart. Failure-injection leaves zero orphaned owned resources.
GATE: make e2e-network (topology + restart + cleanup subset)
REPO: ~/projects/agentpaas
BRANCH: feat/b5-t04d-topology-restart-cleanup
Commit early/often; PR closes #63; golangci-lint 0 issues before kanban_complete.'

A4D_BODY='B5-T04d ADVERSARY #63. Attack: orphan resource leak, restart bypass, partial failure exploitation, zombie container.'
V4D_BODY='B5-T04d VERIFIER #63. Run topology inspect, restart, and partial-create cleanup tests.'

# ── B5-T05: Daemon Crash Reconciliation and Secret-Free Debug Output ──
W5_BODY='ISSUE: #64
GOAL: Reconcile unsafe runtime leftovers after daemon crash. Keep debug output free of raw secrets. Implement make block5-gate.
SCOPE: Kill agent container whose gateway is absent. Reconcile owned resources without touching unrelated Docker resources. No raw secrets in inspect/logs/config dumps. Implement block5-gate Makefile target wrapping make e2e-network. e2e-network runs positive-path canary + bypass suite (12+ attack vectors all BLOCKED).
NON-GOALS: No fleet management. No partial create/start cleanup (T04d).
ACCEPTANCE: Startup reconciliation kills owned agent whose gateway is absent. Reconciliation leaves unrelated Docker resources untouched. Sentinel raw secrets absent from inspect/logs/dumps. make block5-gate passes.
GATE: make block5-gate
REPO: ~/projects/agentpaas
BRANCH: feat/b5-t05-crash-reconciliation-block5-gate
Commit early/often; PR closes #64; golangci-lint 0 issues before kanban_complete.'

A5_BODY='B5-T05 ADVERSARY #64. Attack: reconciliation bypass, unrelated resource deletion, secret leakage in debug output, orphan after crash.'
V5_BODY='B5-T05 VERIFIER #64. Run daemon-crash reconciliation and secret-free debug-output tests. make block5-gate must pass.'

# ═══ CREATE THE CHAIN ═══
# B4-GATE-FIX chain (worker → verifier, no adversary — Makefile only)
echo "Creating B4-GATE-FIX chain..."
echo "  worker: $FIXW"
echo "  verifier: $FIXV"

# B5-T01 chain (worker → adversary → verifier), parent: B4-GATE-FIX verifier
echo "Creating B5-T01 chain..."
T01W=$(mk "B5-T01: RuntimeDriver Interface and Docker Naming" agentpaas-worker "$W1_BODY" feat/b5-t01-runtimedriver-interface "$FIXV" "b5-t01-worker")
T01A=$(mk "B5-T01 ADVERSARY: Security break attempts" agentpaas-adversary "$A1_BODY" feat/b5-t01-runtimedriver-interface "$T01W" "b5-t01-adversary")
T01V=$(mk "B5-T01 VERIFIER: Gate check" agentpaas-verifier "$V1_BODY" feat/b5-t01-runtimedriver-interface "$T01A" "b5-t01-verifier")
echo "  worker: $T01W  adversary: $T01A  verifier: $T01V"

# B5-T02 chain, parent: T01 verifier
echo "Creating B5-T02 chain..."
T02W=$(mk "B5-T02: Dual-Container Gateway-Only Network Topology" agentpaas-worker "$W2_BODY" feat/b5-t02-gateway-only-topology "$T01V" "b5-t02-worker")
T02A=$(mk "B5-T02 ADVERSARY: Security break attempts" agentpaas-adversary "$A2_BODY" feat/b5-t02-gateway-only-topology "$T02W" "b5-t02-adversary")
T02V=$(mk "B5-T02 VERIFIER: Gate check" agentpaas-verifier "$V2_BODY" feat/b5-t02-gateway-only-topology "$T02A" "b5-t02-verifier")
echo "  worker: $T02W  adversary: $T02A  verifier: $T02V"

# B5-T03 chain, parent: T02 verifier
echo "Creating B5-T03 chain..."
T03W=$(mk "B5-T03: Container Hardening" agentpaas-worker "$W3_BODY" feat/b5-t03-container-hardening "$T02V" "b5-t03-worker")
T03A=$(mk "B5-T03 ADVERSARY: Security break attempts" agentpaas-adversary "$A3_BODY" feat/b5-t03-container-hardening "$T03W" "b5-t03-adversary")
T03V=$(mk "B5-T03 VERIFIER: Gate check" agentpaas-verifier "$V3_BODY" feat/b5-t03-container-hardening "$T03A" "b5-t03-verifier")
echo "  worker: $T03W  adversary: $T03A  verifier: $T03V"

# B5-T04a chain, parent: T03 verifier
echo "Creating B5-T04a chain..."
T04AW=$(mk "B5-T04a: Positive Path and External Canary Probes" agentpaas-worker "$W4A_BODY" feat/b5-t04a-canary-probes "$T03V" "b5-t04a-worker")
T04AA=$(mk "B5-T04a ADVERSARY: Security break attempts" agentpaas-adversary "$A4A_BODY" feat/b5-t04a-canary-probes "$T04AW" "b5-t04a-adversary")
T04AV=$(mk "B5-T04a VERIFIER: Gate check" agentpaas-verifier "$V4A_BODY" feat/b5-t04a-canary-probes "$T04AA" "b5-t04a-verifier")
echo "  worker: $T04AW  adversary: $T04AA  verifier: $T04AV"

# B5-T04b chain, parent: T04a verifier
echo "Creating B5-T04b chain..."
T04BW=$(mk "B5-T04b: Host, Loopback, and Docker Bridge Probes" agentpaas-worker "$W4B_BODY" feat/b5-t04b-host-bridge-probes "$T04AV" "b5-t04b-worker")
T04BA=$(mk "B5-T04b ADVERSARY: Security break attempts" agentpaas-adversary "$A4B_BODY" feat/b5-t04b-host-bridge-probes "$T04BW" "b5-t04b-adversary")
T04BV=$(mk "B5-T04b VERIFIER: Gate check" agentpaas-verifier "$V4B_BODY" feat/b5-t04b-host-bridge-probes "$T04BA" "b5-t04b-verifier")
echo "  worker: $T04BW  adversary: $T04BA  verifier: $T04BV"

# B5-T04c chain, parent: T04b verifier
echo "Creating B5-T04c chain..."
T04CW=$(mk "B5-T04c: Protocol and Namespace Bypass Probes" agentpaas-worker "$W4C_BODY" feat/b5-t04c-protocol-bypass-probes "$T04BV" "b5-t04c-worker")
T04CA=$(mk "B5-T04c ADVERSARY: Security break attempts" agentpaas-adversary "$A4C_BODY" feat/b5-t04c-protocol-bypass-probes "$T04CW" "b5-t04c-adversary")
T04CV=$(mk "B5-T04c VERIFIER: Gate check" agentpaas-verifier "$V4C_BODY" feat/b5-t04c-protocol-bypass-probes "$T04CA" "b5-t04c-verifier")
echo "  worker: $T04CW  adversary: $T04CA  verifier: $T04CV"

# B5-T04d chain, parent: T04c verifier
echo "Creating B5-T04d chain..."
T04DW=$(mk "B5-T04d: Topology Inspect, Restart, and Partial-Create Cleanup" agentpaas-worker "$W4D_BODY" feat/b5-t04d-topology-restart-cleanup "$T04CV" "b5-t04d-worker")
T04DA=$(mk "B5-T04d ADVERSARY: Security break attempts" agentpaas-adversary "$A4D_BODY" feat/b5-t04d-topology-restart-cleanup "$T04DW" "b5-t04d-adversary")
T04DV=$(mk "B5-T04d VERIFIER: Gate check" agentpaas-verifier "$V4D_BODY" feat/b5-t04d-topology-restart-cleanup "$T04DA" "b5-t04d-verifier")
echo "  worker: $T04DW  adversary: $T04DA  verifier: $T04DV"

# B5-T05 chain, parent: T04d verifier
echo "Creating B5-T05 chain..."
T05W=$(mk "B5-T05: Daemon Crash Reconciliation and Secret-Free Debug Output" agentpaas-worker "$W5_BODY" feat/b5-t05-crash-reconciliation-block5-gate "$T04DV" "b5-t05-worker")
T05A=$(mk "B5-T05 ADVERSARY: Security break attempts" agentpaas-adversary "$A5_BODY" feat/b5-t05-crash-reconciliation-block5-gate "$T05W" "b5-t05-adversary")
T05V=$(mk "B5-T05 VERIFIER: block5-gate" agentpaas-verifier "$V5_BODY" feat/b5-t05-crash-reconciliation-block5-gate "$T05A" "b5-t05-verifier")
echo "  worker: $T05W  adversary: $T05A  verifier: $T05V"

echo ""
echo "═══ BLOCK 5 KANBAN GRAPH CREATED ═══"
echo "B4-GATE-FIX: $FIXW → $FIXV"
echo "B5-T01: $T01W → $T01A → $T01V"
echo "B5-T02: $T02W → $T02A → $T02V"
echo "B5-T03: $T03W → $T03A → $T03V"
echo "B5-T04a: $T04AW → $T04AA → $T04AV"
echo "B5-T04b: $T04BW → $T04BA → $T04BV"
echo "B5-T04c: $T04CW → $T04CA → $T04CV"
echo "B5-T04d: $T04DW → $T04DA → $T04DV"
echo "B5-T05: $T05W → $T05A → $T05V"
echo ""
echo "First ready task: $FIXW (B4-GATE-FIX worker)"
echo "Run: hermes kanban --board $BOARD dispatch"
