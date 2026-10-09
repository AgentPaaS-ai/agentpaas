package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/openshellrt"
)

// TestLoadOpenShellEgressDropsPort65536 pins the deploy-loader half of the
// O02 hole. ValidatePolicy already rejects port 65536. The loader must not
// copy it onto the rules SandboxPolicy would hand the SDK, and must not
// rewrite it to port 0.
func TestLoadOpenShellEgressDropsPort65536(t *testing.T) {
	dir := t.TempDir()
	policyYAML := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.allowed.example
    ports: [65536]
  - domain: files.allowed.example
    ports: [65535]
  - domain: wttr.in
    ports: [443, 65536]
`
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(policyYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, _, _ := loadOpenShellEgress(dir)
	var saw65535, saw443 bool
	for _, rule := range rules {
		for _, p := range rule.Ports {
			if p == 65536 || p == 0 || p < 1 || p > 65535 {
				t.Fatalf("loadOpenShellEgress copied port %d on %s", p, rule.Domain)
			}
		}
		switch strings.ToLower(rule.Domain) {
		case "api.allowed.example":
			t.Fatalf("loadOpenShellEgress returned port 65536's host with ports %v", rule.Ports)
		case "files.allowed.example":
			saw65535 = containsInt(rule.Ports, 65535)
		case "wttr.in":
			saw443 = containsInt(rule.Ports, 443) && !containsInt(rule.Ports, 65536)
		}
	}
	if !saw65535 {
		t.Fatal("loadOpenShellEgress dropped in-range port 65535")
	}
	if !saw443 {
		t.Fatal("loadOpenShellEgress dropped in-range port 443 or kept 65536")
	}

	pol := openshellrt.SandboxPolicy(rules, "", "")
	for _, ep := range pol.NetworkPolicies["agentpaas-egress"].Endpoints {
		if ep.Port == 65536 || ep.Port == 0 {
			t.Fatalf("loaded policy emitted port %d on %s", ep.Port, ep.Host)
		}
		for _, p := range ep.Ports {
			if p == 65536 || p == 0 {
				t.Fatalf("loaded policy emitted port %d on %s", p, ep.Host)
			}
		}
	}
}

func containsInt(ports []int, want int) bool {
	for _, p := range ports {
		if p == want {
			return true
		}
	}
	return false
}
