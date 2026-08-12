package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyHarnessSentinel_OK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness")
	// Include sentinel among other binary-like content.
	content := []byte("header\noauth_bindings_load_failed\ntrailer")
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	if err := verifyHarnessSentinel(path); err != nil {
		t.Fatalf("verifyHarnessSentinel() error = %v", err)
	}
}

func TestVerifyHarnessSentinel_Missing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness")
	if err := os.WriteFile(path, []byte("stale harness without oauth bindings"), 0o755); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	err := verifyHarnessSentinel(path)
	if err == nil {
		t.Fatal("verifyHarnessSentinel() error = nil, want stale sentinel error")
	}
	if !strings.Contains(err.Error(), "stale") || !strings.Contains(err.Error(), "oauth_bindings_load_failed") {
		t.Fatalf("verifyHarnessSentinel() error = %q, want stale sentinel message", err)
	}
}

func TestVerifyHarnessSentinel_ReadError(t *testing.T) {
	err := verifyHarnessSentinel(filepath.Join(t.TempDir(), "missing-harness"))
	if err == nil {
		t.Fatal("verifyHarnessSentinel() error = nil, want read error")
	}
	if !strings.Contains(err.Error(), "read harness for sentinel scan") {
		t.Fatalf("verifyHarnessSentinel() error = %q, want read error wrap", err)
	}
}

func TestValidateBuildConfig_RejectsMissingSentinel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness")
	if err := os.WriteFile(path, []byte("no sentinel here"), 0o755); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	cfg := BuildConfig{
		ProjectDir:  t.TempDir(),
		ImageTag:    "test:sentinel",
		HarnessPath: path,
	}
	err := validateBuildConfig(&cfg)
	if err == nil {
		t.Fatal("validateBuildConfig() error = nil, want sentinel failure")
	}
	if !strings.Contains(err.Error(), "oauth_bindings_load_failed") {
		t.Fatalf("validateBuildConfig() error = %q, want sentinel message", err)
	}
}

func TestValidateBuildConfig_RequiresHarnessPath(t *testing.T) {
	cfg := BuildConfig{
		ProjectDir: t.TempDir(),
		ImageTag:   "test:no-harness",
	}
	err := validateBuildConfig(&cfg)
	if err == nil {
		t.Fatal("validateBuildConfig() error = nil, want harness path required")
	}
	if !strings.Contains(err.Error(), "harness path is required") {
		t.Fatalf("validateBuildConfig() error = %q, want harness path required", err)
	}
}
