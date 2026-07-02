# Adversary Review: Block 14B-T01 — Gateway Container in Run Handler

## What Changed

The Run handler now creates a gateway topology:
1. Egress network (non-internal) created alongside the internal network
2. Gateway container (ghcr.io/agentgateway/agentgateway:v1.3.0) created with dual-homing
3. Gateway mounts compiled gateway.yaml as /config.yaml (read-only)
4. Agent container remains on internal-only network
5. trackedRun extended with EgressNetwork + Gateway fields
6. Stop handler cleans up gateway container + both networks

## Files to Review

1. internal/daemon/control_handlers.go — Run handler (gateway creation, error cleanup),
   Stop handler (gateway cleanup)
2. internal/daemon/stub_handlers.go — trackedRun struct
3. internal/runtime/docker.go — GatewayImage constant
4. internal/daemon/control_handlers_gateway_test.go — topology test

## Review Focus

1. **Error cleanup completeness**: When any step fails after resources are created,
   are ALL resources cleaned up? Trace every error path in the Run handler:
   - Egress network creation fails → internal network cleaned up?
   - Audit dir creation fails → both networks cleaned up?
   - Gateway Create fails → both networks cleaned up?
   - Gateway Start fails → gateway removed + both networks cleaned up?
   - Agent Create fails → gateway removed + both networks cleaned up?
   - Agent Start fails → gateway + agent removed + both networks cleaned up?

2. **Gateway container security**:
   - Does the gateway run with appropriate user/permissions?
   - Is the config mounted read-only (:ro)?
   - Can the gateway escape its network namespace?
   - Does the gateway have access to the host Docker socket?

3. **Network isolation**:
   - Is the agent truly isolated from the egress network?
   - Can the agent reach the gateway's admin port (15000)?
   - Can the agent bypass the gateway and reach external hosts directly?

4. **Resource leak on Stop**:
   - If Stop is called before the invoke goroutine finishes, does the gateway
     get properly cleaned up?
   - If the daemon crashes mid-run, does reconciliation handle gateway containers?
     (The reconcile.go already has ResourceTypeGateway — verify it still works)

5. **Gateway config mounting**:
   - If no policy has been applied (no gateway.yaml), the gateway starts without
     a config file. What happens? Does it crash-loop? Default-deny?
   - Is the gateway.yaml path race-free? Could PolicyApply write it while Run reads it?

6. **Docker label spoofing**:
   - Are all containers labeled with BOTH managed-by=agentpaas AND resource-type?
   - Can a malicious container inject itself as a gateway?

7. **Concurrency**:
   - If two Run calls happen concurrently, can they interfere?
   - Is the maxConcurrentRuns check still effective with the gateway container
     (now each run creates 2 containers, not 1)?

Report findings as:
FINDING N: [CRITICAL|HIGH|MEDIUM|LOW] [title]
Description of the issue and recommended fix.
