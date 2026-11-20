package home

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ============================================================================
// ADVERSARY TEST SUITE — B2-T01: Local home layout and permissions
//
// These tests attempt to BREAK the security claims of the home package.
// ============================================================================

// ---- Attack Vector 1: Permission bypass via symlinks ----

// TestAdversarySymlinkHomeDir validates that Ensure() refuses to operate on
// a home path that is itself a symlink.
//
// SEVERITY: High
// If the home dir is a symlink to an attacker-controlled directory,
// Ensure() would create subdirectories and set permissions at the symlink
// target, enabling arbitrary writes outside the intended home tree.
func TestAdversarySymlinkHomeDir(t *testing.T) {
	// Create a target directory with 0777 perms
	targetDir := t.TempDir()
	if err := os.Chmod(targetDir, 0777); err != nil {
		t.Fatal(err)
	}

	// Create a symlink from a fake home to the target
	linkDir := filepath.Join(t.TempDir(), ".agentpaas")
	if err := os.Symlink(targetDir, linkDir); err != nil {
		t.Fatal(err)
	}

	hp := NewHomePaths(linkDir)

	// Ensure should refuse because paths.Home is a symlink.
	err := Ensure(hp)
	if err == nil {
		t.Fatal("Ensure() should have refused to follow a symlinked home path, but succeeded")
	}
	if !strings.Contains(err.Error(), "refusing to follow symlink") {
		t.Errorf("error should mention 'refusing to follow symlink', got: %v", err)
	}
	t.Logf("FIXED: Ensure() correctly refuses symlinked home path: %v", err)

	// The target directory should NOT have had its perms changed by Ensure.
	fi, err := os.Stat(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0777 {
		t.Errorf("symlink target perms were changed to %#o (should still be 0777 since Ensure refused)", fi.Mode().Perm())
	}
}

// TestAdversarySymlinkEscape validates that AGENTPAAS_HOME cannot be used to
// symlink-escape to system directories.
//
// SEVERITY: High
// If an attacker can create a symlink in the home path, Ensure() will
// follow it and create files at the target.
func TestAdversarySymlinkEscape(t *testing.T) {
	// Create a temp dir to simulate /etc
	etcDir := t.TempDir()
	if err := os.Chmod(etcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a symlink from an agentpaas home subdir pointing to a sensitive dir
	baseDir := t.TempDir()
	hp := NewHomePaths(baseDir)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Remove the config directory and replace with a symlink to etcDir
	_ = os.RemoveAll(hp.Config)
	if err := os.Symlink(etcDir, hp.Config); err != nil {
		t.Fatal(err)
	}

	// Running Ensure again should replace the symlink? Actually MkdirAll
	// with an existing symlink to a dir should treat it as existing.
	// But the Chmod would operate on the symlink target!
	err := Ensure(hp)
	if err != nil {
		t.Logf("Ensure() returned error on symlink subdir (may be expected): %v", err)
	}

	// Check if the etcDir got its permissions changed
	fi, err := os.Stat(etcDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Symlink target after Ensure: mode=%#o", fi.Mode().Perm())

	// This is a problem: Ensure modifies permissions on the symlink target
	// without verifying it's within the expected home directory.
	if fi.Mode().Perm() == 0700 && etcDir != hp.Config {
		t.Error("BREACH: Ensure() changed perms on symlink target outside home dir!" +
			" Ensure should NOT follow symlinks outside the home tree.")
	}
}

// ---- Attack Vector 2: Path traversal via AGENTPAAS_HOME ----

// TestAdversaryPathTraversal verifies that AGENTPAAS_HOME rejects path traversal
// sequences like "../../etc" and dangerous system paths.
//
// SEVERITY: High
// Now protected by ValidatePath() which rejects non-absolute and system paths.
func TestAdversaryPathTraversal(t *testing.T) {
	// Test that DiscoverHome rejects system paths
	_ = os.Setenv("AGENTPAAS_HOME", "/etc/agentpaas")
	defer func() { _ = os.Unsetenv("AGENTPAAS_HOME") }()

	_, err := DiscoverHome()
	if err == nil {
		t.Error("FAIL: AGENTPAAS_HOME=/etc/agentpaas should be rejected")
	} else {
		t.Logf("FIXED: system path /etc/agentpaas correctly rejected: %v", err)
	}

	// Test relative path traversal is rejected
	_ = os.Setenv("AGENTPAAS_HOME", "../../etc/agentpaas")

	_, err = DiscoverHome()
	if err == nil {
		t.Error("FAIL: AGENTPAAS_HOME=../../etc/agentpaas should be rejected")
	} else {
		t.Logf("FIXED: relative path ../../etc/agentpaas correctly rejected: %v", err)
	}
}

// ---- Attack Vector 3: PID reuse false negative ----

// TestAdversaryPidReuse verifies that IsStalePid returns false (not stale)
// when the PID in the file matches a live process that is NOT the daemon.
//
// SEVERITY: High
// PID reuse causes a false negative: IsStalePid thinks the old daemon is
// still running because a different process now has that PID.
func TestAdversaryPidReuse(t *testing.T) {
	hp := testHomePaths(t)

	// Write the test process's own PID — it's alive but not the daemon
	pid := os.Getpid()
	pidBytes := []byte(fmt.Sprintf("%d\n", pid))
	if err := os.WriteFile(hp.PID, pidBytes, 0600); err != nil {
		t.Fatal(err)
	}

	// IsStalePid says NOT stale — but this PID file was written by the test,
	// not by a daemon! The old daemon that owned PID X is long dead, but
	// a new process (this test) has reused PID X.
	stale, err := IsStalePid(hp.PID)
	if err != nil {
		t.Fatalf("IsStalePid() failed: %v", err)
	}
	if !stale {
		t.Log("BREACH/FALSE_NEGATIVE: PID reuse causes IsStalePid to return false (not stale)")
		t.Logf("  PID %d is reused by a different process (the test itself)", pid)
		t.Log("  CleanStale will NOT clean this stale PID file, and the")
		t.Log("  new daemon will fail to start thinking the old one is alive.")
	}
}

// ---- Attack Vector 4: TOCTOU race in CleanStale ----

// TestAdversaryCleanStalePidTOCTOU simulates a TOCTOU race: a daemon starts
// between IsStalePid's check and CleanStale's os.Remove, causing the live
// daemon's PID file to be deleted.
//
// SEVERITY: High
// There is a genuine race window between the staleness check and the file
// removal. In this test, we demonstrate the window exists by showing
// that CleanStale would remove a PID file that was written AFTER
// IsStalePid checked it.
func TestAdversaryCleanStalePidTOCTOU(t *testing.T) {
	hp := testHomePaths(t)

	// Step 1: Create a stale PID (old daemon's PID)
	if err := os.WriteFile(hp.PID, []byte("999999999\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Step 2: IsStalePid says stale
	stale, err := IsStalePid(hp.PID)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatal("expected stale PID")
	}

	// Step 3: Simulate a new daemon starting RIGHT HERE (after check, before remove)
	// Write a "live" PID to the file — the test process's PID
	newPid := os.Getpid()
	if err := os.WriteFile(hp.PID, []byte(fmt.Sprintf("%d\n", newPid)), 0600); err != nil {
		t.Fatal(err)
	}

	// Step 4: CleanStale re-checks IsStalePid — because the test process is NOT
	// agentpaasd, IsStalePid correctly returns stale (PID reuse protection).
	// So CleanStale removes the PID file, which is correct behavior.
	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() failed: %v", err)
	}

	// The PID file was correctly removed — the process that wrote it is NOT
	// agentpaasd, so it's not a live daemon.
	if _, err := os.Stat(hp.PID); !os.IsNotExist(err) {
		t.Error("PID file should have been removed — the writing process is not agentpaasd")
	} else {
		t.Log("FIXED: PID reuse protection ensures non-daemon PIDs are treated as stale, preventing TOCTOU race")
	}
}

// TestAdversaryCleanStaleSocketTOCTOU simulates a TOCTOU race where a daemon
// starts listening on the socket between IsStaleSocket's check and CleanStale's
// removal.
//
// SEVERITY: High
func TestAdversaryCleanStaleSocketTOCTOU(t *testing.T) {
	hp := shortTempDir(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Create a stale socket (plain file, not a listening socket)
	_ = os.Remove(hp.Socket)
	if err := os.WriteFile(hp.Socket, []byte("not a socket"), 0600); err != nil {
		t.Fatal(err)
	}

	// IsStaleSocket says stale
	stale, err := IsStaleSocket(hp.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatal("expected stale socket")
	}

	// Simulate: a daemon starts listening between check and remove
	// Remove the stale file and create a real listener
	_ = os.Remove(hp.Socket)
	ln, err := net.Listen("unix", hp.Socket)
	if err != nil {
		t.Fatalf("cannot start listener: %v", err)
	}
	defer func() { _ = ln.Close() }()
	defer func() { _ = os.Remove(hp.Socket) }()

	// CleanStale now re-checks staleness, finds a live listener, and
	// correctly refuses to remove the socket.
	err = CleanStale(hp)
	if err == nil {
		t.Fatal("CleanStale should return error when socket is live (TOCTOU protected)")
	}
	if !strings.Contains(err.Error(), "live process") {
		t.Errorf("error should mention 'live process', got: %v", err)
	}
	t.Logf("FIXED: CleanStale correctly refuses to remove live socket (TOCTOU protected): %v", err)

	// Socket still exists
	if _, err := os.Stat(hp.Socket); os.IsNotExist(err) {
		t.Error("live socket was incorrectly removed")
	}
}

// ---- Attack Vector 5: Attacker plants live listener to prevent socket cleanup ----

// TestAdversarySocketPlantedListener validates that an attacker can prevent
// CleanStale from removing a stale socket by planting a real listener on it.
//
// SEVERITY: Medium
// If an attacker can access the socket path, they can start a listener
// and prevent cleanup, causing denial of service for the legitimate daemon.
func TestAdversarySocketPlantedListener(t *testing.T) {
	hp := shortTempDir(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Remove the empty socket file created by Ensure
	_ = os.Remove(hp.Socket)

	// Attacker starts a listener on the stale socket path
	ln, err := net.Listen("unix", hp.Socket)
	if err != nil {
		t.Fatalf("attacker cannot start listener: %v", err)
	}
	defer func() { _ = ln.Close() }()
	defer func() { _ = os.Remove(hp.Socket) }()

	// IsStaleSocket says NOT stale (because something IS listening)
	stale, err := IsStaleSocket(hp.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Error("expected NOT stale — attacker's listener is live")
	}

	// CleanStale returns an error because a live process holds the socket
	err = CleanStale(hp)
	if err == nil {
		t.Fatal("CleanStale should return error when socket is held by a live process")
	}
	if !strings.Contains(err.Error(), "live process") {
		t.Errorf("error should mention 'live process', got: %v", err)
	}
	t.Logf("FIXED: CleanStale returns error preventing socket DoS: %v", err)

	// Socket still exists — correctly not removed
	if _, err := os.Stat(hp.Socket); os.IsNotExist(err) {
		t.Fatal("socket was removed despite having a live listener")
	}
}

// ---- Attack Vector 6: Attacker plants attacker PID to prevent cleanup ----

// TestAdversaryPidPlantedByAttacker validates that an attacker can prevent
// CleanStale from removing a stale PID file by writing a PID that matches
// any running process.
//
// SEVERITY: Medium
func TestAdversaryPidPlantedByAttacker(t *testing.T) {
	hp := testHomePaths(t)

	// Attacker writes the PID of a common system process (e.g., PID 1 or
	// any long-running process) to the PID file, making it appear live.
	if err := os.WriteFile(hp.PID, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}

	// IsStalePid says NOT stale (because PID is alive), but with PID reuse
	// protection, the process name check ensures a non-daemon process is
	// correctly treated as stale.
	stale, err := IsStalePid(hp.PID)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("FAIL: PID reuse protection failed — IsStalePid should return stale" +
			" for a PID belonging to a non-daemon process")
	}
	t.Log("FIXED: PID reuse protection correctly detects non-daemon processes")

	// CleanStale removes it
	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() failed: %v", err)
	}

	// PID file should be gone
	if _, err := os.Stat(hp.PID); !os.IsNotExist(err) {
		t.Fatal("PID file should have been removed as stale")
	}
	t.Log("FIXED: Attacker-planted live PID file is correctly cleaned up by CleanStale")
}

// ---- Attack Vector 7: Empty/malformed PID file edge cases ----

// TestAdversaryPidEmptyFile validates behavior with an empty PID file.
func TestAdversaryPidEmptyFile(t *testing.T) {
	hp := testHomePaths(t)

	// Empty PID file
	if err := os.WriteFile(hp.PID, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	stale, err := IsStalePid(hp.PID)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("expected stale for empty PID file")
	}

	// Whitespace-only PID file
	if err := os.WriteFile(hp.PID, []byte("   \n\t\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stale, err = IsStalePid(hp.PID)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("expected stale for whitespace-only PID file")
	}
}

// TestAdversaryPidMalformed validates that non-numeric, negative,
// and zero PID values are handled.
func TestAdversaryPidMalformed(t *testing.T) {
	hp := testHomePaths(t)

	tests := []struct {
		name string
		data string
	}{
		{"non-numeric", "abcdef\n"},
		{"negative", "-1\n"},
		{"zero", "0\n"},
		{"hex", "0x1234\n"},
		{"float", "3.14\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(hp.PID, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			stale, err := IsStalePid(hp.PID)
			if err != nil {
				t.Fatalf("IsStalePid() failed: %v", err)
			}
			if !stale {
				t.Errorf("expected stale for malformed PID (%s), got not stale", tt.name)
			}
		})
	}
}

// TestAdversaryPidVeryLarge validates behavior with a very large PID value.
//
// strconv.Atoi will overflow on 64-bit for values > 2^63-1, but for smaller
// large values it succeeds and FindProcess+Signal(0) returns "not found".
func TestAdversaryPidVeryLarge(t *testing.T) {
	hp := testHomePaths(t)

	// Very large but valid integer
	if err := os.WriteFile(hp.PID, []byte("2147483647\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stale, err := IsStalePid(hp.PID)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("expected stale for large non-existent PID")
	}
}

// ---- Attack Vector 8: No parent directory permission check ----

// TestAdversaryNoParentDirCheck validates that the code does NOT check
// parent directory permissions. An attacker with write access to the
// parent directory can replace the home dir with a symlink.
//
// SEVERITY: Medium
// The code never checks if the parent directory of the home dir has
// safe permissions.
func TestAdversaryNoParentDirCheck(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	parentDir := filepath.Dir(hp.Home)

	// Show that parent dir permissions are never checked
	fi, err := os.Stat(parentDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Parent dir %s perms: %#o (not checked by code)", parentDir, fi.Mode().Perm())

	// The code doesn't validate parent at all — this is a design gap.
	// On a multi-user system, if a user creates ~/.agentpaas with 0700
	// but the parent (~) is world-readable, an attacker can't directly
	// access the home because of the 0700. But if the attacker has
	// write access to the parent, they could replace the home dir
	// entirely.
	t.Log("NOTE: Parent directory permissions are never validated. If an attacker" +
		" has write access to the parent dir, they can hijack the home directory.")
}

// ---- Attack Vector 9: Concurrent Ensure race ----

// TestAdversaryConcurrentEnsure runs two goroutines calling Ensure()
// simultaneously to detect races.
//
// SEVERITY: Medium
// No mutex is held during Ensure, so file creation and chmod operations
// on the same paths could race.
func TestAdversaryConcurrentEnsure(t *testing.T) {
	hp := testHomePaths(t)

	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Ensure(hp); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	var hadErrors bool
	for err := range errs {
		hadErrors = true
		t.Errorf("concurrent Ensure() error: %v", err)
	}

	if !hadErrors {
		// Check that the resulting state is consistent
		checkDirs := []string{hp.Home, hp.Logs, hp.State, hp.Config, hp.Cache, hp.Tmp}
		for _, d := range checkDirs {
			fi, err := os.Stat(d)
			if err != nil {
				t.Errorf("dir %s missing after concurrent Ensure: %v", d, err)
				continue
			}
			if fi.Mode().Perm() != 0700 {
				t.Errorf("dir %s perms = %#o, want 0700", d, fi.Mode().Perm())
			}
		}
		// Socket is NOT created by Ensure (daemon creates it via net.Listen).
		checkFiles := []string{hp.PID, hp.Lock}
		for _, f := range checkFiles {
			fi, err := os.Stat(f)
			if err != nil {
				t.Errorf("file %s missing after concurrent Ensure: %v", f, err)
				continue
			}
			if fi.Mode().Perm() != 0600 {
				t.Errorf("file %s perms = %#o, want 0600", f, fi.Mode().Perm())
			}
		}
	}
}

// ---- Attack Vector 10: Ensure + ValidatePermissions race ----

// TestAdversaryEnsureValidateRace validates that a hostile process can
// change permissions between Ensure and ValidatePermissions.
//
// SEVERITY: Medium
// There's no atomic operation that both creates and validates permissions.
func TestAdversaryEnsureValidateRace(t *testing.T) {
	hp := testHomePaths(t)

	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Immediately after Ensure (which sets 0700), ValidatePermissions
	// should see 0700. But if another process changes perms to 0755
	// between Ensure and ValidatePermissions, the check catches it.
	// This validates the check, not a vulnerability — the gap is
	// that Ensure sets 0700, then ValidatePermissions verifies, but
	// between those calls someone could change perms again.
	//
	// The actual window is: Chmod(0700) happens, then some attacker
	// does Chmod(0755), then ValidatePermissions reads 0755 and rejects.
	// That works correctly.
	//
	// BUT the reverse is dangerous: attacker could set 0755 *after*
	// ValidatePermissions but *before* the daemon starts serving.
	if err := os.Chmod(hp.Home, 0755); err != nil {
		t.Fatal(err)
	}

	// ValidatePermissions should catch this
	err := ValidatePermissions(hp)
	if err == nil {
		t.Error("BREACH: ValidatePermissions did NOT detect 0755 on home dir after Ensure!")
	} else {
		t.Logf("ValidatePermissions correctly detected perm change: %v", err)
	}
}

// TestAdversarySocketPermsChangedAfterEnsure validates that if socket perms
// are widened after Ensure, ValidatePermissions catches it.
func TestAdversarySocketPermsChangedAfterEnsure(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Ensure() no longer creates the socket file (the daemon does via
	// net.Listen). Create a plain file so we can test perm validation.
	if err := os.WriteFile(hp.Socket, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	// Attacker widens socket perms
	if err := os.Chmod(hp.Socket, 0777); err != nil {
		t.Fatal(err)
	}

	// ValidatePermissions should catch it
	err := ValidatePermissions(hp)
	if err == nil {
		t.Error("BREACH: ValidatePermissions did NOT detect 0777 on socket!")
	} else {
		t.Logf("ValidatePermissions correctly detected socket perm change: %v", err)
	}
}

// TestAdversarySymlinkSocket validates that creating a symlink socket pointing
// elsewhere could trick the daemon.
func TestAdversarySymlinkSocket(t *testing.T) {
	hp := shortTempDir(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Remove the socket and replace with a symlink to a sensitive location
	_ = os.Remove(hp.Socket)
	sensitiveFile := filepath.Join(t.TempDir(), "sensitive")
	if err := os.WriteFile(sensitiveFile, []byte("secret data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sensitiveFile, hp.Socket); err != nil {
		t.Fatal(err)
	}

	// What does IsStaleSocket do with a symlink?
	// os.Stat follows symlinks, finds a regular file, not a socket.
	// net.Dial("unix", ...) on a regular file fails, so returns stale.
	// That's correct.
	stale, err := IsStaleSocket(hp.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("expected stale for symlink pointing to regular file")
	}

	// CleanStale would remove the symlink (the socket path), not the target.
	// This is actually correct behavior — it cleans the stale path.
	if err := CleanStale(hp); err != nil {
		t.Fatalf("CleanStale() failed: %v", err)
	}
	if _, err := os.Stat(hp.Socket); !os.IsNotExist(err) {
		t.Error("symlink socket should have been removed by CleanStale")
	}
	// The target file should still exist
	if _, err := os.Stat(sensitiveFile); os.IsNotExist(err) {
		t.Error("CleanStale should NOT follow symlinks when removing — target was also removed!")
	}
	t.Log("CleanStale removed symlink but target still exists — correct behavior.")
}

// ---- Attack Vector 11: DiscoverHome does not resolve symlinks or validate ----

// TestAdversaryDiscoverHomeNoValidation validates that DiscoverHome rejects
// dangerous or relative paths via ValidatePath.
func TestAdversaryDiscoverHomeNoValidation(t *testing.T) {
	dangerousPaths := []string{
		"/etc",
		"/var/run",
		"../../etc",
		"../.ssh",
		"/tmp/../etc",
		"/dev/shm",
		"/root",
		"~otheruser",
	}
	safePaths := []string{
		"/tmp/agentpaas",
		"/var/folders/agentpaas",
	}
	for _, p := range dangerousPaths {
		t.Run(strings.ReplaceAll(p, "/", "_"), func(t *testing.T) {
			_ = os.Setenv("AGENTPAAS_HOME", p)
			defer func() { _ = os.Unsetenv("AGENTPAAS_HOME") }()
			_, err := DiscoverHome()
			if err == nil {
				t.Errorf("FAIL: dangerous path %q was accepted with no validation", p)
			} else {
				t.Logf("FIXED: dangerous path %q correctly rejected: %v", p, err)
			}
		})
	}
	for _, p := range safePaths {
		t.Run("safe_"+strings.ReplaceAll(p, "/", "_"), func(t *testing.T) {
			_ = os.Setenv("AGENTPAAS_HOME", p)
			defer func() { _ = os.Unsetenv("AGENTPAAS_HOME") }()
			home, err := DiscoverHome()
			if err != nil {
				t.Errorf("safe path %q was rejected: %v", p, err)
				return
			}
			if home != p {
				t.Errorf("expected %s, got %s", p, home)
			}
		})
	}
}

// ---- Attack Vector 12: DiscoverSocketPath via env var also unvalidated ----

// TestAdversaryDiscoverSocketPathNoValidation validates that AGENTPAAS_SOCKET
// also rejects dangerous paths via validation.
func TestAdversaryDiscoverSocketPathNoValidation(t *testing.T) {
	hp := testHomePaths(t)

	dangerousPaths := []string{
		"/etc/daemon.sock",
		"/var/run/daemon.sock",
		"/tmp/../etc/daemon.sock",
	}
	for _, p := range dangerousPaths {
		t.Run(strings.ReplaceAll(p, "/", "_"), func(t *testing.T) {
			_ = os.Setenv("AGENTPAAS_SOCKET", p)
			defer func() { _ = os.Unsetenv("AGENTPAAS_SOCKET") }()

			hp2 := NewHomePaths(hp.Home)
			if hp2.Socket == p {
				t.Errorf("FAIL: dangerous socket path %q was accepted with no validation", p)
			} else {
				t.Logf("FIXED: dangerous socket path %q correctly rejected, fell back to default: %s", p, hp2.Socket)
			}
		})
	}

	// Also test that safe custom paths still work
	_ = os.Setenv("AGENTPAAS_SOCKET", "/tmp/mydaemon.sock")
	defer func() { _ = os.Unsetenv("AGENTPAAS_SOCKET") }()
	hp2 := NewHomePaths(hp.Home)
	if hp2.Socket != "/tmp/mydaemon.sock" {
		t.Errorf("safe custom socket path was not accepted: got %s, want /tmp/mydaemon.sock", hp2.Socket)
	} else {
		t.Log("FIXED: safe custom socket paths are accepted")
	}
}

// ---- Attack Vector 13: Excessive perms on subdirectories not checked ----

// TestAdversarySubdirPermsNotChecked validates that ValidatePermissions
// only checks the home dir and socket, NOT the subdirectory permissions.
//
// SEVERITY: Low
// Subdir perms are set by Ensure but not verified by ValidatePermissions.
func TestAdversarySubdirPermsNotChecked(t *testing.T) {
	hp := testHomePaths(t)
	if err := Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Widen subdir perms
	for _, d := range []string{hp.Logs, hp.State, hp.Config, hp.Cache, hp.Tmp} {
		if err := os.Chmod(d, 0777); err != nil {
			t.Fatal(err)
		}
	}

	// ValidatePermissions only checks home dir and socket — not subdirs
	err := ValidatePermissions(hp)
	if err != nil {
		t.Fatalf("ValidatePermissions() failed (unexpected): %v", err)
	}
	t.Log("NOTE: ValidatePermissions does NOT check subdirectory permissions." +
		" Subdirs with 0777 permissions are not detected.")
}

// ---- Attack Vector 14: Double symlink in home dir ----

// TestAdversaryDoubleSymlink validates that Ensure() refuses to operate
// when the home root path is a double-symlink chain (symlink-to-symlink).
func TestAdversaryDoubleSymlink(t *testing.T) {
	// Create structure:
	// /tmp/base/home -> /tmp/middle -> /tmp/target
	baseDir := t.TempDir()
	middleDir := t.TempDir()
	targetDir := t.TempDir()

	// Target has 0777 perms
	if err := os.Chmod(targetDir, 0777); err != nil {
		t.Fatal(err)
	}

	// Middle is a symlink to target
	if err := os.Symlink(targetDir, filepath.Join(middleDir, "link")); err != nil {
		t.Fatal(err)
	}

	// Home is a symlink to middle/link (double symlink)
	homeLink := filepath.Join(baseDir, "home")
	if err := os.Symlink(filepath.Join(middleDir, "link"), homeLink); err != nil {
		t.Fatal(err)
	}

	hp := NewHomePaths(homeLink)

	// Ensure should refuse because paths.Home is a symlink.
	err := Ensure(hp)
	if err == nil {
		t.Fatal("Ensure() should have refused double-symlink chain home path")
	}
	if !strings.Contains(err.Error(), "refusing to follow symlink") {
		t.Errorf("error should mention 'refusing to follow symlink', got: %v", err)
	}
	t.Logf("FIXED: Ensure() correctly refuses double-symlink chain home path: %v", err)

	// Ensure did NOT create subdirs at the symlink target (because it refused).
	if _, err := os.Stat(filepath.Join(targetDir, "logs")); !os.IsNotExist(err) {
		t.Error("Ensure should NOT have created subdirs in the symlink target")
	}
}