package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/registry"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/spf13/cobra"
)

func setupRegistryTestState(t *testing.T) (stateRoot string) {
	t.Helper()
	stateRoot = t.TempDir()

	// Write one promoted and one not-promoted installed agent.
	writeTestInstalled(t, stateRoot, "weather", "a1b2c3d4", "1.0.0", "weather-pub", true)
	writeTestInstalled(t, stateRoot, "calculator", "f1e2d3c4", "2.0.0", "calc-pub", false)
	return stateRoot
}

func writeTestInstalled(t *testing.T, stateRoot, name, pub8, version, publisher string, promoted bool) {
	t.Helper()

	ref := name + "@" + pub8
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := install.InstallManifest{
		PublisherFingerprint: strings.Repeat(pub8, 8)[:64],
		PublisherName:        publisher,
		AgentName:            name,
		AgentVersion:         version,
		AcceptedPolicyDigest: "sha256:" + strings.Repeat("aa", 32),
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:" + strings.Repeat("bb", 32),
		InstalledAt:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Promoted:             promoted,
	}
	if promoted {
		pt := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
		m.PromotedAt = &pt
		m.PromotedBy = "admin"
	}

	raw, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600)

	lock := &pack.AgentLock{
		SchemaVersion: pack.LockSchemaVersion,
		AgentName:     name,
		AgentVersion:  version,
		ImageDigest:   "sha256:" + strings.Repeat("cc", 32),
		PolicyDigest:  "sha256:" + strings.Repeat("dd", 32),
		Capabilities: []pack.DeclaredCapability{
			{ID: "compute", Description: "Performs calculations"},
		},
	}
	lockRaw, _ := json.Marshal(lock)
	_ = os.WriteFile(filepath.Join(dir, "agent.lock"), lockRaw, 0o600)
}

func TestRegistryList_HumanReadable(t *testing.T) {
	stateRoot := setupRegistryTestState(t)

	origList := registryListFactory
	defer func() { registryListFactory = origList }()

	registryListFactory = func(cmd *cobra.Command) ([]registry.RegistryEntry, error) {
		return registry.ListEntries(stateRoot, nil)
	}

	cmd := newRegistryListCmd()

	// Redirect stdout since tabwriter writes directly to os.Stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry list failed: %v", err)
	}
	_ = w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	// Check that both entries appear in deterministic order (calculator before weather by name)
	calcIdx := strings.Index(out, "calculator")
	weatherIdx := strings.Index(out, "weather")
	if calcIdx < 0 {
		t.Errorf("output missing calculator@f1e2c3d4: %s", out)
	}
	if weatherIdx < 0 {
		t.Errorf("output missing weather@a1b2c3d4: %s", out)
	}
	if calcIdx > weatherIdx {
		t.Errorf("expected calculator before weather (sorted by name), got weather first")
	}
}

func TestRegistryList_JSON(t *testing.T) {
	stateRoot := setupRegistryTestState(t)

	entries, err := registry.ListEntries(stateRoot, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}

	out, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("JSON marshal: %v", err)
	}

	var parsed []registry.RegistryEntry
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("JSON parse: %v\noutput: %s", err, out)
	}

	if len(parsed) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(parsed))
	}

	// Verify promotion field
	var promotedEntry *registry.RegistryEntry
	for i := range parsed {
		if parsed[i].Name == "weather" {
			promotedEntry = &parsed[i]
		}
	}
	if promotedEntry == nil {
		t.Fatal("weather entry not found")
	}
	if !promotedEntry.Promoted {
		t.Error("weather should be promoted")
	}

	// Verify JSON schema golden: key field names
	firstJSON := string(out)
	goldenKeys := []string{
		`"ref"`, `"name"`, `"pub8"`, `"version"`, `"publisher_name"`,
		`"publisher_fingerprint"`, `"package_digest"`, `"policy_digest"`,
		`"install_mode"`, `"local_image_digest"`, `"installed_at"`,
		`"promoted"`,
	}
	for _, k := range goldenKeys {
		if !strings.Contains(firstJSON, k) {
			t.Errorf("JSON output missing key %s", k)
		}
	}
}

