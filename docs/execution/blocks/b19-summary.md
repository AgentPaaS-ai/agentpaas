# Block 19 — AgentGateway Policy Integration (v0.1.2)

**Status:** SPEC
**Date:** 2026-07-06
**Target version:** v0.1.2
**Block scope:** Wire up agentgateway's native policy features into
AgentPaaS's policy compiler, daemon, and SDK. Move credential injection,
token limits, rate limiting, OAuth, and guardrails from the harness to the
gateway where they belong.

## Context

Block 18 manual testing revealed that AgentPaaS uses agentgateway (v1.3.0)
only as a dumb HTTP proxy — hostname-based routing with a catch-all 403
deny. All credential injection, token budgets, and security enforcement
happen at the harness level (inside the agent container). This is
functional but architecturally wrong:

1. The harness process has credential values in memory — not truly
   "brokered" if the agent container is compromised.
2. `CompileCredentialRules()` exists in the policy compiler but is never
   called — dead code.
3. The agentgateway supports rich LLM governance (budget limits, rate
   limiting, token counting, cost tracking, guardrails, RBAC) that
   AgentPaaS doesn't use at all.
4. OAuth flows (ingress auth, egress backend auth) are not supported.

B19 wires up these gateway-native features so the gateway becomes the
true policy enforcement point — the harness becomes a thin RPC relay.

## AgentGateway Feature Inventory

Based on agentgateway v1.3.0 docs and config schema:

### LLM Governance
- **Budget limits** (`llm/budget-limits`): Per-route token budget (max
  tokens per request or per time window). Gateway denies when exceeded.
- **Rate limiting** (`llm/rate-limit`): Requests per second/minute per
  route. Local (in-memory) or remote (Redis-backed).
- **Token counting** (`llm/api-types/token-count`): Gateway counts tokens
  in real-time for budget enforcement and cost tracking.
- **Cost tracking** (`llm/costs`): Per-model cost tracking with custom
  pricing. Exposed via `agctl costs` and OTel metrics.
- **Provider locking**: Route only to whitelisted LLM providers (already
  done via egress, but gateway can enforce at the API level too).

### Security & Auth
- **JWT auth** (`security/jwt`): Validate JWT tokens on ingress (trigger
  API auth). RBAC via CEL policy engine.
- **API key auth** (`security/apikey`): API key authentication on ingress.
- **OAuth2 proxy** (`oauth2-proxy`): OAuth2 authentication for backend
  services (egress OAuth).
- **Backend OAuth** (`backend-oauth`): Gateway obtains and refreshes
  OAuth tokens for upstream LLM providers automatically.
- **CEL-based RBAC** (`llm/rbac`): Fine-grained access control using
  Common Expression Language (who can call which model/tool).
- **TLS** (`tls`): End-to-end TLS for ingress and egress.

### MCP Governance
- **MCP auth** (`mcp/auth`): Authentication for MCP server connections.
- **MCP tool access** (`mcp/tool-access`): Per-tool access control (allow
  or deny specific MCP tools by name).
- **MCP rate limiting** (`mcp/rate-limit`): Rate limit MCP tool calls.

### Traffic Management
- **Request/response transformations** (`traffic-management/transformations`):
  Inject, remove, or modify headers and body content.
- **Retry** (`resiliency/retry`): Automatic retry with backoff for
  failed upstream calls.
- **Timeouts** (`resiliency/timeouts`): Per-route request timeouts.
- **Traffic splitting** (`traffic-management/traffic-split`): Canary
  deployments, A/B testing for agent versions.

### Guardrails
- **Prompt guards** (`llm/prompt-guards`): Multi-layered content
  filtering — regex patterns, OpenAI moderation, custom webhooks.
- **Prompt enrichment** (`llm/prompt-enrichment`): Inject system prompts,
  context, or instructions before forwarding to LLM.

