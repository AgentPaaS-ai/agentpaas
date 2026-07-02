# R17 Research: Non-HTTP Egress Control in Container Sandboxes

## Problem
AgentPaaS P1 uses HTTP_PROXY env vars to route agent traffic through the gateway.
This only covers HTTP/HTTPS. Raw TCP, DNS, and other protocols bypass the gateway.
An agent that unsets HTTP_PROXY or uses non-HTTP protocols can attempt direct connections
(though it fails because the agent is on an internal-only network with no internet gateway).

## Research Findings

### Validated P1-Feasible Approach: iptables + HTTP_PROXY (two-layer enforcement)

**Source:** mattolson/agent-sandbox, 89luca89/clampdown, agentcage
**Status:** PROVEN on Colima + macOS Apple Silicon

The approach combines two layers:
1. **HTTP_PROXY env vars** (existing AgentPaaS approach) — routes HTTP/HTTPS through gateway
2. **iptables rules inside the agent container** — blocks ALL direct outbound TCP except to the gateway/proxy

The iptables init script (from agent-sandbox/images/base/init-firewall.sh):
```bash
# Allow loopback
iptables -A OUTPUT -o lo -j ACCEPT
# Allow established connections
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
# Allow Docker bridge network (where the proxy/gateway sidecar runs)
iptables -A OUTPUT -d <bridge_network> -j ACCEPT
# Drop everything else (direct internet access blocked)
iptables -P OUTPUT DROP
iptables -A OUTPUT -j REJECT
```

This means: even if an agent unsets HTTP_PROXY or opens a raw TCP socket, the packet
hits iptables DROP before reaching the network interface. The ONLY path out is through
the gateway sidecar on the bridge network.

### Requirements
- Agent container needs `cap_add: [NET_ADMIN, NET_RAW]` for iptables
- Works inside Colima's Linux VM (iptables operates in the container's network namespace)
- The init script runs at container start (before the agent process)

### Security Trade-off
NET_ADMIN lets the agent modify its own iptables rules. Mitigations:
1. Agent runs as UID 64000 non-root — iptables requires root, so non-root agent can't modify rules
2. The init script runs as PID 1 (root), sets rules, then drops to UID 64000 for the agent
3. For P1, this is acceptable — the threat model is policy enforcement, not containing a root adversary

### DNS Gap (Critical)
AWS Bedrock AgentCore was bypassed via DNS tunneling (Unit 42, BeyondTrust findings).
DNS queries bypass HTTP_PROXY and iptables OUTPUT DROP (if UDP 53 is allowed).
For P1, the gateway handles DNS (agent uses gateway's DNS proxy). The iptables rules
should ALSO block direct UDP/TCP 53 to external resolvers, forcing DNS through the gateway.

### Alternative Approaches (P2)
- **eBPF (Cilium)**: More robust, no NET_ADMIN needed, but requires kernel 5.10+ and
  Cilium setup — overkill for P1, Colima support uncertain
- **Network namespace sharing**: Agent shares sidecar's network namespace entirely.
  No NET_ADMIN needed but changes the Docker networking model significantly.
- **DNS redirect via iptables DNAT**: Redirect all UDP 53 to the gateway's DNS proxy.
  Addresses the DNS tunneling vector specifically.

## Recommendation for AgentPaaS P1

**Implement the two-layer approach (iptables + existing HTTP_PROXY):**

1. Add an iptables init script to the agent container that:
   - Allows loopback + established connections
   - Allows traffic to the gateway's IP on the internal network
   - Drops all other outbound TCP/UDP (including direct DNS to external resolvers)
2. Add `cap_add: [NET_ADMIN]` to the agent container spec (for iptables only)
3. The init script runs as root (PID 1), sets rules, then execs the agent as UID 64000
4. Keep the existing HTTP_PROXY env vars — they're still needed for the gateway to
   receive and inspect HTTP traffic

**Complexity:** Medium. Requires:
- A small init binary or script in the agent container
- Modifying the daemon's Run handler to add NET_ADMIN cap + init command
- Testing that the rules work with the existing dual-homed gateway topology
- The harness binary (current PID 1) would need to run the firewall init before
  starting, OR a wrapper script does iptables then execs the harness

**Closes the gap:** Yes — raw TCP, DNS, and all non-HTTP protocols are blocked at
the network level, not just the env-var level.

**Colima compatibility:** Yes — iptables operates in the container's Linux network
namespace which Colima provides. Verified by 3 independent open-source projects.
