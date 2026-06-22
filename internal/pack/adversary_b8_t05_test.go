//go:build adversary

package pack

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

func TestAdversaryB8T05_DeployedLockTamper_UndetectedFields(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment error: %v", err)
	}

	// Tamper a field NOT checked in Verify (e.g. CreatedAt or Reproducibility)
	tamperDeployedLock(t, homeDir, lock.AgentName, func(tampered *AgentLock) {
		tampered.CreatedAt = time.Now().Add(24 * time.Hour)
		tampered.Reproducibility.TarOrder = "unsorted-attack"
	})

	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if err != nil {
		t.Logf("tamper on unchecked fields rejected (good): %v", err)
	} else {
		t.Fatal("Verify passed after tampering unchecked fields in agent.lock // ADVERSARY BREAK: unchecked fields allow undetected tamper")
	}
}

func TestAdversaryB8T05_ImageDigestTamper_SelfConsistent(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment error: %v", err)
	}

	deployedDir := DeployedAgentPath(homeDir, lock.AgentName)
	// Change image.digest to match a tampered lock ImageDigest
	newDigest := digestString("tampered-image-attack")
	if err := os.WriteFile(filepath.Join(deployedDir, deployedImageDigestName), []byte(newDigest+"\n"), 0o600); err != nil {
		t.Fatalf("tamper image.digest: %v", err)
	}
	// Also update the lock's ImageDigest and re-sign? But to make self-consistent without valid sig
	tamperDeployedLock(t, homeDir, lock.AgentName, func(tampered *AgentLock) {
		tampered.ImageDigest = newDigest
	})
	// Note: this will break signature, so Verify should catch via sig or hash

	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if err == nil {
		t.Fatal("Verify passed after image.digest + lock image_digest tamper // ADVERSARY BREAK: image.digest tamper not detected")
	}
	if !errors.Is(err, ErrImmutableViolation) {
		t.Fatalf("expected ErrImmutableViolation, got %v", err)
	}
}

func TestAdversaryB8T05_SourceDigestTamper_MatchLock(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment error: %v", err)
	}

	deployedDir := DeployedAgentPath(homeDir, lock.AgentName)
	newSource := digestString("tampered-source-attack")
	if err := os.WriteFile(filepath.Join(deployedDir, deployedSourceDigestName), []byte(newSource+"\n"), 0o600); err != nil {
		t.Fatalf("tamper source_digest: %v", err)
	}
	tamperDeployedLock(t, homeDir, lock.AgentName, func(tampered *AgentLock) {
		tampered.BuildInputDigest = newSource
	})

	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if err == nil {
		t.Fatal("Verify passed after source_digest + lock build_input_digest tamper // ADVERSARY BREAK: source digest tamper self-consistent undetected")
	}
}

func TestAdversaryB8T05_TOCTOURace_TamperDuringVerify(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment error: %v", err)
	}

	deployedDir := DeployedAgentPath(homeDir, lock.AgentName)
	lockPath := filepath.Join(deployedDir, deployedLockName)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(1 * time.Millisecond)
		// Swap the lockfile during verification
		tamperDeployedLock(t, homeDir, lock.AgentName, func(tampered *AgentLock) {
			tampered.AgentVersion = "race-tampered"
		})
	}()

	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	wg.Wait()
	// If no error, race succeeded in bypass
	if err == nil {
		t.Fatal("Verify passed despite TOCTOU tamper // ADVERSARY BREAK: TOCTOU race on lockfile not prevented")
	}
	_ = lockPath // used
}

