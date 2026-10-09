package openshellrt

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	osv1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"
)

func endpointByHost(t *testing.T, rules []EgressRule, host string) osv1.PolicyNetworkEndpoint {
	t.Helper()
	pol := SandboxPolicy(rules, "", "")
	rule := pol.NetworkPolicies["agentpaas-egress"]
	for _, ep := range rule.Endpoints {
		if ep.Host == host {
			return ep
		}
	}
	t.Fatalf("host %q missing from endpoints %+v", host, rule.Endpoints)
	return osv1.PolicyNetworkEndpoint{}
}

func allowMethods(ep osv1.PolicyNetworkEndpoint) []string {
	var got []string
	for _, rule := range ep.Rules {
		if rule.Allow == nil || rule.Allow.Method == "" {
			continue
		}
		got = append(got, rule.Allow.Method)
	}
	return got
}

// TestAdversaryO02_MethodRestrictionDropped pins BREAK 1: a GET-only allow
// must not become a read-write REST endpoint with no method rule.
func TestAdversaryO02_MethodRestrictionDropped(t *testing.T) {
	ep := endpointByHost(t, []EgressRule{{
		Domain:  "wttr.in",
		Ports:   []int{443},
		Methods: []string{"GET"},
	}}, "wttr.in")
	methods := allowMethods(ep)
	if len(methods) != 1 || methods[0] != "GET" {
		t.Fatalf("GET-only allow copied methods %v, want [GET]", methods)
	}
	if ep.Rules[0].Allow == nil || ep.Rules[0].Allow.Path != "**" {
		t.Fatalf("GET allow path = %+v, want ** so the method is not an empty-path deny", ep.Rules[0].Allow)
	}
	if ep.Access == osv1.NetworkAccessPresetReadWrite && len(ep.Rules) == 0 {
		t.Fatal("GET-only allow became read-write REST with no method rule")
	}
	if ep.Access != osv1.NetworkAccessPresetUnspecified {
		t.Fatalf("method rules are mutually exclusive with access, got %v", ep.Access)
	}
	for _, method := range methods {
		if method == "POST" {
			t.Fatal("POST is allowed on a GET-only host")
		}
	}

	open := endpointByHost(t, []EgressRule{{Domain: "wttr.in", Ports: []int{443}}}, "wttr.in")
	if open.Access != osv1.NetworkAccessPresetReadWrite || len(open.Rules) != 0 {
		t.Fatalf("unrestricted host must stay read-write with no method rule, access %v rules %d", open.Access, len(open.Rules))
	}
}

// TestAdversaryO02_WildcardGlobEmitted pins BREAK 2: a wildcard domain must
// not be copied into an OpenShell host glob, including when allow_wildcard
// is false, nil, or true. True still does not match: policy '*.example.com'
// is an any-depth suffix and OpenShell '*' is one label.
func TestAdversaryO02_WildcardGlobEmitted(t *testing.T) {
	f := false
	tr := true
	cases := []struct {
		name  string
		allow *bool
	}{
		{name: "nil gate", allow: nil},
		{name: "false gate", allow: &f},
		{name: "true gate meaning differs", allow: &tr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !EgressWildcardBlocked("*.example.com", tc.allow) {
				t.Fatal("wildcard domain was not blocked")
			}
			pol := SandboxPolicy([]EgressRule{{
				Domain:        "*.example.com",
				Ports:         []int{443},
				AllowWildcard: tc.allow,
			}, {
				Domain: "wttr.in",
				Ports:  []int{443},
			}}, "", "")
			for _, ep := range pol.NetworkPolicies["agentpaas-egress"].Endpoints {
				if strings.Contains(ep.Host, "*") || ep.Host == "evil.example.com" {
					t.Fatalf("SandboxPolicy emitted host glob %q", ep.Host)
				}
			}
			if _, ok := findHost(pol, "wttr.in"); !ok {
				t.Fatal("exact host was dropped with the wildcard")
			}
		})
	}
}

