package openshellrt

import (
	"reflect"
	"strings"
	"testing"

	osv1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"
)

func TestSandboxPolicyAllowlistOmitsDeniedHost(t *testing.T) {
	pol := SandboxPolicy([]EgressRule{
		{Domain: "wttr.in", Ports: []int{443}, Methods: []string{"GET"}},
		{Domain: "openrouter.ai", Ports: []int{443}, Methods: []string{"POST"}},
	}, "ap-o02-broker-key", "openrouter.ai")
	rule := pol.NetworkPolicies["agentpaas-egress"]
	if len(rule.Endpoints) != 2 {
		t.Fatalf("endpoints: got %d", len(rule.Endpoints))
	}
	var sawWttr, sawBroker bool
	for _, ep := range rule.Endpoints {
		if ep.Host == "evil.example.com" {
			t.Fatal("denied host must not be in the OpenShell allowlist")
		}
		if ep.Host == "wttr.in" {
			sawWttr = true
			if ep.CredentialBinding != nil {
				t.Fatal("wttr.in must not bind the broker credential")
			}
		}
		if ep.Host == "openrouter.ai" {
			sawBroker = true
			if ep.Protocol != "rest" || ep.Access != osv1.NetworkAccessPresetReadWrite {
				t.Fatalf("sandbox broker endpoint must stay rest with access, got protocol=%q access=%v", ep.Protocol, ep.Access)
			}
			if ep.CredentialBinding == nil || ep.CredentialBinding.Provider != "ap-o02-broker-key" {
				t.Fatalf("openrouter binding: %+v", ep.CredentialBinding)
			}
		}
	}
	if !sawWttr || !sawBroker {
		t.Fatalf("missing hosts wttr=%v broker=%v", sawWttr, sawBroker)
	}
}

func TestProviderNameSanitizes(t *testing.T) {
	if got := providerName("o02-broker-key"); got != "ap-o02-broker-key" {
		t.Fatalf("got %q", got)
	}
}

// TestBrokerProfileDoesNotEmitUnarmedRestEndpoint pins the gateway rejection
// from O02: protocol rest on a profile endpoint requires rules or access, and
// the pinned SDK converter (NetworkEndpointToProto) copies only Host, Port,
// and Protocol. Credential injection stays on SandboxPolicy, which can set
// Access and CredentialBinding. An endpoint of any protocol would also become
// the credential boundary and reject that binding.
func TestBrokerProfileDoesNotEmitUnarmedRestEndpoint(t *testing.T) {
	epType := reflect.TypeOf(osv1.NetworkEndpoint{})
	for _, name := range []string{"Access", "Rules"} {
		if _, ok := epType.FieldByName(name); ok {
			t.Fatalf("pinned SDK NetworkEndpoint has %s; NetworkEndpointToProto would be able to arm a rest endpoint", name)
		}
	}

	// NetworkEndpointToProto copies only Host, Port, and Protocol. Access and
	// Rules are not on the pinned type, so a rest endpoint is sent unarmed.
	profile := brokerProfile()
	for i, ep := range profile.Endpoints {
		protocol := strings.ToLower(strings.TrimSpace(ep.Protocol))
		if protocol == "rest" {
			t.Fatalf("endpoints[%d]: protocol rest would be sent without access or rules (host %q port %d)", i, ep.Host, ep.Port)
		}
		if protocol != "" && protocol != "tcp" {
			t.Fatalf("endpoints[%d]: protocol %q requires rules or access the pinned converter cannot send", i, ep.Protocol)
		}
	}
	if len(profile.Endpoints) != 0 {
		t.Fatalf("broker profile must be endpointless so sandbox policy credential_binding stays the credential boundary, got %d", len(profile.Endpoints))
	}
}
