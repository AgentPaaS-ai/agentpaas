package install

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

const credMapSentinel = "SENTINEL-B23-T03-NEVER-LEAK-xyzzy"

func testBrokeredPolicyYAML() []byte {
	return []byte(`version: "1.0"
agent:
  name: credmap-agent
egress:
  - domain: "api.example.com"
    ports: [443]
    credential: "api-token"
credentials:
  - id: "api-token"
    type: brokered
    header: Authorization
`)
}

func testDualEgressPolicyYAML() []byte {
	return []byte(`version: "1.0"
agent:
  name: route-agent
egress:
  - domain: "api-a.example.com"
    ports: [443]
    credential: "key-a"
  - domain: "api-b.example.com"
    ports: [443]
    credential: "key-b"
credentials:
  - id: "key-a"
    type: brokered
    header: Authorization
  - id: "key-b"
    type: brokered
    header: Authorization
`)
}

func parseTestPolicy(t *testing.T, raw []byte) *policy.Policy {
	t.Helper()
	p, err := policy.ParsePolicy(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	return p
}

func seedStoreWithSentinel(t *testing.T, names ...string) *secrets.FakeKeyStore {
	t.Helper()
	store := secrets.NewFakeKeyStore()
	ctx := context.Background()
	for _, n := range names {
		val := []byte("value-for-" + n)
		if n == "sentinel-local" {
			val = []byte(credMapSentinel)
		}
		if err := store.Set(ctx, n, val); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	return store
}

func TestResolveCredentialMapping_TTY_MapAll(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := seedStoreWithSentinel(t, "my-local")
	var events []auditEvent
	prompts := []string{"my-local"}
	res, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store, InstallRef: "credmap-agent@abcdef01",
		IsTTY: true,
		Prompt: func(string) (string, error) {
			if len(prompts) == 0 {
				t.Fatal("unexpected prompt")
			}
			out := prompts[0]
			prompts = prompts[1:]
			return out, nil
		},
		EmitAudit: auditCollector(&events),
	})
	if err != nil {
		t.Fatalf("ResolveCredentialMapping: %v", err)
	}
	if res.Map["api-token"] != "my-local" {
		t.Fatalf("map = %+v", res.Map)
	}
	found := false
	for _, e := range events {
		if e.EventType == audit.EventTypeInstallCredentialMapped {
			found = true
		}
	}
	if !found {
		t.Fatal("missing install_credential_mapped audit")
	}
}

func TestResolveCredentialMapping_TTY_DeferWarn(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := seedStoreWithSentinel(t, "my-local")
	var warns []string
	res, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store, InstallRef: "credmap-agent@abcdef01",
		IsTTY: true,
		Prompt: func(string) (string, error) { return "defer", nil },
		PrintWarn: func(msg string) { warns = append(warns, msg) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := res.Map["api-token"]; ok {
		t.Fatal("deferred credential should be omitted from map")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "agentpaas installed map-credential") {
		t.Fatalf("warn = %+v", warns)
	}
}

func TestResolveCredentialMapping_TTY_InvalidLocalRefused(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := seedStoreWithSentinel(t, "good-local")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	_, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store, InstallRef: "credmap-agent@abcdef01",
		IsTTY: true,
		Prompt: func(string) (string, error) { return "missing-name", nil },
	})
	if !errors.Is(err, ErrCredentialMapRefused) {
		t.Fatalf("want ErrCredentialMapRefused, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
	_ = state
}

func TestResolveCredentialMapping_NonTTY_Flags(t *testing.T) {
	pol := parseTestPolicy(t, testDualEgressPolicyYAML())
	store := seedStoreWithSentinel(t, "localA", "localB")
	res, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store,
		MapCredentials: []string{"key-a=localA", "key-b=localB"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Map["key-a"] != "localA" || res.Map["key-b"] != "localB" {
		t.Fatalf("map = %+v", res.Map)
	}
}

func TestResolveCredentialMapping_NonTTY_UndeclaredID(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := seedStoreWithSentinel(t, "my-local")
	_, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store,
		MapCredentials: []string{"not-in-policy=my-local"},
	})
	if !errors.Is(err, ErrCredentialMapInvalid) {
		t.Fatalf("want ErrCredentialMapInvalid, got %v", err)
	}
}

func TestResolveCredentialMapping_NonTTY_MissingLocal(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := secrets.NewFakeKeyStore()
	_, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store,
		MapCredentials: []string{"api-token=missing"},
	})
	if !errors.Is(err, ErrCredentialMapInvalid) {
		t.Fatalf("want ErrCredentialMapInvalid, got %v", err)
	}
}

func TestUnmappedWarning_Actionable(t *testing.T) {
	msg := unmappedWarning("api-token", "agent@a1b2c3d4")
	if !strings.Contains(msg, "agentpaas installed map-credential agent@a1b2c3d4 api-token=<local>") {
		t.Fatalf("warn = %q", msg)
	}
}

func TestApplyMapCredential_UpdatesManifest(t *testing.T) {
	polYAML := testBrokeredPolicyYAML()
	store := seedStoreWithSentinel(t, "receiver-local")
	state, _ := newConsentState(t)
	fp := strings.Repeat("a", 64)
	m := InstallManifest{
		PublisherFingerprint: fp,
		PublisherName:        "pub",
		AgentName:            "credmap-agent",
		AgentVersion:         "1.0.0",
		AcceptedPolicyDigest: "digest",
	}
	if err := state.SaveApprovedInstall(m, polYAML); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ref := FormatInstallRef(m.AgentName, fp)
	var events []auditEvent
	if err := ApplyMapCredential(MapCredentialOpts{
		State: state, Store: store, Ref: ref, Mapping: "api-token=receiver-local",
		EmitAudit: auditCollector(&events),
	}); err != nil {
		t.Fatalf("ApplyMapCredential: %v", err)
	}
	prior, err := state.GetInstallByRef(ref)
	if err != nil || prior == nil {
		t.Fatalf("reload: %v", err)
	}
	if prior.Manifest.CredentialMap["api-token"] != "receiver-local" {
		t.Fatalf("manifest map = %+v", prior.Manifest.CredentialMap)
	}
}

func TestCredentialMap_SentinelAbsentFromOutputs(t *testing.T) {
	pol := parseTestPolicy(t, testBrokeredPolicyYAML())
	store := seedStoreWithSentinel(t, "sentinel-local")
	var warns []string
	var events []auditEvent
	res, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy: pol, Store: store, InstallRef: "credmap-agent@aaaaaaaa",
		IsTTY: true,
		Prompt: func(string) (string, error) { return "sentinel-local", nil },
		PrintWarn: func(msg string) { warns = append(warns, msg) },
		EmitAudit: auditCollector(&events),
	})
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	m := InstallManifest{AgentName: "credmap-agent", PublisherFingerprint: strings.Repeat("a", 64)}
	MergeCredentialMapIntoManifest(&m, res.Map)
	raw, _ := json.Marshal(m)
	blobs := []string{strings.Join(res.DisplayLines, "\n"), strings.Join(warns, "\n"), string(raw)}
	for _, e := range events {
		for _, v := range e.Payload {
			blobs = append(blobs, v)
		}
	}
	for _, blob := range blobs {
		if strings.Contains(blob, credMapSentinel) {
			t.Fatalf("sentinel leaked in output: %q", blob)
		}
	}
}