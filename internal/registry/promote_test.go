package registry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/registry"
)

// writeMinimalInstalledAgent creates a minimal installed agent without lock for promote tests.
func writeMinimalInstalledAgent(t *testing.T, stateRoot, name, pub8, version, publisher string) {
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
		Promoted:             false,
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// countEvents reads the audit JSONL and returns the count of records with a given event type.
func countEvents(t *testing.T, path, eventType string) int {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read audit: %v", err)
	}

	count := 0
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var rec audit.AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.EventType == eventType {
			count++
		}
	}
	return count
}

func TestPromote_SetsPromotedFlag(t *testing.T) {
	stateRoot := t.TempDir()
	writeMinimalInstalledAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", "worker-pub")

	err := registry.Promote(stateRoot, "worker@a1b2c3d4", "test-actor")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Verify manifest on disk.
	ref := "worker@a1b2c3d4"
	manifestPath := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m install.InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if !m.Promoted {
		t.Error("expected promoted=true")
	}
	if m.PromotedAt == nil {
		t.Error("expected non-nil PromotedAt")
	}
	if m.PromotedBy != "test-actor" {
		t.Errorf("PromotedBy = %q, want test-actor", m.PromotedBy)
	}
}

func TestPromote_Idempotent(t *testing.T) {
	stateRoot := t.TempDir()
	writeMinimalInstalledAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", "worker-pub")

	// First promote.
	err := registry.Promote(stateRoot, "worker@a1b2c3d4", "actor-1")
	if err != nil {
		t.Fatalf("Promote 1: %v", err)
	}

	// Capture first PromotedAt.
	ref := "worker@a1b2c3d4"
	manifestPath := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")
	raw, _ := os.ReadFile(manifestPath)
	var m1 install.InstallManifest
	_ = json.Unmarshal(raw, &m1)

	// Second promote (idempotent - should be no-op).
	err = registry.Promote(stateRoot, "worker@a1b2c3d4", "actor-2")
	if err != nil {
		t.Fatalf("Promote 2: %v", err)
	}

	raw2, _ := os.ReadFile(manifestPath)
	var m2 install.InstallManifest
	_ = json.Unmarshal(raw2, &m2)

	// Second promote should not change PromotedAt or PromotedBy.
	if !m2.Promoted {
		t.Error("expected promoted=true after second promote")
	}
	if m1.PromotedAt == nil || m2.PromotedAt == nil {
		t.Fatal("both PromotedAt must be non-nil")
	}
	if !m1.PromotedAt.Equal(*m2.PromotedAt) {
		t.Errorf("PromotedAt changed on idempotent promote: %v -> %v", m1.PromotedAt, m2.PromotedAt)
	}
	if m2.PromotedBy != "actor-1" {
		t.Errorf("PromotedBy changed on idempotent promote: %q, want actor-1", m2.PromotedBy)
	}
}

func TestPromote_EmitsAuditEvent(t *testing.T) {
	stateRoot := t.TempDir()
	writeMinimalInstalledAgent(t, stateRoot, "calculator", "f1e2d3c4", "2.0.0", "calc-pub")

	err := registry.Promote(stateRoot, "calculator@f1e2d3c4", "admin")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Check audit.jsonl exists.
	auditPath := filepath.Join(stateRoot, "audit.jsonl")
	count := countEvents(t, auditPath, audit.EventTypePackagePromoted)
	if count < 1 {
		t.Errorf("expected at least 1 %s event, got %d", audit.EventTypePackagePromoted, count)
	}

	// Read the event and check payload.
	data, _ := os.ReadFile(auditPath)
	var foundRec *audit.AuditRecord
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var rec audit.AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.EventType == audit.EventTypePackagePromoted {
			foundRec = &rec
			break
		}
	}
	if foundRec == nil {
		t.Fatal("could not find package_promoted event in audit log")
	}
	if foundRec.Actor != "admin" {
		t.Errorf("Actor = %q, want admin", foundRec.Actor)
	}
	if foundRec.Payload == nil {
		t.Fatal("expected non-nil payload")
	}
	// Check required payload fields.
	if _, ok := foundRec.Payload["agent_ref"]; !ok {
		t.Error("payload missing agent_ref")
	}
	if _, ok := foundRec.Payload["fingerprint"]; !ok {
		t.Error("payload missing fingerprint")
	}
	if _, ok := foundRec.Payload["digest"]; !ok {
		t.Error("payload missing digest")
	}
}

func TestDemote_ClearsPromotedFlag(t *testing.T) {
	stateRoot := t.TempDir()
	writeMinimalInstalledAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", "worker-pub")

	// First promote.
	err := registry.Promote(stateRoot, "worker@a1b2c3d4", "actor")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Demote.
	err = registry.Demote(stateRoot, "worker@a1b2c3d4")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}

	// Check manifest.
	ref := "worker@a1b2c3d4"
	manifestPath := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")
	raw, _ := os.ReadFile(manifestPath)
	var m install.InstallManifest
	_ = json.Unmarshal(raw, &m)

	if m.Promoted {
		t.Error("expected promoted=false after demote")
	}
	if m.PromotedAt != nil {
		t.Errorf("expected nil PromotedAt, got %v", m.PromotedAt)
	}
	if m.PromotedBy != "" {
		t.Errorf("expected empty PromotedBy, got %q", m.PromotedBy)
	}
}

