# How Enforcement Works

AgentPaaS enforces policy at the network boundary. Every agent run gets an
isolated agent container and a dual-homed gateway sidecar. This document
explains the topology and how `policy.yaml` becomes runtime controls.

See also: [policy-reference.md](policy-reference.md),
[threat-model.md](threat-model.md), [quickstart.md](quickstart.md).

## Gateway topology

Each run creates two Docker containers on separate networks:

1. **Agent container** — attached only to an **internal** bridge network.
   No default route to the internet. DNS resolves only through the gateway
   stub.
2. **Gateway container** (`agentgateway`) — **dual-homed** on both the
   internal network and a dedicated **egress** network with internet access.

```
┌─────────────────────────────────────────────┐
│  Docker Host                                │
│                                             │
│  ┌──────────┐     ┌──────────┐             │
│  │  Agent   │─────│ Gateway  │──── Internet│
│  │ (internal│     │(dual-homed│            │
│  │  network)│     │ net)     │             │
│  └──────────┘     └──────────┘             │
│      │                  │                  │
│  internal net      egress net              │
│  (no internet)     (internet)              │
└─────────────────────────────────────────────┘
```

The agent cannot reach the internet directly. All outbound HTTP/HTTPS traffic
must pass through the gateway.

The PRIMARY egress control is network topology isolation: the agent
container's Docker network has no default route to the internet. An
additional iptables egress firewall runs inside the agent container as
**defense-in-depth** — it applies `OUTPUT DROP` rules to the container's own
network stack. This firewall may be unavailable in some container
environments; the harness continues without it. Topology isolation remains
the hard boundary.

## HTTP_PROXY routing

The run handler sets standard proxy environment variables on the agent
container:

- `HTTP_PROXY` / `HTTPS_PROXY` → gateway egress port (`:7799`)
- `NO_PROXY` → loopback only

The agent process uses `http.ProxyFromEnvironment` (Go) or equivalent
behavior in the Python SDK harness. Every outbound HTTP request is sent to
the gateway as a `CONNECT` or forward-proxy request.

**P1 limitation:** only HTTP/HTTPS traffic routed through the proxy is
inspected and policy-checked. Raw TCP/UDP is blocked by network isolation,
not deep inspection. See [known-limitations.md](known-limitations.md).

## Policy compilation

When you run `agent policy apply`, AgentPaaS:

1. Parses and validates `policy.yaml` (unknown fields are rejected).
2. Computes a canonical `policy_digest`.
3. Compiles egress rules into agentgateway configuration, including
   `frontendPolicies.networkAuthorization` CEL rules.

Example compiled rule:

```yaml
frontendPolicies:
  networkAuthorization:
    rules:
      - allow: dns.domain == "api.weather.gov"
```

Domains not matching any `allow` rule are denied by default.

## Enforcement at runtime

When the agent makes an outbound HTTP call:

1. The request is proxied to the gateway on the internal network.
2. The gateway evaluates `frontendPolicies.networkAuthorization` CEL rules
   against the destination domain, method, and port.
3. **Allowed** traffic is forwarded to the egress network and out to the
   internet. Brokered credentials are injected at the gateway — raw
   secrets never enter the agent container.
4. **Denied** traffic receives a `403 Forbidden` or connection error back
   to the agent.
5. Every allow/deny decision is written to the signed audit chain.

Denied calls surface in the dashboard and via `agent audit list`.

## Credential injection

Credentials declared in `policy.yaml` are resolved from the macOS keychain
by the secrets broker. The gateway injects them as HTTP headers on allowed
outbound requests. The agent code never sees the secret value.

Direct file leases (compatibility mode) mount a secret file into the agent
container only when explicitly declared in policy.

## Ingress

Ingress rules in `policy.yaml` configure the gateway to accept inbound
trigger API requests on a declared path and port. Ingress traffic is also
policy-scoped and audited.

## Audit trail

All enforcement decisions — egress allows, denials, credential use, MCP
calls — append to a hash-chained JSONL audit log maintained by the daemon.
Export and verify the chain on a second machine:
[audit-export.md](audit-export.md).

## Related reading

- [Policy reference](policy-reference.md) — authoring `policy.yaml`
- [Threat model](threat-model.md) — STRIDE controls mapped to enforcement
- [Known limitations](known-limitations.md) — HTTP_PROXY-only, no transparent proxy