// Package policy parses, validates, canonicalizes, and compiles agent
// policy.yaml documents into agentgateway configuration and related artifacts.
//
// Policies declare agent identity, egress allow-lists, credentials, MCP
// servers, hooks, ingress rules, LLM budget/rate-limit/provider-lock controls,
// ingress auth (JWT or API key), guardrails, transformations, observability,
// and routed-run extensions (model routes, routed_run block).
//
// # Parse and validate
//
// ParsePolicy uses strict YAML decoding (unknown fields rejected at every
// struct level), enforces schema versions "1.0"/"1.1", and validates enums such
// as credential types. ValidatePolicy returns structured ValidationError values
// without embedding secret material in messages.
//
// # Compile
//
// CompileGatewayConfig emits deterministic agentgateway YAML for binds, routes,
// auth, budgets, and backends. Companion compilers produce DNS allow-lists and
// credential rules consumed at deploy/run time.
//
// # Canonical form
//
// Canonicalize normalizes a Policy into a stable representation for digests and
// diff/delta tooling used by pack and operator recommend-patch flows.
package policy
