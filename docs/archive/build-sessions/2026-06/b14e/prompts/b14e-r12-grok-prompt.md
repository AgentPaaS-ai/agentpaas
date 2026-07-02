# B14E Task: R12 — Extend orphan reconciliation to remove gateways + networks

## Repository
`~/projects/agentpaas` — on `main` branch. Commit directly to main.

## Context (read this first)
`internal/daemon/control_handlers.go` has `reconcileOrphanedContainers(ctx)` at line ~1197.
Read the FULL function before editing (it's ~80 lines). Current behavior:
- Lists containers with labels `agentpaas.managed-by=agentpaas` AND `agentpaas.resource-type=agent`
- Removes orphaned agent containers (those whose runID is not in the active `s.runs` map)
- Removes orphaned INTERNAL networks (resource-type=net-internal) matching the runID

**THE GAP (R12):** It does NOT remove:
1. Gateway containers (resource-type=gateway)
2. Egress networks (resource-type=net-egress)

So a crashed daemon leaves gateway containers + egress networks behind as orphans.

## What to fix

### 1. Extend reconcileOrphanedContainers to also clean gateways + egress networks

In `internal/daemon/control_handlers.go`, extend the function so that after the agent-container
cleanup loop, it ALSO:

**A. Removes orphaned gateway containers:**
- List containers with labels `agentpaas.managed-by=agentpaas` AND `agentpaas.resource-type=gateway`
  (use the existing `runtime.LabelResourceType` and `runtime.ResourceTypeGateway` constants —
  verify these constants exist by grepping `internal/runtime/` for `ResourceTypeGateway`; if the
  constant doesn't exist, use the string literal `"gateway"` and add the constant to the runtime
  package naming.go alongside the existing ResourceType constants).
- For each gateway container whose runID is not in `knownRuns`: stop (if running, 10s timeout) then
  remove. Record an audit event `container_reconciled` with action "stopped_and_removed" or "removed".

**B. Removes orphaned egress networks:**
- List networks with `agentpaas.managed-by=agentpaas` AND `agentpaas.resource-type=net-egress`
  (verify `runtime.ResourceTypeNetEgress` exists; add the constant if not).
- For each whose runID is not in `knownRuns`: RemoveNetwork. Audit event.

**CRITICAL — label filter discipline (security):**
ALWAYS filter on BOTH `managed-by=agentpaas` AND `resource-type=<type>`. NEVER list with only
`resource-type` — that allows label spoofing (any user can create a container with
`agentpaas.resource-type=gateway`). The existing agent-cleanup code already does this dual filter;
mirror it exactly for gateways and networks.

**Re-use the existing internal-network cleanup pattern** that's already in the function (it lists
internal networks and removes those matching the orphaned runID). Add the egress network list+remove
alongside it.

### 2. Add a test in `internal/daemon/control_handlers_test.go`
**TestReconcile_RemovesGatewayAndEgressNetwork** (guard with `AGENTPAAS_DOCKER_TESTS=1` skip):
- Use the in-process controlServer pattern (see existing reconcile tests).
- Pre-populate `s.runs` is EMPTY (so everything is an orphan).
- Create a fake/stub scenario: use the mock runtime driver (NewDockerRuntimeWithDriver) to inject
  fake ListContainers / ListNetworks responses that return gateway containers + egress networks.
- Call reconcileOrphanedContainers.
- Assert that Stop + Remove were called on the gateway container, and RemoveNetwork on the egress net.

If a full mock-driver test is too complex, at minimum add a unit test that verifies the gateway
label constants exist and the function compiles/links correctly. A mock-driver test is preferred.

## Constraints
- Read the FULL reconcileOrphanedContainers function and the runtime label constants FIRST.
- `go build ./...` must compile.
- `go test ./internal/daemon/... -count=1 -timeout 120s` must pass.
- `go vet ./internal/daemon/...` clean.
- Do NOT change the existing agent-cleanup logic — only ADD gateway + egress cleanup.
- Commit: `git add -A && git commit -m "fix(daemon): R12 reconcile orphaned gateways + egress networks (B14E)"`

## Report
Commit hash, files changed, test pass/fail, and whether you added new ResourceType constants.
