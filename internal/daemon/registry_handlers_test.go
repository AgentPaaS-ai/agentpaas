package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// setupRegistryTest creates a controlServer with a routed store and
// installs a test agent with manifests so registry reads resolve.
func setupRegistryTest(t *testing.T, installAgent bool) *controlServer {
	t.Helper()
	tmp := t.TempDir()
	paths := home.NewHomePaths(tmp)
	if err := home.Ensure(paths); err != nil {
		t.Fatal(err)
	}
	s := &controlServer{homePaths: paths, version: VersionInfo{DaemonVersion: "test"}}
	if err := s.initRoutedStores(routedStoreRoot(paths)); err != nil {
		t.Fatalf("initRoutedStores: %v", err)
	}

	if !installAgent {
		return s
	}

	// Create agent directory and install manifest.
	ref := "weather@a1b2c3d4"
	agentDir := filepath.Join(paths.State, "agents", ref)
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	manifest := install.InstallManifest{
		AgentName:            "weather",
		AgentVersion:         "1.0.0",
		PublisherName:        "TestPub",
		PublisherFingerprint: "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
		InstallMode:          "prebuilt-image",
		LocalImageDigest:     "sha256:local123",
		InstalledAt:          now,
		Alias:                "prod-weather",
		Promoted:             true,
		PromotedAt:           timePtr(now.Add(time.Hour)),
		PromotedBy:           "admin",
		CredentialMap: map[string]string{
			"api-key": "secret-value-should-not-leak",
		},
	}
	writeJSON(t, filepath.Join(agentDir, "install-manifest.json"), manifest)

	// Create lockfile with capabilities.
	lock := pack.AgentLock{
		ImageDigest:  "sha256:pkg123",
		PolicyDigest: "sha256:pol456",
		Publisher: &pack.PublisherInfo{
			Name:        "TestPub",
			Fingerprint: "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
		},
		Capabilities: []pack.DeclaredCapability{
			{ID: "weather.fetch", Description: "Fetch weather data"},
			{ID: "weather.alert", Description: "Send weather alerts"},
		},
	}
	writeJSON(t, filepath.Join(agentDir, "agent.lock"), lock)

	// Create a deployment so the registry join populates deployment fields.
	ctx := context.Background()
	resp, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName:    "weather",
		PackageVersion: "1.0.0",
		BundleDigest:   "sha256:bundle1",
		PolicyDigest:   "sha256:pol456",
		ActorIdentity:  "tester",
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	_ = resp

	return s
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// writeJSON marshals v as JSON and writes it to path.
func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestListRegistry_ReturnsEntriesWithDeploymentStatus(t *testing.T) {
	s := setupRegistryTest(t, true)
	ctx := context.Background()

	resp, err := s.ListRegistry(ctx, &controlv1.ListRegistryRequest{})
	if err != nil {
		t.Fatalf("ListRegistry: %v", err)
	}
	entries := resp.GetEntries()
	if len(entries) == 0 {
		t.Fatal("expected at least one registry entry")
	}

	e := entries[0]
	if e.GetRef() != "weather@a1b2c3d4" {
		t.Fatalf("ref=%q want weather@a1b2c3d4", e.GetRef())
	}
	if e.GetName() != "weather" {
		t.Fatalf("name=%q", e.GetName())
	}
	if e.GetVersion() != "1.0.0" {
		t.Fatalf("version=%q", e.GetVersion())
	}
	if e.GetPublisherName() != "TestPub" {
		t.Fatalf("publisher_name=%q", e.GetPublisherName())
	}
	if e.GetInstallMode() != "prebuilt-image" {
		t.Fatalf("install_mode=%q", e.GetInstallMode())
	}
	if e.GetAlias() != "prod-weather" {
		t.Fatalf("alias=%q", e.GetAlias())
	}
	if !e.GetPromoted() {
		t.Fatal("expected promoted=true")
	}
	if e.GetPromotedBy() != "admin" {
		t.Fatalf("promoted_by=%q", e.GetPromotedBy())
	}

	// Verify deployment join populated.
	if e.GetDeploymentId() == "" {
		t.Fatal("expected deployment_id to be populated from store join")
	}
	if e.GetDeploymentStatus() != "ACTIVE" {
		t.Fatalf("deployment_status=%q want ACTIVE", e.GetDeploymentStatus())
	}
	if e.GetBundleDigest() != "sha256:bundle1" {
		t.Fatalf("bundle_digest=%q", e.GetBundleDigest())
	}

	// Verify credential IDs are present but values are NOT leaked.
	ids := e.GetCredentialIds()
	if len(ids) != 1 || ids[0] != "api-key" {
		t.Fatalf("credential_ids=%v want [api-key]", ids)
	}
}

func TestShowRegistry_ReturnsFullEntryWithCapabilities(t *testing.T) {
	s := setupRegistryTest(t, true)
	ctx := context.Background()

	resp, err := s.ShowRegistry(ctx, &controlv1.ShowRegistryRequest{Ref: "weather@a1b2c3d4"})
	if err != nil {
		t.Fatalf("ShowRegistry: %v", err)
	}
	e := resp.GetEntry()
	if e == nil {
		t.Fatal("expected entry")
	}
	if e.GetRef() != "weather@a1b2c3d4" {
		t.Fatalf("ref=%q", e.GetRef())
	}

	// Verify capabilities are populated.
	caps := e.GetCapabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	if caps[0].GetId() != "weather.fetch" {
		t.Fatalf("cap[0].id=%q", caps[0].GetId())
	}
	if caps[0].GetDescription() != "Fetch weather data" {
		t.Fatalf("cap[0].description=%q", caps[0].GetDescription())
	}
	if caps[1].GetId() != "weather.alert" {
		t.Fatalf("cap[1].id=%q", caps[1].GetId())
	}

	// Verify deployment join.
	if e.GetDeploymentId() == "" {
		t.Fatal("expected deployment_id")
	}
	if e.GetDeploymentStatus() != "ACTIVE" {
		t.Fatalf("deployment_status=%q", e.GetDeploymentStatus())
	}

	// Verify package/policy digests from lockfile.
	if e.GetPackageDigest() != "sha256:pkg123" {
		t.Fatalf("package_digest=%q", e.GetPackageDigest())
	}
	if e.GetPolicyDigest() != "sha256:pol456" {
		t.Fatalf("policy_digest=%q", e.GetPolicyDigest())
	}
}

func TestShowRegistry_UnknownRefReturnsError(t *testing.T) {
	s := setupRegistryTest(t, true)
	ctx := context.Background()

	_, err := s.ShowRegistry(ctx, &controlv1.ShowRegistryRequest{Ref: "nobody@deadbeef"})
	if err == nil {
		t.Fatal("expected error for unknown ref")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v", st.Code())
	}
}

func TestListRegistry_EmptyRegistryReturnsEmptyList(t *testing.T) {
	s := setupRegistryTest(t, false) // no agents installed
	ctx := context.Background()

	resp, err := s.ListRegistry(ctx, &controlv1.ListRegistryRequest{})
	if err != nil {
		t.Fatalf("ListRegistry on empty registry: %v", err)
	}
	if len(resp.GetEntries()) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(resp.GetEntries()))
	}
}
