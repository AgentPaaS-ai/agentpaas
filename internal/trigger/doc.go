// Package trigger serves the AgentPaaS Trigger API over gRPC on port 7718 and
// REST through grpc-gateway on port 7717.
//
// Authentication is required even on loopback. Requests must present a valid
// Bearer API key or mTLS identity before invoking TriggerService methods.
//
// CORS is deny-by-default: only explicitly allowed origins receive CORS
// response headers, and browser-originated requests without explicit
// authentication still receive an unauthenticated response.
package trigger
