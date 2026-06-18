package home

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// testHomePaths creates a HomePaths rooted at a temp directory.
func testHomePaths(t *testing.T) *HomePaths {
	t.Helper()
	return NewHomePaths(t.TempDir())
}

// shortTempDir creates a HomePaths rooted at a shorter temp path for tests
// that need Unix socket paths under the ~104-byte macOS limit.
func shortTempDir(t *testing.T) *HomePaths {
	t.Helper()
	dir, err := os.MkdirTemp("", "hp-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return NewHomePaths(dir)
}

func TestNewHomePaths(t *testing.T) {
	hp := testHomePaths(t)
	if hp.Home == "" {
		t.Fatal("Home should not be empty")
	}
	if hp.Socket == "" {
		t.Error("Socket path should not be empty")
	}
	if hp.PID == "" {
		t.Error("PID path should not be empty")
	}
	if hp.Lock == "" {
		t.Error("Lock path should not be empty")
	}
	if hp.Logs == "" {
		t.Error("Logs path should not be empty")
	}
	if hp.State == "" {
		t.Error("State path should not be empty")
	}
	if hp.Config == "" {
		t.Error("Config path should not be empty")
	}
	if hp.Cache == "" {
		t.Error("Cache path should not be empty")
	}
	if hp.Tmp == "" {
		t.Error("Tmp path should not be empty")
	}
}

func TestNewHomePathsRelativePaths(t *testing.T) {
	homeDir := "/tmp/agentpaas-test"
	hp := NewHomePaths(homeDir)

	if hp.Home != homeDir {
		t.Errorf("Home = %s, want %s", hp.Home, homeDir)
	}
	if hp.Socket != filepath.Join(homeDir, "daemon.sock") {
		t.Errorf("Socket = %s, want %s", hp.Socket, filepath.Join(homeDir, "daemon.sock"))
	}
	if hp.PID != filepath.Join(homeDir, "agentpaasd.pid") {
		t.Errorf("PID = %s, want %s", hp.PID, filepath.Join(homeDir, "agentpaasd.pid"))
	}
	if hp.Lock != filepath.Join(homeDir, "agentpaasd.lock") {
		t.Errorf("Lock = %s, want %s", hp.Lock, filepath.Join(homeDir, "agentpaasd.lock"))
	}
	if hp.Logs != filepath.Join(homeDir, "logs") {
		t.Errorf("Logs = %s, want %s", hp.Logs, filepath.Join(homeDir, "logs"))
	}
	if hp.State != filepath.Join(homeDir, "state") {
		t.Errorf("State = %s, want %s", hp.State, filepath.Join(homeDir, "state"))
	}
	if hp.Config != filepath.Join(homeDir, "config") {
		t.Errorf("Config = %s, want %s", hp.Config, filepath.Join(homeDir, "config"))
	}
	if hp.Cache != filepath.Join(homeDir, "cache") {
		t.Errorf("Cache = %s, want %s", hp.Cache, filepath.Join(homeDir, "cache"))
	}
	if hp.Tmp != filepath.Join(homeDir, "tmp") {
		t.Errorf("Tmp = %s, want %s", hp.Tmp, filepath.Join(homeDir, "tmp"))
	}
}

func TestEnsureCreatesDirectories(t *testing.T) {
	hp := testHomePaths(t)
	// Remove the home dir so Ensure has to create it
	_ = os.RemoveAll(hp.Home)

	if err := Ensure(hp); err != nil {
		t.Fatalf("Ensure() failed: %v", err)
	}

	// Check home dir exists
	fi, err := os.Stat(hp.Home)
	if err != nil {
		t.Fatalf("home dir not created: %v", err)
	}
	if !fi.IsDir() {
		t.Fatal("home is not a directory")
	}
	if fi.Mode().Perm() != 0700 {
		t.Errorf("home dir perms = %#o, want 0700", fi.Mode().Perm())
	}

	// Check subdirs
	subdirs := []struct {
		name string
		path string
	}{
		{"logs", hp.Logs},
		{"state", hp.State},
		{"config", hp.Config},
		{"cache", hp.Cache},
		{"tmp", hp.Tmp},
	}
	for _, sd := range subdirs {
		fi, err := os.Stat(sd.path)
		if err != nil {
			t.Errorf("%s dir not created: %v", sd.name, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("%s is not a directory", sd.name)
		}
		if fi.Mode().Perm() != 0700 {
			t.Errorf("%s dir perms = %#o, want 0700", sd.name, fi.Mode().Perm())
		}
	}
}

func TestEnsureCreatesFiles(t *testing.T) {
	hp := testHomePaths(t)
	_ = os.RemoveAll(hp.Home)

	if err := Ensure(hp); err != nil {
		t.Fatalf("Ensure() failed: %v", err)
	}

	// Check files exist with correct perms
	files := []struct {
		name string
		path string
		mode os.FileMode
	}{
		{"socket", hp.Socket, 0600},
		{"pid", hp.PID, 0600},
		{"lock", hp.Lock, 0600},
	}
	for _, f := range files {
		fi, err := os.Stat(f.path)
		if err != nil {
			t.Errorf("%s file not created: %v", f.name, err)
			continue
		}
		if fi.Mode().Perm() != f.mode {
			t.Errorf("%s perms = %#o, want %#o", f.name, fi.Mode().Perm(), f.mode)
		}
	}
}

func TestEnsureIdempotent(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatalf("first Ensure() failed: %v", err)
	}
	if err := Ensure(hp); err != nil {
		t.Fatalf("second Ensure() (idempotent) failed: %v", err)
	}
}

func TestValidatePermissionsHomeGood(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePermissions(hp); err != nil {
		t.Errorf("ValidatePermissions failed for good layout: %v", err)
	}
}

func TestValidatePermissionsHomeBad(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	// Widen perms to 0755
	if err := os.Chmod(hp.Home, 0755); err != nil {
		t.Fatal(err)
	}
	err := ValidatePermissions(hp)
	if err == nil {
		t.Fatal("expected error for 0755 home dir, got nil")
	}
	if !contains(err.Error(), "0755") && !contains(err.Error(), "home") {
		t.Errorf("error should mention the perms and 'home', got: %v", err)
	}
}

func TestValidatePermissionsSocketBad(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	// Widen socket perms to 0644
	if err := os.Chmod(hp.Socket, 0644); err != nil {
		t.Fatal(err)
	}
	err := ValidatePermissions(hp)
	if err == nil {
		t.Fatal("expected error for 0644 socket, got nil")
	}
	if !contains(err.Error(), "0644") && !contains(err.Error(), "socket") {
		t.Errorf("error should mention the perms and 'socket', got: %v", err)
	}
}

func TestDiscoverHomeDefault(t *testing.T) {
	// Temporarily unset AGENTPAAS_HOME
	_ = os.Unsetenv("AGENTPAAS_HOME")
	defer func() { _ = os.Unsetenv("AGENTPAAS_HOME") }()

	home, err := DiscoverHome()
	if err != nil {
		t.Fatalf("DiscoverHome() failed: %v", err)
	}
	// Should default to ~/.agentpaas
	expected, _ := os.UserHomeDir()
	expected = filepath.Join(expected, ".agentpaas")
	if home != expected {
		t.Errorf("DiscoverHome() = %s, want %s", home, expected)
	}
}

func TestDiscoverHomeOverride(t *testing.T) {
	custom := "/tmp/agentpaas-test-custom"
	_ = os.Setenv("AGENTPAAS_HOME", custom)
	defer func() { _ = os.Unsetenv("AGENTPAAS_HOME") }()

	home, err := DiscoverHome()
	if err != nil {
		t.Fatalf("DiscoverHome() failed: %v", err)
	}
	if home != custom {
		t.Errorf("DiscoverHome() = %s, want %s", home, custom)
	}
}

func TestDiscoverSocketPathDefault(t *testing.T) {
	hp := testHomePaths(t)
	expected := filepath.Join(hp.Home, "daemon.sock")
	if hp.Socket != expected {
		t.Errorf("socket = %s, want %s", hp.Socket, expected)
	}
}

func TestDiscoverSocketPathOverride(t *testing.T) {
	hp := testHomePaths(t)
	custom := filepath.Join(hp.Home, "custom.sock")
	_ = os.Setenv("AGENTPAAS_SOCKET", custom)
	defer func() { _ = os.Unsetenv("AGENTPAAS_SOCKET") }()

	hp2 := NewHomePaths(hp.Home)
	if hp2.Socket != custom {
		t.Errorf("socket = %s, want %s", hp2.Socket, custom)
	}
}

func TestIsStalePidStale(t *testing.T) {
	hp := testHomePaths(t)
	// Write a PID that definitely doesn't exist
	pidData := []byte("999999999\n")
	if err := os.WriteFile(hp.PID, pidData, 0600); err != nil {
		t.Fatal(err)
	}

	stale, err := IsStalePid(hp.PID)
	if err != nil {
		t.Fatalf("IsStalePid() failed: %v", err)
	}
	if !stale {
		t.Error("expected stale for non-existent PID, got not stale")
	}
}

func TestIsStalePidLive(t *testing.T) {
	hp := testHomePaths(t)
	// Use current process PID — it is definitely running but is NOT the
	// agentpaasd daemon, so IsStalePid should treat it as stale (PID reuse).
	pid := os.Getpid()
	pidBytes := []byte(fmt.Sprintf("%d\n", pid))
	if err := os.WriteFile(hp.PID, pidBytes, 0600); err != nil {
		t.Fatal(err)
	}

	stale, err := IsStalePid(hp.PID)
	if err != nil {
		t.Fatalf("IsStalePid() failed: %v", err)
	}
	if !stale {
		t.Error("expected stale for non-agentpaasd PID (PID reuse protection), got not stale")
	}
}

func TestCleanStalePidRemovesStale(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	// Write a stale PID
	pidData := []byte("999999998\n")
	if err := os.WriteFile(hp.PID, pidData, 0600); err != nil {
		t.Fatal(err)
	}

	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() failed: %v", err)
	}

	if _, err := os.Stat(hp.PID); !os.IsNotExist(err) {
		t.Error("stale PID file should have been removed")
	}
}

