package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/openshellrt"
	osv1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"
)

// TestAdversaryO02_LoaderEmitsWildcardAndDropsMethods pins the daemon loader
// half of BREAK 2 and the method drop on the loaded rules. allow_wildcard
// false must not be passed through as an OpenShell host glob, and a GET
// allow must survive translation.
func TestAdversaryO02_LoaderEmitsWildcardAndDropsMethods(t *testing.T) {
	t.Run("allow_wildcard false", func(t *testing.T) {
		assertLoaderSkipsWildcard(t, "false")
	})
	t.Run("allow_wildcard true meaning differs", func(t *testing.T) {
		assertLoaderSkipsWildcard(t, "true")
	})
}

func assertLoaderSkipsWildcard(t *testing.T, allow string) {
	t.Helper()
	dir := t.TempDir()
	policyYAML := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "*.example.com"
    ports: [443]
    methods: [GET]
    allow_wildcard: ` + allow + `
  - domain: wttr.in
    ports: [443, 80]
    methods: [GET]
`
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(policyYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, _, _ := loadOpenShellEgress(dir)
	for _, rule := range rules {
		if strings.Contains(rule.Domain, "*") {
			t.Fatalf("loader passed wildcard domain %q with allow_wildcard: %s", rule.Domain, allow)
		}
	}
	var sawWttr bool
	for _, rule := range rules {
		if !strings.EqualFold(rule.Domain, "wttr.in") {
			continue
		}
		sawWttr = true
		if len(rule.Methods) != 1 || rule.Methods[0] != "GET" {
			t.Fatalf("loader dropped methods: %v", rule.Methods)
		}
	}
	if !sawWttr {
		t.Fatal("loader dropped exact host wttr.in")
	}

	pol := openshellrt.SandboxPolicy(rules, "", "")
	for _, ep := range pol.NetworkPolicies["agentpaas-egress"].Endpoints {
		if strings.Contains(ep.Host, "*") {
			t.Fatalf("loader translation emitted host glob %q", ep.Host)
		}
	}
	var sawGET bool
	for _, ep := range pol.NetworkPolicies["agentpaas-egress"].Endpoints {
		if ep.Host != "wttr.in" {
			continue
		}
		for _, rule := range ep.Rules {
			if rule.Allow != nil && rule.Allow.Method == "GET" {
				sawGET = true
			}
		}
		if ep.Access == osv1.NetworkAccessPresetReadWrite && len(ep.Rules) == 0 {
			t.Fatal("loader translation dropped GET into read-write with no method rule")
		}
		if len(ep.Ports) != 2 || ep.Ports[0] != 443 || ep.Ports[1] != 80 {
			t.Fatalf("loader translation collapsed ports to %d %v", ep.Port, ep.Ports)
		}
	}
	if !sawGET {
		t.Fatal("loader translation dropped the GET method restriction")
	}
}
