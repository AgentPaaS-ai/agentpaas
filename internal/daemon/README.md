# package daemon

## Purpose

`daemon` is the AgentPaaS control plane process library. It binds a Unix-domain
gRPC server (`ControlService`), enforces single-instance home locking, tracks
agent runs and Docker resources, and exposes pack/run/policy/secret/audit/
operator/routed-run APIs used by the CLI and other local clients.

## Key Types

| Type | Role |
|------|------|
| `Daemon` | Process lifecycle: New/Start/Ready/Stop, lock, socket, gRPC server |
| `Option` / `WithAllowRoot` / `WithDashboard` | Construction options |
| `VersionInfo` | CLI/daemon version and git commit metadata |
| `controlServer` | Unexported ControlService implementation |
| `trackedRun` | In-memory run tracking (container, network, status, time envelope) |
| `ConfirmationStore` / `PendingConfirmation` | Short-TTL trust-boundary change approvals |
| `dockerResourceManager` | Dashboard resource listing over DockerRuntime |

## Key Functions

| Symbol | Role |
|--------|------|
| `New` | Construct a daemon from `home.HomePaths` and version info |
| `(*Daemon).Start` | Bind socket, take lock, register services, serve |
| `(*Daemon).Ready` / `IsReady` | Flip readiness gate for interceptors |
| `(*Daemon).Stop` / `HandleSignal` | Graceful drain and cleanup |
| `CheckRoot` | Refuse root unless explicitly allowed |
| `CurrentVersion` | Build-time version snapshot |
| Control RPCs | `Pack`, `Run`, `Stop`, `Logs`, `PolicyApply`, secret/audit/cron/operator/routed handlers |

## Architecture

```
CLI / local client
        |  gRPC over Unix socket (0600)
        v
   Daemon (readiness interceptors)
        |
        +-- controlServer
        |     +-- pack/run via runtime.DockerRuntime
        |     +-- policy compile / secrets / audit
        |     +-- routed stores (deployment/run/workflow)
        |     +-- trigger cron bridge
        |     +-- operator diagnostics
        +-- flock lock file (single instance)
        +-- optional dashboard ResourceManager
```

Startup validates home paths, acquires the lock, listens on the socket, and
stays non-ready until the process owner calls `Ready()`. Shutdown rejects new
RPCs and drains in-flight work.

## Usage

```go
paths := home.NewHomePaths(homeDir)
d, err := daemon.New(paths, daemon.CurrentVersion())
if err != nil {
    return err
}
ctx := context.Background()
if err := d.Start(ctx); err != nil {
    return err
}
d.Ready()
// ... block on signals ...
return d.Stop(ctx)
```

Entry point wiring lives in `cmd/agentpaasd`. Clients dial the socket via
`internal/cli` connection helpers.