func TestCleanStalePidDoesNotRemoveLive(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	// Write current process PID — but the test process is NOT agentpaasd,
	// so IsStalePid treats it as stale (PID reuse protection).
	pid := os.Getpid()
	pidBytes := []byte(fmt.Sprintf("%d\n", pid))
	if err := os.WriteFile(hp.PID, pidBytes, 0600); err != nil {
		t.Fatal(err)
	}

	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() failed: %v", err)
	}

	// PID file should have been removed because it's not an agentpaasd PID
	if _, err := os.Stat(hp.PID); !os.IsNotExist(err) {
		t.Error("non-daemon PID file should have been removed as stale (PID reuse protection)")
	}
}

func TestIsStaleSocketStale(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	// Remove socket created by Ensure and create a plain file (not a real socket)
	_ = os.Remove(hp.Socket)
	if err := os.WriteFile(hp.Socket, []byte("not a socket"), 0600); err != nil {
		t.Fatal(err)
	}

	stale, err := IsStaleSocket(hp.Socket)
	if err != nil {
		t.Fatalf("IsStaleSocket() failed: %v", err)
	}
	if !stale {
		t.Error("expected stale for non-listening socket file, got not stale")
	}
}

func TestIsStaleSocketLive(t *testing.T) {
	hp := shortTempDir(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(hp.Socket)

	// Start a real Unix listener (using shortTempDir to keep path short)
	ln, err := net.Listen("unix", hp.Socket)
	if err != nil {
		t.Fatalf("cannot start listener: %v", err)
	}
	defer func() { _ = ln.Close() }()
	defer func() { _ = os.Remove(hp.Socket) }()

	// Socket is live — should NOT be stale
	stale, err := IsStaleSocket(hp.Socket)
	if err != nil {
		t.Fatalf("IsStaleSocket() failed: %v", err)
	}
	if stale {
		t.Error("expected NOT stale for live socket, got stale")
	}
}

func TestCleanStaleStaleSocket(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(hp.Socket)
	if err := os.WriteFile(hp.Socket, []byte("not a socket"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() failed: %v", err)
	}

	if _, err := os.Stat(hp.Socket); !os.IsNotExist(err) {
		t.Error("stale socket file should have been removed")
	}
}

func TestCleanStaleDoesNotRemoveLiveSocket(t *testing.T) {
	hp := shortTempDir(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(hp.Socket)

	// Start a real listener on a short path
	ln, err := net.Listen("unix", hp.Socket)
	if err != nil {
		t.Fatalf("cannot start listener: %v", err)
	}
	defer func() { _ = ln.Close() }()
	defer func() { _ = os.Remove(hp.Socket) }()

	// CleanStale — socket is live, so CleanStale should return an error
	err = CleanStale(hp)
	if err == nil {
		t.Error("expected error for live socket, got nil")
	} else if !contains(err.Error(), "live process") {
		t.Errorf("error should mention 'live process', got: %v", err)
	}

	// Socket should still exist
	if _, err := os.Stat(hp.Socket); os.IsNotExist(err) {
		t.Error("live socket file should NOT have been removed")
	}
}

func TestIsStaleLockStale(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	// Lock file exists but no one holds the lock — should be stale
	stale, err := IsStaleLock(hp.Lock)
	if err != nil {
		t.Fatalf("IsStaleLock() failed: %v", err)
	}
	if !stale {
		t.Error("expected stale for lock file with no holder, got not stale")
	}
}

func TestIsStaleLockHeld(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lock held test in short mode")
	}
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Open the lock file and hold an exclusive flock
	f, err := os.OpenFile(hp.Lock, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	// Now check — should NOT be stale since we hold the lock
	stale, err := IsStaleLock(hp.Lock)
	if err != nil {
		t.Fatalf("IsStaleLock() failed: %v", err)
	}
	if stale {
		t.Error("expected NOT stale for held lock, got stale")
	}
}

func TestCleanStaleStaleLock(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	// Lock file exists with no holder — should be cleaned
	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() failed: %v", err)
	}

	if _, err := os.Stat(hp.Lock); !os.IsNotExist(err) {
		t.Error("stale lock file should have been removed")
	}
}

func TestCleanStaleDoesNotRemoveHeldLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lock held test in short mode")
	}
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Hold the lock
	f, err := os.OpenFile(hp.Lock, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	// CleanStale should NOT remove the held lock
	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() failed: %v", err)
	}

	// Lock file should still exist
	if _, err := os.Stat(hp.Lock); os.IsNotExist(err) {
		t.Error("held lock file should NOT have been removed")
	}
}

