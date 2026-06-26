package daemon

import "os"

// egressFirewallEnabled returns true when agent containers should get NET_ADMIN and
// iptables egress enforcement (default on). Set AGENTPAAS_EGRESS_FIREWALL=0 to disable.
func egressFirewallEnabled() bool {
	v := os.Getenv("AGENTPAAS_EGRESS_FIREWALL")
	if v == "" {
		return true
	}
	return v != "0" && v != "false" && v != "no"
}