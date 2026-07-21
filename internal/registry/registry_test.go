package registry_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

// testDeploymentStore is an in-memory store for registry tests.
type testDeploymentStore struct {
	deployments map[routedrun.DeploymentID]*routedrun.DeploymentRecord
	aliases     map[string]*routedrun.AliasRecord
}

func newTestDeploymentStore() *testDeploymentStore {
	return &testDeploymentStore{
		deployments: make(map[routedrun.DeploymentID]*routedrun.DeploymentRecord),
		aliases:     make(map[string]*routedrun.AliasRecord),
	}
}

func (s *testDeploymentStore) ListDeployments(ctx context.Context) ([]*routedrun.DeploymentRecord, error) {
	var out []*routedrun.DeploymentRecord
	for _, d := range s.deployments {
		out = append(out, d)
	}
	return out, nil
}

func (s *testDeploymentStore) GetDeployment(ctx context.Context, id routedrun.DeploymentID) (*routedrun.DeploymentRecord, error) {
	return s.deployments[id], nil
}

func (s *testDeploymentStore) ListAliases(ctx context.Context) ([]*routedrun.AliasRecord, error) {
	var out []*routedrun.AliasRecord
	for _, a := range s.aliases {
		out = append(out, a)
	}
	return out, nil
}

// Stubs for methods we don't test.
func (s *testDeploymentStore) CreateDeployment(ctx context.Context, dep *routedrun.DeploymentRecord) error {
	return nil
}
func (s *testDeploymentStore) SetDeploymentStatus(ctx context.Context, id routedrun.DeploymentID, status routedrun.DeploymentStatus, gen int64) error {
	return nil
}
func (s *testDeploymentStore) CompareAndSwapAlias(ctx context.Context, a *routedrun.AliasRecord) error {
	return nil
}
func (s *testDeploymentStore) ResolveAlias(ctx context.Context, alias string) (*routedrun.AliasRecord, error) {
	return s.aliases[alias], nil
}
func (s *testDeploymentStore) AdmitInvocation(ctx context.Context, req *routedrun.InvocationRequest, gen int64) (*routedrun.InvocationReceipt, error) {
	return nil, nil
}
func (s *testDeploymentStore) GetInvocationByIdempotency(ctx context.Context, caller, key string) (*routedrun.InvocationReceipt, error) {
	return nil, nil
}
func (s *testDeploymentStore) ListInvocations(ctx context.Context) ([]*routedrun.InvocationReceipt, error) {
	return nil, nil
}

