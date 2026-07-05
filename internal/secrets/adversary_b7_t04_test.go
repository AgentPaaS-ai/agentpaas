package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const adversarySecretValue = "adversary-lease-secret-VALUE-0123456789"

func TestAdversary_B7_T04_LeaseHandleNeverExposesSecretValue(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	defer func() { _ = handle.Cleanup() }()

	rendered := handle.String() + "\n" + handle.GoString()
	data, _ := json.Marshal(handle)
	rendered += "\n" + string(data)
	if strings.Contains(rendered, adversarySecretValue) {
		t.Fatalf("LeaseHandle exposed secret value")
	}
	if strings.Contains(rendered, handle.FilePath) {
		t.Fatalf("LeaseHandle exposed FilePath")
	}
}

func TestAdversary_B7_T04_LeaseFileModeExactly0400(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	defer func() { _ = handle.Cleanup() }()

	info, err := os.Stat(handle.FilePath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o400 {
		t.Fatalf("lease file mode = %o, want exactly 0400", got)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("lease file is a symlink")
	}
}

func TestAdversary_B7_T04_ReadLeaseRejectsPostCreationSymlinkReplacement(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	defer func() { _ = handle.Cleanup() }()

	// Attacker replaces the lease file with symlink to sensitive target
	target := filepath.Join(t.TempDir(), "target-passwd")
	if err := os.WriteFile(target, []byte("root:x:0:0"), 0o600); err != nil {
		t.Fatalf("Write target: %v", err)
	}
	if err := os.Remove(handle.FilePath); err != nil {
		t.Fatalf("Remove lease: %v", err)
	}
	if err := os.Symlink(target, handle.FilePath); err != nil {
		t.Fatalf("Symlink attack: %v", err)
	}

	_, err = ReadLease(ctx, handle)
	if err == nil {
		t.Fatal("ReadLease succeeded on post-creation symlink, want rejection")
	}
	if !strings.Contains(err.Error(), "symlink") && !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("ReadLease error = %v, want symlink or regular file rejection", err)
	}
}

func TestAdversary_B7_T04_LeaseRejectsSymlinkAtCreationPath(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// Pre-create run dir path as symlink
	runDir := filepath.Join(dir, "run-active")
	target := filepath.Join(dir, "real-run")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("Mkdir target: %v", err)
	}
	if err := os.Symlink(target, runDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	store := NewFakeKeyStore()
	if err := store.Set(ctx, "api-token", []byte(adversarySecretValue)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	lease, err := NewDirectLease(DirectLeaseConfig{
		Store:      store,
		Policy:     newAdversaryPolicy(),
		ActiveRuns: []string{"run-active"},
		LeaseDir:   dir,
	})
	if err != nil {
		t.Fatalf("NewDirectLease: %v", err)
	}

	_, err = lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err == nil {
		t.Fatal("Lease over pre-created symlink path succeeded, want error")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Lease error = %v, want symlink rejection", err)
	}
}

func TestAdversary_B7_T04_ConcurrentReadLeaseAndCleanupNoRaceOrPanic(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	// Note: do not defer cleanup here, we race it

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = ReadLease(ctx, handle)
	}()
	go func() {
		defer wg.Done()
		_ = handle.Cleanup()
	}()
	wg.Wait()
	// If -race detects data race or panic occurs, test will fail under race detector
}

func TestAdversary_B7_T04_LeaseNeverSetsEnvironmentVariable(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	defer func() { _ = handle.Cleanup() }()

	envs := os.Environ()
	for _, e := range envs {
		if strings.Contains(e, adversarySecretValue) {
			t.Fatalf("lease set env var containing secret: %s", e)
		}
	}
}

func TestAdversary_B7_T04_CleanupRemovesFileButNotDirectory(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	runDir := filepath.Dir(handle.FilePath)
	if err := handle.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	if _, err := os.Stat(handle.FilePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists after Cleanup: %v", err)
	}
	// Directory should remain (run dir not removed)
	if _, err := os.Stat(runDir); err != nil {
		t.Fatalf("run directory removed by Cleanup, want preserved: %v", err)
	}
}

func TestAdversary_B7_T04_MultipleLeasesSameCredentialNoInterference(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	h1, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease1: %v", err)
	}
	defer func() { _ = h1.Cleanup() }()

	// Second lease for same credential in same run will hit O_EXCL conflict
	h2, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err == nil {
		defer func() { _ = h2.Cleanup() }()
		t.Fatal("second Lease for same credential in same run succeeded, want error (O_EXCL)")
	}
	// Different run would be separate, but test confirms isolation attempt
}