### Observability
- **Access logging** (`traffic-management/transformations/access-logs`):
  Structured access logs for all requests.
- **OTel tracing** (`observability/tracing`): Distributed tracing.
- **LLM observability** (`llm/observability`): Token counts, latency,
  model usage metrics.

## B19 Task Breakdown

### T1: Gateway-Level Credential Injection (P0)
Wire `CompileCredentialRules()` into `CompileGatewayConfig()`. The gateway
config includes per-route credential injection rules. The daemon resolves
credential values from Keychain and passes them to the gateway (not the
harness). The harness `http_with_credential()` becomes a hint to the
gateway — the agent code sends only the credential_id, and the gateway
injects the actual header value.

**Files:**
- `internal/policy/compiler.go` — Add credential rules to gateway config
- `internal/daemon/control_handlers.go` — Pass resolved credentials to
  gateway container (env vars or mounted secret file), NOT to harness
- `internal/harness/rpc_server.go` — Remove credential injection from
  handleHTTP (gateway does it now)
- Tests: gateway config includes credential rules, harness no longer
  injects credentials

### T2: LLM Token Budget & Rate Limiting (P0)
Add `llm_budget` and `llm_rate_limit` sections to policy.yaml. The policy
compiler emits agentgateway `localRateLimit` and budget limit policies
on the LLM route.

```yaml
llm_budget:
  max_tokens: 10000        # total tokens per invoke
  max_tokens_per_request: 2000  # per-LLM-call limit

llm_rate_limit:
  requests_per_minute: 30
  tokens_per_minute: 50000
```

**Files:**
- `internal/policy/schema.go` — Add LLMBudget, LLMRateLimit structs
- `internal/policy/compiler.go` — Emit gateway rate limit + budget policies
- `internal/policy/parser.go` — Parse new fields
- `internal/policy/validation.go` — Validate new fields
- Tests: policy with budget/rate limits compiles to correct gateway config

### T3: LLM Provider Locking (P1)
Enforce that LLM calls can ONLY go to the configured provider's domain.
Currently done via egress policy (B17), but the gateway should also
validate the LLM endpoint at the API level.

```yaml
llm:
  provider: openrouter
  model: deepseek/deepseek-v4-flash
  credential: openrouter-key
  allowed_endpoints:
    - https://openrouter.ai/api/v1/chat/completions
```

**Files:**
- `internal/policy/compiler.go` — Add route match for LLM endpoint
- `internal/llm/adapter.go` — Expose endpoint list per provider
- Tests: gateway config restricts LLM route to provider endpoint only

### T4: Ingress Auth — JWT & API Key (P1)
Add `ingress_auth` section to policy.yaml. The gateway validates incoming
trigger requests against JWT or API key before forwarding to the agent.

```yaml
ingress_auth:
  type: jwt  # or api_key
  jwt:
    issuer: https://auth.example.com
    audience: agentpaas
    jwks_url: https://auth.example.com/.well-known/jwks.json
  api_key:
    header: X-API-Key
    credential: trigger-api-key  # Keychain secret name
```

**Files:**
- `internal/policy/schema.go` — Add IngressAuth struct
- `internal/policy/compiler.go` — Emit JWT/API key auth policies on ingress route
- `internal/daemon/control_handlers.go` — Pass auth config to gateway
- Tests: gateway config includes auth policies

### T5: Egress OAuth — Backend Token Refresh (P1)
The gateway obtains and refreshes OAuth tokens for upstream LLM providers.
This solves the xAI OAuth token expiry problem (tokens expire in ~6h).

```yaml
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: https://api.x.ai/oauth/token
    client_id: <client_id>
    refresh_token_credential: xai-refresh-token  # Keychain secret
    header: Authorization
```

**Files:**
- `internal/policy/schema.go` — Add OAuth credential type
- `internal/policy/compiler.go` — Emit backend-oauth config for gateway
- `internal/daemon/control_handlers.go` — Pass OAuth config to gateway
- Tests: OAuth credential compiles to backend-oauth gateway config