// writeInstalledAgent creates a minimal installed agent on disk for testing.
func writeInstalledAgent(t *testing.T, stateRoot, agentName, pub8, version, publisherName string, promoted bool) *install.InstallManifest {
	t.Helper()

	ref := agentName + "@" + pub8
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := install.InstallManifest{
		PublisherFingerprint: strings.Repeat(pub8, 8)[:64],
		PublisherName:        publisherName,
		AgentName:            agentName,
		AgentVersion:         version,
		AcceptedPolicyDigest: "sha256:" + hex.EncodeToString(make([]byte, 32)),
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:" + strings.Repeat("a", 64),
		InstalledAt:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Alias:                "",
		Promoted:             promoted,
	}
	if promoted {
		pt := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
		m.PromotedAt = &pt
		m.PromotedBy = "test-actor"
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Write a minimal agent.lock with capabilities
	lock := &pack.AgentLock{
		SchemaVersion:    pack.LockSchemaVersion,
		AgentName:        agentName,
		AgentVersion:     version,
		Publisher:        &pack.PublisherInfo{Name: publisherName, Fingerprint: strings.Repeat(pub8, 8)[:64]},
		ImageDigest:      "sha256:" + strings.Repeat("b", 64),
		PolicyDigest:     "sha256:" + strings.Repeat("c", 64),
		BuildInputDigest: "sha256:" + strings.Repeat("d", 64),
		Capabilities: []pack.DeclaredCapability{
			{ID: "text_generation", Description: "Generates text from prompts"},
			{ID: "tool_calling", Description: "Calls external tools"},
		},
	}
	lockRaw, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.lock"), lockRaw, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	return &m
}

func TestListEntries_EmptyRegistry(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestListEntries_InstalledNotPromoted(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	m := writeInstalledAgent(t, stateRoot, "weather", "a1b2c3d4", "1.0.0", "weather-pub", false)

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Name != "weather" {
		t.Errorf("Name = %q, want weather", e.Name)
	}
	if e.Pub8 != "a1b2c3d4" {
		t.Errorf("Pub8 = %q, want a1b2c3d4", e.Pub8)
	}
	if e.Promoted {
		t.Errorf("Promoted = true, want false")
	}
	if e.PromotedAt != nil {
		t.Errorf("PromotedAt = %v, want nil", e.PromotedAt)
	}
	_ = m // used for digest checks later
}

func TestListEntries_InstalledPromoted(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	writeInstalledAgent(t, stateRoot, "worker", "f1e2d3c4", "2.0.0", "worker-pub", true)

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if !e.Promoted {
		t.Errorf("Promoted = false, want true")
	}
	if e.PromotedAt == nil {
		t.Errorf("PromotedAt = nil, want non-nil")
	} else if !e.PromotedAt.Equal(time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("PromotedAt = %v", e.PromotedAt)
	}
	if e.PromotedBy != "test-actor" {
		t.Errorf("PromotedBy = %q, want test-actor", e.PromotedBy)
	}
}

func TestListEntries_Migration_PreRegistryStateReadsAsNotPromoted(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	ref := "legacy-agent@deadbeef"
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write an old manifest without promoted fields
	oldManifest := map[string]interface{}{
		"publisher_fingerprint": strings.Repeat("deadbeef", 8)[:64],
		"publisher_name":        "legacy-pub",
		"agent_name":            "legacy-agent",
		"agent_version":         "0.5.0",
		"accepted_policy_digest": "sha256:" + strings.Repeat("e", 64),
		"install_mode":          "prebuilt-image",
		"local_image_digest":    "sha256:" + strings.Repeat("f", 64),
		"installed_at":          "2024-01-01T00:00:00Z",
	}
	raw, err := json.MarshalIndent(oldManifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Also need an agent.lock
	lock := &pack.AgentLock{
		SchemaVersion: pack.LockSchemaVersion,
		AgentName:     "legacy-agent",
		AgentVersion:  "0.5.0",
		ImageDigest:   "sha256:" + strings.Repeat("g", 64),
	}
	lockRaw, _ := json.Marshal(lock)
	_ = os.WriteFile(filepath.Join(dir, "agent.lock"), lockRaw, 0o600)

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Promoted {
		t.Errorf("pre-registry state should read as not promoted, got promoted=true")
	}
}

func TestListEntries_DeterministicOrder(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	// Create agents in reverse order to verify sorting
	writeInstalledAgent(t, stateRoot, "zulu", "aaaaaaaa", "1.0.0", "pub-z", false)
	writeInstalledAgent(t, stateRoot, "alpha", "bbbbbbbb", "2.0.0", "pub-a", false)
	writeInstalledAgent(t, stateRoot, "alpha", "cccccccc", "1.0.0", "pub-a2", false)

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Expected order: name asc, then version asc
	// alpha@cccccccc (v1.0.0), alpha@bbbbbbbb (v2.0.0), zulu@aaaaaaaa (v1.0.0)
	if entries[0].Name != "alpha" || entries[0].Pub8 != "cccccccc" || entries[0].Version != "1.0.0" {
		t.Errorf("entry 0: %s@%s v%s", entries[0].Name, entries[0].Pub8, entries[0].Version)
	}
	if entries[1].Name != "alpha" || entries[1].Pub8 != "bbbbbbbb" || entries[1].Version != "2.0.0" {
		t.Errorf("entry 1: %s@%s v%s", entries[1].Name, entries[1].Pub8, entries[1].Version)
	}
	if entries[2].Name != "zulu" || entries[2].Pub8 != "aaaaaaaa" || entries[2].Version != "1.0.0" {
		t.Errorf("entry 2: %s@%s v%s", entries[2].Name, entries[2].Pub8, entries[2].Version)
	}
}

func TestListEntries_CredentialIDsNotValues(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	m := install.InstallManifest{
		PublisherFingerprint: strings.Repeat("a", 64),
		PublisherName:        "test-pub",
		AgentName:            "secrets-agent",
		AgentVersion:         "1.0.0",
		AcceptedPolicyDigest: "sha256:" + strings.Repeat("a", 64),
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:" + strings.Repeat("b", 64),
		InstalledAt:          time.Now().UTC(),
		CredentialMap: map[string]string{
			"OPENAI_API_KEY":      "openai-prod-key",
			"ANTHROPIC_API_KEY":   "anthropic-prod-key",
		},
	}

	ref := "secrets-agent@" + strings.Repeat("a", 64)[:8]
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600)
	lock := &pack.AgentLock{
		SchemaVersion: pack.LockSchemaVersion,
		AgentName:     "secrets-agent",
		AgentVersion:  "1.0.0",
		ImageDigest:   "sha256:" + strings.Repeat("c", 64),
	}
	lockRaw, _ := json.Marshal(lock)
	_ = os.WriteFile(filepath.Join(dir, "agent.lock"), lockRaw, 0o600)

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// Verify credential IDs are present but secret store values are NOT.
	e := entries[0]
	if len(e.CredentialIDs) != 2 {
		t.Fatalf("expected 2 credential IDs, got %d: %v", len(e.CredentialIDs), e.CredentialIDs)
	}
	hasOpenAI := false
	hasAnthropic := false
	for _, id := range e.CredentialIDs {
		if id == "OPENAI_API_KEY" {
			hasOpenAI = true
		}
		if id == "ANTHROPIC_API_KEY" {
			hasAnthropic = true
		}
		// Credential IDs must not contain the secret store value
		if strings.Contains(id, "prod-key") {
			t.Errorf("credential ID leaked secret value: %q", id)
		}
	}
	if !hasOpenAI || !hasAnthropic {
		t.Errorf("missing expected credential IDs: openai=%v anthropic=%v", hasOpenAI, hasAnthropic)
	}
}

func TestShowEntry_ByName(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	writeInstalledAgent(t, stateRoot, "weather", "a1b2c3d4", "1.0.0", "weather-pub", true)

	entry, err := registry.ShowEntry(stateRoot, "weather@a1b2c3d4", store)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Name != "weather" {
		t.Errorf("Name = %q, want weather", entry.Name)
	}
	if len(entry.Capabilities) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(entry.Capabilities))
	}
	if entry.Capabilities[0].ID != "text_generation" {
		t.Errorf("cap[0] = %q, want text_generation", entry.Capabilities[0].ID)
	}
}

