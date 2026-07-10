package install

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

// testLLMOnlyPolicyYAML returns a policy with NO declared credentials —
// the agent relies entirely on the signed lock's agent_yaml.llm.credential.
// This mirrors the weather bundle's real layout.
func testLLMOnlyPolicyYAML() []byte {
	return []byte(`version: "1.0"
agent:
  name: weather-agent
egress:
  - domain: "wttr.in"
    ports: [443]
  - domain: "openrouter.ai"
    ports: [443]
credentials: []
`)
}

// testLLMOnlyLock returns a signed lock with an LLM credential but no
// declared policy credentials — mirroring the weather bundle.
func testLLMOnlyLock(agentName, credentialName string) *pack.AgentLock {
	return &pack.AgentLock{
		SchemaVersion: 2,
		AgentName:     agentName,
		AgentYAML: &pack.AgentYAML{
			Name:    agentName,
			Version: "1.0.0",
			LLM: pack.LLMConfig{
				Provider:   "openrouter",
				Model:      "anthropic/claude-sonnet-4",
				Credential: credentialName,
			},
		},
	}
}

// seedInstalledAgent writes the installed state layout (agent.lock,
// install-manifest.json, policy.yaml, source/) under
// state/agents/<name>@<pub8>/ — matching what MaterializeInstall produces.
func seedInstalledAgent(t *testing.T, stateRoot, agentName, fp, credentialName string) string {
	t.Helper()
	pub8 := fp[:8]
	ref := agentName + "@" + pub8
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write lock
	lock := testLLMOnlyLock(agentName, credentialName)
	lockBytes, err := pack.LockfileCanonicalJSON(lock)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.lock"), lockBytes, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	// Write policy
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), testLLMOnlyPolicyYAML(), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	// Write manifest
	manifest := InstallManifest{
		PublisherFingerprint: fp,
		AgentName:            agentName,
		AgentVersion:         "1.0.0",
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}
	manifestRaw, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "install-manifest.json"), manifestRaw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	// Write local_image.digest
	if err := os.WriteFile(filepath.Join(dir, "local_image.digest"), []byte(manifest.LocalImageDigest+"\n"), 0o600); err != nil {
		t.Fatalf("write digest: %v", err)
	}
	// Create source dir (VerifyInstalledAgent reads it)
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	// Write sbom
	if err := os.WriteFile(filepath.Join(dir, "sbom.spdx.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write sbom: %v", err)
	}
	// Write parent-bundle.ref
	if err := os.WriteFile(filepath.Join(dir, "parent-bundle.ref"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write parent: %v", err)
	}
	return ref
}

