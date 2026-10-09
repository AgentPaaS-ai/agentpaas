package openshellrt

import (
	"testing"

	osv1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"
)

// TestPort65536NotCopied pins the O02 hole: a port outside 1-65535 must not
// be copied into an OpenShell endpoint port, and must not be rewritten to 0.
// OpenShell documents port 0 as unrestricted. 65536 truncates to that value.
func TestPort65536NotCopied(t *testing.T) {
	if got := declaredPorts([]int{65536}); copiedIllegalPort(got) {
		t.Fatalf("declaredPorts([]int{65536}) = %v, want the out-of-range port dropped, not copied and not rewritten to 0", got)
	}
	kept := declaredPorts([]int{65535, 443, 65536, 0, -1})
	if !portsContain(kept, 65535) || !portsContain(kept, 443) || copiedIllegalPort(kept) {
		t.Fatalf("declaredPorts dropped an in-range port or copied 65536: %v", kept)
	}

	only := SandboxPolicy([]EgressRule{{
		Domain: "api.allowed.example",
		Ports:  []int{65536},
	}}, "", "")
	assertPolicyOmitsIllegalPort(t, only)
	if _, ok := findHost(only, "api.allowed.example"); ok {
		t.Fatal("SandboxPolicy emitted api.allowed.example for out-of-range port 65536")
	}

	mixed := SandboxPolicy([]EgressRule{{
		Domain: "api.allowed.example",
		Ports:  []int{443, 65536},
	}}, "", "")
	assertPolicyOmitsIllegalPort(t, mixed)
	ep, ok := findHost(mixed, "api.allowed.example")
	if !ok || ep.Port != 443 || !portsContain(ep.Ports, 443) || copiedIllegalPort(ep.Ports) || ep.Port == 0 || ep.Port == 65536 {
		t.Fatalf("in-range port 443 was reduced or 65536 was copied: ok=%v port=%d ports=%v", ok, ep.Port, ep.Ports)
	}

	boundary := SandboxPolicy([]EgressRule{{
		Domain: "api.allowed.example",
		Ports:  []int{65535},
	}}, "", "")
	ep, ok = findHost(boundary, "api.allowed.example")
	if !ok || ep.Port != 65535 || !portsContain(ep.Ports, 65535) {
		t.Fatalf("in-range port 65535 was not emitted: ok=%v port=%d ports=%v", ok, ep.Port, ep.Ports)
	}
}

func copiedIllegalPort(ports []uint32) bool {
	for _, p := range ports {
		if p == 0 || p > 65535 {
			return true
		}
	}
	return false
}

func portsContain(ports []uint32, want uint32) bool {
	for _, p := range ports {
		if p == want {
			return true
		}
	}
	return false
}

func assertPolicyOmitsIllegalPort(t *testing.T, pol *osv1.SandboxPolicy) {
	t.Helper()
	if pol == nil {
		t.Fatal("nil sandbox policy")
	}
	for _, ep := range pol.NetworkPolicies["agentpaas-egress"].Endpoints {
		if ep.Port == 65536 || ep.Port == 0 || copiedIllegalPort(ep.Ports) {
			t.Fatalf("SandboxPolicy copied port %d ports %v on %s", ep.Port, ep.Ports, ep.Host)
		}
		for _, p := range ep.Ports {
			if p == 65536 || p == 0 {
				t.Fatalf("SandboxPolicy copied port %d on %s", p, ep.Host)
			}
		}
	}
}
