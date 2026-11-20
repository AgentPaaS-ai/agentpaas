// Package trigger serves the AgentPaaS Trigger API over gRPC (default port
// 7718) and REST through grpc-gateway (default port 7717).
//
// The Trigger API admits external and scheduled invocations: webhook handlers,
// API-key authentication, idempotency, SSE event streams, cron management,
// cancel/handoff flows, and durable outbox/event-store integration.
//
// # Authentication and exposure
//
// Authentication is required even on loopback. Requests must present a valid
// Bearer API key (or other configured Authenticator) before invoking
// TriggerService methods. Binding with Exposed=true requires a configured API
// key authenticator; misconfiguration fails closed at server construction.
//
// # CORS
//
// CORS is deny-by-default: only explicitly allowed origins receive CORS
// response headers, and browser-originated requests without explicit
// authentication still receive an unauthenticated response.
//
// # Durability helpers
//
// Supporting types include IdempotencyStore, EventBus, durable filesystem
// event stores, outbox delivery, and cron schedulers used by the daemon cron
// RPCs and long-running trigger workloads.
package trigger
