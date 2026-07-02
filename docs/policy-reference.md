# Policy Reference

`policy.yaml` is the canonical policy file for an agent. It lives in git
next to `agent.yaml`, is reviewed in PRs, and compiles into agentgateway
configuration at apply time.

See [how-enforcement-works.md](how-enforcement-works.md) for runtime
enforcement and [quickstart.md](quickstart.md) for a worked example.

## File format

```yaml
version: "1"
agent:
  name: <agent-name>
  description: <optional description>
egress: []        # outbound network rules (allow-list)
credentials: []   # credential bindings for egress
mcp_servers: []   # MCP server definitions (optional)
hooks: []         # webhook destinations (optional)
ingress: []       # inbound trigger listeners (optional)
```

Unknown fields are rejected. Typos fail closed instead of silently weakening
security.

## Top-level fields

| Field | Required | Description |
|---|---|---|
| `version` | yes | Policy schema version. Use `"1"` for P1. |
| `agent.name` | yes | Agent identity; should match `agent.yaml` name. |
| `agent.description` | no | Human-readable description. |
| `egress` | no | Allow-list of outbound destinations. Empty = default deny. |
| `credentials` | no | Credential sources injected by the gateway broker. |
| `ingress` | no | Inbound webhook/trigger listeners. |

## Egress rules

Each entry in `egress` is an **allow** rule. Domains not listed are denied
(default deny). There is no explicit `action: deny` field — omission is
denial.

| Field | Required | Description |
|---|---|---|
| `domain` | usually | Exact hostname (e.g. `api.openai.com`). Subdomains require explicit entries or `allow_wildcard: true`. |
| `ports` | no | Allowed ports (e.g. `[443]`). |
| `methods` | no | Allowed HTTP methods (e.g. `[GET, POST]`). |
| `credential` | no | Credential `id` to bind for this destination. |
| `allow_wildcard` | no | Set `true` to allow wildcard domains like `*.example.com`. |
| `cidr` | no | IP-CIDR allow rule (requires `allow_private: true` for RFC1918). |

Compiled into agentgateway CEL rules:

```yaml
frontendPolicies:
  networkAuthorization:
    rules:
      - allow: dns.domain == "api.example.com"
```

## Credentials

| Field | Required | Description |
|---|---|---|
| `id` | yes | Unique credential identifier referenced by egress rules. |
| `type` | yes | One of: `header`, `brokered`, `file`, `direct_lease`. |
| `header` | for `header` | HTTP header name for injection (e.g. `Authorization`). |
| `value` | for `header` | Header value template (e.g. `Bearer ${secret}`). |
| `service` | for `brokered` | Keychain service name. |
| `path` | for `file` / `direct_lease` | Mount path inside the container. |
| `mode` | for `direct_lease` | `file` (P1; `env` is rejected). |
| `reason` | for `direct_lease` | Justification string (required). |

Brokered credentials are resolved from the macOS keychain and injected at
the gateway. The agent never sees the raw secret.

## Ingress rules

| Field | Required | Description |
|---|---|---|
| `path` | yes | URL path prefix for the trigger listener. |
| `port` | no | Listen port (default `7718`). |

## Examples

### Allow one domain

```yaml
version: "1"
agent:
  name: weather-agent
egress:
  - domain: api.weather.gov
    ports: [443]
credentials: []
ingress: []
```

### Allow multiple domains

```yaml
version: "1"
agent:
  name: invoice-chaser
egress:
  - domain: api.openai.com
    ports: [443]
    credential: openai-prod
  - domain: api.stripe.com
    ports: [443]
    methods: [GET]
    credential: stripe-readonly
  - domain: hooks.slack.com
    ports: [443]
    methods: [POST]
credentials:
  - id: openai-prod
    type: brokered
    service: openai-prod
  - id: stripe-readonly
    type: brokered
    service: stripe-readonly
ingress: []
```

### Default deny

An empty egress list denies all outbound traffic:

```yaml
version: "1"
agent:
  name: locked-down-agent
egress: []
credentials: []
ingress: []
```

Any HTTP call from the agent receives `403` or a connection error at the
gateway. This is the secure default scaffolded by `agent init --noninteractive`.

### Credential injection

```yaml
version: "1"
agent:
  name: api-client
egress:
  - domain: api.example.com
    ports: [443]
    credential: api-key
credentials:
  - id: api-key
    type: header
    header: X-API-Key
    value: "${api-key}"
ingress: []
```

Store the secret in the keychain before running:

```bash
agent secrets set api-key
```

## Validation rules

- Domain matching is exact by default. `example.com` does not allow
  `api.example.com`.
- Wildcard domains require `allow_wildcard: true`.
- Private CIDR ranges require `allow_private: true`.
- Brokered credential injection is header-only in P1.
- The compiler produces a deterministic `policy_digest` regardless of YAML
  key order or comments.

## Related docs

- [How enforcement works](how-enforcement-works.md)
- [Known limitations](known-limitations.md)
- [Threat model](threat-model.md)