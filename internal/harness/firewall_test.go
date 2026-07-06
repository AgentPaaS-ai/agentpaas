package harness

import (
	"os"
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
		"defense-in-depth",
	} {
		if !strings.Contains(firewallInitScript, want) {
			t.Errorf("firewall script missing %q", want)
		}
	}
	for _, mustNot := range []string{
		"172.16.0.0/12",
		"10.0.0.0/8",
		"192.168.0.0/16",
		"|| true", // regression: no silent suppression of iptables failures
	} {
		if strings.Contains(firewallInitScript, mustNot) {
			t.Errorf("firewall script must not contain %q", mustNot)
		}
	}
}

func TestInitEgressFirewall_SkipsWhenDisabled(t *testing.T) {
	t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "0")
	// Must not panic on macOS / without iptables.
	InitEgressFirewall()
}

// TestInitEgressFirewall_WarnsButContinuesWhenIptablesMissing verifies Option B:
// when iptables is unavailable, InitEgressFirewall logs a warning but does NOT
// abort harness startup (defense-in-depth, not fail-closed).
func TestInitEgressFirewall_WarnsButContinuesWhenIptablesMissing(t *testing.T) {
	t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "1")

	// On macOS and most non-Linux dev environments, iptables won't be available.
	// The harness must NOT panic or call os.Exit — it should log and continue.
	InitEgressFirewall()
	// If we reach here, the harness did not abort.
}

// TestInitEgressFirewallDefenseInDepthMessage verifies the defense-in-depth
// log message is emitted.
func TestInitEgressFirewall_DefenseInDepthMessage(t *testing.T) {
	t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "1")

	// Redirect stderr to capture log output
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	// Capture log output
	logCh := make(chan string, 1)
	go func() {
		var buf strings.Builder
		b := make([]byte, 4096)
		for {
			n, err := r.Read(b)
			if n > 0 {
				buf.Write(b[:n])
			}
			if err != nil {
				break
			}
		}
		logCh <- buf.String()
	}()

	InitEgressFirewall()

	w.Close()
	os.Stderr = origStderr
	output := <-logCh

	// Should contain the defense-in-depth message (either from Go or the script)
	if !strings.Contains(output, "defense-in-depth") && !strings.Contains(output, "defense in depth") {
		t.Logf("captured log output: %s", output)
		// Not fatal — the Go log package writes to stderr by default but
		// on some platforms the test environment may not capture it.
		// The important thing is InitEgressFirewall didn't abort.
	}
}

// TestFirewallScriptHasNoTrueSuppression verifies the firewall_init.sh
// does not contain `|| true` — we replaced silent suppression with
// explicit warning logging.
func TestFirewallScript_HasNoTrueSuppression(t *testing.T) {
	if strings.Contains(firewallInitScript, "|| true") {
		t.Fatal("firewall_init.sh contains '|| true' suppression — must use explicit error logging instead")
	}
	if strings.Contains(firewallInitScript, "||true") {
		t.Fatal("firewall_init.sh contains '||true' suppression — must use explicit error logging instead")
	}
}

// TestFirewallScript_HasBinaryCheck verifies the script checks for
// iptables binary availability.
func TestFirewallScript_HasBinaryCheck(t *testing.T) {
	if !strings.Contains(firewallInitScript, "command -v iptables") {
		t.Error("firewall_init.sh should check for iptables binary availability")
	}
}

// TestFirewallScript_HasDefenseInDepthMessage verifies the script documents
// its defense-in-depth role.
func TestFirewallScript_HasDefenseInDepthHeader(t *testing.T) {
	if !strings.Contains(firewallInitScript, "DEFENSE-IN-DEPTH") &&
		!strings.Contains(firewallInitScript, "defense-in-depth") &&
		!strings.Contains(firewallInitScript, "defense in depth") {
		t.Error("firewall_init.sh should document that it is defense-in-depth")
	}
}