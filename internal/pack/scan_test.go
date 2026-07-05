package pack

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

const testAWSKey = "AKIAIOSFODNN7EXAMPLE"

type recordingAuditAppender struct {
	records []audit.AuditRecord
	err     error
}

func (r *recordingAuditAppender) Append(record audit.AuditRecord) error {
	if r.err != nil {
		return r.err
	}
	r.records = append(r.records, record)

	return nil
}

func TestScanSecretsCleanSource(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "agent.py", "print('clean')\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("ScanSecrets() error = %v", err)
	}
	if len(result.SourceFindings) != 0 {
		t.Fatalf("source findings = %d, want 0", len(result.SourceFindings))
	}
	if len(result.ContextFindings) != 0 {
		t.Fatalf("context findings = %d, want 0", len(result.ContextFindings))
	}
}

func TestScanSecretsPlantedKeyInSource(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, ".agentpaasignore", "ignored.env\n")
	writeScanTestFile(t, projectDir, "ignored.env", "name=value\naws="+testAWSKey+"\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher("ignored.env\n"),
	})
	if err != nil {
		t.Fatalf("ScanSecrets() error = %v", err)
	}
	if len(result.SourceFindings) != 1 {
		t.Fatalf("source findings = %d, want 1", len(result.SourceFindings))
	}
	finding := result.SourceFindings[0]
	if finding.File != "ignored.env" || finding.Line != 2 {
		t.Fatalf("finding location = %s:%d, want ignored.env:2", finding.File, finding.Line)
	}
	if finding.Secret != "AKIA***MPLE" {
		t.Fatalf("masked secret = %q, want %q", finding.Secret, "AKIA***MPLE")
	}
}

func TestScanSecretsPlantedKeyInContext(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "config.env", "aws="+testAWSKey+"\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("ScanSecrets() error = %v", err)
	}
	if len(result.ContextFindings) != 1 {
		t.Fatalf("context findings = %d, want 1", len(result.ContextFindings))
	}
	finding := result.ContextFindings[0]
	if finding.File != "config.env" || finding.Line != 1 {
		t.Fatalf("context finding location = %s:%d, want config.env:1", finding.File, finding.Line)
	}
}

func TestScanSecretsIgnoredSourceStillScanned(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, ".agentpaasignore", "secrets/\n")
	writeScanTestFile(t, projectDir, "secrets/key.txt", testAWSKey+"\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher("secrets/\n"),
	})
	if err != nil {
		t.Fatalf("ScanSecrets() error = %v", err)
	}
	if len(result.SourceFindings) != 1 {
		t.Fatalf("source findings = %d, want 1", len(result.SourceFindings))
	}
	if result.SourceFindings[0].File != "secrets/key.txt" {
		t.Fatalf("source finding file = %q, want secrets/key.txt", result.SourceFindings[0].File)
	}
}

func TestScanSecretsIgnoredFileNotInContextFindings(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, ".agentpaasignore", "ignored.env\n")
	writeScanTestFile(t, projectDir, "ignored.env", testAWSKey+"\n")

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher("ignored.env\n"),
	})
	if err != nil {
		t.Fatalf("ScanSecrets() error = %v", err)
	}
	if len(result.SourceFindings) != 1 {
		t.Fatalf("source findings = %d, want 1", len(result.SourceFindings))
	}
	if len(result.ContextFindings) != 0 {
		t.Fatalf("context findings = %d, want 0", len(result.ContextFindings))
	}
}

func TestScanSecretsContextSizeWarning(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, "large.bin", "123456789")
	previous := contextSizeWarningThreshold
	contextSizeWarningThreshold = 8
	defer func() { contextSizeWarningThreshold = previous }()

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("ScanSecrets() error = %v", err)
	}
	if !result.ContextSizeWarning {
		t.Fatal("ContextSizeWarning = false, want true")
	}
}

func TestScanSecretsAllowPatternWithAudit(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, ".agentpaasignore", "ignored.env\n")
	writeScanTestFile(t, projectDir, "ignored.env", testAWSKey+"\n")
	auditLog := &recordingAuditAppender{}

	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir:    projectDir,
		Ignore:        NewIgnoreMatcher("ignored.env\n"),
		AllowPatterns: []string{`AKIA[A-Z0-9]{16}`},
		AuditAppend:   auditLog,
	})
	if err != nil {
		t.Fatalf("ScanSecrets() error = %v", err)
	}
	if result.HasSecrets() {
		t.Fatalf("HasSecrets() = true, want false")
	}
	if len(auditLog.records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(auditLog.records))
	}
	if auditLog.records[0].EventType != secretAllowPatternEventType {
		t.Fatalf("audit event type = %q, want %q", auditLog.records[0].EventType, secretAllowPatternEventType)
	}
}

func TestScanSecretsAllowPatternWithoutAudit(t *testing.T) {
	projectDir := t.TempDir()
	installMockGitleaks(t)
	writeScanTestFile(t, projectDir, ".agentpaasignore", "ignored.env\n")
	writeScanTestFile(t, projectDir, "ignored.env", testAWSKey+"\n")

	_, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir:    projectDir,
		Ignore:        NewIgnoreMatcher("ignored.env\n"),
		AllowPatterns: []string{`AKIA[A-Z0-9]{16}`},
	})
	if err == nil {
		t.Fatal("ScanSecrets() error = nil, want error")
	}
}