func TestRegistryShow_JSON(t *testing.T) {
	stateRoot := setupRegistryTestState(t)

	entry, err := registry.ShowEntry(stateRoot, "weather@a1b2c3d4", nil)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}

	out, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("JSON marshal: %v", err)
	}

	var parsed registry.RegistryEntry
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("JSON parse: %v\noutput: %s", err, out)
	}

	// JSON schema golden assertions
	if parsed.Ref != "weather@a1b2c3d4" {
		t.Errorf("Ref = %q", parsed.Ref)
	}
	if parsed.Version != "1.0.0" {
		t.Errorf("Version = %q", parsed.Version)
	}
	if !parsed.Promoted {
		t.Error("Promoted should be true")
	}
	if len(parsed.Capabilities) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(parsed.Capabilities))
	}
	if parsed.Capabilities[0].ID != "compute" {
		t.Errorf("Capability[0].ID = %q, want compute", parsed.Capabilities[0].ID)
	}
}

func TestRegistryShow_HumanReadable(t *testing.T) {
	stateRoot := setupRegistryTestState(t)

	origShow := registryShowFactory
	defer func() { registryShowFactory = origShow }()

	registryShowFactory = func(cmd *cobra.Command, ref string) (*registry.RegistryEntry, error) {
		return registry.ShowEntry(stateRoot, ref, nil)
	}

	cmd := newRegistryShowCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"weather@a1b2c3d4"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry show failed: %v", err)
	}
	out := buf.String()

	// Check key fields are present
	checks := []string{
		"weather@a1b2c3d4",
		"1.0.0",
		"weather-pub",
		"Promoted:    true",
		"compute",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("output missing %q:\n%s", c, out)
		}
	}
}

func TestRegistryShow_Ambiguous(t *testing.T) {
	stateRoot := t.TempDir()
	writeTestInstalled(t, stateRoot, "same-name", "aaaaaaaa", "1.0.0", "pub-a", false)
	writeTestInstalled(t, stateRoot, "same-name", "bbbbbbbb", "2.0.0", "pub-b", false)

	origShow := registryShowFactory
	defer func() { registryShowFactory = origShow }()

	registryShowFactory = func(cmd *cobra.Command, ref string) (*registry.RegistryEntry, error) {
		return registry.ShowEntry(stateRoot, ref, nil)
	}

	cmd := newRegistryShowCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"same-name"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for ambiguous name, got nil")
	}
}

func TestRegistryShow_NotFound(t *testing.T) {
	stateRoot := t.TempDir()

	origShow := registryShowFactory
	defer func() { registryShowFactory = origShow }()

	registryShowFactory = func(cmd *cobra.Command, ref string) (*registry.RegistryEntry, error) {
		return registry.ShowEntry(stateRoot, ref, nil)
	}

	cmd := newRegistryShowCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"nonexistent@deadbeef"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for not-found ref, got nil")
	}
}

func TestRegistryList_Empty(t *testing.T) {
	stateRoot := t.TempDir()

	origList := registryListFactory
	defer func() { registryListFactory = origList }()

	registryListFactory = func(cmd *cobra.Command) ([]registry.RegistryEntry, error) {
		return registry.ListEntries(stateRoot, nil)
	}

	cmd := newRegistryListCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry list empty failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No installed packages") {
		t.Errorf("expected 'No installed packages', got: %s", out)
	}
}

func TestRegistryPromote_SetsPromotedFlag(t *testing.T) {
	stateRoot := setupRegistryTestState(t)

	// Override the promote factory.
	origPromote := registryPromoteFactory
	defer func() { registryPromoteFactory = origPromote }()

	registryPromoteFactory = func(cmd *cobra.Command, stateRootDir, ref, actor string) error {
		_ = stateRootDir // command's state root; use test stateRoot instead
		return registry.Promote(stateRoot, ref, actor)
	}

	cmd := newRegistryPromoteCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"weather@a1b2c3d4"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry promote failed: %v", err)
	}

	// Verify the manifest is now promoted.
	manifestPath := filepath.Join(stateRoot, "agents", "weather@a1b2c3d4", "install-manifest.json")
	raw, _ := os.ReadFile(manifestPath)
	var m install.InstallManifest
	_ = json.Unmarshal(raw, &m)
	if !m.Promoted {
		t.Error("expected promoted=true after CLI promote")
	}
}