func TestAdversaryB8T05_MissingFiles_ClearError(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment error: %v", err)
	}

	deployedDir := DeployedAgentPath(homeDir, lock.AgentName)

	// Delete agent.lock
	os.Remove(filepath.Join(deployedDir, deployedLockName))
	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if err == nil {
		t.Fatal("Verify passed with missing agent.lock // ADVERSARY BREAK: missing lockfile silent pass")
	}

	// Re-deploy then delete image.digest
	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("re-Record error: %v", err)
	}
	os.Remove(filepath.Join(deployedDir, deployedImageDigestName))
	err = VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if err == nil {
		t.Fatal("Verify passed with missing image.digest // ADVERSARY BREAK: missing image.digest silent pass")
	}
}

func TestAdversaryB8T05_SymlinkAttack_Lockfile(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)
	appender := &fakeAuditAppender{}

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment error: %v", err)
	}

	deployedDir := DeployedAgentPath(homeDir, lock.AgentName)
	lockPath := filepath.Join(deployedDir, deployedLockName)

	// Replace with symlink to /dev/null (or another file)
	os.Remove(lockPath)
	if err := os.Symlink("/dev/null", lockPath); err != nil {
		t.Fatalf("symlink create: %v", err)
	}

	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if err == nil {
		t.Fatal("Verify passed with symlink agent.lock // ADVERSARY BREAK: symlink not rejected by rejectSymlinkPath")
	}
	if !strings.Contains(err.Error(), "symlink") && !strings.Contains(err.Error(), "immutable") {
		t.Logf("error: %v", err)
	}
}

func TestAdversaryB8T05_PathTraversal_AgentName(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	evilName := "../../etc/passwd-attack"
	err := validateDeployedAgentInput(homeDir, evilName)
	if err == nil {
		t.Fatal("validate accepted path traversal agent name // ADVERSARY BREAK: path traversal in agentName not rejected")
	}
}

func TestAdversaryB8T05_RecordDeployment_AtomicityPartial(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)

	// Simulate partial write by manually creating dir and partial files (bypass atomic for test)
	deployedDir := DeployedAgentPath(homeDir, lock.AgentName)
	if err := os.MkdirAll(deployedDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write only agent.lock, omit others
	data, _ := json.Marshal(lock)
	os.WriteFile(filepath.Join(deployedDir, deployedLockName), data, 0o600)

	// Now call Verify - should fail due to missing files
	appender := &fakeAuditAppender{}
	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, appender)
	if err == nil {
		t.Fatal("Verify passed on partial deployment // ADVERSARY BREAK: partial RecordDeployment allows verification pass")
	}
}

func TestAdversaryB8T05_AuditNilAppender_NoPanic(t *testing.T) {
	homeDir := symlinkSafeTempDir(t)
	lock, _ := signedTestLock(t)

	if err := RecordDeployment(homeDir, lock.AgentName, lock); err != nil {
		t.Fatalf("RecordDeployment error: %v", err)
	}

	// Tamper
	tamperDeployedLock(t, homeDir, lock.AgentName, func(tampered *AgentLock) {
		tampered.AgentVersion = "audit-nil"
	})

	// nil appender should not panic, still return error
	err := VerifyDeployedIntegrity(homeDir, lock.AgentName, nil)
	if err == nil || !errors.Is(err, ErrImmutableViolation) {
		t.Fatalf("expected immutable violation even with nil appender, got %v", err)
	}
}

func TestAdversaryB8T05_BuildInputDigest_WhitespaceOnlyChange(t *testing.T) {
	// This tests if digest is sensitive to whitespace (should change for strict)
	// Use ComputeBuildInputDigest if exported, but simulate via existing e2e logic
	rootDir := symlinkSafeTempDir(t)
	projectDir := filepath.Join(rootDir, "project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	writeBuildTestFile(t, projectDir, "agent.yaml", []byte("name: test\n"))
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('v1')"))

	d1, err := ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("digest1: %v", err)
	}

	// whitespace only change
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('v1') \n")) // trailing space
	d2, err := ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("digest2: %v", err)
	}
	if d1 == d2 {
		t.Fatal("digest unchanged on whitespace-only edit // ADVERSARY BREAK: build input digest insensitive to whitespace")
	}
}