func TestScanSecretsNoGitleaks(t *testing.T) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		t.Skip("gitleaks not available")
	}

	projectDir := t.TempDir()
	writeScanTestFile(t, projectDir, "agent.py", "print('clean')\n")
	result, err := ScanSecrets(context.Background(), ScanConfig{
		ProjectDir: projectDir,
		Ignore:     NewIgnoreMatcher(""),
	})
	if err != nil {
		t.Fatalf("ScanSecrets() with real gitleaks error = %v", err)
	}
	if result.HasSecrets() {
		t.Fatal("HasSecrets() = true, want false")
	}
}

func TestRunGitleaksMissingBinaryFailsClosed(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := runGitleaks(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("runGitleaks() error = nil, want error")
	}
}

func TestMaskSecret(t *testing.T) {
	got := maskSecret(testAWSKey)
	if got != "AKIA***MPLE" {
		t.Fatalf("maskSecret() = %q, want %q", got, "AKIA***MPLE")
	}
}

func TestMaskSecretShort(t *testing.T) {
	got := maskSecret("abc")
	if got != "****" {
		t.Fatalf("maskSecret() = %q, want %q", got, "****")
	}
}

func TestMaskSecretEmpty(t *testing.T) {
	got := maskSecret("")
	if got != "" {
		t.Fatalf("maskSecret() = %q, want empty string", got)
	}
}

func TestComputeContextSize(t *testing.T) {
	projectDir := t.TempDir()
	writeScanTestFile(t, projectDir, "a.txt", "123")
	writeScanTestFile(t, projectDir, "nested/b.txt", "12345")

	size, err := computeContextSize(projectDir, NewIgnoreMatcher(""))
	if err != nil {
		t.Fatalf("computeContextSize() error = %v", err)
	}
	if size != 8 {
		t.Fatalf("size = %d, want 8", size)
	}
}

func TestComputeContextSizeRespectsIgnore(t *testing.T) {
	projectDir := t.TempDir()
	writeScanTestFile(t, projectDir, "keep.txt", "123")
	writeScanTestFile(t, projectDir, "skip.txt", "12345")
	writeScanTestFile(t, projectDir, "ignored/nested.txt", "1234567")

	size, err := computeContextSize(projectDir, NewIgnoreMatcher("skip.txt\nignored/\n"))
	if err != nil {
		t.Fatalf("computeContextSize() error = %v", err)
	}
	if size != 3 {
		t.Fatalf("size = %d, want 3", size)
	}
}

func TestComputeContextSizeRejectsSymlink(t *testing.T) {
	projectDir := t.TempDir()
	writeScanTestFile(t, projectDir, "target.txt", "123")
	err := os.Symlink(filepath.Join(projectDir, "target.txt"), filepath.Join(projectDir, "link.txt"))
	if err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err = computeContextSize(projectDir, NewIgnoreMatcher(""))
	if err == nil {
		t.Fatal("computeContextSize() error = nil, want error")
	}
}

func TestHasSecrets(t *testing.T) {
	empty := &ScanResult{}
	if empty.HasSecrets() {
		t.Fatal("empty.HasSecrets() = true, want false")
	}
	withSource := &ScanResult{SourceFindings: []SecretFinding{{File: "a.txt", Line: 1}}}
	if !withSource.HasSecrets() {
		t.Fatal("withSource.HasSecrets() = false, want true")
	}
	withContext := &ScanResult{ContextFindings: []SecretFinding{{File: "b.txt", Line: 2}}}
	if !withContext.HasSecrets() {
		t.Fatal("withContext.HasSecrets() = false, want true")
	}
}

func TestFailClosed(t *testing.T) {
	empty := &ScanResult{}
	if empty.FailClosed() {
		t.Fatal("empty.FailClosed() = true, want false")
	}
	withFinding := &ScanResult{SourceFindings: []SecretFinding{{File: "a.txt", Line: 1}}}
	if !withFinding.FailClosed() {
		t.Fatal("withFinding.FailClosed() = false, want true")
	}
}

func installMockGitleaks(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	script := `#!/bin/sh
dir=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--source" ]; then
		shift
		dir="$1"
	fi
	shift
done
found=0
printf '['
first=1
find "$dir" -type f | sort | while IFS= read -r file; do
	line=$(grep -n -E -m 1 'AKIA[A-Z0-9]{16}' "$file")
	if [ -n "$line" ]; then
		line_no=${line%%:*}
		secret=$(printf '%s' "$line" | grep -E -o 'AKIA[A-Z0-9]{16}' | head -n 1)
		rel=${file#"$dir"/}
		if [ "$first" -eq 0 ]; then
			printf ','
		fi
		first=0
		found=1
		printf '{"File":"%s","StartLine":%s,"RuleID":"aws-access-token","Secret":"%s"}' "$rel" "$line_no" "$secret"
	fi
done
printf ']'
if [ "$found" -eq 1 ]; then
	exit 1
fi
exit 0
`
	path := filepath.Join(binDir, "gitleaks")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock gitleaks: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeScanTestFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestApplyAllowPatternsAuditAppendError(t *testing.T) {
	wantErr := errors.New("audit down")
	_, err := applyAllowPatterns([]SecretFinding{{
		File:      "a.txt",
		Line:      1,
		Rule:      "rule",
		Secret:    maskSecret(testAWSKey),
		rawSecret: testAWSKey,
	}}, []string{`AKIA[A-Z0-9]{16}`}, &recordingAuditAppender{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