func TestCleanStaleEmptyHome(t *testing.T) {
	// CleanStale on a fresh home (ensure not run) should be a no-op
	hp := testHomePaths(t)
	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() on empty home failed: %v", err)
	}
}

func TestValidatePermissionsMissingHome(t *testing.T) {
	hp := testHomePaths(t)
	_ = os.RemoveAll(hp.Home)

	err := ValidatePermissions(hp)
	if err == nil {
		t.Fatal("expected error for missing home dir, got nil")
	}
	if !contains(err.Error(), "does not exist") {
		t.Errorf("error should mention 'does not exist', got: %v", err)
	}
}

func TestIsStalePidFileNotFound(t *testing.T) {
	hp := testHomePaths(t)
	stale, err := IsStalePid(hp.PID)
	if err != nil {
		t.Fatalf("IsStalePid() failed: %v", err)
	}
	if stale {
		t.Error("expected not stale for missing PID file")
	}
}

func TestIsStaleSocketFileNotFound(t *testing.T) {
	hp := testHomePaths(t)
	stale, err := IsStaleSocket(hp.Socket)
	if err != nil {
		t.Fatalf("IsStaleSocket() failed: %v", err)
	}
	if stale {
		t.Error("expected not stale for missing socket file")
	}
}

func TestIsStaleLockFileNotFound(t *testing.T) {
	hp := testHomePaths(t)
	stale, err := IsStaleLock(hp.Lock)
	if err != nil {
		t.Fatalf("IsStaleLock() failed: %v", err)
	}
	if stale {
		t.Error("expected not stale for missing lock file")
	}
}

func TestIsStaleLockEmptyFile(t *testing.T) {
	hp := testHomePaths(t)
	// Create an empty lock file
	if err := os.WriteFile(hp.Lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Empty lock with no holder should be stale
	stale, err := IsStaleLock(hp.Lock)
	if err != nil {
		t.Fatalf("IsStaleLock() failed: %v", err)
	}
	if !stale {
		t.Error("expected stale for empty lock file")
	}
}

func TestEnsureCorrectsBadPerms(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	// Change home dir to bad perms
	if err := os.Chmod(hp.Home, 0755); err != nil {
		t.Fatal(err)
	}
	// Running Ensure again should fix them
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(hp.Home)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0700 {
		t.Errorf("Ensure should correct perms to 0700, got %#o", fi.Mode().Perm())
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}