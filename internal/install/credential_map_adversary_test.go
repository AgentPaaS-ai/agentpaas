package install

import (
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

func TestAdversary_ManifestMapOnlyRenamesLookupKey(t *testing.T) {
	resolver := secrets.MapCredentialResolver{Map: map[string]string{
		"key-a": "local-a",
		"key-b": "local-a",
	}}
	if local, ok := resolver.Resolve("key-a"); !ok || local != "local-a" {
		t.Fatalf("resolve key-a = %q %v", local, ok)
	}
	if local, ok := resolver.Resolve("key-b"); !ok || local != "local-a" {
		t.Fatalf("resolve key-b = %q %v", local, ok)
	}
	if _, ok := resolver.Resolve("not-declared"); ok {
		t.Fatal("undeclared id should not resolve")
	}
}

func TestAdversary_FakeScopeWideningMapEntryDoesNotChangePolicyIDs(t *testing.T) {
	pol := parseTestPolicy(t, testDualEgressPolicyYAML())
	m := InstallManifest{CredentialMap: map[string]string{
		"key-a": "wide-secret",
		"key-b": "wide-secret",
		"extra-evil": "wide-secret",
	}}
	MergeCredentialMapIntoManifest(&m, m.CredentialMap)
	if !isDeclaredBrokeredCredential(pol, "key-a") || !isDeclaredBrokeredCredential(pol, "key-b") {
		t.Fatal("signed policy credentials unchanged")
	}
	if isDeclaredBrokeredCredential(pol, "extra-evil") {
		t.Fatal("manifest cannot declare new policy credentials")
	}
	// Egress rules still reference only signed policy credential IDs.
	if pol.Egress[0].Credential != "key-a" || pol.Egress[1].Credential != "key-b" {
		t.Fatalf("egress credentials = %s %s", pol.Egress[0].Credential, pol.Egress[1].Credential)
	}
}