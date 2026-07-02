package pack

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

type fakeAuditAppender struct {
	records []audit.AuditRecord
}

func (f *fakeAuditAppender) Append(record audit.AuditRecord) error {
	f.records = append(f.records, record)
	return nil
}

func TestRecordDeployment_CreatesFiles(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}

	deployedDir := DeployedAgentPath(homeDir, lock.AgentName)
	for _, name := range []string{"agent.lock", "image.digest", "deployed_at", "source_digest"} {
		path := filepath.Join(deployedDir, name)
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
}

func TestLoadDeployedAgent_RoundTrip(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}
	deployed, err := LoadDeployedAgent(homeDir, lock.AgentName)
	if err != nil {
		t.Fatalf("LoadDeployedAgent() error = %v", err)
	}

	if deployed.AgentName != lock.AgentName {
		t.Fatalf("AgentName = %q, want %q", deployed.AgentName, lock.AgentName)
	}
	if deployed.ImageDigest != lock.ImageDigest {
		t.Fatalf("ImageDigest = %q, want %q", deployed.ImageDigest, lock.ImageDigest)
	}
	if deployed.SourceDigest != lock.BuildInputDigest {
		t.Fatalf("SourceDigest = %q, want %q", deployed.SourceDigest, lock.BuildInputDigest)
	}
	if deployed.LockfileSig != lock.LockfileSignature {
		t.Fatalf("LockfileSig mismatch")
	}
	if deployed.DeployedAt.IsZero() {
		t.Fatal("DeployedAt is zero")
	}
}

func TestVerifyDeployedIntegrity_NoTamper_Passes(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}
	if err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender); err != nil {
		t.Fatalf("VerifyDeployedIntegrity() error = %v", err)
	}
	if len(appender.records) != 0 {
		t.Fatalf("expected no audit records, got %d", len(appender.records))
	}
}

func TestVerifyDeployedIntegrity_TamperedLockFile_Rejected(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}
	tamperDeployedLock(t, homeDir, lock.AgentName, func(tampered *AgentLock) {
		tampered.AgentVersion = "9.9.9"
	})

	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if !errors.Is(err, ErrImmutableViolation) {
		t.Fatalf("VerifyDeployedIntegrity() error = %v, want ErrImmutableViolation", err)
	}
	assertImmutableViolationAudit(t, appender)
}

func TestVerifyDeployedIntegrity_ReformattedLockFile_Rejected(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}
	path := filepath.Join(DeployedAgentPath(homeDir, lock.AgentName), "agent.lock")
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(agent.lock): %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile(agent.lock): %v", err)
	}

	err = VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if !errors.Is(err, ErrImmutableViolation) {
		t.Fatalf("VerifyDeployedIntegrity() error = %v, want ErrImmutableViolation", err)
	}
	assertImmutableViolationAudit(t, appender)
}

func TestVerifyDeployedIntegrity_TamperedImageDigest_Rejected(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}
	path := filepath.Join(DeployedAgentPath(homeDir, lock.AgentName), "image.digest")
	if err := os.WriteFile(path, []byte(digestString("tampered-image")), 0o600); err != nil {
		t.Fatalf("WriteFile(image.digest): %v", err)
	}

	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if !errors.Is(err, ErrImmutableViolation) {
		t.Fatalf("VerifyDeployedIntegrity() error = %v, want ErrImmutableViolation", err)
	}
	assertImmutableViolationAudit(t, appender)
}

func TestVerifyDeployedIntegrity_TamperedSourceDigest_Rejected(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}
	path := filepath.Join(DeployedAgentPath(homeDir, lock.AgentName), "source_digest")
	if err := os.WriteFile(path, []byte(digestString("tampered-source")), 0o600); err != nil {
		t.Fatalf("WriteFile(source_digest): %v", err)
	}

	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if !errors.Is(err, ErrImmutableViolation) {
		t.Fatalf("VerifyDeployedIntegrity() error = %v, want ErrImmutableViolation", err)
	}
	assertImmutableViolationAudit(t, appender)
}

func TestIsDeployed_TrueFalse(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)

	if IsDeployed(homeDir, lock.AgentName) {
		t.Fatal("IsDeployed() = true before deployment")
	}
	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment() error = %v", err)
	}
	if !IsDeployed(homeDir, lock.AgentName) {
		t.Fatal("IsDeployed() = false after deployment")
	}
}

func TestDeployedAgentPath(t *testing.T) {
	homeDir := filepath.Join(string(filepath.Separator), "tmp", "agentpaas-home")
	got := DeployedAgentPath(homeDir, "example")
	want := filepath.Join(homeDir, "state", "agents", "example")
	if got != want {
		t.Fatalf("DeployedAgentPath() = %q, want %q", got, want)
	}
}

func tamperDeployedLock(t *testing.T, homeDir, agentName string, mutate func(*AgentLock)) {
	t.Helper()

	path := filepath.Join(DeployedAgentPath(homeDir, agentName), "agent.lock")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(agent.lock): %v", err)
	}
	var lock AgentLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatalf("Unmarshal(agent.lock): %v", err)
	}
	mutate(&lock)
	data, err = json.Marshal(&lock)
	if err != nil {
		t.Fatalf("Marshal(agent.lock): %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile(agent.lock): %v", err)
	}
}

func assertImmutableViolationAudit(t *testing.T, appender *fakeAuditAppender) {
	t.Helper()

	if len(appender.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(appender.records))
	}
	if appender.records[0].EventType != audit.EventTypeImmutableViolation {
		t.Fatalf("EventType = %q, want %q", appender.records[0].EventType, audit.EventTypeImmutableViolation)
	}
}
