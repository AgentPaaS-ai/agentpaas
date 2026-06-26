# Block 14B-T01: Gateway Container in Run Handler (Micro-chunk 1 of 3)

## Context

AgentPaaS is a Go project at /Users/pms88/projects/agentpaas. The daemon's Run
handler creates an internal-only Docker network and puts the agent on it. There
is NO gateway container, so when the agent calls agent.http(), it fails with a
DNS error (network isolation), NOT a policy decision. The audit event says
egress_denied but it's really egress_unreachable.

The existing topology is proven in tests (internal/runtime/topology_test.go):
- Internal network (internal: true)
- Egress network (non-internal)
- Gateway container: dual-homed (connected to BOTH networks)
- Agent container: internal-only

## Gateway Image

The gateway uses the official agentgateway Docker image:
`ghcr.io/agentgateway/agentgateway:v1.3.0`

The config is consumed via `-f /config.yaml`. The daemon already compiles a
gateway config in PolicyApply (writes to `$HOME/.config/agentpaas/gateway.yaml`).
For T01, we mount this config (if it exists) into the gateway container.

## What to Implement (THIS micro-chunk only)

### 1. Add gateway image constant

In `internal/runtime/docker.go` (or a new constants file), add:

```go
// GatewayImage is the official agentgateway Docker image for the egress gateway.
// Pinned to v1.3.0 (matches third_party/agentgateway/VERSION).
const GatewayImage = "ghcr.io/agentgateway/agentgateway:v1.3.0"
```

### 2. Extend trackedRun to track gateway container

In `internal/daemon/stub_handlers.go`, add fields to `trackedRun`:

```go
type trackedRun struct {
    Container    runtime.ContainerID
    Network      string         // internal network ID
    EgressNetwork string        // egress network ID (NEW)
    Gateway      runtime.ContainerID // gateway container ID (NEW, empty if no gateway)
    AuditDir     string
    Status       string
    CancelInvoke context.CancelFunc
    InvokeDone   chan struct{}
    InvokeErr    error
}
```

### 3. Modify Run handler to create gateway + egress network

In `internal/daemon/control_handlers.go`, in the `Run()` method:

After creating the internal network (line ~185), add:

```go
// Create egress network (non-internal — has internet access).
egressNetID, err := rt.CreateNetwork(ctx, runtime.NetworkSpec{
    Name:     runtime.NetworkName("egress", runID),
    Internal: false,
    Labels:   runtime.Labels(runtime.ResourceTypeNetEgress, runID),
})
if err != nil {
    _ = rt.RemoveNetwork(ctx, netID)
    return nil, status.Errorf(codes.Internal, "create egress network: %v", err)
}
```

After creating the internal network, create the gateway container:

```go
// Create gateway container (dual-homed: internal + egress).
// The gateway reads the compiled policy config and enforces allow/deny rules.
gatewayConfigPath := filepath.Join(s.homePaths.Config, "gateway.yaml")

// Determine if a policy has been applied (gateway.yaml exists).
// If no policy: default-deny gateway (still creates the container, but no egress allowed).
var gatewayBinds []string
if _, err := os.Stat(gatewayConfigPath); err == nil {
    // Policy exists — mount it read-only into the gateway container.
    gatewayBinds = []string{fmt.Sprintf("%s:/config.yaml:ro", gatewayConfigPath)}
}

gatewayID, err := rt.Create(ctx, runtime.ContainerSpec{
    Image:   runtime.GatewayImage,
    Command: []string{"-f", "/config.yaml"},
    Labels:  runtime.Labels(runtime.ResourceTypeGateway, runID),
    NetworkIDs: []string{string(netID), string(egressNetID)}, // dual-homed
    Binds:   gatewayBinds,
})
if err != nil {
    _ = rt.RemoveNetwork(ctx, egressNetID)
    _ = rt.RemoveNetwork(ctx, netID)
    return nil, status.Errorf(codes.Internal, "create gateway container: %v", err)
}

if err := rt.Start(ctx, gatewayID); err != nil {
    _ = rt.Remove(ctx, gatewayID, true)
    _ = rt.RemoveNetwork(ctx, egressNetID)
    _ = rt.RemoveNetwork(ctx, netID)
    return nil, status.Errorf(codes.Internal, "start gateway container: %v", err)
}
```

