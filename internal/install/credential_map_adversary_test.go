package install

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

const adversaryInstallSentinel = "ADVERSARY-B23T03-INSTALL-LEAK-GUARD-888"

func TestAdversary_B23T03_ManifestMapOnlyRenamesLookupKey(t *testing.T) {
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

func TestAdversary_B23T03_FakeScopeWideningMapEntryDoesNotChangePolicyIDs(t *testing.T) {
	pol := parseTestPolicy(t, testDualEgressPolicyYAML())
	m := InstallManifest{CredentialMap: map[string]string{
		"key-a":      "wide-secret",
		"key-b":      "wide-secret",
		"extra-evil": "wide-secret",
	}}
	MergeCredentialMapIntoManifest(&m, m.CredentialMap)
	if !isDeclaredBrokeredCredential(pol, "key-a") || !isDeclaredBrokeredCredential(pol, "key-b") {
		t.Fatal("signed policy credentials unchanged")
	}
	if isDeclaredBrokeredCredential(pol, "extra-evil") {
		t.Fatal("manifest cannot declare new policy credentials")
	}
	if pol.Egress[0].Credential != "key-a" || pol.Egress[1].Credential != "key-b" {
		t.Fatalf("egress credentials = %s %s", pol.Egress[0].Credential, pol.Egress[1].Credential)
	}
}

func TestAdversary_B23T03_ApplyMapCredential_InvalidDeclaredManifestUnchanged(t *testing.T) {
	polYAML := testBrokeredPolicyYAML()
	store := seedStoreWithSentinel(t, "good-local")
	state, _ := newConsentState(t)
	fp := strings.Repeat("b", 64)
	m := InstallManifest{
		PublisherFingerprint: fp,
		PublisherName:        "pub",
		AgentName:            "credmap-agent",
		AgentVersion:         "1.0.0",
		AcceptedPolicyDigest: "digest",
		CredentialMap:        map[string]string{"api-token": "good-local"},
	}
	if err := state.SaveApprovedInstall(m, polYAML); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ref := FormatInstallRef(m.AgentName, fp)

	err := ApplyMapCredential(MapCredentialOpts{
		State: state, Store: store, Ref: ref, Mapping: "not-declared=good-local",
	})
	if !errors.Is(err, ErrCredentialMapInvalid) {
		t.Fatalf("want ErrCredentialMapInvalid, got %v", err)
	}
	prior, err := state.GetInstallByRef(ref)
	if err != nil || prior == nil {
		t.Fatalf("reload: %v", err)
	}
	if prior.Manifest.CredentialMap["api-token"] != "good-local" {
		t.Fatalf("manifest mutated on invalid declared id: %+v", prior.Manifest.CredentialMap)
	}
	if _, ok := prior.Manifest.CredentialMap["not-declared"]; ok {
		t.Fatal("manifest gained attacker entry")
	}
}

func TestAdversary_B23T03_ApplyMapCredential_MissingLocalManifestUnchanged(t *testing.T) {
	polYAML := testBrokeredPolicyYAML()
	store := secrets.NewFakeKeyStore()
	state, _ := newConsentState(t)
	fp := strings.Repeat("c", 64)
	m := InstallManifest{
		PublisherFingerprint: fp,
		AgentName:            "credmap-agent",
		AgentVersion:         "1.0.0",
		AcceptedPolicyDigest: "digest",
	}
	if err := state.SaveApprovedInstall(m, polYAML); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ref := FormatInstallRef(m.AgentName, fp)

	err := ApplyMapCredential(MapCredentialOpts{
		State: state, Store: store, Ref: ref, Mapping: "api-token=missing-local",
	})
	if !errors.Is(err, ErrCredentialMapInvalid) {
		t.Fatalf("want ErrCredentialMapInvalid, got %v", err)
	}
	prior, err := state.GetInstallByRef(ref)
	if err != nil || prior == nil {
		t.Fatalf("reload: %v", err)
	}
	if len(prior.Manifest.CredentialMap) != 0 {
		t.Fatalf("manifest mutated on missing local: %+v", prior.Manifest.CredentialMap)
	}
}

func TestAdversary_B23T03_TTY_InvalidLocalThreePromptsRefusedNoStateWrite(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := seedStoreWithSentinel(t, "good-local")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)

	_, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store, InstallRef: "credmap-agent@abcdef01",
		IsTTY: true,
		Prompt: func(string) (string, error) { return "bad-name", nil },
	})
	if !errors.Is(err, ErrCredentialMapRefused) {
		t.Fatalf("want ErrCredentialMapRefused after %d prompts, got %v", credentialMapPromptLimit, err)
	}
	assertNoStateGrowth(t, root, before)
	_ = state
}

func TestAdversary_B23T03_NonTTY_UndeclaredIDRefused(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := seedStoreWithSentinel(t, "my-local")
	_, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store,
		MapCredentials: []string{"evil-id=my-local"},
	})
	if !errors.Is(err, ErrCredentialMapInvalid) {
		t.Fatalf("want ErrCredentialMapInvalid, got %v", err)
	}
}

func TestAdversary_B23T03_NonTTY_NonexistentLocalRefused(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := secrets.NewFakeKeyStore()
	_, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store,
		MapCredentials: []string{"api-token=ghost-local"},
	})
	if !errors.Is(err, ErrCredentialMapInvalid) {
		t.Fatalf("want ErrCredentialMapInvalid, got %v", err)
	}
}

func TestAdversary_B23T03_ManifestJSONStoresNamesOnlyNotSecretValues(t *testing.T) {
	ctx := context.Background()
	store := secrets.NewFakeKeyStore()
	if err := store.Set(ctx, "name-only-local", []byte(adversaryInstallSentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	res, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store,
		MapCredentials: []string{"api-token=name-only-local"},
	})
	if err != nil {
		t.Fatalf("ResolveCredentialMapping: %v", err)
	}
	m := InstallManifest{AgentName: "credmap-agent", PublisherFingerprint: strings.Repeat("d", 64)}
	MergeCredentialMapIntoManifest(&m, res.Map)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(raw)
	if strings.Contains(jsonStr, adversaryInstallSentinel) {
		t.Fatalf("manifest JSON leaked secret value: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "name-only-local") {
		t.Fatalf("manifest should store local name only: %s", jsonStr)
	}
}

func TestAdversary_B23T03_InstallMappingOutputsNeverContainSentinel(t *testing.T) {
	ctx := context.Background()
	store := secrets.NewFakeKeyStore()
	if err := store.Set(ctx, "sentinel-holder", []byte(adversaryInstallSentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	var warns []string
	var events []auditEvent
	res, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store, InstallRef: "credmap-agent@aaaaaaaa",
		IsTTY: true,
		Prompt:    func(string) (string, error) { return "sentinel-holder", nil },
		PrintWarn: func(msg string) { warns = append(warns, msg) },
		EmitAudit: auditCollector(&events),
	})
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	m := InstallManifest{AgentName: "credmap-agent", PublisherFingerprint: strings.Repeat("e", 64)}
	MergeCredentialMapIntoManifest(&m, res.Map)
	raw, _ := json.Marshal(m)
	blobs := []string{strings.Join(res.DisplayLines, "\n"), strings.Join(warns, "\n"), string(raw)}
	for _, e := range events {
		if e.EventType != audit.EventTypeInstallCredentialMapped {
			continue
		}
		for _, v := range e.Payload {
			blobs = append(blobs, v)
		}
	}
	for _, blob := range blobs {
		if strings.Contains(blob, adversaryInstallSentinel) {
			t.Fatalf("install output leaked sentinel: %q", blob)
		}
	}
}