# package trigger

## Purpose

`trigger` implements the external Trigger API: authenticated gRPC + REST
gateway servers for invoking agents, streaming events, managing cron, and
recording durable trigger-side state (idempotency, outbox, event store).

## Key Types

| Type | Role |
|------|------|
| `Server` / `ServerConfig` | Dual gRPC/REST server |
| `Authenticator` / `APIKeyAuthenticator` | Request authentication |
| `CORSMiddleware` | Explicit-origin CORS |
| `IdempotencyStore` | Replay/conflict handling |
| `EventBus` | In-process event fan-out |
| Durable event store types | Filesystem-backed event durability |
| Cron schedule types | Cron add/list/remove integration |
| Webhook / SSE helpers | HTTP ingress and server-sent events |

## Key Functions

| Symbol | Role |
|--------|------|
| `New` | Construct server from config (fail-closed expose checks) |
| `(*Server).Start` / `Stop` | Listen and graceful shutdown |
| TriggerService RPCs | Invoke, status, cancel, stream, cron, … |
| Auth middleware | Bearer API key validation |
| Outbox/handoff helpers | Reliable side effects and workflow handoff |

## Architecture

```
External client
    |  REST :7717 (grpc-gateway)  or  gRPC :7718
    v
Authn + payload caps + CORS policy
    |
    v
TriggerService
    +-- idempotency store
    +-- event bus / SSE
    +-- durable event store / outbox
    +-- cron scheduler
    v
daemon control plane / routed admission
```

Defaults bind loopback addresses. Payload size defaults to 1 MiB.

## Usage

```go
srv, err := trigger.New(trigger.ServerConfig{
    Authenticator: apiKeys,
    Audit:         auditAppender,
    IdempotencyStore: idemp,
    EventBus: eventBus,
})
if err != nil {
    return err
}
return srv.Start(ctx)
```
