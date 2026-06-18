package home

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// HomePaths holds every path in the agentpaas home directory layout.
// All paths are absolute and derive from a single root (Home).
//
// Callers should obtain a HomePaths via NewHomePaths(DiscoverHome()) and
// then pass it to Ensure(), ValidatePermissions(), and CleanStale().
type HomePaths struct {
	// Home is the root agentpaas data directory (~/.agentpaas by default).
	Home string

	// Socket is the Unix domain socket path for daemon control.
	Socket string

	// PID is the path to the daemon's PID file.
	PID string

	// Lock is the path to the daemon's flock file.
	Lock string

	// Logs is the directory for daemon log output.
	Logs string

	// State is the directory for persistent runtime state.
	State string

	// Config is the directory for user-provided configuration files.
	Config string

	// Cache is the directory for cached data (templates, compiled rules, etc.).
	Cache string

	// Tmp is the directory for ephemeral scratch files.
	Tmp string
}

const (
	homeDirName  = ".agentpaas"
	socketName   = "daemon.sock"
	pidName      = "agentpaasd.pid"
	lockName     = "agentpaasd.lock"
	logsDirName  = "logs"
	stateDirName = "state"
	configDirName = "config"
	cacheDirName = "cache"
	tmpDirName   = "tmp"

	// homePerm is the required permission mode for the home directory.
	homePerm = os.FileMode(0700)

	// socketPerm is the required permission mode for the socket file.
	socketPerm = os.FileMode(0600)

	// subdirPerm is the required permission mode for all subdirectories.
	subdirPerm = os.FileMode(0700)

	// filePerm is the required permission mode for PID and lock files.
	filePerm = os.FileMode(0600)
)

// DiscoverHome returns the agentpaas home directory.
//
// It checks the AGENTPAAS_HOME environment variable first. If unset, it
// defaults to ~/.agentpaas (the user's home directory joined with
// ".agentpaas").
func DiscoverHome() (string, error) {
	if h := os.Getenv("AGENTPAAS_HOME"); h != "" {
		return h, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home: cannot determine user home directory: %w", err)
	}
	return filepath.Join(userHome, homeDirName), nil
}

// DiscoverSocketPath returns the socket path for the given home directory.
//
// It checks the AGENTPAAS_SOCKET environment variable first. If unset, it
// defaults to <homeDir>/daemon.sock.
func DiscoverSocketPath(homeDir string) string {
	if s := os.Getenv("AGENTPAAS_SOCKET"); s != "" {
		return s
	}
	return filepath.Join(homeDir, socketName)
}

// NewHomePaths creates a HomePaths struct rooted at homeDir.
//
// All paths are derived deterministically from homeDir. Socket path respects
// the AGENTPAAS_SOCKET environment variable if set.
func NewHomePaths(homeDir string) *HomePaths {
	return &HomePaths{
		Home:   homeDir,
		Socket: DiscoverSocketPath(homeDir),
		PID:    filepath.Join(homeDir, pidName),
		Lock:   filepath.Join(homeDir, lockName),
		Logs:   filepath.Join(homeDir, logsDirName),
		State:  filepath.Join(homeDir, stateDirName),
		Config: filepath.Join(homeDir, configDirName),
		Cache:  filepath.Join(homeDir, cacheDirName),
		Tmp:    filepath.Join(homeDir, tmpDirName),
	}
}

// Ensure creates the home directory and all subdirectories and runtime files
// with the required secure permissions.
//
// It creates:
//   - Home directory (mode 0700)
//   - logs/, state/, config/, cache/, tmp/ subdirectories (mode 0700)
//   - Empty daemon.sock, agentpaasd.pid, and agentpaasd.lock files (mode 0600)
//
// Ensure is idempotent – calling it multiple times is safe. If a path
// already exists with correct permissions, it is left unchanged.
func Ensure(paths *HomePaths) error {
	// Create home directory.
	if err := os.MkdirAll(paths.Home, homePerm); err != nil {
		return fmt.Errorf("home: cannot create home directory %s: %w", paths.Home, err)
	}
	// Ensure home directory has correct permissions (fix if needed).
	if err := os.Chmod(paths.Home, homePerm); err != nil {
		return fmt.Errorf("home: cannot set permissions on %s: %w", paths.Home, err)
	}

	// Create subdirectories.
	dirs := []string{
		paths.Logs,
		paths.State,
		paths.Config,
		paths.Cache,
		paths.Tmp,
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, subdirPerm); err != nil {
			return fmt.Errorf("home: cannot create directory %s: %w", d, err)
		}
		if err := os.Chmod(d, subdirPerm); err != nil {
			return fmt.Errorf("home: cannot set permissions on %s: %w", d, err)
		}
	}

	// Create runtime files (empty, with correct permissions).
	runtimeFiles := []struct {
		path string
		mode os.FileMode
	}{
		{paths.Socket, filePerm},
		{paths.PID, filePerm},
		{paths.Lock, filePerm},
	}
	for _, rf := range runtimeFiles {
		if err := touchFile(rf.path, rf.mode); err != nil {
			return fmt.Errorf("home: %w", err)
		}
	}

	return nil
}

