package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoc_ReadmeStatesTopologyIsPrimaryEgressControl verifies the README
// explicitly states that network topology isolation is the primary egress
// control, not the iptables firewall.
func TestDoc_ReadmeStatesTopologyIsPrimary(t *testing.T) {
	content := readDocFile(t, "../../README.md")

	mustContain := []string{
		"The PRIMARY egress control is network topology isolation",
		"defense-in-depth",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("README.md must contain %q", s)
		}
	}
}

// TestDoc_ReadmeDoesNotClaimFirewallAsPrimary verifies the README does not
// claim the iptables firewall as a primary guarantee.
func TestDoc_ReadmeDoesNotClaimFirewallAsPrimary(t *testing.T) {
	content := readDocFile(t, "../../README.md")

	mustNotContain := []string{
		"firewall is the primary",
		"primary egress control is the firewall",
		"firewall guarantees",
		"iptables is the primary",
		"firewall enforces",
	}
	for _, s := range mustNotContain {
		if strings.Contains(content, s) {
			t.Errorf("README.md must not claim firewall as primary: found %q", s)
		}
	}
}

// TestDoc_EnforcementWorksStatesTopologyIsPrimary verifies
// how-enforcement-works.md states topology is the primary control.
func TestDoc_EnforcementWorksStatesTopologyIsPrimary(t *testing.T) {
	content := readDocFile(t, "../../docs/how-enforcement-works.md")

	mustContain := []string{
		"The PRIMARY egress control is network topology isolation",
		"defense-in-depth",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("docs/how-enforcement-works.md must contain %q", s)
		}
	}
}

// TestDoc_KnownLimitationsStatesFirewallIsDefenseInDepth verifies
// known-limitations.md states the firewall is defense-in-depth.
func TestDoc_KnownLimitationsStatesFirewallIsDefenseInDepth(t *testing.T) {
	content := readDocFile(t, "../../docs/known-limitations.md")

	if !strings.Contains(content, "defense-in-depth") ||
		!strings.Contains(content, "primary") {
		t.Error("docs/known-limitations.md must state firewall is defense-in-depth with topology as primary")
	}
}

// TestDoc_ThreatModelDoesNotClaimFirewallAsPrimary verifies threat-model.md
// does not overclaim the firewall.
func TestDoc_ThreatModelDoesNotClaimFirewallAsPrimary(t *testing.T) {
	content := readDocFile(t, "../../docs/threat-model.md")

	mustNotContain := []string{
		"firewall is the primary",
		"primary egress control is the firewall",
	}
	for _, s := range mustNotContain {
		if strings.Contains(content, s) {
			t.Errorf("docs/threat-model.md must not claim firewall as primary: found %q", s)
		}
	}
}

// readDocFile reads a documentation file relative to the internal/harness directory.
func readDocFile(t *testing.T, relPath string) string {
	t.Helper()
	absPath := filepath.Join("..", "..", relPath)
	// Also try without the extra parent traversal for when running from project root
	data, err := os.ReadFile(absPath)
	if err != nil {
		// Try from project root
		data, err = os.ReadFile(relPath)
		if err != nil {
			t.Fatalf("failed to read doc %s (tried %s and %s): %v", relPath, absPath, relPath, err)
		}
	}
	return string(data)
}