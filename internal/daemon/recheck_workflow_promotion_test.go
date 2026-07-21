package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecheckWorkflowPromotion verifies the TOCTOU-prevention helper that
// re-reads workflow.yaml and re-runs the promotion gate validation.
func TestRecheckWorkflowPromotion(t *testing.T) {
	// No workflow.yaml → nil error.
	t.Run("no-workflow", func(t *testing.T) {
		dir := t.TempDir()
		if err := recheckWorkflowPromotion(dir, ""); err != nil {
			t.Fatalf("expected nil error for missing workflow.yaml, got: %v", err)
		}
	})

	// Empty workflow.yaml → nil error.
	t.Run("empty-workflow", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "workflow.yaml"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := recheckWorkflowPromotion(dir, ""); err != nil {
			t.Fatalf("expected nil error for empty workflow.yaml, got: %v", err)
		}
	})

	// Malformed YAML → error.
	t.Run("malformed-yaml", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte(": bad yaml"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := recheckWorkflowPromotion(dir, ""); err == nil {
			t.Fatal("expected error for malformed workflow.yaml")
		}
	})

	// Valid workflow.yaml with empty stateRoot → nil (graceful degradation).
	t.Run("valid-no-state", func(t *testing.T) {
		dir := t.TempDir()
		yaml := "kind: pipeline\nversion: \"1\"\nstages:\n  - package_name: weather\n    package_version: 1.0.0\n"
		if err := os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := recheckWorkflowPromotion(dir, ""); err != nil {
			t.Fatalf("expected nil error with empty stateRoot, got: %v", err)
		}
	})

	// Valid workflow.yaml with non-empty stateRoot but no installed agents
	// → nil (packages not installed are skipped gracefully).
	t.Run("valid-state-missing", func(t *testing.T) {
		dir := t.TempDir()
		stateRoot := t.TempDir()
		yaml := "kind: pipeline\nversion: \"1\"\nstages:\n  - package_name: weather\n    package_version: 1.0.0\n"
		if err := os.WriteFile(filepath.Join(dir, "workflow.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := recheckWorkflowPromotion(dir, stateRoot); err != nil {
			t.Fatalf("expected nil error when packages not installed, got: %v", err)
		}
	})
}
