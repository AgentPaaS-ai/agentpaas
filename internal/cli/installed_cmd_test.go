package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestInstalledListCmd(t *testing.T) {
	installedListFactory = func(cmd *cobra.Command) ([]install.InstalledAgentEntry, error) {
		return []install.InstalledAgentEntry{{
			Ref:         "weather@a1b2c3d4",
			Alias:       "wx",
			Version:     "1.0.0",
			Publisher:   "Acme",
			InstalledAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			Mode:        "local-rebuild",
		}}, nil
	}
	t.Cleanup(func() { installedListFactory = defaultListInstalled })

	out, _, err := executeCmd("installed", "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "weather@a1b2c3d4") || !strings.Contains(out, "local-rebuild") {
		t.Fatalf("out = %q", out)
	}
}

func TestInstalledRemoveCmd(t *testing.T) {
	var removed string
	installedRemoveFactory = func(cmd *cobra.Command, ref string) error {
		removed = ref
		return nil
	}
	t.Cleanup(func() { installedRemoveFactory = defaultRemoveInstalled })

	out, _, err := executeCmd("installed", "remove", "agent@deadbeef")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if removed != "agent@deadbeef" {
		t.Fatalf("removed = %q", removed)
	}
	if !strings.Contains(out, "removed") {
		t.Fatalf("out = %q", out)
	}
}