# B14E Adversary Review — R12, R17, R1

Review three security-sensitive changes from Block 14E. For each, identify
HIGH/MEDIUM/LOW findings with concrete bypass scenarios. Output structured
FINDING N: [severity] [title] blocks.

## R17 — iptables egress firewall (commit e245144)

Files changed:
- internal/harness/firewall_init.sh (the iptables script)
- internal/harness/firewall.go (Go wrapper that runs the script)
- internal/daemon/egress_firewall.go
- internal/daemon/control_handlers.go (adds NET_ADMIN cap + AGENTPAAS_GATEWAY_IP env)
- internal/runtime/docker.go + driver.go (CapAdd field)

Threat model: the agent container runs as UID 64000 non-root. The firewall
init runs as PID 1 root. The agent gets CAP_NET_ADMIN.

Review focus:
1. Can a non-root agent (UID 64000) modify/delete the iptables rules set by PID 1?
   (iptables requires root — verify the agent truly can't flush rules without root)
2. Is CAP_NET_ADMIN exploitable by a non-root process? What can UID 64000 + NET_ADMIN do?
3. Does the firewall script correctly block ALL non-HTTP egress? Check:
   - Raw TCP to external IPs
   - UDP (especially DNS port 53 to external resolvers)
   - IPv6 (if the container has IPv6 connectivity, iptables v4 rules don't apply)
   - ICMP
4. The script allows RFC 1918 private ranges (172.16/12, 10/8, 192.168/16). Is this
   too broad? Could an attacker route through a Docker network in those ranges?
5. Is the AGENTPAAS_EGRESS_FIREWALL=0 bypass documented and acceptable?
6. Does NET_ADMIN on the agent allow network namespace attacks (creating new netns
   to escape the firewall)?

## R1 — conditional tlog suppression (commit 93527fc)

Files: internal/pack/lock.go — buildCosignSignArgs, buildCosignVerifyArgs

Review focus:
1. For production refs (non-localhost), the code no longer passes --signing-config
   and no longer passes --insecure-ignore-tlog. Is the sign→verify round-trip correct?
2. Can an attacker craft an image ref that looks like localhost but resolves externally?
   (e.g. "localhost.evil.com:5000" — does isLocalRegistryRef use substring match?)
3. What happens if cosign sign WITHOUT tlog suppression fails (Rekor unavailable)?
   Does the error propagate correctly or does it fall back to no-tlog?

## R12 — orphan reconciliation (existing code, commit 9f79f50 verified it's present)

Files: internal/daemon/control_handlers.go reconcileOrphanedContainers (~line 1197)

Review focus:
1. Does the gateway + egress cleanup use the dual-label filter (managed-by AND resource-type)?
2. Can a non-AgentPaaS container with spoofed labels trigger removal of real gateway containers?
3. Race: can a Run RPC arriving during reconciliation have its gateway removed?
