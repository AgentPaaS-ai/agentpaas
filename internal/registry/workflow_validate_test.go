package registry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/registry"
	"gopkg.in/yaml.v3"
)

func TestValidateWorkflowPromoted_Nil(t *testing.T) {
	errs := registry.ValidateWorkflowPromotedPackages("", nil)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors for nil workflow: %v", errs)
	}
}

func TestValidateWorkflowPromoted_EmptyStateRoot(t *testing.T) {
	yml := `kind: pipeline
pipeline:
  stages:
    - name: fetch
      package_name: weather
      package_version: "1.0.0"
      bundle_digest: sha256:abc123
      handoff: public
`
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// Empty stateRoot: check skipped, no errors.
	errs := registry.ValidateWorkflowPromotedPackages("", &wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected promotion errors with empty stateRoot: %v", errs)
	}
}

func TestValidateWorkflowPromoted_Service(t *testing.T) {
	yml := `kind: standalone
services:
  - service_id: rag-ingest
    package_name: rag-service
    package_version: "1.0.0"
    bundle_digest: sha256:rag123
`
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := registry.ValidateWorkflowPromotedPackages("", &wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected promotion errors: %v", errs)
	}
}

func TestValidateWorkflowPromoted_ChildAllowlist(t *testing.T) {
	yml := `kind: parent_child
parent_child:
  parent_identity:
    package_name: parent
    package_version: "1.0.0"
    bundle_digest: sha256:parent
  child_allowlist:
    - package_name: child-x
      package_version: "1.0.0"
      bundle_digest: sha256:child
  max_fanout: 5
  max_concurrency: 3
  leaf_only_depth: true
`
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	errs := registry.ValidateWorkflowPromotedPackages("", &wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected promotion errors: %v", errs)
	}
}

func TestValidateWorkflowPromoted_UnpromotedPackage(t *testing.T) {
	// Create a state root with an installed but NOT promoted package.
	stateRoot := t.TempDir()

	ref := "weather@a1b2c3d4"
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	m := install.InstallManifest{
		PublisherFingerprint: strings.Repeat("a1b2c3d4", 8)[:64],
		PublisherName:        "weather-pub",
		AgentName:            "weather",
		AgentVersion:         "1.0.0",
		AcceptedPolicyDigest: "sha256:" + strings.Repeat("aa", 32),
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:" + strings.Repeat("bb", 32),
		InstalledAt:          time.Now().UTC(),
		Promoted:             false,
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600)

	yml := `kind: pipeline
pipeline:
  stages:
    - name: fetch
      package_name: weather
      package_version: "1.0.0"
      bundle_digest: sha256:abc123
      handoff: public
`
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	errs := registry.ValidateWorkflowPromotedPackages(stateRoot, &wf)
	if len(errs) == 0 {
		t.Fatal("expected promotion validation errors for un-promoted package, got none")
	}
	// Check the error message is actionable.
	found := false
	for _, e := range errs {
		if strings.Contains(e, "not promoted") && strings.Contains(e, "agentpaas registry promote") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("error should be actionable with promote command hint: %v", errs)
	}
}

func TestValidateWorkflowPromoted_PromotedPackagePasses(t *testing.T) {
	// Create a state root with an installed AND promoted package.
	stateRoot := t.TempDir()

	ref := "weather@a1b2c3d4"
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	now := time.Now().UTC()
	m := install.InstallManifest{
		PublisherFingerprint: strings.Repeat("a1b2c3d4", 8)[:64],
		PublisherName:        "weather-pub",
		AgentName:            "weather",
		AgentVersion:         "1.0.0",
		AcceptedPolicyDigest: "sha256:" + strings.Repeat("aa", 32),
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:" + strings.Repeat("bb", 32),
		InstalledAt:          now,
		Promoted:             true,
		PromotedAt:           &now,
		PromotedBy:           "admin",
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600)

	yml := `kind: pipeline
pipeline:
  stages:
    - name: fetch
      package_name: weather
      package_version: "1.0.0"
      bundle_digest: sha256:abc123
      handoff: public
`
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	errs := registry.ValidateWorkflowPromotedPackages(stateRoot, &wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected promotion errors for promoted package: %v", errs)
	}
}

func TestValidateWorkflowPromoted_DemoteThenFail(t *testing.T) {
	// Promote, then demote - new workflow validation should fail.
	stateRoot := t.TempDir()

	ref := "weather@a1b2c3d4"
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	now := time.Now().UTC()
	m := install.InstallManifest{
		PublisherFingerprint: strings.Repeat("a1b2c3d4", 8)[:64],
		PublisherName:        "weather-pub",
		AgentName:            "weather",
		AgentVersion:         "1.0.0",
		AcceptedPolicyDigest: "sha256:" + strings.Repeat("aa", 32),
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:" + strings.Repeat("bb", 32),
		InstalledAt:          now,
		Promoted:             true,
		PromotedAt:           &now,
		PromotedBy:           "admin",
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600)

	// Verify promoted passes.
	yml := `kind: pipeline
pipeline:
  stages:
    - name: fetch
      package_name: weather
      package_version: "1.0.0"
      bundle_digest: sha256:abc123
      handoff: public
`
	var wf pack.WorkflowYAML
	_ = yaml.Unmarshal([]byte(yml), &wf)

	errs := registry.ValidateWorkflowPromotedPackages(stateRoot, &wf)
	if len(errs) > 0 {
		t.Fatalf("promoted package should pass: %v", errs)
	}

	// Demote the package.
	if err := registry.Demote(stateRoot, "weather@a1b2c3d4"); err != nil {
		t.Fatalf("Demote: %v", err)
	}

	// Now workflow validation should fail.
	errs = registry.ValidateWorkflowPromotedPackages(stateRoot, &wf)
	if len(errs) == 0 {
		t.Fatal("expected validation errors after demote, got none")
	}
}

func TestValidateWorkflowPromoted_LocallyOwnedPackage(t *testing.T) {
	// B23 regression: locally owned packages (not installed via registry)
	// should not cause validation errors when stateRoot is set but package
	// isn't installed — we gracefully skip unresolvable packages.
	stateRoot := t.TempDir()

	yml := `kind: standalone
services:
  - service_id: local-svc
    package_name: my-local-pkg
    package_version: "0.0.1"
    bundle_digest: sha256:local
`
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Package not installed: should skip gracefully, no error.
	errs := registry.ValidateWorkflowPromotedPackages(stateRoot, &wf)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors for non-installed local package: %v", errs)
	}
}
