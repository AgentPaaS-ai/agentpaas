package harness

import (
	"strings"
	"testing"
)

func TestEgressFirewallEnabled_DefaultOn(t *testing.T) {
	t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "")
	if !EgressFirewallEnabled() {
		t.Fatal("want default enabled")
	}
}

func TestEgressFirewallEnabled_Disabled(t *testing.T) {
	for _, v := range []string{"0", "false", "no"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("AGENTPAAS_EGRESS_FIREWALL", v)
			if EgressFirewallEnabled() {
				t.Fatalf("want disabled for %q", v)
			}
		})
	}
}

func TestEmbeddedFirewallScript_Content(t *testing.T) {
	if firewallInitScript == "" {
		t.Fatal("embedded firewall_init.sh is empty")
	}
	for _, want := range []string{
		"AGENTPAAS_GATEWAY_IP",
		"iptables -A OUTPUT -o lo -j ACCEPT",
		"iptables -P OUTPUT DROP",
		"ip6tables -A OUTPUT -o lo -j ACCEPT",
		"ip6tables -P OUTPUT DROP",
		"AGENTPAAS_GATEWAY_SUBNET",
		"|| true",
	} {
		if !strings.Contains(firewallInitScript, want) {
			t.Errorf("firewall script missing %q", want)
		}
	}
}

func TestInitEgressFirewall_SkipsWhenDisabled(t *testing.T) {
	t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "0")
	// Must not panic on macOS / without iptables.
	InitEgressFirewall()
}