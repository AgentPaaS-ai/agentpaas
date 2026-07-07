package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"github.com/spf13/cobra"
)

func TestInstalledMapCredentialCmd(t *testing.T) {
	polYAML := []byte(`version: "1.0"
agent:
  name: cli-agent
egress:
  - domain: "api.example.com"
    ports: [443]
    credential: "api-token"
credentials:
  - id: "api-token"
    type: brokered
    header: Authorization
`)
	store := secrets.NewFakeKeyStore()
	stateRoot := filepath.Join(t.TempDir(), "state")
	state := &install.FileInstallState{StateRoot: stateRoot}
	fp := strings.Repeat("b", 64)
	m := install.InstallManifest{
		PublisherFingerprint: fp,
		PublisherName:        "pub",
		AgentName:            "cli-agent",
		AgentVersion:         "0.1.0",
		AcceptedPolicyDigest: "d",
	}
	if err := state.SaveApprovedInstall(m, polYAML); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ref := install.FormatInstallRef("cli-agent", fp)

	installStateFactory = func(cmd *cobra.Command) (install.InstallStateStore, error) {
		return state, nil
	}
	secretStoreFactory = func(cmd *cobra.Command) (secrets.SecretStore, error) {
		return store, nil
	}
	t.Cleanup(func() {
		installStateFactory = newDefaultInstallState
		secretStoreFactory = newDefaultSecretStore
	})

	if err := store.Set(t.Context(), "cli-local", []byte("secret-value-not-in-output")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	out, _, err := executeCmd("installed", "map-credential", ref, "api-token=cli-local")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Credential mapping saved") {
		t.Fatalf("out = %q", out)
	}
	prior, err := state.GetInstallByRef(ref)
	if err != nil || prior == nil {
		t.Fatalf("reload: %v", err)
	}
	if prior.Manifest.CredentialMap["api-token"] != "cli-local" {
		t.Fatalf("map = %+v", prior.Manifest.CredentialMap)
	}
	if strings.Contains(out, "secret-value-not-in-output") {
		t.Fatal("CLI output leaked secret value")
	}
}