func TestDemote_Idempotent(t *testing.T) {
	stateRoot := t.TempDir()
	writeMinimalInstalledAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", "worker-pub")

	// Demote an already-not-promoted package (idempotent no-op).
	err := registry.Demote(stateRoot, "worker@a1b2c3d4")
	if err != nil {
		t.Fatalf("Demote (idempotent): %v", err)
	}

	// Second demote should also succeed.
	err = registry.Demote(stateRoot, "worker@a1b2c3d4")
	if err != nil {
		t.Fatalf("Demote 2: %v", err)
	}

	// Verify still not promoted.
	ref := "worker@a1b2c3d4"
	manifestPath := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")
	raw, _ := os.ReadFile(manifestPath)
	var m install.InstallManifest
	_ = json.Unmarshal(raw, &m)

	if m.Promoted {
		t.Error("expected promoted=false")
	}
}

func TestDemote_EmitsAuditEvent(t *testing.T) {
	stateRoot := t.TempDir()
	writeMinimalInstalledAgent(t, stateRoot, "calculator", "f1e2d3c4", "2.0.0", "calc-pub")

	// Promote first, then demote.
	err := registry.Promote(stateRoot, "calculator@f1e2d3c4", "admin")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}

	err = registry.Demote(stateRoot, "calculator@f1e2d3c4")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}

	// Check for package_demoted event.
	auditPath := filepath.Join(stateRoot, "audit.jsonl")
	count := countEvents(t, auditPath, audit.EventTypePackageDemoted)
	if count < 1 {
		t.Errorf("expected at least 1 %s event, got %d", audit.EventTypePackageDemoted, count)
	}

	// Verify payload on demote event.
	data, _ := os.ReadFile(auditPath)
	var foundRec *audit.AuditRecord
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var rec audit.AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.EventType == audit.EventTypePackageDemoted {
			foundRec = &rec
		}
	}
	if foundRec == nil {
		t.Fatal("could not find package_demoted event in audit log")
	}
	if foundRec.Payload == nil {
		t.Fatal("expected non-nil payload")
	}
	if _, ok := foundRec.Payload["agent_ref"]; !ok {
		t.Error("payload missing agent_ref")
	}
	if _, ok := foundRec.Payload["fingerprint"]; !ok {
		t.Error("payload missing fingerprint")
	}
	if _, ok := foundRec.Payload["digest"]; !ok {
		t.Error("payload missing digest")
	}
}

func TestPromote_UnknownRef(t *testing.T) {
	stateRoot := t.TempDir()

	err := registry.Promote(stateRoot, "nobody@deadbeef", "actor")
	if err == nil {
		t.Error("expected error for unknown ref, got nil")
	}
}

func TestPromote_ByAlias(t *testing.T) {
	stateRoot := t.TempDir()
	writeMinimalInstalledAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", "worker-pub")

	// Set an alias on the manifest.
	ref := "worker@a1b2c3d4"
	manifestPath := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")
	raw, _ := os.ReadFile(manifestPath)
	var m install.InstallManifest
	_ = json.Unmarshal(raw, &m)
	m.Alias = "myworker"
	raw2, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(manifestPath, raw2, 0o600)

	err := registry.Promote(stateRoot, "myworker", "actor")
	if err != nil {
		t.Fatalf("Promote by alias: %v", err)
	}

	// Verify promoted.
	raw3, _ := os.ReadFile(manifestPath)
	var m2 install.InstallManifest
	_ = json.Unmarshal(raw3, &m2)
	if !m2.Promoted {
		t.Error("expected promoted=true after promote by alias")
	}
}

func TestPromote_Demote_FullCycle(t *testing.T) {
	stateRoot := t.TempDir()
	writeMinimalInstalledAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", "worker-pub")

	// Promote -> verify
	if err := registry.Promote(stateRoot, "worker@a1b2c3d4", "admin"); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Demote -> verify
	if err := registry.Demote(stateRoot, "worker@a1b2c3d4"); err != nil {
		t.Fatalf("Demote: %v", err)
	}

	// Promote again -> verify
	if err := registry.Promote(stateRoot, "worker@a1b2c3d4", "admin2"); err != nil {
		t.Fatalf("Promote 2: %v", err)
	}

	// Check all three audit events exist.
	auditPath := filepath.Join(stateRoot, "audit.jsonl")
	promoteCount := countEvents(t, auditPath, audit.EventTypePackagePromoted)
	demoteCount := countEvents(t, auditPath, audit.EventTypePackageDemoted)
	if promoteCount < 2 {
		t.Errorf("expected at least 2 promote events, got %d", promoteCount)
	}
	if demoteCount < 1 {
		t.Errorf("expected at least 1 demote event, got %d", demoteCount)
	}
}