// TestT38_ApplyMapCredential_AcceptsSignedLLMCredential verifies that
// ApplyMapCredential accepts a mapping for the signed LLM credential even
// when the policy has credentials: [].
func TestT38_ApplyMapCredential_AcceptsSignedLLMCredential(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	fp := strings.Repeat("f", 64)
	agentName := "weather-agent"
	credentialName := "openrouter-key"
	ref := seedInstalledAgent(t, stateRoot, agentName, fp, credentialName)

	store := secrets.NewFakeKeyStore()
	ctx := context.Background()
	if err := store.Set(ctx, "my-local-openrouter-key", []byte("secret-value-not-leaked")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	state := &FileInstallState{StateRoot: stateRoot}
	policyYAML := testLLMOnlyPolicyYAML()
	manifest := InstallManifest{
		PublisherFingerprint: fp,
		AgentName:            agentName,
		AgentVersion:         "1.0.0",
	}
	if err := state.SaveApprovedInstall(manifest, policyYAML); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	if err := ApplyMapCredential(MapCredentialOpts{
		State:     state,
		Store:     store,
		Ref:       ref,
		Mapping:   credentialName + "=my-local-openrouter-key",
		StateRoot: stateRoot,
	}); err != nil {
		t.Fatalf("ApplyMapCredential for LLM credential: %v", err)
	}

	// Verify manifest has the mapping
	prior, err := state.GetInstallByRef(ref)
	if err != nil || prior == nil {
		t.Fatalf("reload: %v", err)
	}
	if prior.Manifest.CredentialMap[credentialName] != "my-local-openrouter-key" {
		t.Fatalf("manifest map = %+v", prior.Manifest.CredentialMap)
	}
}

// TestT38_ApplyMapCredential_RejectsUndeclaredID verifies that an
// undeclared credential ID is still rejected (fail-closed).
func TestT38_ApplyMapCredential_RejectsUndeclaredID(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	fp := strings.Repeat("a", 64)
	agentName := "weather-agent"
	ref := seedInstalledAgent(t, stateRoot, agentName, fp, "openrouter-key")

	store := secrets.NewFakeKeyStore()
	ctx := context.Background()
	_ = store.Set(ctx, "my-local", []byte("val"))

	state := &FileInstallState{StateRoot: stateRoot}
	manifest := InstallManifest{
		PublisherFingerprint: fp,
		AgentName:            agentName,
		AgentVersion:         "1.0.0",
	}
	if err := state.SaveApprovedInstall(manifest, testLLMOnlyPolicyYAML()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := ApplyMapCredential(MapCredentialOpts{
		State:     state,
		Store:     store,
		Ref:       ref,
		Mapping:   "undeclared-id=my-local",
		StateRoot: stateRoot,
	})
	if !errors.Is(err, ErrCredentialMapInvalid) {
		t.Fatalf("want ErrCredentialMapInvalid, got %v", err)
	}
}

// TestT38_ApplyMapCredential_MissingLocalSecret verifies that mapping to
// a non-existent local secret fails.
func TestT38_ApplyMapCredential_MissingLocalSecret(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	fp := strings.Repeat("b", 64)
	agentName := "weather-agent"
	ref := seedInstalledAgent(t, stateRoot, agentName, fp, "openrouter-key")

	store := secrets.NewFakeKeyStore() // empty store

	state := &FileInstallState{StateRoot: stateRoot}
	manifest := InstallManifest{
		PublisherFingerprint: fp,
		AgentName:            agentName,
		AgentVersion:         "1.0.0",
	}
	if err := state.SaveApprovedInstall(manifest, testLLMOnlyPolicyYAML()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := ApplyMapCredential(MapCredentialOpts{
		State:     state,
		Store:     store,
		Ref:       ref,
		Mapping:   "openrouter-key=nonexistent-local",
		StateRoot: stateRoot,
	})
	if !errors.Is(err, ErrCredentialMapInvalid) {
		t.Fatalf("want ErrCredentialMapInvalid, got %v", err)
	}
}

// TestT38_ApplyMapCredential_NoRawSecretInOutput verifies that the raw
// secret value never appears in the manifest or audit output.
func TestT38_ApplyMapCredential_NoRawSecretInOutput(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	fp := strings.Repeat("c", 64)
	agentName := "weather-agent"
	ref := seedInstalledAgent(t, stateRoot, agentName, fp, "openrouter-key")

	const sentinel = "RAW-SECRET-NEVER-LEAK-T38-xyzzy"
	store := secrets.NewFakeKeyStore()
	ctx := context.Background()
	if err := store.Set(ctx, "my-local", []byte(sentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	state := &FileInstallState{StateRoot: stateRoot}
	manifest := InstallManifest{
		PublisherFingerprint: fp,
		AgentName:            agentName,
		AgentVersion:         "1.0.0",
	}
	if err := state.SaveApprovedInstall(manifest, testLLMOnlyPolicyYAML()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var auditPayloads []string
	if err := ApplyMapCredential(MapCredentialOpts{
		State:     state,
		Store:     store,
		Ref:       ref,
		Mapping:   "openrouter-key=my-local",
		StateRoot: stateRoot,
		EmitAudit: func(eventType string, payload map[string]string) {
			for _, v := range payload {
				auditPayloads = append(auditPayloads, v)
			}
		},
	}); err != nil {
		t.Fatalf("ApplyMapCredential: %v", err)
	}

	// Check manifest JSON
	prior, _ := state.GetInstallByRef(ref)
	raw, _ := json.Marshal(prior.Manifest)
	allBlobs := append([]string{string(raw)}, auditPayloads...)
	for _, blob := range allBlobs {
		if strings.Contains(blob, sentinel) {
			t.Fatalf("raw secret leaked in output: %q", blob)
		}
	}
}

// TestT38_ResolveCredentialMapping_IncludesLLMCredential verifies that
// the install-time mapping flow also recognizes the signed LLM credential.
func TestT38_ResolveCredentialMapping_IncludesLLMCredential(t *testing.T) {
	pol := parseTestPolicy(t, testLLMOnlyPolicyYAML())
	store := seedStoreWithSentinel(t, "my-local-openrouter")
	lock := testLLMOnlyLock("weather-agent", "openrouter-key")

	res, err := ResolveCredentialMapping(CredentialMapOpts{
		Policy:        pol,
		Store:         store,
		InstallRef:    "weather-agent@abcdef01",
		Lock:          lock,
		MapCredentials: []string{"openrouter-key=my-local-openrouter"},
	})
	if err != nil {
		t.Fatalf("ResolveCredentialMapping: %v", err)
	}
	if res.Map["openrouter-key"] != "my-local-openrouter" {
		t.Fatalf("map = %+v", res.Map)
	}
}

// TestT38_SignedLLMCredentialID verifies the helper reads the LLM credential
// from the installed lock.
func TestT38_SignedLLMCredentialID(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	fp := strings.Repeat("d", 64)
	ref := seedInstalledAgent(t, stateRoot, "weather-agent", fp, "openrouter-key")

	id, err := SignedLLMCredentialID(stateRoot, ref)
	if err != nil {
		t.Fatalf("SignedLLMCredentialID: %v", err)
	}
	if id != "openrouter-key" {
		t.Fatalf("id = %q, want openrouter-key", id)
	}
}

// TestT38_SignedLLMCredentialID_NoLock verifies empty string when no lock.
func TestT38_SignedLLMCredentialID_NoLock(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	id, err := SignedLLMCredentialID(stateRoot, "nonexistent@12345678")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Fatalf("id = %q, want empty", id)
	}
}
