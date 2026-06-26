# Block 14B-T02: Policy Enforcement at Runtime

## Context

AgentPaaS is a Go project at /Users/pms88/projects/agentpaas. T01 created a
gateway container (agentgateway v1.3.0) that is dual-homed on the internal and
egress networks. The gateway mounts a compiled gateway.yaml config.

However, the gateway does NOT currently enforce policy because:
1. The agent container makes direct HTTP calls (via the harness's http.Client)
2. The agent is on the internal-only network, so its HTTP calls fail with DNS
   errors (network isolation), NOT policy decisions
3. The gateway is running but no traffic flows through it

## How agentgateway Enforcement Works

agentgateway uses `frontendPolicies.networkAuthorization` with CEL rules.
Example from the golden config:
```yaml
frontendPolicies:
  networkAuthorization:
    - allow: dns.domain == "api.openai.com"
    - allow: dns.domain == "api.stripe.com"
```

The gateway intercepts connections at L4 and evaluates CEL rules. Denied
connections are refused. Allowed connections are proxied to the backend.

For this to work, the agent's traffic must flow through the gateway. There are
two approaches:

### Approach A: HTTP Proxy (SIMPLER for P1)
Set HTTP_PROXY/HTTPS_PROXY environment variables in the agent container to point
to the gateway's listen port. The harness's http.Client already respects these
env vars (Go's http.ProxyFromEnvironment).

The gateway config already has:
```yaml
binds:
  - port: 7799
    listeners:
      - protocol: HTTP
        routes:
          - name: egress
```

So if we set HTTPS_PROXY=http://gateway:7799 in the agent container env, the
harness's HTTP calls will route through the gateway, which enforces the policy.

The gateway IP on the internal network is assigned by Docker. We need to discover
it after container creation.

### Approach B: Transparent Proxy (P2)
Configure iptables/Docker network DNS to force all traffic through the gateway.
More complex, requires network-level changes. P2.

## What to Implement (Approach A)

### Part 1: Discover gateway IP on internal network

After starting the gateway container, we need its IP on the internal network
so we can set it as the HTTP proxy for the agent.

In `internal/runtime/docker.go`, the `InspectContainerNetworks` method already
returns network info including IP addresses. Add a helper:

```go
// InspectContainerIP returns the IP address of a container on a specific network.
// Returns empty string if the container is not attached to the network.
func (d *DockerRuntime) InspectContainerIP(ctx context.Context, id ContainerID, networkID string) (string, error) {
    if d.driver != nil {
        return d.driver.InspectContainerIP(ctx, id, networkID)
    }
    if string(id) == "" {
        return "", ErrContainerNotFound
    }
    // Use InspectContainerNetworks and find the IP for the matching network.
    networks, err := d.InspectContainerNetworks(ctx, id)
    if err != nil {
        return "", err
    }
    for _, n := range networks {
        if n.NetworkID == networkID || n.Name == networkID {
            return n.IPAddress, nil
        }
    }
    return "", nil
}
```

Check ContainerNetworkInfo struct to see if it has IPAddress. If not, add it.

### Part 2: Set HTTP_PROXY in agent container env

In `internal/daemon/control_handlers.go`, Run handler:

After starting the gateway container and BEFORE creating the agent container,
discover the gateway's IP on the internal network:

```go
// Discover gateway IP on internal network for HTTP proxy configuration.
gatewayIP, err := rt.InspectContainerIP(ctx, gatewayID, string(netID))
if err != nil {
    fmt.Fprintf(os.Stderr, "daemon: discover gateway IP: %v\n", err)
    // Non-fatal: agent will use direct connections (which fail on internal network)
}
```

Then add the proxy env vars to the agent container spec:

```go
proxyEnv := []string{
    "AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl",
    "AGENTPAAS_AGENT_PATH=/app/main.py",
}
if gatewayIP != "" {
    proxyEnv = append(proxyEnv,
        fmt.Sprintf("HTTP_PROXY=http://%s:7799", gatewayIP),
        fmt.Sprintf("HTTPS_PROXY=http://%s:7799", gatewayIP),
        fmt.Sprintf("http_proxy=http://%s:7799", gatewayIP),
        fmt.Sprintf("https_proxy=http://%s:7799", gatewayIP),
        "NO_PROXY=localhost,127.0.0.1",
        "no_proxy=localhost,127.0.0.1",
    )
}
```

Use `proxyEnv` instead of the hardcoded env in the agent ContainerSpec.

### Part 3: Update e2e test expectations

The existing e2e test (`TestE2E_PackRunInvokeStopAudit`) expects egress_denied
for BOTH api.weather.gov and evil-exfil.example.com because both fail on the
internal-only network.

With T02, if a policy allowing api.weather.gov has been applied, the allowed
call should now SUCCEED (through the gateway proxy). The denied call to
evil-exfil.example.com should still fail but with a different reason.

However — the e2e test does NOT apply a policy before running. So all calls
still fail (default-deny). The test expectations may still be correct.

To properly test T02, add a new test that:
1. Applies a policy allowing api.weather.gov
2. Runs the agent
3. Verifies the allowed call succeeds (egress_allowed audit event)
4. Verifies the denied call fails (egress_denied with policy reason)

This test should be gated behind AGENTPAAS_DOCKER_TESTS=1 and requires real
network access to api.weather.gov (or a mock upstream).

### Part 4: Verify gateway config compatibility

The compiled gateway config from `policy.CompileGatewayConfig` must be accepted
by the real agentgateway binary. The smoke test (`internal/policy/smoke_test.go`)
already verifies this with `--validate-only`. No changes needed unless the
config format is wrong.

## Tests

1. `TestInspectContainerIP` (internal/runtime) — mock test verifying the IP
   lookup logic.

2. `TestRun_SetsProxyEnvWhenGatewayIPAvailable` (internal/daemon) — verify the
   agent container env includes HTTP_PROXY/HTTPS_PROXY when gatewayIP is
   discovered.

3. `TestRun_OmitsProxyEnvWhenNoGatewayIP` — when InspectContainerIP fails or
   returns empty, no proxy env is set (agent falls back to direct connections).

4. Update existing mock tests to handle the new InspectContainerIP call.

## Constraints

- Do NOT change the harness handleHTTP code (it already respects HTTP_PROXY
  via Go's http.ProxyFromEnvironment default transport).
- Do NOT implement Approach B (transparent proxy).
- The gateway port 7799 must match the egress bind port in the compiled config.
  Read internal/policy/compiler.go to verify which port is used for egress binds.
- If InspectContainerIP is not available on the RuntimeDriver interface, add it.
- Run `make lint` and `go test ./internal/daemon/... ./internal/runtime/... -race -count=1`.
- ALL existing tests MUST still pass.

## What NOT to Do

- Do NOT change the gateway container creation (T01 handles that).
- Do NOT change the harness code.
- Do NOT change the policy compiler.
- Do NOT add new gateway config fields.
