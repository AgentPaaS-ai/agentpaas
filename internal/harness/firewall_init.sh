#!/bin/sh
# AgentPaaS agent container firewall init.
# Runs as root (PID 1) before the agent process starts.
# Blocks all direct outbound except to the gateway on the internal network.
#
# This iptables firewall is DEFENSE-IN-DEPTH. The PRIMARY egress control is
# Docker network topology isolation (internal-only network, no default route
# to the internet). If iptables/ip6tables are unavailable or any rule fails,
# this script logs a warning and continues — the agent is still isolated by
# the network topology.

log_warn() {
    echo "harness: firewall defense-in-depth: $*" >&2
}

# Check iptables availability — warn if missing, but continue (defense-in-depth).
if ! command -v iptables >/dev/null 2>&1; then
    log_warn "iptables not found; firewall defense-in-depth unavailable (topology isolation remains primary)"
fi

if ! command -v ip6tables >/dev/null 2>&1; then
    log_warn "ip6tables not found; firewall defense-in-depth unavailable (topology isolation remains primary)"
fi

GATEWAY_IP="${AGENTPAAS_GATEWAY_IP:-}"

# Allow loopback
if command -v iptables >/dev/null 2>&1; then
    if ! iptables -A OUTPUT -o lo -j ACCEPT 2>/dev/null; then
        log_warn "iptables rule (loopback ACCEPT) failed; continuing"
    fi

    # Allow established connections (return traffic)
    if ! iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT 2>/dev/null; then
        log_warn "iptables rule (ESTABLISHED,RELATED ACCEPT) failed; continuing"
    fi

    # If gateway IP is known, allow traffic to it
    if [ -n "$GATEWAY_IP" ]; then
        if ! iptables -A OUTPUT -d "$GATEWAY_IP" -j ACCEPT 2>/dev/null; then
            log_warn "iptables rule (gateway IP ACCEPT) failed; continuing"
        fi
    fi

    # Allow only the specific internal Docker bridge subnet (gateway + harness RPC).
    # AGENTPAAS_GATEWAY_SUBNET is set by the daemon to the /16 derived from the gateway IP.
    # When unset, no broad private-space allow is added — fail closed via OUTPUT DROP.
    GATEWAY_SUBNET="${AGENTPAAS_GATEWAY_SUBNET:-}"
    if [ -n "$GATEWAY_SUBNET" ]; then
        if ! iptables -A OUTPUT -d "$GATEWAY_SUBNET" -j ACCEPT 2>/dev/null; then
            log_warn "iptables rule (gateway subnet ACCEPT) failed; continuing"
        fi
    fi

    # Drop all other outbound (direct internet access blocked)
    if ! iptables -P OUTPUT DROP 2>/dev/null; then
        log_warn "iptables policy (OUTPUT DROP) failed; continuing"
    fi
fi

# IPv6: same policy (drop all outbound except loopback/established)
if command -v ip6tables >/dev/null 2>&1; then
    if ! ip6tables -A OUTPUT -o lo -j ACCEPT 2>/dev/null; then
        log_warn "ip6tables rule (loopback ACCEPT) failed; continuing"
    fi
    if ! ip6tables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT 2>/dev/null; then
        log_warn "ip6tables rule (ESTABLISHED,RELATED ACCEPT) failed; continuing"
    fi
    # Drop all other IPv6 outbound
    if ! ip6tables -P OUTPUT DROP 2>/dev/null; then
        log_warn "ip6tables policy (OUTPUT DROP) failed; continuing"
    fi
fi

echo "harness: firewall_init.sh complete (defense-in-depth)"