func TestRegistryDemote_ClearsPromotedFlag(t *testing.T) {
	stateRoot := setupRegistryTestState(t)

	// First, promote via API to have something to demote.
	if err := registry.Promote(stateRoot, "weather@a1b2c3d4", "admin"); err != nil {
		t.Fatalf("Promote setup: %v", err)
	}

	// Override the demote factory.
	origDemote := registryDemoteFactory
	defer func() { registryDemoteFactory = origDemote }()

	registryDemoteFactory = func(cmd *cobra.Command, stateRootDir, ref string) error {
		_ = stateRootDir // command's state root; use test stateRoot instead
		return registry.Demote(stateRoot, ref)
	}

	cmd := newRegistryDemoteCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"weather@a1b2c3d4"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry demote failed: %v", err)
	}

	// Verify the manifest is now not promoted.
	manifestPath := filepath.Join(stateRoot, "agents", "weather@a1b2c3d4", "install-manifest.json")
	raw, _ := os.ReadFile(manifestPath)
	var m install.InstallManifest
	_ = json.Unmarshal(raw, &m)
	if m.Promoted {
		t.Error("expected promoted=false after CLI demote")
	}
}

func TestRegistryPromote_UnknownRef(t *testing.T) {
	stateRoot := t.TempDir()

	origPromote := registryPromoteFactory
	defer func() { registryPromoteFactory = origPromote }()

	registryPromoteFactory = func(cmd *cobra.Command, stateRootDir, ref, actor string) error {
		_ = stateRootDir // command's state root; use test stateRoot instead
		return registry.Promote(stateRoot, ref, actor)
	}

	cmd := newRegistryPromoteCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"nobody@deadbeef"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for unknown ref, got nil")
	}
}

func TestRegistryDemote_Idempotent(t *testing.T) {
	stateRoot := setupRegistryTestState(t)

	// Demote a package that isn't promoted (idempotent).
	origDemote := registryDemoteFactory
	defer func() { registryDemoteFactory = origDemote }()

	registryDemoteFactory = func(cmd *cobra.Command, stateRootDir, ref string) error {
		_ = stateRootDir // command's state root; use test stateRoot instead
		return registry.Demote(stateRoot, ref)
	}

	cmd := newRegistryDemoteCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"calculator@f1e2d3c4"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry demote idempotent failed: %v", err)
	}
}

func TestRegistryList_WithDeploymentStore(t *testing.T) {
	stateRoot := setupRegistryTestState(t)

	// Open a LocalStore and create a deployment so the registry join
	// populates deployment status/ID for the test agent.
	storeRoot := filepath.Join(stateRoot, "routed")
	store, err := routedrun.OpenLocalStore(storeRoot)
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}

	ctx := context.Background()
	// Create a deployment matching the installed "weather" agent.
	dep := &routedrun.DeploymentRecord{
		PackageName:    "weather",
		PackageVersion: "1.0.0",
		BundleDigest:   "sha256:bundle1",
		PolicyDigest:   "sha256:" + strings.Repeat("aa", 32),
		CreatedBy:      "tester",
	}
	if err := store.CreateDeployment(ctx, dep); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	// Call ListEntries with the store to verify deployment join.
	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}

	// Find the weather entry and verify deployment fields are populated.
	var weatherEntry *registry.RegistryEntry
	for i := range entries {
		if entries[i].Name == "weather" {
			weatherEntry = &entries[i]
			break
		}
	}
	if weatherEntry == nil {
		t.Fatal("weather entry not found in registry list")
	}
	if weatherEntry.DeploymentID == nil {
		t.Error("expected DeploymentID to be populated from store join, got nil")
	}
	if weatherEntry.DeploymentStatus != "ACTIVE" {
		t.Errorf("DeploymentStatus = %q, want ACTIVE", weatherEntry.DeploymentStatus)
	}
	if weatherEntry.Generation != 1 {
		t.Errorf("Generation = %d, want 1", weatherEntry.Generation)
	}
	if weatherEntry.BundleDigest != "sha256:bundle1" {
		t.Errorf("BundleDigest = %q, want sha256:bundle1", weatherEntry.BundleDigest)
	}
}