func TestShowEntry_ByAlias(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	m := writeInstalledAgent(t, stateRoot, "helper", "c1d2e3f4", "3.0.0", "helper-pub", false)

	// Set alias in the manifest
	m.Alias = "myhelper"
	raw, _ := json.MarshalIndent(m, "", "  ")
	dir := filepath.Join(stateRoot, "agents", "helper@c1d2e3f4")
	_ = os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600)

	entry, err := registry.ShowEntry(stateRoot, "myhelper", store)
	if err != nil {
		t.Fatalf("ShowEntry by alias: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Name != "helper" {
		t.Errorf("Name = %q, want helper", entry.Name)
	}
	if entry.Alias != "myhelper" {
		t.Errorf("Alias = %q, want myhelper", entry.Alias)
	}
}

func TestShowEntry_NotFound(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	_, err := registry.ShowEntry(stateRoot, "nobody@deadbeef", store)
	if err == nil {
		t.Error("expected error for unknown agent, got nil")
	}
}

func TestShowEntry_AmbiguousNameListsCandidates(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	writeInstalledAgent(t, stateRoot, "same-name", "aaaaaaaa", "1.0.0", "pub-a", false)
	writeInstalledAgent(t, stateRoot, "same-name", "bbbbbbbb", "2.0.0", "pub-b", false)

	_, err := registry.ShowEntry(stateRoot, "same-name", store)
	if err == nil {
		t.Error("expected error for ambiguous bare name, got nil")
	}
	// The error message should mention candidates
	if err != nil && !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error should mention ambiguity: %v", err)
	}
}

func TestListEntries_DigestsMatchLock(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	m := writeInstalledAgent(t, stateRoot, "digest-test", "a1a1a1a1", "1.0.0", "pub-d", false)

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]

	// Verify the package digest matches what we wrote in the lock
	if e.PackageDigest != "sha256:"+strings.Repeat("b", 64) {
		t.Errorf("PackageDigest = %q, want sha256:%s", e.PackageDigest, strings.Repeat("b", 64))
	}
	if e.PolicyDigest != "sha256:"+strings.Repeat("c", 64) {
		t.Errorf("PolicyDigest = %q, want sha256:%s", e.PolicyDigest, strings.Repeat("c", 64))
	}

	// Verify install digest matches manifest
	if e.LocalImageDigest != m.LocalImageDigest {
		t.Errorf("LocalImageDigest = %q, want %q", e.LocalImageDigest, m.LocalImageDigest)
	}
}