func TestAdversary_B7_T04_LeaseRejectsPathTraversalInCredentialID(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "../../etc/passwd", []byte(adversarySecretValue)); err != nil {
		t.Fatalf("Set traversal name: %v", err)
	}
	p := newAdversaryPolicy()
	// Note: policy credential ID is valid, but we pass traversal as credentialID param
	lease, err := NewDirectLease(DirectLeaseConfig{
		Store:      store,
		Policy:     p,
		ActiveRuns: []string{"run-active"},
		LeaseDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewDirectLease: %v", err)
	}

	_, err = lease.Lease(ctx, "run-active", "../../etc/passwd", "egress[0]")
	if err == nil {
		t.Fatal("Lease with traversal credentialID succeeded, want rejection")
	}
	if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "credential") {
		t.Fatalf("Lease error = %v, want unsafe path rejection", err)
	}
}

func TestAdversary_B7_T04_ReadLeaseRejectsArbitraryNonLeaseFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "arbitrary.txt")
	if err := os.WriteFile(path, []byte(adversarySecretValue), 0o600); err != nil {
		t.Fatalf("Write arbitrary: %v", err)
	}

	// Missing valid=true and other fields
	_, err := ReadLease(ctx, LeaseHandle{FilePath: path})
	if err == nil {
		t.Fatal("ReadLease on arbitrary file succeeded, want rejection")
	}
	if !strings.Contains(err.Error(), "valid lease handle") {
		t.Fatalf("ReadLease error = %v, want valid lease handle error", err)
	}
}

func TestAdversary_B7_T04_AuditEventsDoNotLeakSecretValue(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	defer func() { _ = handle.Cleanup() }()

	_, err = ReadLease(ctx, handle)
	if err != nil {
		t.Fatalf("ReadLease: %v", err)
	}

	for _, rec := range flow.audit.recordsSnapshot() {
		data, _ := json.Marshal(rec)
		if bytes.Contains(data, []byte(adversarySecretValue)) {
			t.Fatalf("audit event leaked secret: %s", data)
		}
	}
}

func TestAdversary_B7_T04_LeaseFileLeftBehindOnMissingCleanup(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	// Simulate crash: do NOT call Cleanup
	// In P1 this is a known limitation (no atexit/finalizer guarantee)
	if _, err := os.Stat(handle.FilePath); err != nil {
		t.Fatalf("lease file missing immediately (unexpected): %v", err)
	}
	// Documented limitation: file remains after process exit without explicit Cleanup
	t.Logf("CONFIRMED LIMITATION: lease file %s left behind when Cleanup not called (crash simulation)", handle.FilePath)
	// Cleanup for test hygiene
	_ = handle.Cleanup()
}

func TestAdversary_B7_T04_ConcurrentLeaseCreationNoRace(t *testing.T) {
	ctx := context.Background()
	flow := newAdversaryFlow(t)
	// Concurrent Lease on SAME credential exercises O_EXCL + locking paths without policy mismatch
	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
			if err != nil {
				errCh <- err
				return
			}
			_ = h.Cleanup()
		}()
	}
	wg.Wait()
	close(errCh)
	// Expect most to error (O_EXCL or duplicate), but no panics or data races under -race
	for e := range errCh {
		_ = e // errors expected
	}
}

func newAdversaryFlow(t *testing.T) *directLeaseFlow {
	t.Helper()
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "api-token", []byte(adversarySecretValue)); err != nil {
		t.Fatalf("Set secret: %v", err)
	}
	auditSink := &recordingAuditSink{}
	dir := t.TempDir()
	lease, err := NewDirectLease(DirectLeaseConfig{
		Store:      store,
		Policy:     newAdversaryPolicy(),
		ActiveRuns: []string{"run-active"},
		Audit:      auditSink,
		LeaseDir:   dir,
		Now: func() time.Time {
			return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewDirectLease: %v", err)
	}
	return &directLeaseFlow{lease: lease, audit: auditSink, dir: dir}
}

func newAdversaryPolicy() *policy.Policy {
	return &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "test-agent"},
		Egress: []policy.EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, Credential: "api-token"},
		},
		Credentials: []policy.Credential{
			{ID: "api-token", Type: "file_lease", Service: "test-store", Reason: "adversary test"},
		},
	}
}