// touchFile creates an empty file at path with the given mode if it does not
// already exist. If the file exists, it ensures the mode is correct.
func touchFile(path string, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, mode)
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", path, err)
	}
	f.Close()
	// Ensure correct permissions even if file existed with wrong ones.
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("cannot set permissions on %s: %w", path, err)
	}
	return nil
}

// ValidatePermissions checks that the home directory and socket file have
// secure permissions. It returns an actionable error if any check fails.
//
// The daemon MUST call this before serving. If permissions are too broad,
// ValidatePermissions refuses to initialize (fail-closed) and tells the
// user exactly how to fix each problem.
func ValidatePermissions(paths *HomePaths) error {
	// Check home directory.
	fi, err := os.Stat(paths.Home)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("home directory %s does not exist — run 'agentpaasd init' or 'Ensure()'", paths.Home)
		}
		return fmt.Errorf("home: cannot stat home directory %s: %w", paths.Home, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("home: %s is not a directory", paths.Home)
	}
	if fi.Mode().Perm()&^homePerm != 0 {
		return fmt.Errorf(
			"home directory %s has permissions %#o, which are too broad — "+
				"run 'chmod 0700 %s' to restrict access to the owning user only",
			paths.Home, fi.Mode().Perm(), paths.Home,
		)
	}

	// Check socket file (if it exists).
	if fi, err := os.Stat(paths.Socket); err == nil {
		if fi.Mode().Perm()&^socketPerm != 0 {
			return fmt.Errorf(
				"socket file %s has permissions %#o, which are too broad — "+
					"run 'chmod 0600 %s' to restrict access to the owning user only",
				paths.Socket, fi.Mode().Perm(), paths.Socket,
			)
		}
	}
	// Socket file not existing yet is fine — the daemon will create it.

	return nil
}

// IsStalePid checks whether the PID file at pidFile is stale.
//
// A PID file is stale if it exists but the process identified by the PID
// inside it is not running. This function reads the PID from the file and
// sends signal 0 to the process. If the process does not exist, the PID
// file is considered stale.
func IsStalePid(pidFile string) (bool, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("home: cannot read PID file %s: %w", pidFile, err)
	}

	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return true, nil // empty PID file is stale
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return true, nil // unparseable PID is stale
	}

	// Signal 0 is a null check — it only checks if the process exists
	// without actually sending a signal.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true, nil // process not found
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return true, nil // process not running
	}
	return false, nil // process is running
}

// IsStaleSocket checks whether the socket file at sockFile is stale.
//
// A socket file is stale if it exists but nothing is listening on it.
// This function attempts to dial the Unix socket. If the connection
// succeeds, something is listening and the socket is live. If the
// connection fails, the socket is considered stale.
func IsStaleSocket(sockFile string) (bool, error) {
	if _, err := os.Stat(sockFile); os.IsNotExist(err) {
		return false, nil
	}

	conn, err := net.Dial("unix", sockFile)
	if err != nil {
		// Cannot connect — either no listener or file is not a real socket.
		return true, nil
	}
	conn.Close()
	return false, nil
}

// IsStaleLock checks whether the lock file at lockFile is stale.
//
// A lock file is stale if it exists but no process holds an exclusive
// POSIX flock on it. This function opens the file and attempts to
// acquire an exclusive non-blocking flock. If the lock is acquired, no
// other process holds it, so the file is stale. If EWOULDBLOCK is
// returned, a live process holds the lock, so the file is not stale.
func IsStaleLock(lockFile string) (bool, error) {
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		return false, nil
	}

	f, err := os.OpenFile(lockFile, os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("home: cannot open lock file %s: %w", lockFile, err)
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK {
			// Another process holds the lock — not stale.
			return false, nil
		}
		return false, fmt.Errorf("home: flock error on %s: %w", lockFile, err)
	}

	// We acquired the lock — no one else holds it, so it's stale.
	// Release it immediately since we only needed to check.
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return true, nil
}

// CleanStale detects and removes stale runtime files from the home directory.
//
// It checks the PID file, socket file, and lock file. For each file that
// is provably stale (no live process owns it), CleanStale removes it.
//
// CleanStale is safe to call during daemon startup: it will never remove
// a file that a live daemon process might own, because each check
// positively proves the owning process is gone before removing the file.
func CleanStale(paths *HomePaths) error {
	// Check PID file.
	stale, err := IsStalePid(paths.PID)
	if err != nil {
		return fmt.Errorf("home: cannot check PID file staleness: %w", err)
	}
	if stale {
		if err := os.Remove(paths.PID); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("home: cannot remove stale PID file %s: %w", paths.PID, err)
		}
	}

	// Check socket file.
	stale, err = IsStaleSocket(paths.Socket)
	if err != nil {
		return fmt.Errorf("home: cannot check socket file staleness: %w", err)
	}
	if stale {
		if err := os.Remove(paths.Socket); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("home: cannot remove stale socket file %s: %w", paths.Socket, err)
		}
	}

	// Check lock file.
	stale, err = IsStaleLock(paths.Lock)
	if err != nil {
		return fmt.Errorf("home: cannot check lock file staleness: %w", err)
	}
	if stale {
		if err := os.Remove(paths.Lock); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("home: cannot remove stale lock file %s: %w", paths.Lock, err)
		}
	}

	return nil
}