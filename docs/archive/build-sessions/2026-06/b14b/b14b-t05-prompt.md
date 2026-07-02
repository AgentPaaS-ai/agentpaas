# Block 14B-T05: Trigger Server Startup in Local-First Mode

## Context

AgentPaaS is a Go project at /Users/pms88/projects/agentpaas. The daemon
(internal/daemon/server.go) creates an EventBus for live run timeline events
but does NOT start the trigger API server (gRPC :7718 / REST :7717). External
invocations are impossible — auto-invoke via docker exec is the ONLY invocation
path.

The trigger package (internal/trigger/) already has a complete server with:
- `trigger.New(ServerConfig{GRPCAddr, RESTAddr, EventBus, ...}) (*Server, error)`
- `Server.Start(ctx context.Context) error` — starts gRPC + REST listeners
- `Server.Stop()` — graceful shutdown (GracefulStop + REST shutdown)
- `ServerConfig.EventBus` field already exists

The `TriggerService.Invoke` (internal/trigger/server.go:402) creates a stub
run (PENDING) in a RunStore but doesn't actually invoke the daemon's Run handler.

## Architecture

```
Daemon
├── gRPC server (Unix socket) → controlServer (Run, Stop, Pack, etc.)
├── Dashboard (HTTP :8090) → timeline, resources
└── Trigger server (gRPC :7718 + REST :7717) ← NEW
    └── TriggerService.Invoke → calls controlServer.Run()
```

## What to Implement

### Part 1: Add triggerServer field to Daemon struct

In `internal/daemon/server.go`, add to the Daemon struct (around line 55,
near `eventBus`):

```go
triggerServer *trigger.Server
```

### Part 2: Start trigger server in daemon Start()

In `internal/daemon/server.go`, in the `Start()` method, AFTER the
`controlServer` is created and registered (around line 302, after
`controlv1.RegisterControlServiceServer(d.server, controlServer)`):

```go
// Start trigger server for external invocations (loopback-only for P1).
triggerGRPCAddr := os.Getenv("AGENTPAAS_TRIGGER_GRPC_ADDR")
if triggerGRPCAddr == "" {
    triggerGRPCAddr = "127.0.0.1:7718"
}
triggerRESTAddr := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR")
if triggerRESTAddr == "" {
    triggerRESTAddr = "127.0.0.1:7717"
}

triggerSrv, err := trigger.New(trigger.ServerConfig{
    GRPCAddr:  triggerGRPCAddr,
    RESTAddr:  triggerRESTAddr,
    EventBus:  d.eventBus,
    Audit:     auditWriter,
})
if err != nil {
    fmt.Fprintf(os.Stderr, "daemon: trigger server init: %v\n", err)
} else {
    // Wire the trigger service to the daemon's Run handler.
    triggerSrv.SetInvokeFunc(func(ctx context.Context, agentName string) (string, error) {
        resp, err := controlServer.Run(ctx, &controlv1.RunRequest{
            AgentName: agentName,
        })
        if err != nil {
            return "", err
        }
        return resp.GetRunId(), nil
    })
    triggerCtx, triggerCancel := context.WithCancel(context.Background())
    if err := triggerSrv.Start(triggerCtx); err != nil {
        fmt.Fprintf(os.Stderr, "daemon: trigger server start: %v\n", err)
        triggerCancel()
    } else {
        d.triggerServer = triggerSrv
        d.triggerCancel = triggerCancel
    }
}
```

Add `triggerCancel context.CancelFunc` to the Daemon struct too.

IMPORTANT: You need to add a `SetInvokeFunc` method to the trigger.Server
(or expose the TriggerService). Read server.go to understand the relationship.
The Server struct has `triggerService *TriggerService` (private). You need to
add a method like:

```go
// In internal/trigger/server.go:
func (s *Server) SetInvokeFunc(fn func(ctx context.Context, agentName string) (string, error)) {
    s.triggerService.invokeFunc = fn
}
```

And add the field to TriggerService:
```go
type TriggerService struct {
    triggerv1.UnimplementedTriggerServiceServer
    audit       audit.AuditAppender
    maxPayload  int
    idempotency *IdempotencyStore
    cancelGracePeriod time.Duration

    invokeFunc func(ctx context.Context, agentName string) (string, error) // NEW
}
```

### Part 3: Wire TriggerService.Invoke to use invokeFunc

In `internal/trigger/server.go`, modify `TriggerService.Invoke` (line 402).

Currently after the idempotency check, it creates a stub PENDING run. Change it
to: if `s.invokeFunc != nil`, call it to get the real runID, then set status
to RUNNING:

```go
// After idempotency check, before creating the run:
if s.invokeFunc != nil {
    actualRunID, err := s.invokeFunc(ctx, req.GetAgentName())
    if err != nil {
        return nil, status.Errorf(codes.Internal, "invoke agent: %v", err)
    }
    runID = actualRunID // use the daemon's real run ID
    run := &triggerv1.Run{
        RunId:     runID,
        AgentName: req.GetAgentName(),
        Status:    triggerv1.RunStatus_RUN_STATUS_RUNNING,
    }
    entry := s.runStore.Register(runID, req.GetAgentName())
    run.CreatedAt = entry.toRun().GetCreatedAt()
    s.runStore.MarkStarted(runID)
    return &triggerv1.InvokeResponse{Run: run}, nil
}
// else: existing stub behavior (for backward compat with trigger-only tests)
```

### Part 4: Graceful shutdown in Stop()

In `internal/daemon/server.go`, in `Stop()`, add BEFORE `d.cleanupFiles()`:

```go
if d.triggerCancel != nil {
    d.triggerCancel()
}
if d.triggerServer != nil {
    d.triggerServer.Stop()
}
```

## Tests

Write tests in `internal/daemon/server_trigger_test.go`:

1. `TestTriggerServer_StartsOnLoopback` — create a Daemon with a temp home dir,
   start it (allowRoot), dial 127.0.0.1:7718 with gRPC, call Invoke with a
   fake agent name. Verify it returns a run ID and RUNNING status.
   Use a mock or skip if Docker isn't available.

2. `TestTriggerServer_AddressesFromEnv` — set AGENTPAAS_TRIGGER_GRPC_ADDR to
   a custom port, start the daemon, verify the trigger server binds there.

3. `TestTriggerServer_GracefulShutdown` — start daemon, verify trigger server
   is listening (try to dial it), stop daemon, verify the connection fails.

4. `TestTriggerService_InvokeFuncWired` — unit test that TriggerService.Invoke
   calls the injected invokeFunc when set, and returns the real runID.

5. `TestTriggerService_InvokeFuncNil_StubBehavior` — when invokeFunc is nil,
   the existing stub behavior still works (returns PENDING run).

## Constraints

- The trigger server MUST bind to loopback only (127.0.0.1) in P1.
  Do NOT add a --expose flag (that's P2).
- Use the existing `trigger.New(ServerConfig{...})` constructor.
- Do NOT change the TriggerService.Invoke stub behavior when invokeFunc is nil
  (existing tests depend on it).
- Run `make lint` and `go test ./internal/daemon/... ./internal/trigger/... -race -count=1`
  — both must pass.
- ALL existing tests MUST still pass.
- The trigger server start failure must NOT be fatal for the daemon (daemon
  should still start even if the trigger port is in use — log the error).

## What NOT to Do

- Do NOT add authentication for the trigger server (P1 loopback-only).
- Do NOT implement REST API endpoints beyond what trigger.New provides.
- Do NOT change the Run handler logic — just wire the trigger to call it.
- Do NOT modify existing trigger package tests.
- Do NOT change ServerConfig fields.