func TestListEntries_DeploymentJoined(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	writeInstalledAgent(t, stateRoot, "deployed", "d1d1d1d1", "1.0.0", "dep-pub", true)

	// Create a matching deployment
	dep := &routedrun.DeploymentRecord{
		DeploymentID:    "dep_test001",
		PackageName:     "deployed",
		PackageVersion:  "1.0.0",
		Generation:      3,
		Status:          routedrun.DeploymentActive,
		BundleDigest:    "sha256:" + strings.Repeat("b1", 32),
		PolicyDigest:    "sha256:" + strings.Repeat("c1", 32),
		ImageLockDigest: "sha256:" + strings.Repeat("d1", 32),
	}
	store.deployments["dep_test001"] = dep

	// Create an alias
	alias := &routedrun.AliasRecord{
		Alias:              "prod",
		TargetDeploymentID: "dep_test001",
	}
	store.aliases["prod"] = alias

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.DeploymentID == nil || *e.DeploymentID != "dep_test001" {
		t.Errorf("DeploymentID = %v, want dep_test001", e.DeploymentID)
	}
	foundProd := false
	for _, a := range e.Aliases {
		if a == "prod" {
			foundProd = true
		}
	}
	if !foundProd {
		t.Errorf("expected alias 'prod' in entry, got %v", e.Aliases)
	}
}

func TestShowEntry_DeploymentDigests(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	writeInstalledAgent(t, stateRoot, "digdep", "e1e1e1e1", "1.0.0", "dd-pub", false)

	dep := &routedrun.DeploymentRecord{
		DeploymentID:   "dep_dig001",
		PackageName:    "digdep",
		PackageVersion: "1.0.0",
		Generation:     1,
		Status:         routedrun.DeploymentActive,
		BundleDigest:   "sha256:bundle-dig",
	}
	store.deployments["dep_dig001"] = dep

	entry, err := registry.ShowEntry(stateRoot, "digdep@e1e1e1e1", store)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.BundleDigest != "sha256:bundle-dig" {
		t.Errorf("BundleDigest = %q", entry.BundleDigest)
	}
}

func TestListEntries_Bounded(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	// Create more agents than the bound
	for i := range 15 {
		name := "agent"
		if i < 10 {
			name += "0"
		}
		name += string(rune('0' + i%10))
		pub8 := hex.EncodeToString(sha256.New().Sum([]byte{byte(i)}))[:8]
		writeInstalledAgent(t, stateRoot, name, pub8, "1.0.0", "pub", false)
	}

	entries, err := registry.ListEntries(stateRoot, store)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	// Should return at most MaxListEntries
	if len(entries) > registry.MaxListEntries {
		t.Errorf("ListEntries returned %d entries, want <= %d", len(entries), registry.MaxListEntries)
	}
	// Should be sorted
	for i := 1; i < len(entries); i++ {
		if entries[i].Name < entries[i-1].Name {
			t.Errorf("entries not sorted by name: %q before %q at pos %d", entries[i-1].Name, entries[i].Name, i)
		}
	}
}