### T6: Gateway-Level Guardrails (P2)
Add `guardrails` section to policy.yaml. Multi-layered content filtering
on LLM prompts and responses.

```yaml
guardrails:
  - type: regex
    pattern: "(?i)(password|secret|api.key)"
    action: block
  - type: moderation
    provider: openai
    credential: openai-key
  - type: webhook
    url: https://guardrails.example.com/check
```

**Files:**
- `internal/policy/schema.go` — Add Guardrail struct
- `internal/policy/compiler.go` — Emit guardrail policies
- Tests: guardrail policy compiles to correct gateway config

### T7: Request/Response Transformations (P2)
Allow policy to inject system prompts, remove headers, or modify request
bodies before they reach the LLM.

```yaml
transformations:
  request:
    inject_headers:
      X-Agent-ID: "${agent_name}"
    inject_system_prompt: "You are a helpful assistant. Always be concise."
  response:
    remove_headers:
      - X-Internal-Debug
```

**Files:**
- `internal/policy/schema.go` — Add Transformation structs
- `internal/policy/compiler.go` — Emit transformation policies
- Tests: transformation policy compiles correctly

### T8: Per-Route Timeouts & Retry (P2)
Add timeout and retry policies per egress route.

```yaml
egress:
  - domain: openrouter.ai
    ports: [443]
    timeout: 30s
    retry:
      max_attempts: 3
      backoff: exponential
      max_backoff: 10s
```

**Files:**
- `internal/policy/schema.go` — Add Timeout, Retry structs to EgressRule
- `internal/policy/compiler.go` — Emit timeout/retry policies per route
- Tests: egress with timeout/retry compiles correctly

### T9: Cost Tracking & Observability (P2)
Wire up agentgateway's cost tracking and OTel metrics. Token counts and
costs are reported per-run.

```yaml
observability:
  cost_tracking: true
  otel_endpoint: http://localhost:4317
```

**Files:**
- `internal/policy/schema.go` — Add Observability struct
- `internal/policy/compiler.go` — Emit OTel config for gateway
- `internal/daemon/control_handlers.go` — Read cost data from gateway after run
- Tests: observability config compiles, cost data captured

### T10: MCP Tool Access Control (P2)
Per-tool access control for MCP servers.

```yaml
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["@modelcontextprotocol/server-filesystem"]
    allowed_tools:
      - read_file
      - list_directory
    denied_tools:
      - write_file
      - delete_file
```

**Files:**
- `internal/policy/schema.go` — Add allowed_tools/denied_tools to MCPServer
- `internal/policy/compiler.go` — Emit MCP tool access policies
- Tests: MCP with tool access control compiles correctly

### T11: B19 Manual Testing (P0)
Re-run T1-T10 from B18 with the new gateway-level policies. Verify:
- Credentials injected by gateway (not harness)
- Token budgets enforced at gateway
- Rate limiting works
- Ingress auth blocks unauthenticated trigger calls
- Egress OAuth refreshes tokens automatically
- Guardrails block sensitive content
- Cost tracking reports per-run costs

## Success Gates

1. `go build ./...` compiles clean
2. `go test ./internal/... -count=1 -timeout 10m` — 0 failures
3. B19 T1-T10 manual tests pass
4. policy.yaml with all new fields validates and compiles to correct
   agentgateway config
5. Gateway config is valid YAML that agentgateway v1.3.0 accepts
6. Existing agents without new fields still work (backward compat)

## Backward Compatibility

- All new policy.yaml fields are optional. Existing policies with only
  `egress` and `credentials` continue to work.
- The harness credential injection path remains as a fallback when the
  gateway doesn't have credential rules (for older policy versions).
- The `http_with_credential()` SDK method continues to work — it just
  becomes a hint to the gateway instead of the harness.
