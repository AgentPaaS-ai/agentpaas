# Adversary Review: 14A0-T02 — Orphan Container Reconciliation

You are the security adversary (grok-4.3). Review the T02 orphan reconciliation code for security, correctness, and race conditions. DO NOT write code — only review and report findings.

## Files to Review

1. `internal/daemon/control_handlers.go` — method `reconcileOrphanedContainers` (line ~1024)
2. `internal/runtime/docker.go` — `ListContainers` (line ~477) and `ListNetworks` (line ~538) driver delegation
3. `internal/daemon/server.go` — wiring in `Start()` (line ~304-306)
4. `internal/daemon/control_handlers_test.go` — tests at line ~1472, 1508, 1555

## What T02 Does

On daemon `Start()`, after gRPC server registration but before `d.started=true`, the daemon calls `reconcileOrphanedContainers()`. This method:
1. Lists all Docker containers with label `agentpaas/resource-type=agent`
2. For containers NOT in the in-memory `s.runs` map (orphans): stops + removes them
3. Removes orphaned networks (label `agentpaas/managed-by=agentpaas` + `agentpaas/resource-type=net-internal`)
4. Emits `container_reconciled` and `reconciliation_complete` audit events

## Review Checklist

### Security
- [ ] Does the label filter (`agentpaas/resource-type=agent`) GUARANTEE only AgentPaaS containers are touched? Could a non-agentpaas container have this label? Is the label trustworthy?
- [ ] Could a malicious actor craft a container with agentpaas labels to trigger deletion of another container?
- [ ] Is there any path where a container is removed without being stopped first?
- [ ] Are there any containers that should be RE-TRACKED instead of destroyed (e.g., long-running agents from a prior daemon instance)?

### Race Conditions
- [ ] `reconcileOrphanedContainers` runs in `Start()` BEFORE `d.started=true`. Could a Run RPC arrive during reconciliation (gRPC server is already listening)? The Run handler would add to `s.runs` while reconciliation iterates `knownRuns` (a snapshot copy). Is there a TOCTOU where a just-tracked run's container gets removed?
- [ ] The `knownRuns` snapshot is taken under `s.runMu`, but `reconcileOrphanedContainers` releases the lock before iterating containers. A Run handler could track a new container between the snapshot and the removal loop.
- [ ] Is the 30-second timeout on `reconcileCtx` sufficient? What if Docker is slow?

### Resource Leaks
- [ ] For each orphaned container, networks are listed INSIDE the container loop (line ~1060). This lists ALL internal networks for EVERY container — is this O(n*m)? Should the network listing be done once outside the loop?
- [ ] Are there paths where `rt.Stop` fails but `rt.Remove` is still attempted? (Yes — line 1050-1054. Is this correct?)
- [ ] If `rt.Remove` fails (line 1054), the `continue` skips the network cleanup for that container. Is the network then leaked?

### Error Handling
- [ ] If `rt.ListContainers` fails, the method falls through to network reconciliation. Is this correct? Should it abort?
- [ ] All errors are logged to stderr (`fmt.Fprintf(os.Stderr, ...)`) but not returned. The caller (`Start()`) has no way to know reconciliation failed. Is silent failure acceptable here?
- [ ] The `reconciliation_complete` audit event only fires if `removals > 0`. If reconciliation found orphans but failed to remove them, no completion event fires. Is this a gap?

### Audit Correctness
- [ ] `container_reconciled` audit payload has `action: "stopped_and_removed"` even if the container was already stopped (not running). Is this misleading?
- [ ] If a container removal fails, `container_reconciled` is NOT emitted (the `continue` at line 1056 skips it). Should a `reconciliation_failed` event be emitted instead?

### Test Coverage
- [ ] `TestReconcileOrphans_StopsOrphanedContainers` — tests the happy path. Does it test partial failures (Stop succeeds, Remove fails)?
- [ ] `TestReconcileOrphans_KeepsTrackedContainers` — tests that tracked containers are not touched. Does it test the race condition (container tracked DURING reconciliation)?
- [ ] `TestReconcileOrphans_NoDocker_SkipsGracefully` — tests Docker unavailable. Does it test ListContainers returning an error while Docker IS available?

## Output Format

For each finding, report:
```
FINDING N: [SEVERITY: CRITICAL/HIGH/MEDIUM/LOW]
Category: [Security/Race/Leak/ErrorHandling/Audit/Test]
Description: <what's wrong>
Location: <file:line>
Impact: <what could go wrong>
Fix: <suggested fix — do NOT implement, just describe>
```

If no findings, report "NO BREAKS" for each category.

Be aggressive but accurate. Do not report style issues or cosmetic concerns.
