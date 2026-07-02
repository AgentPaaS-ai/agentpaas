package pack

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

func TestImmutablePromptUpdatePath_DigestChangesAndTamperRejected(t *testing.T) {
	rootDir := symlinkSafeTempDir(t)
	projectDir := filepath.Join(rootDir, "project")
	homeDir := filepath.Join(rootDir, "home")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(project): %v", err)
	}
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(home): %v", err)
	}

	writeBuildTestFile(t, projectDir, "agent.yaml", []byte("name: prompt-agent\nversion: 0.1.0\nruntime: python\nentry: main.py\n"))
	writeBuildTestFile(t, projectDir, "main.py", []byte("SYSTEM_PROMPT = \"You are a helpful assistant v1\"\n"))
	digest1, err := ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest(v1) error = %v", err)
	}

	writeBuildTestFile(t, projectDir, "main.py", []byte("SYSTEM_PROMPT = \"You are a helpful assistant v2\"\n"))
	digest2, err := ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest(v2) error = %v", err)
	}
	if digest1 == digest2 {
		t.Fatalf("digest did not change after prompt edit: %s", digest1)
	}

	lock, _ := signedTestLock(t)
	lock.AgentName = "prompt-agent"
	lock.BuildInputDigest = digest1
	signLockForTest(t, lock)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}
	if err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender); err != nil {
		t.Fatalf("VerifyDeployedIntegrity(no tamper) error = %v", err)
	}

	tamperDeployedLock(t, homeDir, lock.AgentName, func(tampered *AgentLock) {
		tampered.AgentVersion = "0.2.0"
	})
	err = VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if !errors.Is(err, ErrImmutableViolation) {
		t.Fatalf("VerifyDeployedIntegrity(tampered) error = %v, want ErrImmutableViolation", err)
	}
	if len(appender.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(appender.records))
	}
	if appender.records[0].EventType != audit.EventTypeImmutableViolation {
		t.Fatalf("EventType = %q, want %q", appender.records[0].EventType, audit.EventTypeImmutableViolation)
	}
}
