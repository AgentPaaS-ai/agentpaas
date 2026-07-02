#!/bin/sh
# AgentPaaS agent container firewall init.
# Runs as root (PID 1) before the agent process starts.
# Blocks all direct outbound except to the gateway on the internal network.
set -e

GATEWAY_IP="${AGENTPAAS_GATEWAY_IP:-}"

# Allow loopback
iptables -A OUTPUT -o lo -j ACCEPT 2>/dev/null || true
# Allow established connections (return traffic)
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT 2>/dev/null || true

# If gateway IP is known, allow traffic to it
if [ -n "$GATEWAY_IP" ]; then
    iptables -A OUTPUT -d "$GATEWAY_IP" -j ACCEPT 2>/dev/null || true
fi

# Allow only the specific internal Docker bridge subnet (gateway + harness RPC).
# AGENTPAAS_GATEWAY_SUBNET is set by the daemon to the /16 derived from the gateway IP.
# Falls back to broad RFC1918 if unset (backward compat for older daemon versions).
GATEWAY_SUBNET="${AGENTPAAS_GATEWAY_SUBNET:-}"
if [ -n "$GATEWAY_SUBNET" ]; then
    iptables -A OUTPUT -d "$GATEWAY_SUBNET" -j ACCEPT 2>/dev/null || true
else
    iptables -A OUTPUT -d 172.16.0.0/12 -j ACCEPT 2>/dev/null || true
    iptables -A OUTPUT -d 10.0.0.0/8 -j ACCEPT 2>/dev/null || true
    iptables -A OUTPUT -d 192.168.0.0/16 -j ACCEPT 2>/dev/null || true
fi

# Drop all other outbound (direct internet access blocked)
iptables -P OUTPUT DROP 2>/dev/null || true

# IPv6: same policy (drop all outbound except loopback/established)
ip6tables -A OUTPUT -o lo -j ACCEPT 2>/dev/null || true
ip6tables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT 2>/dev/null || true
# Drop all other IPv6 outbound
ip6tables -P OUTPUT DROP 2>/dev/null || true