### 4. Update trackedRun creation

Update the trackedRun struct creation (line ~229):

```go
tracked := &trackedRun{
    Container:     containerID,
    Network:       string(netID),
    EgressNetwork: string(egressNetID),
    Gateway:       gatewayID,
    AuditDir:      hostAuditDir,
    Status:        "running",
    InvokeDone:    make(chan struct{}),
}
```

### 5. Update Stop handler to clean up gateway

In the Stop handler, after stopping the agent container and before removing
networks, add gateway cleanup:

```go
// Stop and remove gateway container.
if tracked.Gateway != "" {
    _ = rt.Stop(ctx, tracked.Gateway, &timeout)
    _ = rt.Remove(ctx, tracked.Gateway, req.GetForce())
}
```

And update network cleanup to remove BOTH networks:

```go
if netID != "" {
    _ = rt.RemoveNetwork(ctx, runtime.NetworkID(netID))
}
if tracked.EgressNetwork != "" {
    _ = rt.RemoveNetwork(ctx, runtime.NetworkID(tracked.EgressNetwork))
}
```

Note: `tracked.EgressNetwork` is already on the trackedRun struct by the time
Stop reads it. But check — Stop currently reads `netID := tracked.Network`.
Read the current code to see how it extracts fields from the claimed run.

### 6. Update error cleanup in Run handler

Every error return after creating resources MUST clean up ALL created resources.
Audit the Run handler and ensure that if gateway creation fails, both networks
are cleaned up. If container creation fails, gateway + both networks cleaned up.

## What NOT to Change (This Micro-chunk)

- Do NOT change the harness handleHTTP code (T02 will make it route through gateway).
- Do NOT change the agent container's network attachment (agent stays internal-only).
- Do NOT change PolicyApply (it already writes gateway.yaml).
- Do NOT add a default-deny fallback config file yet (that's T02).
- Do NOT change the existing e2e test (TestE2E_PackRunInvokeStopAudit) — it should
  still pass. The gateway adds a container but the agent still fails egress because
  it's on internal-only network. The gateway is a no-op from the agent's perspective
  until T02 wires the routing.

## Important: Agent Network Attachment

The agent container currently connects to `[netID]` (internal only). This is
CORRECT for the topology — the agent should NOT have direct access to the egress
network. The gateway is the only path out. Do NOT add egressNetID to the agent's
NetworkIDs.

## Tests

Update existing tests that may break:
1. Any test that checks `trackedRun.Network` may need to also handle
   `EgressNetwork` and `Gateway`.
2. Check `internal/daemon/control_handlers_test.go` for tests that create
   mock runs — they may need the new fields.

Add a new test in `internal/daemon/control_handlers_gateway_test.go`:

`TestRun_CreatesGatewayAndEgressNetwork` — using the mock runtime driver,
call Run(), verify:
- Gateway container was created with GatewayImage
- Gateway has ResourceTypeGateway label
- Gateway is connected to both internal + egress networks (dual-homed)
- Egress network was created with Internal=false
- Agent is connected to internal network only

## Constraints

- The gateway image `ghcr.io/agentgateway/agentgateway:v1.3.0` must be pulled
  if not present. The existing `ensureImage()` in docker.go handles this.
  Check that Create() calls ensureImage() for the gateway image too.
- Run `make lint` and `go test ./internal/daemon/... ./internal/runtime/... -race -count=1`
  — both must pass.
- ALL existing tests MUST still pass.
- The agent container env must still include AGENTPAAS_AUDIT_PATH and
  AGENTPAAS_AGENT_PATH.
