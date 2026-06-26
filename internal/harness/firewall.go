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
// Skips gracefully when disabled, not on Linux, or when iptables is unavailable.
func InitEgressFirewall() {
	if !EgressFirewallEnabled() {
		log.Printf("harness: egress firewall disabled (AGENTPAAS_EGRESS_FIREWALL=%q)", os.Getenv("AGENTPAAS_EGRESS_FIREWALL"))
		return
	}
	if runtime.GOOS != "linux" {
		log.Printf("harness: egress firewall skipped (GOOS=%s, need linux)", runtime.GOOS)
		return
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		log.Printf("harness: egress firewall skipped (iptables not found: %v)", err)
		return
	}
	if _, err := exec.LookPath("sh"); err != nil {
		log.Printf("harness: egress firewall skipped (sh not found: %v)", err)
		return
	}

	tmp, err := os.CreateTemp("", "agentpaas-firewall-*.sh")
	if err != nil {
		log.Printf("harness: egress firewall skipped (temp script: %v)", err)
		return
	}
	path := tmp.Name()
	defer func() { _ = os.Remove(path) }()

	if _, err := tmp.WriteString(firewallInitScript); err != nil {
		_ = tmp.Close()
		log.Printf("harness: egress firewall skipped (write script: %v)", err)
		return
	}
	if err := tmp.Chmod(0o700); err != nil {
		_ = tmp.Close()
		log.Printf("harness: egress firewall skipped (chmod script: %v)", err)
		return
	}
	if err := tmp.Close(); err != nil {
		log.Printf("harness: egress firewall skipped (close script: %v)", err)
		return
	}

	cmd := exec.Command("sh", path)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("harness: egress firewall init failed (continuing): %v: %s", err, string(out))
		return
	}
	if len(out) > 0 {
		log.Printf("harness: egress firewall init: %s", string(out))
	}
	log.Printf("harness: egress firewall rules applied (gateway=%q)", os.Getenv("AGENTPAAS_GATEWAY_IP"))
}