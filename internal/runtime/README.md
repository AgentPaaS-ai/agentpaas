# package runtime

## Purpose

`runtime` is the execution substrate: Docker-backed container/network driver,
label ownership, post-crash reconcile, activation modes, inbox/approval
durability, and LLM envelope/stream contracts used by daemon and harness.

## Key Types

| Type | Role |
|------|------|
| `RuntimeDriver` | Container/network driver interface |
| `DockerRuntime` | Docker implementation |
| `ContainerSpec` / `ContainerID` / `ContainerStatus` | Container lifecycle |
| `NetworkSpec` / `NetworkID` | Network lifecycle |
| `RuntimeProfile` | Feature negotiation profile |
| `ModelCallEnvelope` / `StreamEvent` | LLM call and stream contracts |
| `DurableInboxStore` / `InboxMessage` | WAL-backed inbox |
| `ApprovalStore` / `ApprovalRequest` | Human approval WAL store |
| `WakeRegistry` / `WakeSignal` | Wake notification fan-out |
| `ResourceCollector` / `PerfHarness` | Metrics and perf conformance |
| Activation lifecycle types | on_demand / warm / resident |

## Key Functions

| Symbol | Role |
|--------|------|
| `NewDockerRuntime` | Construct Docker driver |
| Driver methods | Create/Start/Stop/Remove/Exec/Logs/Stats/Networks |
| `ContainerName` / `NetworkName` / `Labels` | Deterministic naming |
| `ReconcileAfterCrash` / `ReconcileMCPServers` | Orphan discovery |
| `Negotiate` / `IsCompatible` | Runtime profile negotiation |
| `Validate` on envelopes/events | Reject unsafe LLM payloads |
| `ZeroAuthorityInvariant` | Warm-idle authority check |
| Inbox/approval APIs | Append, list, resolve, recover WAL |

## Architecture

```
daemon / supervisor
        |
        v
 RuntimeDriver (DockerRuntime)
        +-- agent container (harness PID 1)
        +-- gateway container (agentgateway image)
        +-- MCP containers
        +-- internal/egress networks
        |
        +-- labels: managed-by=agentpaas
        v
 ReconcileAfterCrash on daemon start
```

Hardening flags are applied inside Create based on security policy so callers
need not restate them on every spec.

## Usage

```go
rt, err := runtime.NewDockerRuntime()
if err != nil {
    return err
}
id, err := rt.Create(ctx, runtime.ContainerSpec{
    Image: imageRef,
    Labels: runtime.Labels("agent", runID),
    NetworkIDs: []string{string(netID)},
})
```
