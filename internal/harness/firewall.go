package harness

import (
	_ "embed"
	"log"
	"os"
	"os/exec"
	"runtime"
)

//go:embed firewall_init.sh
var firewallInitScript string

// EgressFirewallEnabled reports whether the container should apply iptables egress rules.
// AGENTPAAS_EGRESS_FIREWALL defaults to enabled ("1"); set "0" to disable.
func EgressFirewallEnabled() bool {
	v := os.Getenv("AGENTPAAS_EGRESS_FIREWALL")
	if v == "" {
		return true
	}
	return v != "0" && v != "false" && v != "no"
}

// InitEgressFirewall applies best-effort iptables OUTPUT rules before the Python worker starts.
// This firewall is defense-in-depth; the primary egress control is Docker network topology
// isolation (internal-only network + gateway sidecar). When iptables is unavailable or any
// rule fails, a warning is logged but harness startup continues.
// Call DropNetAdminCapability immediately after this (see cmd/harness/main.go) so the agent
// process cannot flush rules via inherited CAP_NET_ADMIN.
func InitEgressFirewall() {
	if !EgressFirewallEnabled() {
		log.Printf("harness: egress firewall disabled (AGENTPAAS_EGRESS_FIREWALL=%q); topology isolation remains primary", os.Getenv("AGENTPAAS_EGRESS_FIREWALL"))
		return
	}

	log.Printf("harness: egress firewall is defense-in-depth; topology isolation is the primary control")

	if runtime.GOOS != "linux" {
		log.Printf("harness: egress firewall defense-in-depth unavailable (GOOS=%s, need linux); topology isolation remains primary", runtime.GOOS)
		return
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		log.Printf("harness: egress firewall defense-in-depth unavailable (iptables not found: %v); topology isolation remains primary", err)
		return
	}
	if _, err := exec.LookPath("sh"); err != nil {
		log.Printf("harness: egress firewall defense-in-depth unavailable (sh not found: %v); topology isolation remains primary", err)
		return
	}

	tmp, err := os.CreateTemp("", "agentpaas-firewall-*.sh")
	if err != nil {
		log.Printf("harness: egress firewall defense-in-depth unavailable (temp script: %v); topology isolation remains primary", err)
		return
	}
	path := tmp.Name()
	defer func() { _ = os.Remove(path) }() // best-effort remove

	if _, err := tmp.WriteString(firewallInitScript); err != nil {
		_ = tmp.Close() // best-effort close
		log.Printf("harness: egress firewall defense-in-depth unavailable (write script: %v); topology isolation remains primary", err)
		return
	}
	if err := tmp.Chmod(0o700); err != nil {
		_ = tmp.Close() // best-effort close
		log.Printf("harness: egress firewall defense-in-depth unavailable (chmod script: %v); topology isolation remains primary", err)
		return
	}
	if err := tmp.Close(); err != nil {
		log.Printf("harness: egress firewall defense-in-depth unavailable (close script: %v); topology isolation remains primary", err)
		return
	}

	cmd := exec.Command("sh", path)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("harness: egress firewall init failed (continuing — defense-in-depth): %v: %s", err, string(out))
		return
	}
	if len(out) > 0 {
		log.Printf("harness: egress firewall init: %s", string(out))
	}
	log.Printf("harness: egress firewall rules applied (defense-in-depth; gateway=%q)", os.Getenv("AGENTPAAS_GATEWAY_IP"))
}