func TestShowEntry_FullEntry(t *testing.T) {
	stateRoot := t.TempDir()
	store := newTestDeploymentStore()

	now := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)
	pt := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)

	m := install.InstallManifest{
		PublisherFingerprint: strings.Repeat("ff", 32),
		PublisherName:        "full-pub",
		AgentName:            "full-agent",
		AgentVersion:         "1.0.0",
		AcceptedPolicyDigest: "sha256:policy-dig",
		InstallMode:          "prebuilt-image",
		LocalImageDigest:     "sha256:" + strings.Repeat("ff", 32),
		InstalledAt:          now,
		CredentialMap:        map[string]string{"LLM_KEY": "secret-store-key"},
		Alias:                "full",
		Promoted:             true,
		PromotedAt:           &pt,
		PromotedBy:           "admin",
	}

	ref := "full-agent@" + "ffffffff"
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600)

	lock := &pack.AgentLock{
		SchemaVersion:        pack.LockSchemaVersion,
		AgentName:            "full-agent",
		AgentVersion:         "1.0.0",
		Publisher:            &pack.PublisherInfo{Name: "full-pub", Fingerprint: strings.Repeat("ff", 32)},
		ImageDigest:          "sha256:lock-img-dig",
		PolicyDigest:         "sha256:lock-pol-dig",
		BuildInputDigest:     "sha256:lock-build-dig",
		PublicKeyFingerprint: strings.Repeat("ff", 32),
		Capabilities: []pack.DeclaredCapability{
			{ID: "text_gen", Description: "Text generation"},
		},
	}
	lockRaw, _ := json.Marshal(lock)
	_ = os.WriteFile(filepath.Join(dir, "agent.lock"), lockRaw, 0o600)

	dep := &routedrun.DeploymentRecord{
		DeploymentID:    "dep_full001",
		PackageName:     "full-agent",
		PackageVersion:  "1.0.0",
		Generation:      2,
		Status:          routedrun.DeploymentActive,
		BundleDigest:    "sha256:bundle-full",
		PolicyDigest:    "sha256:dep-pol-dig",
		ImageLockDigest: "sha256:dep-img-lock",
		CreatedAt:       now,
	}
	store.deployments["dep_full001"] = dep

	alias := &routedrun.AliasRecord{
		Alias:              "prod-full",
		TargetDeploymentID: "dep_full001",
	}
	store.aliases["prod-full"] = alias

	entry, err := registry.ShowEntry(stateRoot, "full", store)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}

	// Verify all fields
	if entry.Ref != "full-agent@ffffffff" {
		t.Errorf("Ref = %q", entry.Ref)
	}
	if entry.Name != "full-agent" {
		t.Errorf("Name = %q", entry.Name)
	}
	if entry.Pub8 != "ffffffff" {
		t.Errorf("Pub8 = %q", entry.Pub8)
	}
	if entry.Version != "1.0.0" {
		t.Errorf("Version = %q", entry.Version)
	}
	if entry.PublisherName != "full-pub" {
		t.Errorf("PublisherName = %q", entry.PublisherName)
	}
	if entry.PublisherFingerprint != strings.Repeat("ff", 32) {
		t.Errorf("PublisherFingerprint = %q", entry.PublisherFingerprint)
	}
	if entry.InstallMode != "prebuilt-image" {
		t.Errorf("InstallMode = %q", entry.InstallMode)
	}
	if !entry.InstalledAt.Equal(now) {
		t.Errorf("InstalledAt = %v", entry.InstalledAt)
	}
	if entry.PackageDigest != "sha256:lock-img-dig" {
		t.Errorf("PackageDigest = %q", entry.PackageDigest)
	}
	if entry.PolicyDigest != "sha256:lock-pol-dig" {
		t.Errorf("PolicyDigest = %q", entry.PolicyDigest)
	}
	if entry.Alias != "full" {
		t.Errorf("Alias = %q", entry.Alias)
	}
	if !entry.Promoted {
		t.Errorf("Promoted should be true")
	}
	if !entry.PromotedAt.Equal(pt) {
		t.Errorf("PromotedAt = %v", entry.PromotedAt)
	}
	if entry.PromotedBy != "admin" {
		t.Errorf("PromotedBy = %q", entry.PromotedBy)
	}
	if len(entry.CredentialIDs) != 1 || entry.CredentialIDs[0] != "LLM_KEY" {
		t.Errorf("CredentialIDs = %v", entry.CredentialIDs)
	}
	if len(entry.Capabilities) != 1 || entry.Capabilities[0].ID != "text_gen" {
		t.Errorf("Capabilities = %v", entry.Capabilities)
	}
	if len(entry.Aliases) < 1 || entry.Aliases[0] != "prod-full" {
		t.Errorf("Aliases = %v", entry.Aliases)
	}
	if entry.DeploymentID == nil || *entry.DeploymentID != "dep_full001" {
		t.Errorf("DeploymentID = %v", entry.DeploymentID)
	}
	if entry.DeploymentStatus != "ACTIVE" {
		t.Errorf("DeploymentStatus = %q", entry.DeploymentStatus)
	}
}
