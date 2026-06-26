# Adversary Review: Block 14B-T02 — Policy Enforcement via HTTP_PROXY

## What Changed

The Run handler now discovers the gateway container's IP on the internal network
and sets HTTP_PROXY/HTTPS_PROXY env vars in the agent container, routing all
HTTP traffic through the agentgateway's egress port (7799).

Changes:
- New RuntimeDriver method: InspectContainerIP(ctx, id, networkID) → IP string
- ContainerNetworkInfo.IPAddress field added
- Run handler: gateway IP discovery + proxy env injection
- InspectContainerNetworks delegation guard added (was missing)

## Files to Review

1. internal/daemon/control_handlers.go — proxy env injection (lines ~267-300)
2. internal/runtime/docker.go — InspectContainerIP, InspectContainerNetworks
3. internal/runtime/driver.go — ContainerNetworkInfo.IPAddress, interface method
4. internal/daemon/control_handlers_proxy_test.go — proxy tests
5. internal/daemon/control_handlers_e2e_test.go — e2e policy test

## Review Focus

1. **Proxy bypass**: Can the agent bypass HTTP_PROXY by:
   - Using a non-HTTP protocol (raw TCP, DNS)?
   - Setting its own env vars to override HTTP_PROXY?
   - Connecting directly to the egress network?
   - Using NO_PROXY to exclude blocked domains?

2. **Gateway IP discovery race**: 
   - Is the gateway IP stable after container start?
   - Could Docker reassign the IP while the agent is running?
   - What if InspectContainerIP returns empty (gateway not yet on network)?

3. **NO_PROXY configuration**:
   - Is localhost,127.0.0.1 sufficient?
   - Should it include the internal network range?
   - Can an attacker use NO_PROXY to bypass the gateway?

4. **Env var injection security**:
   - Are the env vars properly sanitized (no injection in the IP)?
   - Could a malicious gateway IP redirect traffic to an attacker?

5. **HTTPS proxy compatibility**:
   - Does agentgateway support HTTPS CONNECT proxying?
   - Will the harness's http.Client work with HTTPS through the proxy?
   - Are there certificate validation issues?

6. **Delegation guard on InspectContainerNetworks**:
   - Was it missing before? Does adding it break any existing behavior?

Report findings as:
FINDING N: [CRITICAL|HIGH|MEDIUM|LOW] [title]
Description and recommended fix.
