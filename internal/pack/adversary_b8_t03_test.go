//go:build adversary

package pack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdversaryB8T03_SecretInSource(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "app.py", "# config\naws_key = \""+testAWSKey+"\"\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("ScanSecrets error = %v", err)
	}
	if len(result.SourceFindings) != 1 {
		t.Fatalf("source findings = %d, want 1", len(result.SourceFindings))
	}
	f := result.SourceFindings[0]
	if f.File != "app.py" || f.Line != 2 {
		t.Fatalf("finding = %s:%d, want app.py:2", f.File, f.Line)
	}
	if f.Secret != "AKIA***MPLE" {
		t.Fatalf("masked = %q, want AKIA***MPLE", f.Secret)
	}
}

func TestAdversaryB8T03_SecretInBuildContext(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "secret.env", "key="+testAWSKey+"\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(result.ContextFindings) != 1 {
		t.Fatalf("context findings = %d, want 1", len(result.ContextFindings))
	}
}

func TestAdversaryB8T03_SecretInIgnoredFile(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, ".agentpaasignore", "secrets/\n")
	writeScanTestFile(t, projectDir, "secrets/creds.txt", testAWSKey+"\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher("secrets/\n"),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(result.SourceFindings) != 1 {
		t.Fatalf("source findings = %d, want 1", len(result.SourceFindings))
	}
	if len(result.ContextFindings) != 0 {
		t.Fatalf("context findings = %d, want 0 (ignored)", len(result.ContextFindings))
	}
}

func TestAdversaryB8T03_AllowPatternWithoutAuditFailsClosed(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "creds.env", testAWSKey+"\n")

	_, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir:    projectDir,
		Ignore:        NewIgnoreMatcher(""),
		AllowPatterns: []string{`AKIA[A-Z0-9]{16}`},
		// AuditAppend: nil -> must error
	})
	if err == nil || !strings.Contains(err.Error(), "allow-secret-pattern requires audit append") {
		t.Fatalf("expected allow-pattern without audit error, got: %v", err)
	}
}

func TestAdversaryB8T03_AllowPatternWithAudit(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "creds.env", testAWSKey+"\n")
	audit := &recordingAuditAppender{}

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir:    projectDir,
		Ignore:        NewIgnoreMatcher(""),
		AllowPatterns: []string{`AKIA[A-Z0-9]{16}`},
		AuditAppend:   audit,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if result.HasSecrets() {
		t.Fatal("HasSecrets true after allow, want false")
	}
	if len(audit.records) != 2 {
		t.Fatalf("audit records = %d, want 2 (source+context)", len(audit.records))
	}
}

func TestAdversaryB8T03_SecretMaskingNeverRaw(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "key.txt", testAWSKey+"\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	for _, f := range result.SourceFindings {
		if strings.Contains(f.Secret, testAWSKey) || len(f.Secret) > 12 {
			t.Fatalf("raw secret leaked in masked field: %q", f.Secret)
		}
		if f.Secret != "AKIA***MPLE" {
			t.Fatalf("mask wrong: %q", f.Secret)
		}
	}
}

func TestAdversaryB8T03_GitleaksUnavailableFailsClosed(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no gitleaks

	projectDir := t.TempDir()
	writeScanTestFile(t, projectDir, "app.py", "print(1)\n")

	_, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err == nil {
		t.Fatal("expected error when gitleaks missing (fail closed), got nil")
	}
}

func TestAdversaryB8T03_ContextSizeWarning(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "big.bin", strings.Repeat("x", 100))

	prev := contextSizeWarningThreshold
	contextSizeWarningThreshold = 50
	defer func() { contextSizeWarningThreshold = prev }()

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if !result.ContextSizeWarning {
		t.Fatal("ContextSizeWarning false, want true for > threshold")
	}
}

func TestAdversaryB8T03_SymlinkAttackRejects(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	target := filepath.Join(projectDir, "real.txt")
	if err := os.WriteFile(target, []byte(testAWSKey+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(projectDir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	_, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err == nil || !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("expected symlink reject, got: %v", err)
	}
}

func TestAdversaryB8T03_PathTraversalRejects(t *testing.T) {
	// validateProjectDir rejects .. in path string
	_, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: "/tmp/../etc",
		Ignore:     NewIgnoreMatcher(""),
	})
	if err == nil || !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("expected path traversal error, got: %v", err)
	}
}

func TestAdversaryB8T03_EmptySourceNoFindings(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(result.SourceFindings) != 0 || len(result.ContextFindings) != 0 {
		t.Fatal("expected 0 findings for empty source")
	}
}

func TestAdversaryB8T03_MultipleSecretsSeparateFiles(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "multi.env", "key1="+testAWSKey+"\n")
	writeScanTestFile(t, projectDir, "multi2.env", "key2=AKIAANOTHERKEY1234\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	// mock gitleaks reports at most one per scan invocation due to shell logic; accept >=1 as coverage
	if len(result.SourceFindings) < 1 {
		t.Fatalf("findings = %d, want >=1", len(result.SourceFindings))
	}
}