// TestAdversaryO02_DeniedHostNotAdded pins the user-observable side of
// BREAK 2: allow_wildcard false must not add a DNS glob that matches
// evil.example.com.
func TestAdversaryO02_DeniedHostNotAdded(t *testing.T) {
	allow := false
	pol := SandboxPolicy([]EgressRule{{
		Domain:        "*.example.com",
		Ports:         []int{443},
		AllowWildcard: &allow,
	}}, "", "")
	for _, ep := range pol.NetworkPolicies["agentpaas-egress"].Endpoints {
		if ep.Host == "*.example.com" || ep.Host == "evil.example.com" {
			t.Fatalf("denied host is reachable via endpoint host %q", ep.Host)
		}
	}
	if len(pol.NetworkPolicies["agentpaas-egress"].Endpoints) != 0 {
		t.Fatalf("wildcard rule was added: %+v", pol.NetworkPolicies["agentpaas-egress"].Endpoints)
	}
}

// TestAdversaryO02_ProviderNameCollision pins BREAK 3: distinct credential
// ids must not collapse to one provider name. The original id is hashed in.
func TestAdversaryO02_ProviderNameCollision(t *testing.T) {
	a := providerName("cred-a")
	b := providerName("cred_a")
	if a == b || a == "ap-cred-a" || b == "ap-cred-a" {
		t.Fatalf("cred-a and cred_a collapsed: %q and %q", a, b)
	}
	sumA := sha256.Sum256([]byte("cred-a"))
	sumB := sha256.Sum256([]byte("cred_a"))
	if !strings.Contains(a, hex.EncodeToString(sumA[:8])) {
		t.Fatalf("provider name %q does not hash the original id cred-a", a)
	}
	if !strings.Contains(b, hex.EncodeToString(sumB[:8])) {
		t.Fatalf("provider name %q does not hash the original id cred_a", b)
	}
	for _, name := range []string{a, b} {
		if strings.Contains(name, "_") || len(name) > 63 {
			t.Fatalf("provider name %q is not a DNS-1123 label of at most 63 characters", name)
		}
	}
}

// TestAdversaryO02_DeclaredPortsCollapsed pins BREAK 4: every declared port
// is on the SDK Ports slice. If both Port and Ports are set, Ports wins, so
// the singular field must not be the only copy. When 443 is listed, the
// singular port is 443, not Ports[0].
func TestAdversaryO02_DeclaredPortsCollapsed(t *testing.T) {
	first := endpointByHost(t, []EgressRule{{
		Domain: "wttr.in",
		Ports:  []int{443, 80},
	}}, "wttr.in")
	assertPortsSlice(t, first, 443, 80)
	if first.Port != 443 {
		t.Fatalf("ports [443, 80] singular port = %d, want 443", first.Port)
	}

	second := endpointByHost(t, []EgressRule{{
		Domain: "wttr.in",
		Ports:  []int{80, 443},
	}}, "wttr.in")
	assertPortsSlice(t, second, 80, 443)
	if second.Port != 443 {
		t.Fatalf("singular port kept Ports[0]=%d, want 443 when it is listed", second.Port)
	}

	only80 := endpointByHost(t, []EgressRule{{
		Domain: "wttr.in",
		Ports:  []int{80},
	}}, "wttr.in")
	assertPortsSlice(t, only80, 80)
	if only80.Port != 80 {
		t.Fatalf("port 80 was replaced with %d", only80.Port)
	}
}

func assertPortsSlice(t *testing.T, ep osv1.PolicyNetworkEndpoint, want ...uint32) {
	t.Helper()
	if len(ep.Ports) != len(want) {
		t.Fatalf("SDK Ports = %v, want %v (singular port %d is dropped when Ports is set)", ep.Ports, want, ep.Port)
	}
	for i, p := range want {
		if ep.Ports[i] != p {
			t.Fatalf("SDK Ports = %v, want %v", ep.Ports, want)
		}
	}
}

func findHost(pol *osv1.SandboxPolicy, host string) (osv1.PolicyNetworkEndpoint, bool) {
	if pol == nil {
		return osv1.PolicyNetworkEndpoint{}, false
	}
	for _, ep := range pol.NetworkPolicies["agentpaas-egress"].Endpoints {
		if ep.Host == host {
			return ep, true
		}
	}
	return osv1.PolicyNetworkEndpoint{}, false
}
