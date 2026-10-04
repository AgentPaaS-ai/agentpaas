package openshellrt

import (
	"reflect"
	"slices"
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

// TestSandboxPolicyReadOnlyIncludesAgentpaas pins the O02 Landlock failure:
// an empty filesystem policy is replaced by the v0.1.1 proxy baseline
// (/usr, /lib, /etc, /app, /var/log, /proc, /dev/urandom), which does not
// grant execute on /agentpaas. The supervisor then cannot spawn
// /agentpaas/harness (os error 13) even though the file is mode 0555 and
// uid 64000 can execute it under docker run. A set filesystem policy
// replaces that baseline, so both the entrypoint and the baseline paths
// must be listed, and IncludeWorkdir must stay true.
func TestSandboxPolicyReadOnlyIncludesAgentpaas(t *testing.T) {
	pol := SandboxPolicy(nil, "", "")
	if pol == nil || pol.Filesystem == nil {
		t.Fatal("empty filesystem policy is replaced by a baseline that omits /agentpaas")
	}
	if !pol.Filesystem.IncludeWorkdir {
		t.Fatal("IncludeWorkdir must be true so the workdir is not dropped when filesystem policy is set")
	}
	want := []string{
		"/agentpaas",
		"/usr",
		"/lib",
		"/etc",
		"/app",
		"/var/log",
		"/proc",
		"/dev/urandom",
	}
	for _, path := range want {
		if !slices.Contains(pol.Filesystem.ReadOnly, path) {
			t.Fatalf("SandboxPolicy read-only paths omit %q: %v", path, pol.Filesystem.ReadOnly)
		}
	}
}

func TestProviderNameSanitizes(t *testing.T) {
	if got := providerName("o02-broker-key"); got != "ap-o02-broker-key" {
		t.Fatalf("got %q", got)
	}
}

// TestBrokerProviderVisibleInWorkspaceDefault pins the O02 failure where
// Profiles().Get succeeded and Providers().Ensure then reported that
// agentpaas-broker was not in the requested scope. Ensure looks up
// provider.profile_workspace. Empty is the global scope and does not see a
// profile imported into workspace default. The secret stays on the provider,
// under the env var the profile declares, not in the agent environment.
func TestBrokerProviderVisibleInWorkspaceDefault(t *testing.T) {
	const secret = "broker-unit-fixture"
	provider := brokerProvider("ap-o02-broker-key", secret)
	if provider.Type != profileID {
		t.Fatalf("type: got %q want %q", provider.Type, profileID)
	}
	if provider.Spec.ProfileWorkspace != workspaceDefault {
		t.Fatalf("profile_workspace: got %q want %q; empty is the global scope Ensure missed", provider.Spec.ProfileWorkspace, workspaceDefault)
	}
	if provider.Spec.ProfileWorkspace == "" {
		t.Fatal("empty profile_workspace is not the scope a workspace import is visible in")
	}

	profile := brokerProfile()
	if len(profile.Endpoints) != 0 {
		t.Fatalf("imported profile must stay endpointless, got %d endpoints", len(profile.Endpoints))
	}
	if len(profile.Credentials) != 1 || len(profile.Credentials[0].EnvVars) != 1 {
		t.Fatal("profile must declare one env var; that is the key the gateway stores")
	}
	key := profile.Credentials[0].EnvVars[0]
	if key == "" || key == "api_key" {
		t.Fatalf("stored credential key %q is not the profile env var", key)
	}
	if got := provider.Spec.Credentials[key]; got != secret {
		t.Fatalf("provider credential %q: got %q", key, got)
	}
	if _, ok := provider.Spec.Credentials["api_key"]; ok {
		t.Fatal("api_key is not an accepted stored key when the profile declares an env var")
	}

	workspaceScoped := &osv1.ProviderProfile{ID: profileID, Scope: "workspace"}
	if profileVisibleToEnsure(workspaceScoped, "") {
		t.Fatal("Get of a workspace profile is not proof it is in the empty scope Ensure used")
	}
	if !profileVisibleToEnsure(workspaceScoped, workspaceDefault) {
		t.Fatal("after import, the workspace profile must be visible to Ensure in workspace default")
	}
	if profileVisibleToEnsure(workspaceScoped, "other") {
		t.Fatal("a default-workspace profile must not be treated as visible in another workspace")
	}
	if profileVisibleToEnsure(nil, workspaceDefault) {
		t.Fatal("missing profile is not visible")
	}
	if profileVisibleToEnsure(&osv1.ProviderProfile{ID: "other", Scope: "workspace"}, workspaceDefault) {
		t.Fatal("a different profile id is not the broker profile Ensure requests")
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
